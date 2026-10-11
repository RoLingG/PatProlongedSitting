//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// autostartKey 是 Windows 用户级自启注册表路径
const (
	autostartKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValue   = "PatProlongedSitting"
)

// ApplyAutoStart 根据配置写入或删除开机自启注册表项
// 仅在 Windows 下有效，其他平台静默跳过
func ApplyAutoStart(enabled bool) {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("获取可执行文件路径失败: %v", err)
		return
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		log.Printf("解析可执行文件绝对路径失败: %v", err)
		return
	}

	// 开发模式下是临时目录里的 exe，不应写入自启
	if isDevBuild(exe) {
		return
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		log.Printf("打开自启注册表项失败: %v", err)
		return
	}
	defer k.Close()

	if enabled {
		// 路径含空格时需要用引号包裹
		value := `"` + exe + `"`
		if err := k.SetStringValue(autostartValue, value); err != nil {
			log.Printf("写入开机自启失败: %v", err)
		}
		return
	}

	// 关闭自启：删除值，若不存在则忽略错误
	if err := k.DeleteValue(autostartValue); err != nil && err != registry.ErrNotExist {
		log.Printf("移除开机自启失败: %v", err)
	}
}

// isDevBuild 粗略判断当前是否运行在开发模式（wails dev 产生的临时 exe）
func isDevBuild(exe string) bool {
	tmp := os.TempDir()
	rel, err := filepath.Rel(tmp, exe)
	if err != nil {
		return false
	}
	// 位于临时目录内视为开发构建
	return !filepath.IsAbs(rel) && rel != ".." && len(rel) > 0 && rel[0] != '.'
}
