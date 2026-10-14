package main

import (
	"sync"
	"testing"
	"time"
)

// stubConfig 构造一个测试用配置（走内存，不落盘）
func stubConfig(mod func(*ConfigData)) *Config {
	c := &Config{data: defaultConfigData()}
	if mod != nil {
		mod(&c.data)
	}
	c.data.normalize()
	return c
}

// captureEnds 记录每次阶段结束，供断言使用
type captureEnds struct {
	mu    sync.Mutex
	ends  []Phase
	nexts []Phase
}

func (c *captureEnds) record(ended, next Phase) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ends = append(c.ends, ended)
	c.nexts = append(c.nexts, next)
}

func (c *captureEnds) snapshot() ([]Phase, []Phase) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Phase(nil), c.ends...), append([]Phase(nil), c.nexts...)
}

// TestPhaseAlternation 验证阶段按 久坐 → 站立 → 久坐 交替推进
//
// 注意：阶段到点后不会自动进入下一阶段，而是停下响铃、等待用户停止。
// 因此每次触发到点后，还需调用 SetRinging(false) 模拟"用户停止响铃"，
// 下一阶段才会真正开始。
func TestPhaseAlternation(t *testing.T) {
	// 直接改写 remaining 触发到点，避免等待真实时长
	cfg := stubConfig(func(c *ConfigData) {
		c.SitSeconds = 60
		c.StandSeconds = 60
		c.EndMode = EndForever
	})

	caps := &captureEnds{}
	timer := NewTimer(cfg, func(ended, next Phase, _ TimerState) {
		caps.record(ended, next)
	}, nil)

	if got := timer.State().Phase; got != PhaseIdle {
		t.Fatalf("初始阶段应为 idle，实际 %s", got)
	}

	timer.Start()
	if got := timer.State().Phase; got != PhaseSitting {
		t.Fatalf("开始后应进入 sitting，实际 %s", got)
	}
	if !timer.State().Running {
		t.Fatal("开始后 Running 应为 true")
	}

	// 直接把剩余时间压到 1 秒，触发一次阶段切换
	timer.mu.Lock()
	timer.remaining = time.Second
	timer.mu.Unlock()

	waitFor(t, func() bool {
		ends, _ := caps.snapshot()
		return len(ends) >= 1
	}, 3*time.Second)

	ends, nexts := caps.snapshot()
	if ends[0] != PhaseSitting {
		t.Fatalf("第一次结束的阶段应为 sitting，实际 %s", ends[0])
	}
	if nexts[0] != PhaseStanding {
		t.Fatalf("sitting 之后应进入 standing，实际 %s", nexts[0])
	}

	// 到点后应处于响铃等待态：还在 running，但阶段尚未切换
	if got := timer.State().Phase; got != PhaseSitting {
		t.Fatalf("响铃未停止前不应切换阶段，实际 %s", got)
	}
	if !timer.State().Ringing {
		t.Fatal("到点后应处于响铃状态")
	}

	// 停止响铃 → 此时才真正进入站立阶段
	timer.SetRinging(false)
	if got := timer.State().Phase; got != PhaseStanding {
		t.Fatalf("停止响铃后应进入 standing，实际 %s", got)
	}
	if timer.State().Ringing {
		t.Fatal("停止响铃后 Ringing 应为 false")
	}

	// 再触发一次，验证 站立 → 久坐
	timer.mu.Lock()
	timer.remaining = time.Second
	timer.mu.Unlock()

	waitFor(t, func() bool {
		ends, _ := caps.snapshot()
		return len(ends) >= 2
	}, 3*time.Second)

	ends, nexts = caps.snapshot()
	if ends[1] != PhaseStanding {
		t.Fatalf("第二次结束的阶段应为 standing，实际 %s", ends[1])
	}
	if nexts[1] != PhaseSitting {
		t.Fatalf("standing 之后应回到 sitting，实际 %s", nexts[1])
	}

	timer.SetRinging(false)

	// 完成一轮后 completed 应为 1
	if got := timer.State().Completed; got != 1 {
		t.Fatalf("完成一套循环后 completed 应为 1，实际 %d", got)
	}

	timer.Stop()
}

// TestEndCountStopsAfterLimit 验证「提醒 N 次后停止」不会多提醒
func TestEndCountStopsAfterLimit(t *testing.T) {
	cfg := stubConfig(func(c *ConfigData) {
		c.SitSeconds = 60
		c.StandSeconds = 60
		c.EndMode = EndCount
		c.RemindCount = 3
	})

	caps := &captureEnds{}
	timer := NewTimer(cfg, func(ended, next Phase, _ TimerState) {
		caps.record(ended, next)
	}, nil)

	timer.Start()

	// 反复触发阶段切换，直到计时器自行停止。
	// 每轮到点后会停下响铃，需要模拟"用户停止响铃"才推进到下一轮。
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		timer.mu.Lock()
		running := timer.running
		if running && !timer.ringing {
			timer.remaining = time.Second
		}
		timer.mu.Unlock()

		if !running {
			break
		}
		// 若正在响铃，等它到点后停铃推进
		if timer.State().Ringing {
			timer.SetRinging(false)
		}
		time.Sleep(120 * time.Millisecond)
	}

	ends, _ := caps.snapshot()
	if len(ends) != 3 {
		t.Fatalf("提醒次数上限为 3，实际触发 %d 次", len(ends))
	}

	final := timer.State()
	if final.Running {
		t.Fatal("达到提醒上限后计时器应停止")
	}
	if final.RemindCount != 3 {
		t.Fatalf("提醒计数应为 3，实际 %d", final.RemindCount)
	}
}

