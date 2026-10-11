package main

import (
	"context"
	"sync"
	"time"
)

// Phase 表示当前所处的阶段
type Phase string

const (
	PhaseSitting  Phase = "sitting"
	PhaseStanding Phase = "standing"
	PhaseIdle     Phase = "idle"
)

// TimerState 是计时器的对外快照，会推送给前端和托盘
type TimerState struct {
	Phase        Phase `json:"phase"`
	RemainingSec int   `json:"remainingSec"`
	TotalSec     int   `json:"totalSec"`
	Running      bool  `json:"running"`
	Paused       bool  `json:"paused"`
	Completed    int   `json:"completed"`   // 已完成的完整循环数
	RemindCount  int   `json:"remindCount"` // 已发出的提醒次数
	Ringing      bool  `json:"ringing"`     // 是否正在响铃
}

// onPhaseEnd 在某个阶段计时归零时被调用，用于触发提醒
type onPhaseEnd func(ended Phase, next Phase, state TimerState)

// Timer 驱动久坐/站立交替的循环计时
//
// 状态流转：久坐 →（到点）→ 站立 →（到点）→ 久坐 …
// 一次「久坐 + 站立」记为一套完整循环。
type Timer struct {
	mu sync.Mutex

	cfg *Config

	phase     Phase
	remaining time.Duration
	total     time.Duration
	running   bool
	paused    bool

	completed   int
	remindCount int
	ringing     bool

	// pendingPhase 暂存"到点、正在响铃等待"时该进入的下一阶段。
	//
	// 到点后不立即切阶段，而是停下来响铃，把下一阶段记在这里；
	// 等停止响铃（SetRinging(false)）才真正切换并从头计时。
	// 这样响铃期间倒计时不走，用户起身/坐下时不会被计时赶着走。
	// 空字符串表示没有待推进的阶段。
	pendingPhase Phase

	ticker     *time.Ticker
	stopCh     chan struct{}
	onPhaseEnd onPhaseEnd
	onTick     func(TimerState)
}

// NewTimer 创建一个处于 idle 状态的计时器
func NewTimer(cfg *Config, onEnd onPhaseEnd, onTick func(TimerState)) *Timer {
	return &Timer{
		cfg:        cfg,
		phase:      PhaseIdle,
		onPhaseEnd: onEnd,
		onTick:     onTick,
	}
}

// Start 从头开始一轮计时（进入久坐阶段）
func (t *Timer) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopLocked()

	t.phase = PhaseSitting
	t.total = t.phaseDurationLocked(PhaseSitting)
	t.remaining = t.total
	t.running = true
	t.paused = false
	t.completed = 0
	t.remindCount = 0
	t.ringing = false
	t.pendingPhase = ""

	t.spawnLocked()
	t.emitLocked()
}

// Pause 暂停倒计时（响铃不受影响）
func (t *Timer) Pause() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running || t.paused {
		return
	}
	t.paused = true
	t.emitLocked()
}

// Resume 恢复倒计时
func (t *Timer) Resume() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running || !t.paused {
		return
	}
	// 响铃期间倒计时本就不走，恢复要等停止响铃（见 SetRinging）
	if t.ringing {
		return
	}
	t.paused = false
	t.emitLocked()
}

// Snooze 延后当前阶段：剩余时间设为 delay 并停止响铃。
//
// 与 Stop 的区别：Stop 放弃这一轮，Snooze 只是推迟——
// 待推进的下一阶段保留，新倒计时归零后照常进入。
// 循环数与提醒次数都不变，用户只是晚点起身，不该少算一次。
func (t *Timer) Snooze(delay time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if delay < 0 {
		delay = 0
	}

	t.ringing = false
	t.remaining = delay
	// total 一并同步，否则进度条会停在和 remaining 不匹配的位置
	t.total = delay
	t.paused = false
	t.running = true

	// 到点时 ticker 已停，这里重新起上
	t.stopLocked()
	t.spawnLocked()

	t.emitLocked()
}

// Stop 完全停止计时并回到 idle
func (t *Timer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopLocked()
	t.phase = PhaseIdle
	t.remaining = 0
	t.total = 0
	t.running = false
	t.paused = false
	t.ringing = false
	// 用户主动停止，丢弃待推进的阶段
	t.pendingPhase = ""
	t.emitLocked()
}

// SetRinging 更新响铃状态，用于前端同步
//
// 停止响铃（v=false）时，如果有一个因到点而挂起的下一阶段，
// 就从此处推进：切换阶段、按新时长从头开始，并让倒计时继续跑。
// SetRinging 更新响铃状态。
//
// 由响铃转为不响铃时，若存在挂起的下一阶段，就在此处推进它：
// 切换阶段、按新时长从头计时。"响铃期间不计时"这条规则由计时器
// 统一保证，前端只需调 SetRinging(false)，不必自己判断该 Start 还是 Resume。
func (t *Timer) SetRinging(v bool) {
	t.mu.Lock()

	wasRinging := t.ringing
	t.ringing = v

	if wasRinging && !v && t.pendingPhase != "" {
		next := t.pendingPhase
		t.pendingPhase = ""

		t.phase = next
		t.total = t.phaseDurationLocked(next)
		t.remaining = t.total
		t.paused = false

		t.running = true
		if t.stopCh == nil {
			t.spawnLocked()
		}
	}

	t.mu.Unlock()
	t.emit()
}

