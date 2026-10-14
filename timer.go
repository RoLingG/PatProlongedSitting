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
	// dispatchCh 是状态快照的发送队列，由专职 goroutine 串行消费。
	// 引入它是因为 onTick 里的工作是"通知"性质，不该阻塞计时。
	dispatchCh chan TimerState
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

// PauseWithRing 暂停计时，并在响铃时一并停止响铃。
//
// 与 SetRinging(false) 的区别：那个会推进到下一阶段，
// 这个只停铃、保留 pendingPhase，阶段停在原地等用户继续。
// 用于"暂停"这样一个明确的用户动作——用户要的是停下来，
// 而不是"把这次提醒消掉、继续往下走"。
//
// 返回值表示是否确有响铃被中止，供调用方决定要不要通知前端停音频。
func (t *Timer) PauseWithRing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	wasRinging := t.ringing
	if wasRinging {
		t.ringing = false
		// ticker 在到点时已停，这里也不重启：阶段要等用户继续才走
	}

	if t.running && !t.paused {
		t.paused = true
	}

	t.emitLocked()
	return wasRinging
}

// Resume 恢复倒计时。
// 若暂停时正处在"到点挂起"状态（pendingPhase 非空），则在此推进到下一阶段，
// 相当于把暂停期间被中止的那次提醒"补"上。
func (t *Timer) Resume() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running || !t.paused {
		return
	}
	t.paused = false

	if t.pendingPhase != "" {
		next := t.pendingPhase
		t.pendingPhase = ""
		t.phase = next
		t.total = t.phaseDurationLocked(next)
		t.remaining = t.total
		if t.stopCh == nil {
			t.spawnLocked()
		}
	}

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
	defer t.mu.Unlock()
	t.dispatchLocked(t.stateLocked())
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

// dispatchLocked 把状态快照投递给发送队列，调用方必须持有锁。
//
// 不直接调用 onTick：它要做发事件、改托盘、改窗口标题，
// 这些都比计时本身慢，同步调用会拖住 ticker goroutine 影响计时精度。
// 投递到队列后由专职 goroutine 串行发送，既不阻塞计时，也保证先后顺序。
func (t *Timer) dispatchLocked(state TimerState) {
	if t.onTick == nil {
		return
	}
	// 没有发送 goroutine 时（尚未启动或已停止）现起一个：
	// 推送状态这件事任何时刻都要能工作，不该依赖计时器是否在跑，
	// 否则 Stop() 之后前端收不到最终状态，界面会停在旧样子。
	t.startDispatchLocked()

	select {
	case t.dispatchCh <- state:
	default:
		// 队列满说明发送端积压。丢弃当前这条而不是阻塞计时——
		// 下一次推送会带来更新的状态，旧状态本来也没有保留价值。
	}
}

// spawnLocked 启动 ticker 与发送 goroutine，调用方必须持有锁
func (t *Timer) spawnLocked() {
	t.ticker = time.NewTicker(time.Second)
	t.stopCh = make(chan struct{})
	t.startDispatchLocked()

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

// stopLocked 关闭 ticker 与信号通道，调用方必须持有锁。
// 若关掉 dispatchCh，发送 goroutine 会在排空队列后自然退出。
func (t *Timer) stopLocked() {
	if t.stopCh != nil {
		close(t.stopCh)
		t.stopCh = nil
	}
	if t.ticker != nil {
		t.ticker.Stop()
		t.ticker = nil
	}
	t.closeDispatchLocked()
}

// closeDispatchLocked 关闭发送队列并置空，调用方必须持有锁。
// 调用后 dispatchLocked 会转为直接投递（无发送 goroutine 时仍要能推送状态）。
func (t *Timer) closeDispatchLocked() {
	if t.dispatchCh != nil {
		close(t.dispatchCh)
		t.dispatchCh = nil
	}
}

// startDispatchLocked 确保发送 goroutine 在运行，调用方必须持有锁
func (t *Timer) startDispatchLocked() {
	if t.dispatchCh != nil {
		return
	}
	t.dispatchCh = make(chan TimerState, 4)
	dispatchCh := t.dispatchCh
	go func() {
		for state := range dispatchCh {
			t.onTick(state)
		}
	}()
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
		t.dispatchLocked(t.stateLocked())
		t.mu.Unlock()
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
	t.dispatchLocked(state)
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
	t.dispatchLocked(t.stateLocked())
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