// TestPauseResume 验证暂停期间倒计时不推进
func TestPauseResume(t *testing.T) {
	cfg := stubConfig(func(c *ConfigData) {
		c.SitSeconds = 60
		c.EndMode = EndForever
	})

	timer := NewTimer(cfg, nil, nil)
	timer.Start()

	timer.PauseWithRing()
	if !timer.State().Paused {
		t.Fatal("暂停后 Paused 应为 true")
	}

	timer.mu.Lock()
	before := timer.remaining
	timer.mu.Unlock()

	time.Sleep(1500 * time.Millisecond)

	timer.mu.Lock()
	after := timer.remaining
	timer.mu.Unlock()

	if before != after {
		t.Fatalf("暂停期间剩余时间不应变化：%v → %v", before, after)
	}

	timer.Resume()
	if timer.State().Paused {
		t.Fatal("恢复后 Paused 应为 false")
	}

	timer.Stop()
	if got := timer.State().Phase; got != PhaseIdle {
		t.Fatalf("停止后阶段应为 idle，实际 %s", got)
	}
}

// TestConfigNormalize 验证非法配置会被修正
func TestConfigNormalize(t *testing.T) {
	cfg := stubConfig(func(c *ConfigData) {
		c.SitSeconds = 0
		c.StandSeconds = -5
		c.Volume = 2.5
		c.Mode = "bogus"
		c.RepeatTimes = 0
		c.EndMode = "bogus"
		c.RemindCount = -1
	})

	got := cfg.Snapshot()
	if got.SitSeconds != defaultSitSeconds {
		t.Errorf("SitSeconds 应回退为默认值 %d，实际 %d", defaultSitSeconds, got.SitSeconds)
	}
	if got.StandSeconds != defaultStandSeconds {
		t.Errorf("StandSeconds 应回退为默认值 %d，实际 %d", defaultStandSeconds, got.StandSeconds)
	}
	if got.Volume != 1 {
		t.Errorf("Volume 应被钳制为 1，实际 %v", got.Volume)
	}
	if got.Mode != ModeLoop {
		t.Errorf("非法 Mode 应回退为 loop，实际 %s", got.Mode)
	}
	if got.RepeatTimes != 1 {
		t.Errorf("RepeatTimes 应被修正为 1，实际 %d", got.RepeatTimes)
	}
	if got.EndMode != EndForever {
		t.Errorf("非法 EndMode 应回退为 forever，实际 %s", got.EndMode)
	}
	if got.RemindCount != 1 {
		t.Errorf("RemindCount 应被修正为 1，实际 %d", got.RemindCount)
	}
}

// TestLegacyMinutesMigration 验证旧版「分钟」配置能迁移为「秒」
func TestLegacyMinutesMigration(t *testing.T) {
	cfg := stubConfig(func(c *ConfigData) {
		// 模拟从磁盘读入的旧版配置
		c.SitSeconds = 0
		c.StandSeconds = 0
		c.SitMinutesLegacy = 30
		c.StandMinutesLegacy = 8
	})

	got := cfg.Snapshot()
	wantSit := 30 * 60
	wantStand := 8 * 60
	if got.SitSeconds != wantSit {
		t.Errorf("旧版 30 分钟应迁移为 %d 秒，实际 %d", wantSit, got.SitSeconds)
	}
	if got.StandSeconds != wantStand {
		t.Errorf("旧版 8 分钟应迁移为 %d 秒，实际 %d", wantStand, got.StandSeconds)
	}
	// 迁移后旧字段应被清空
	if got.SitMinutesLegacy != 0 || got.StandMinutesLegacy != 0 {
		t.Errorf("迁移后旧字段应清空，实际 sit=%d stand=%d",
			got.SitMinutesLegacy, got.StandMinutesLegacy)
	}
}

// TestPhaseSecondsOverride 验证秒级设置能精确控制阶段时长
func TestPhaseSecondsOverride(t *testing.T) {
	cfg := stubConfig(func(c *ConfigData) {
		c.SitSeconds = 90 // 1 分 30 秒
		c.StandSeconds = 45
		c.EndMode = EndForever
	})

	timer := NewTimer(cfg, nil, nil)
	timer.Start()

	state := timer.State()
	if state.TotalSec != 90 {
		t.Errorf("久坐阶段总时长应为 90 秒，实际 %d", state.TotalSec)
	}
	if state.RemainingSec != 90 {
		t.Errorf("久坐阶段剩余应为 90 秒，实际 %d", state.RemainingSec)
	}

	timer.Stop()
}

// waitFor 轮询等待条件成立，超时则测试失败
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("等待条件超时")
}
