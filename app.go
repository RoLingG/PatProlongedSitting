package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// 前端事件名，前后端通过这几条事件通信
const (
	// EventTimerTick 每秒推送一次计时状态
	EventTimerTick = "timer:tick"
	// EventPhaseEnd 某个阶段结束、需要开始提醒
	EventPhaseEnd = "timer:phaseEnd"
	// EventStopRing 请求前端立即停止响铃
	EventStopRing = "ring:stop"
	// EventConfigChanged 配置已在别处更新，通知前端刷新
	EventConfigChanged = "config:changed"
)

// appTitle 是应用名，同时用于窗口标题与托盘提示
const appTitle = "久坐提醒"

// App 是暴露给前端绑定的应用对象
type App struct {
	ctx context.Context

	cfg      *Config
	timer    *Timer
	tray     *Tray
	notifier *Notifier

	mu      sync.Mutex
	quiting bool

	// lastTitle 缓存上次写入的窗口标题，避免每秒重复调用 Win32
	lastTitle string
}

// NewApp 构造应用实例
func NewApp() *App {
	cfg := LoadConfig()
	a := &App{
		cfg:      cfg,
		notifier: NewNotifier(""),
	}
	a.timer = NewTimer(cfg, a.onPhaseEnd, a.onTick)
	a.tray = NewTray(TrayCallbacks{
		OnToggleTimer: a.ToggleTimer,
		OnStopRing:    a.StopRing,
		OnShowWindow:  a.ShowWindow,
		OnQuit:        a.Quit,
		OnReady:       a.pushState,
	})
	// 通知按钮的回调，按下的若是延后则延长当前阶段，其余都按停止响铃处理
	a.notifier.SetActionHandler(func(action string) {
		if action == actionSnooze {
			a.SnoozeRing()
			return
		}
		a.StopRing()
	})
	return a
}

// startup 由 Wails 在应用启动时调用
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// 通知系统依赖 ctx，须在发送任何通知之前完成初始化
	a.notifier.Initialize(ctx)
}

// domReady 由 Wails 在前端加载完成后调用；此时启动托盘
func (a *App) domReady(ctx context.Context) {
	a.ctx = ctx
	// 启动时让注册表自启项与实际配置保持一致
	ApplyAutoStart(a.cfg.Snapshot().AutoStart)
	go a.tray.Run()
	a.pushState()
}

// shutdown 由 Wails 在应用退出前调用
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	a.quiting = true
	a.mu.Unlock()
}

// ---- 暴露给前端的方法 ----

// GetConfig 返回当前配置的纯数据副本
func (a *App) GetConfig() ConfigData {
	return a.cfg.Snapshot()
}

// SaveConfig 保存配置并立即应用（重设计时参数会重置当前阶段）
func (a *App) SaveConfig(next ConfigData) ConfigData {
	if err := a.cfg.Update(next); err != nil {
		log.Printf("保存配置失败: %v", err)
	}
	// 同步开机自启设置
	ApplyAutoStart(next.AutoStart)
	a.timer.ResetCounters()
	a.pushState()
	a.emit(EventConfigChanged, a.cfg.Snapshot())
	return a.cfg.Snapshot()
}

// StartTimer 开始一轮新的久坐/站立循环
func (a *App) StartTimer() {
	a.timer.Start()
	a.pushState()
}

// PauseTimer 暂停倒计时
func (a *App) PauseTimer() {
	a.timer.Pause()
	a.pushState()
}

// ResumeTimer 恢复倒计时
func (a *App) ResumeTimer() {
	a.timer.Resume()
	a.pushState()
}

// StopTimer 停止计时并回到未启动状态
func (a *App) StopTimer() {
	a.timer.Stop()
	a.pushState()
}

// ToggleTimer 在开始/暂停/继续之间切换，供托盘菜单使用
func (a *App) ToggleTimer() {
	s := a.timer.State()
	switch {
	case !s.Running:
		a.timer.Start()
	case s.Paused:
		a.timer.Resume()
	default:
		a.timer.Pause()
	}
	a.pushState()
}

// GetTimerState 返回当前计时状态
func (a *App) GetTimerState() TimerState {
	return a.timer.State()
}

// StopRing 请求前端停止响铃（托盘菜单入口）
func (a *App) StopRing() {
	a.timer.SetRinging(false)
	a.emit(EventStopRing, nil)
	a.pushState()
}

// snoozeMinutes 是「延后」一次的时长
const snoozeMinutes = 5

