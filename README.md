<div align="center">
  <img src="build/appicon.png" alt="久坐提醒" width="120" />
  <h1>久坐提醒</h1>
  <p>一个常驻系统托盘的久坐提醒工具，设定时长后自动在「久坐 → 站立」之间循环计时，到点用提示音和系统通知提醒你起身或坐下。</p>
  <p>轻量、无账号、无网络请求 —— 所有配置与音频都在本地。</p>
</div>
## 功能

- **循环计时** — 久坐与站立时长可分别设置（精确到秒），自动交替
- **到点提醒** — 提示音 + 系统通知，通知上带「停止响铃」与「延后 5 分钟」两个按钮
- **暂停 / 继续** — 临时走开或开会时暂停，回来接着算；响铃时暂停会一并停铃
- **重置** — 主界面与托盘菜单均可一键停止计时、回到未启动
- **自定义提示音** — 内置合成提示音，也可选择本地音频文件，支持试听与暂停
- **响铃期间不计时** — 到点后倒计时停住，等你停止响铃才进入下一阶段
- **开机自启** — 可选项，写入当前用户的自启项
- **任务栏进度** — 窗口标题实时显示剩余时间，不打开窗口也能瞟一眼
- **单文件分发** — 前端资源编译进二进制，运行时无外部依赖

## 交互速查

| 操作 | 效果 |
| --- | --- |
| 托盘图标 **双击** | 打开主界面 |
| 托盘图标 **右键** | 弹出菜单（开始/暂停、重置、停止响铃、打开、退出） |
| 主页计时卡 **点击** | 响铃时停止响铃，进入下一阶段 |
| 主页计时卡 **右键** | 响铃时延后 5 分钟 |
| 系统通知 **按钮** | 「停止响铃」进入下一阶段；「延后 5 分钟」推迟 |

> 关闭窗口只会最小化到托盘，计时照常进行。要完全退出请用托盘菜单里的「退出」。

**「停止响铃」与「暂停」的区别**：响铃时点「停止响铃」会进入下一阶段，点「暂停」则停在当前阶段等你继续。三者都停铃，但去向不同：

| 操作 | 阶段 |
| --- | --- |
| 停止响铃 | 进入下一阶段 |
| 暂停 | 停在原地 |
| 重置 | 回到未启动 |

## 技术栈

| | |
| --- | --- |
| 后端 | Go + [Wails](https://wails.io) v2 + [energye/systray](https://github.com/energye/systray) |
| 前端 | Vue 3 + TypeScript + Vite |
| 动效 | GSAP |
| 音频 | Web Audio API（不用 `<audio>` 标签，为了精确控音量与无缝循环） |

## 快速开始

需要 Go 1.21+、Node.js 18+，以及 [Wails CLI](https://wails.io/docs/gettingstarted/installation)：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

然后：

```bash
git clone <repo-url>
cd PatProlongedSitting-ts

# 实时开发（前端热重载 + Go 热重启）
wails dev

# 构建单文件可执行程序，产物在 build/bin/
wails build
```

只想跑前端看看界面：

```bash
cd frontend && npm install && npm run dev
```

> 此时后端调用会失败 —— 界面能看，但计时数据是死的。完整功能请用 `wails dev`。

## 项目结构

```
.
├── main.go                 程序入口：窗口选项、资源嵌入
├── app.go                  Wails 绑定层：暴露给前端的方法 + 窗口标题同步
├── timer.go                计时核心：阶段机、倒计时、暂停/延后
├── config.go               配置读写、旧配置迁移、字段归一化
├── tray.go                 系统托盘：菜单、图标、鼠标交互
├── notify.go               系统通知：初始化、类别注册、按钮回调
├── autostart_*.go          开机自启（按平台分文件）
├── timer_test.go           计时逻辑单元测试
│
├── frontend/
│   └── src/
│       ├── App.vue             页面外壳：主页与设置页
│       ├── components/         视图组件
│       ├── composables/        纯逻辑（audio 播放 / motion 动效）
│       ├── styles/             设计令牌与全局样式
│       └── types/              前后端共享类型
│
├── build/                  打包资源：图标、清单、安装脚本
└── docs/                   设计文档与动效 demo
```

### 两条改动须知

后端是单一 `package main`，Go 文件**平铺在根目录**（有意为之，不拆包）。

- **别移动 Go 文件位置** —— `main.go` / `tray.go` 里有 `//go:embed`，路径相对文件所在目录
- **别改 `_windows.go` / `_other.go` 后缀** —— 那是 Go 的构建约束，工具链靠它按平台选文件

前端的动效**统一走 `composables/motion.ts`**，组件不直接 import GSAP，
这样缓动语汇一致、`prefers-reduced-motion` 只需在一处降级。

## 配置文件

写入 `os.UserConfigDir()` 下的 `PatProlongedSitting/`，与程序位置无关。
旧版按「分钟」存储的配置会在读取时自动迁移为「秒」。

## 文档

- [`docs/demo/`](docs/demo/) — 「静 → 动 → 静」响铃动效的完整拆解 + 可运行 demo
- [`docs/clay/`](docs/clay/) — 界面设计系统参考
- [`docs/development.md`](docs/development.md) — 开发说明

## 平台支持

主要面向 **Windows**（托盘交互、通知、开机自启均为 Windows 实现得最完整）。
macOS / Linux 有对应代码路径，但未经充分测试。

## License

[MIT](LICENSE) © 2026 RoLingG
