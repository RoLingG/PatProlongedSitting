package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "久坐提醒",
		Width:     1024,
		Height:    768,
		MinWidth:  720,
		MinHeight: 520,

		// 关闭窗口时隐藏到托盘而不是退出，保证后台计时与响铃持续工作
		HideWindowOnClose: true,
		// 启动时不显示窗口，静默驻留托盘，需要时从托盘或双击图标打开
		StartHidden: true,

		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 255, G: 251, B: 242, A: 1},

		OnStartup:  app.startup,
		OnDomReady: app.domReady,
		OnShutdown: app.shutdown,

		Bind: []any{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