// SnoozeRing 延后提醒：停止响铃，并把当前阶段延长 snoozeMinutes 分钟
func (a *App) SnoozeRing() {
	a.timer.Snooze(snoozeMinutes * time.Minute)
	a.emit(EventStopRing, nil)
	a.pushState()
}

// ShowWindow 显示并聚焦主窗口
func (a *App) ShowWindow() {
	if a.ctx == nil {
		return
	}
	// 先取消最小化再显示，否则最小化状态下 show 可能不生效
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}

// PickSoundFile 打开系统文件选择框，让用户挑选音效文件
func (a *App) PickSoundFile() string {
	if a.ctx == nil {
		return ""
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择提示音文件",
		Filters: []runtime.FileFilter{
			{DisplayName: "音频文件", Pattern: "*.wav;*.mp3;*.ogg;*.m4a;*.aac;*.flac"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return ""
	}
	return path
}

// ReadSoundFile 读取音效文件内容，供前端播放
// 前端将字节转为 Blob URL 后交给 Web Audio API 播放
func (a *App) ReadSoundFile(path string) []byte {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		log.Printf("读取音效文件失败: %v", err)
		return nil
	}
	return data
}

// Quit 完全退出应用
func (a *App) Quit() {
	a.mu.Lock()
	if a.quiting {
		a.mu.Unlock()
		return
	}
	a.quiting = true
	a.mu.Unlock()

	a.timer.Stop()
	a.tray.Quit()

	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

// ---- 内部回调 ----

// onTick 每秒把状态推给前端与托盘
func (a *App) onTick(state TimerState) {
	a.emit(EventTimerTick, state)
	a.updateTray(state)
	a.updateTitle(state)
}

// onPhaseEnd 在某阶段结束时发送通知并请求前端响铃
func (a *App) onPhaseEnd(ended Phase, next Phase, state TimerState) {
	title, message := reminderText(ended)
	if a.ctx != nil {
		go a.notifier.Notify(a.ctx, title, message)
	}

	snap := a.cfg.Snapshot()
	payload := map[string]any{
		"ended":       ended,
		"next":        next,
		"title":       title,
		"message":     message,
		"soundPath":   snap.SoundPath,
		"volume":      snap.Volume,
		"mode":        snap.Mode,
		"repeatTimes": snap.RepeatTimes,
		"state":       state,
	}
	a.emit(EventPhaseEnd, payload)
	a.updateTray(state)
	a.updateTitle(state)
}

// pushState 主动向前端推送一次状态
func (a *App) pushState() {
	state := a.timer.State()
	a.emit(EventTimerTick, state)
	a.updateTray(state)
	a.updateTitle(state)
}

// emit 向前端发送事件（ctx 未就绪时静默跳过）
func (a *App) emit(event string, payload any) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, event, payload)
}

// updateTray 同步托盘菜单显示
func (a *App) updateTray(state TimerState) {
	startLabel := "开始"
	switch {
	case state.Running && state.Paused:
		startLabel = "继续"
	case state.Running:
		startLabel = "暂停"
	}
	a.tray.Update(state, statusText(state), startLabel, state.Ringing)
}

// updateTitle 把当前状态写进窗口标题，作为轻量的任务栏进度提示
// 标题每秒都会变，因此先与上次比较，只在真正变化时调用 Win32
func (a *App) updateTitle(state TimerState) {
	if a.ctx == nil {
		return
	}

	title := appTitle
	if state.Running {
		title = statusText(state) + " — " + appTitle
	}

	if title == a.lastTitle {
		return
	}
	a.lastTitle = title
	runtime.WindowSetTitle(a.ctx, title)
}

// reminderText 根据结束的阶段返回提醒标题与正文
func reminderText(ended Phase) (string, string) {
	if ended == PhaseSitting {
		return "该起身活动了", "久坐时间结束，站起来走动一下，放松肩颈。"
	}
	return "该坐下休息了", "站立时间结束，可以坐下继续工作了。"
}

// statusText 生成托盘上的状态描述
func statusText(state TimerState) string {
	if !state.Running {
		return "未启动"
	}

	phaseName := "久坐"
	if state.Phase == PhaseStanding {
		phaseName = "站立"
	}

	prefix := ""
	if state.Paused {
		prefix = "[已暂停] "
	}

	return prefix + phaseName + " " + formatDuration(state.RemainingSec)
}

// formatDuration 把秒数格式化为托盘用的紧凑时长文本
// 不足 1 小时显示 mm:ss，超过则显示 h:mm:ss
func formatDuration(totalSec int) string {
	if totalSec < 0 {
		totalSec = 0
	}
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
