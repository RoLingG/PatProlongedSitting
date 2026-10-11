# 前端（frontend）

久坐提醒的界面层：Vue 3 + TypeScript + Vite，动效由 GSAP 驱动。

界面通过 Wails 生成的绑定调用 Go 后端，不直接发起网络请求。

## 目录结构

```
frontend/
├── src/
│   ├── App.vue              页面外壳：主页与设置页的视图切换与整体状态
│   ├── main.ts              应用挂载入口（挂载 App，引入全局样式）
│   │
│   ├── components/          可复用视图组件
│   │   ├── DurationInput.vue  时:分:秒 三段式时长输入（滚轮 + 步进）
│   │   ├── Segmented.vue      分段控件，滑动指示器的单选组
│   │   └── Portrait.vue       吉祥物：位图 + 背后的动态装饰
│   │
│   ├── composables/         与视图无关的逻辑
│   │   ├── audio.ts           AudioPlayer：提示音播放、试听、停止
│   │   └── motion.ts          动效层：全部 GSAP 动画集中于此
│   │
│   ├── styles/
│   │   ├── tokens.css         设计令牌：颜色、圆角、间距、阴影、字号
│   │   └── base.css           全局样式：字体、滚动条、焦点、动效降级
│   │
│   ├── types/
│   │   └── types.ts           前后端共享的类型与格式化函数
│   │
│   └── assets/
│       ├── fonts/             Nunito（随包分发，不依赖系统字体）
│       └── images/            mascot.png
│
├── wailsjs/                 Wails 自动生成的前端绑定（勿手改）
├── dist/                    Vite 构建产物，被 Go 侧 go:embed 打进二进制
└── index.html               页面模板
```

## 分层约定

**组件不直接 import gsap。** 所有动画走 `composables/motion.ts`，好处有三：

1. 缓动语汇统一，全应用动作手感一致；
2. `prefers-reduced-motion` 只需在一处降级；
3. 无限循环的补间集中管理生命周期，避免视图卸载后泄漏。

**颜色不写死在组件里。** 一律取 `styles/tokens.css` 的变量。当前配色限定三个色系：
玫瑰（久坐 / 主操作）、松绿（站立）、墨黑（文字与次要结构）。
面积超过屏幕四分之一的色块必须用去饱和的 `-surface` 版本。

**类型定义在 `types/types.ts`**，与 Go 侧 `app_config.go`、`app_timer.go`
里的对应结构保持同步（字段以 Wails 生成的 `wailsjs/go/models.ts` 为准）。

## 命令

```bash
npm install      # 安装依赖
npm run dev      # 启动 Vite 开发服务器（http://localhost:5173）
npm run build    # 类型检查 + 构建到 dist/
npm run preview  # 预览构建产物
```

> 单独跑 `npm run dev` 时后端调用会失败——界面能看，但计时状态是死的。
> 需要完整功能请在项目根目录跑 `wails dev`。

## 动效降级

`styles/base.css` 与 `composables/motion.ts` 都检查 `prefers-reduced-motion`。
系统开启「减弱动画」时，装饰性动效全部停用，只保留必要的状态切换。

## 样式分层

- **`tokens.css`** — 只定义 CSS 变量，不含选择器逻辑。改配色、圆角、间距从这里改。
- **`base.css`** — 只做全局重置与元素级默认样式，不含组件样式。
- **组件样式** — 写在各自 `.vue` 的 `<style scoped>` 里，不外泄。
