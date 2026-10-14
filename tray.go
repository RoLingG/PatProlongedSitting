package main

import (
	_ "embed"
	"runtime"
	"sync"

	"github.com/energye/systray"
)

// trayIcon 是内置的托盘图标（16x16 ICO）
//
//go:embed frontend/src/assets/images/icon.ico
var trayIcon []byte

// TrayCallbacks 汇总托盘菜单各动作对应的回调
type TrayCallbacks struct {
	OnToggleTimer func()        // 开始 / 暂停 / 继续
	OnResetTimer  func()        // 停止计时并重置
	OnStopRing    func()        // 停止响铃
	OnShowWindow  func()        // 打开主界面
	OnQuit        func()        // 退出程序
	OnReady       func()        // 托盘就绪后触发，用于推送初始状态
	StatusFunc    func() string // 返回当前状态的简短描述，用于菜单首项
}

// Tray 封装系统托盘的生命周期
type Tray struct {
	cb TrayCallbacks

	// mu 串行化对托盘控件的修改
	mu sync.Mutex

	mStart    *systray.MenuItem
	mReset    *systray.MenuItem
	mStopRing *systray.MenuItem
	mShow     *systray.MenuItem
	mQuit     *systray.MenuItem
	mStatus   *systray.MenuItem
}

// NewTray 创建一个托盘控制器
func NewTray(cb TrayCallbacks) *Tray {
	return &Tray{cb: cb}
}

// Run 启动托盘，该函数会阻塞，必须在独立 goroutine 中调用
func (t *Tray) Run() {
	// Win32 的窗口消息队列属于创建窗口的那个线程，而 Go 的 goroutine 会在
	// OS 线程间迁移。若消息循环被迁走，就再也收不到该窗口的消息，
	// 表现为托盘点击永久无响应。故在此锁定线程。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	systray.Run(t.onReady, t.onExit)
}

// Quit 请求托盘退出
func (t *Tray) Quit() {
	systray.Quit()
}

// 鼠标交互：左键双击打开主界面，右键弹出菜单，左键单击不响应
func (t *Tray) onReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("")
	systray.SetTooltip("久坐提醒")

	systray.SetOnDClick(func(menu systray.IMenu) {
		if t.cb.OnShowWindow != nil {
			t.cb.OnShowWindow()
		}
	})
	systray.SetOnRClick(func(menu systray.IMenu) {
		// 注册了右键回调后菜单不再自动弹出，须显式调用
		_ = menu.ShowMenu()
	})

	// 只读状态行，显示当前阶段与剩余时间
	t.mStatus = systray.AddMenuItem("未启动", "当前计时状态")
	t.mStatus.Disable()

	systray.AddSeparator()

	t.mStart = systray.AddMenuItem("开始", "开始 / 暂停 / 继续计时")
	t.mReset = systray.AddMenuItem("重置", "停止计时并回到未启动")
	t.mStopRing = systray.AddMenuItem("停止响铃", "立即停止当前提示音")
	t.mStopRing.Disable()

	systray.AddSeparator()

	t.mShow = systray.AddMenuItem("打开主界面", "显示设置窗口")
	t.mQuit = systray.AddMenuItem("退出", "完全退出程序")

	t.mStart.Click(func() {
		if t.cb.OnToggleTimer != nil {
			t.cb.OnToggleTimer()
		}
	})
	t.mReset.Click(func() {
		if t.cb.OnResetTimer != nil {
			t.cb.OnResetTimer()
		}
	})
	t.mStopRing.Click(func() {
		if t.cb.OnStopRing != nil {
			t.cb.OnStopRing()
		}
	})
	t.mShow.Click(func() {
		if t.cb.OnShowWindow != nil {
			t.cb.OnShowWindow()
		}
	})
	t.mQuit.Click(func() {
		if t.cb.OnQuit != nil {
			t.cb.OnQuit()
		}
	})

	// 托盘就绪，通知外部推送初始状态
	if t.cb.OnReady != nil {
		t.cb.OnReady()
	}
}

func (t *Tray) onExit() {
	// 托盘退出时无需额外清理，交由 App 处理
}

// Update 根据最新状态刷新托盘菜单文案与可用性
func (t *Tray) Update(state TimerState, statusText string, startLabel string, ringing bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.mStatus == nil {
		return
	}

	systray.SetTooltip("久坐提醒 — " + statusText)
	_ = state

	t.mStatus.SetTitle(statusText)
	t.mStart.SetTitle(startLabel)

	if ringing {
		t.mStopRing.Enable()
	} else {
		t.mStopRing.Disable()
	}
}
