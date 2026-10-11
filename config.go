package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// RemindMode 定义单次提醒中音频的播放模式
type RemindMode string

const (
	// ModeTimes 按指定次数播放
	ModeTimes RemindMode = "times"
	// ModeLoop 循环播放直到手动停止
	ModeLoop RemindMode = "loop"
)

// EndMode 定义整体提醒会持续多久
type EndMode string

const (
	// EndForever 一直提醒，不自动停止
	EndForever EndMode = "forever"
	// EndCount 提醒满指定次数后自动停止
	EndCount EndMode = "count"
)

// ConfigData 是配置的纯数据部分（不含锁），用于前后端传输与持久化
type ConfigData struct {
	// 时长以秒存储，以支持「时:分:秒」粒度的设置
	SitSeconds   int `json:"sitSeconds"`
	StandSeconds int `json:"standSeconds"`

	// 旧版以分钟存储时长，仅在反序列化旧配置时读取，迁移后不再写回
	SitMinutesLegacy   int `json:"sitMinutes,omitempty"`
	StandMinutesLegacy int `json:"standMinutes,omitempty"`

	// SoundPath 为空时使用内置提示音
	SoundPath string `json:"soundPath"`
	// Volume 取值范围 0.0 ~ 1.0
	Volume float64 `json:"volume"`
	// Mode 为 times 时按 RepeatTimes 播放，为 loop 时循环直到手动停止
	Mode        RemindMode `json:"mode"`
	RepeatTimes int        `json:"repeatTimes"`

	// EndMode 为 count 时，提醒满 RemindCount 次后自动停止计时
	EndMode     EndMode `json:"endMode"`
	RemindCount int     `json:"remindCount"`

	AutoStart bool `json:"autoStart"`
}

// 阶段时长的默认值与上下限
const (
	defaultSitSeconds   = 45 * 60
	defaultStandSeconds = 5 * 60
	// 上限 24 小时，避免误输入导致计时失控
	maxPhaseSeconds = 24 * 60 * 60
	minPhaseSeconds = 1
)

// defaultConfigData 返回带合理默认值的配置数据
func defaultConfigData() ConfigData {
	return ConfigData{
		SitSeconds:   defaultSitSeconds,
		StandSeconds: defaultStandSeconds,
		SoundPath:    "",
		Volume:       0.7,
		Mode:         ModeLoop,
		RepeatTimes:  3,
		EndMode:      EndForever,
		RemindCount:  10,
		AutoStart:    false,
	}
}

// migrateLegacy 把旧版的「分钟」字段迁移到新的「秒」字段
// 仅当新字段缺失（未设置）且旧字段存在时生效
func (c *ConfigData) migrateLegacy() {
	if c.SitSeconds == 0 && c.SitMinutesLegacy > 0 {
		c.SitSeconds = c.SitMinutesLegacy * 60
	}
	if c.StandSeconds == 0 && c.StandMinutesLegacy > 0 {
		c.StandSeconds = c.StandMinutesLegacy * 60
	}
	// 迁移完成后清空旧字段，避免重复写入
	c.SitMinutesLegacy = 0
	c.StandMinutesLegacy = 0
}

// normalize 修正越界或非法的配置值，避免运行时异常
func (c *ConfigData) normalize() {
	c.migrateLegacy()

	if c.SitSeconds < minPhaseSeconds {
		c.SitSeconds = defaultSitSeconds
	}
	if c.SitSeconds > maxPhaseSeconds {
		c.SitSeconds = maxPhaseSeconds
	}
	if c.StandSeconds < minPhaseSeconds {
		c.StandSeconds = defaultStandSeconds
	}
	if c.StandSeconds > maxPhaseSeconds {
		c.StandSeconds = maxPhaseSeconds
	}

	if c.Volume < 0 {
		c.Volume = 0
	}
	if c.Volume > 1 {
		c.Volume = 1
	}
	if c.Mode != ModeTimes && c.Mode != ModeLoop {
		c.Mode = ModeLoop
	}
	if c.RepeatTimes < 1 {
		c.RepeatTimes = 1
	}
	if c.EndMode != EndForever && c.EndMode != EndCount {
		c.EndMode = EndForever
	}
	if c.RemindCount < 1 {
		c.RemindCount = 1
	}
}

// Config 在纯数据外附加并发保护与持久化能力
type Config struct {
	mu   sync.RWMutex
	data ConfigData
}

// LoadConfig 从磁盘读取配置；若不存在或损坏则返回默认配置
func LoadConfig() *Config {
	c := &Config{data: defaultConfigData()}

	path, err := configPath()
	if err != nil {
		return c
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		// 首次运行，写入默认配置
		_ = c.Save()
		return c
	}

	var loaded ConfigData
	if err := json.Unmarshal(raw, &loaded); err != nil {
		// 配置损坏，回退到默认值
		return c
	}

	// 记录归一化前的形态，用于判断是否需要写回磁盘
	before := loaded
	loaded.normalize()
	c.data = loaded

	// 若归一化改动了内容（例如从旧版「分钟」字段迁移到「秒」），
	// 则立即落盘，避免每次启动都重复迁移
	if !cfgEqual(before, loaded) {
		_ = c.Save()
	}
	return c
}

// cfgEqual 比较两份配置数据是否等价（含旧字段，用于判断迁移是否发生）
func cfgEqual(a, b ConfigData) bool {
	return a == b
}

// configPath 返回配置文件在用户配置目录下的完整路径
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "PatProlongedSitting")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(appDir, "config.json"), nil
}

// Snapshot 返回配置的只读副本，供前端读取与内部使用
func (c *Config) Snapshot() ConfigData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data
}

// Update 用新值覆盖配置、校验并持久化
func (c *Config) Update(next ConfigData) error {
	next.normalize()

	c.mu.Lock()
	c.data = next
	c.mu.Unlock()

	return c.Save()
}

// UpdateFields 在锁保护下修改配置字段并持久化
func (c *Config) UpdateFields(fn func(*ConfigData)) error {
	c.mu.Lock()
	fn(&c.data)
	c.data.normalize()
	c.mu.Unlock()

	return c.Save()
}

// Save 线程安全地持久化配置
func (c *Config) Save() error {
	c.mu.RLock()
	data := c.data
	c.mu.RUnlock()

	path, err := configPath()
	if err != nil {
		return err
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, raw, 0o644)
}
