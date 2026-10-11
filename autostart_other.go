//go:build !windows

package main

// ApplyAutoStart 在非 Windows 平台为空实现
// 开机自启目前仅支持 Windows
func ApplyAutoStart(enabled bool) {
	_ = enabled
}