// emit 推送一次状态快照（自行加锁，供未持锁的公开方法使用）
func (t *Timer) emit() {
	t.mu.Lock()
	state := t.stateLocked()
	t.mu.Unlock()
	if t.onTick != nil {
		t.onTick(state)
	}
}

// ResetCounters 清零已完成循环数与提醒次数（配置变更后调用）
func (t *Timer) ResetCounters() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.completed = 0
	t.remindCount = 0
	if t.running {
		t.total = t.phaseDurationLocked(t.phase)
		t.remaining = t.total
	}
	t.emitLocked()
}

// State 返回当前状态快照
func (t *Timer) State() TimerState {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stateLocked()
}

// ---- 内部实现 ----

// phaseDurationLocked 读取配置并换算为对应阶段的时长
func (t *Timer) phaseDurationLocked(p Phase) time.Duration {
	snap := t.cfg.Snapshot()
	seconds := snap.SitSeconds
	if p == PhaseStanding {
		seconds = snap.StandSeconds
	}
	if seconds < minPhaseSeconds {
		seconds = minPhaseSeconds
	}
	return time.Duration(seconds) * time.Second
}

// spawnLocked 启动 ticker goroutine，调用方必须持有锁
func (t *Timer) spawnLocked() {
	t.ticker = time.NewTicker(time.Second)
	t.stopCh = make(chan struct{})

	ticker := t.ticker
	stopCh := t.stopCh

	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				t.tick()
			}
		}
	}()
}

// stopLocked 关闭 ticker 与信号通道，调用方必须持有锁
func (t *Timer) stopLocked() {
	if t.stopCh != nil {
		close(t.stopCh)
		t.stopCh = nil
	}
	if t.ticker != nil {
		t.ticker.Stop()
		t.ticker = nil
	}
}

// tick 每秒推进一次倒计时
func (t *Timer) tick() {
	t.mu.Lock()

	if !t.running || t.paused {
		t.mu.Unlock()
		return
	}

	t.remaining -= time.Second

	if t.remaining > 0 {
		state := t.stateLocked()
		t.mu.Unlock()
		if t.onTick != nil {
			t.onTick(state)
		}
		return
	}

	// 当前阶段结束：停在原地进入响铃态，下一阶段暂存在 pendingPhase
	ended := t.phase
	next := PhaseStanding
	if ended == PhaseStanding {
		next = PhaseSitting
		t.completed++
	}

	t.pendingPhase = next
	// 保持 0，界面上显示 00:00 直到用户停止响铃
	t.remaining = 0

	t.remindCount++
	t.ringing = true

	// 停掉 ticker：否则它继续每秒触发，会把 remaining 减成负数
	t.stopLocked()

	// 达到提醒上限时，本次仍提醒，但之后不再计时
	snap := t.cfg.Snapshot()
	exhausted := snap.EndMode == EndCount && t.remindCount >= snap.RemindCount

	state := t.stateLocked()

	if exhausted {
		// 保留 ringing，让用户手动停铃；但清空 pendingPhase 归为 idle，
		// 停止响铃后不再进入下一阶段
		t.running = false
		t.pendingPhase = ""
		t.phase = PhaseIdle
		t.total = 0
		state = t.stateLocked()
	}

	t.mu.Unlock()

	if t.onPhaseEnd != nil {
		t.onPhaseEnd(ended, next, state)
	}
	if t.onTick != nil {
		t.onTick(state)
	}
}

// stateLocked 组装状态快照，调用方必须持有锁
func (t *Timer) stateLocked() TimerState {
	return TimerState{
		Phase:        t.phase,
		RemainingSec: int(t.remaining.Seconds()),
		TotalSec:     int(t.total.Seconds()),
		Running:      t.running,
		Paused:       t.paused,
		Completed:    t.completed,
		RemindCount:  t.remindCount,
		Ringing:      t.ringing,
	}
}

// emitLocked 推送状态到前端，调用方必须持有锁
func (t *Timer) emitLocked() {
	if t.onTick == nil {
		return
	}
	state := t.stateLocked()
	go t.onTick(state)
}

// WaitDone 阻塞直到计时器被完全停止，用于优雅退出
func (t *Timer) WaitDone(ctx context.Context) {
	t.mu.Lock()
	ch := t.stopCh
	t.mu.Unlock()

	if ch == nil {
		return
	}

	select {
	case <-ch:
	case <-ctx.Done():
	}
}
