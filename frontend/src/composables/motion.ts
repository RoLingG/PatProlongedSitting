/**
 * 动效层
 *
 * 所有 GSAP 动画集中于此，组件不直接 import gsap，
 * 使缓动语汇统一，减弱动画的降级与补间回收也集中在一处。
 */
import gsap from 'gsap'

/** 系统是否偏好减弱动画 */
export function reducedMotion(): boolean {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** 全局缓动常量，保证各处的动作手感一致 */
export const EASE = {
  /** 减速，用于入场 */
  out: 'power3.out',
  /** 进出自如，用于常规过渡 */
  inOut: 'power2.inOut',
  /** 回弹，用于状态切换 */
  back: 'back.out(1.7)',
  /** 极轻回弹，用于按钮按下 */
  backSoft: 'back.out(1.2)',
} as const

/* ------------------------------------------------------------------ *
 * 入场编排
 * ------------------------------------------------------------------ */

/** 首屏入场：标题、计时卡、统计、按钮依次上浮淡入，只跑一次 */
export function playIntro(root: HTMLElement): gsap.core.Timeline {
  const tl = gsap.timeline({ defaults: { ease: EASE.out } })

  if (reducedMotion()) {
    tl.set(root.querySelectorAll('[data-anim]'), { opacity: 1, y: 0 })
    return tl
  }

  tl.from('[data-anim="header"]', { opacity: 0, y: -12, duration: 0.5 })
    .from('[data-anim="clock"]', { opacity: 0, y: 22, scale: 0.94, duration: 0.65 }, '-=0.28')
    .from('[data-anim="stats"]', { opacity: 0, y: 16, duration: 0.5 }, '-=0.38')
    .from('[data-anim="controls"] > *', { opacity: 0, y: 14, duration: 0.4, stagger: 0.07 }, '-=0.3')

  return tl
}

/** 视图切换：新视图上浮入场。用 autoAlpha 保证隐藏时同时不可交互 */
export function playViewEnter(el: HTMLElement, from: 'right' | 'left' = 'right'): gsap.core.Timeline {
  const tl = gsap.timeline({ defaults: { ease: EASE.out } })
  const x = from === 'right' ? 28 : -28

  gsap.set(el, { autoAlpha: 1, pointerEvents: 'auto', x: reducedMotion() ? 0 : x, opacity: 0 })

  if (reducedMotion()) {
    tl.set(el, { opacity: 1 })
    return tl
  }

  tl.to(el, { opacity: 1, x: 0, duration: 0.42 })
    .from(
      el.querySelectorAll('[data-stagger]'),
      { opacity: 0, y: 16, duration: 0.42, stagger: 0.06 },
      '-=0.3',
    )

  return tl
}

/** 视图退场；返回 timeline，调用方可在 onComplete 里收尾（如回收 context） */
export function playViewLeave(el: HTMLElement, to: 'right' | 'left'): gsap.core.Timeline {
  const x = to === 'right' ? 28 : -28
  const tl = gsap.timeline()

  if (reducedMotion()) {
    tl.set(el, { opacity: 0, autoAlpha: 0, pointerEvents: 'none' })
    return tl
  }

  tl.to(el, {
    opacity: 0,
    x,
    duration: 0.24,
    ease: 'power2.in',
    onComplete: () => gsap.set(el, { autoAlpha: 0, pointerEvents: 'none' }),
  })

  return tl
}

/** 吉祥物入场：从下方轻微浮起并回弹 */
export function popMascot(el: HTMLElement): void {
  if (reducedMotion()) {
    gsap.set(el, { opacity: 1, y: 0 })
    return
  }
  gsap.from(el, {
    opacity: 0,
    y: 18,
    scale: 0.9,
    duration: 0.7,
    ease: EASE.back,
    overwrite: true,
  })
}

/** 进度条填充更新：用 GSAP 补间宽度，比 CSS transition 更可控 */
export function tweenBarFill(el: HTMLElement, ratio: number): void {
  gsap.to(el, {
    width: `${Math.max(0, Math.min(1, ratio)) * 100}%`,
    duration: reducedMotion() ? 0 : 0.9,
    ease: 'power1.out',
    overwrite: true,
  })
}

/* ------------------------------------------------------------------ *
 * 秒数
 * ------------------------------------------------------------------ */

/** 秒数变化：轻微按下再弹回，模拟数字"跳动" */
export function pulseNumber(el: HTMLElement): void {
  if (reducedMotion()) return
  gsap.fromTo(
    el,
    { scale: 1 },
    { scale: 1.055, duration: 0.13, ease: EASE.backSoft, yoyo: true, repeat: 1, overwrite: true },
  )
}

/* ------------------------------------------------------------------ *
 * 状态反馈
 * ------------------------------------------------------------------ */

/** 阶段切换：计时卡整体一次轻柔回弹 */
export function popPhase(el: HTMLElement): void {
  if (reducedMotion()) return
  gsap.fromTo(
    el,
    { scale: 0.965 },
    { scale: 1, duration: 0.6, ease: EASE.back, overwrite: true },
  )
}

// 响铃动效由两条独立 tween 组成，分别驱动 scale 与 rotation。
// 两者周期相差数倍（1.1s 与 0.14s），叠加后产生不规则的抖动感。
// 必须都设 overwrite: false，否则后启动的会 kill 掉前一个。

let breatheTween: gsap.core.Tween | null = null
let shakeTween: gsap.core.Tween | null = null

/** 进入响铃态：开始缓慢呼吸与快速小幅摇晃 */
export function startBreathe(el: HTMLElement): void {
  stopBreathe(el)
  if (reducedMotion()) return

  // 归零角度，避免上一次残留的旋转影响本次
  gsap.set(el, { rotation: 0 })

  breatheTween = gsap.to(el, {
    scale: 1.03,
    duration: 1.1,
    ease: 'sine.inOut',
    yoyo: true,
    repeat: -1,
    overwrite: false,
  })

  shakeTween = gsap.to(el, {
    rotation: 0.9,
    duration: 0.14,
    ease: 'sine.inOut',
    yoyo: true,
    repeat: -1,
    overwrite: false,
  })
}

// 恢复分三段：rotation 先反向回摆一次，再弹性归零，scale 并行用 back 收束。
// 先停住晃动、再定住体量，读起来是逐渐安静而非瞬间静止。
export function stopBreathe(el: HTMLElement): void {
  if (breatheTween) {
    breatheTween.kill()
    breatheTween = null
  }
  if (shakeTween) {
    shakeTween.kill()
    shakeTween = null
  }

  if (reducedMotion()) {
    gsap.to(el, { scale: 1, rotation: 0, duration: 0, overwrite: true })
    return
  }

  const tl = gsap.timeline()

  // 余震：向反方向轻轻一摆，再归零
  tl.to(el, {
    rotation: -0.45,
    duration: 0.18,
    ease: 'power2.out',
  }).to(el, {
    rotation: 0,
    duration: 0.42,
    ease: 'elastic.out(1, 0.55)',
  })

  // scale 与晃动并行开始，但用 back 缓动收得更"有重量"
  tl.to(
    el,
    {
      scale: 1,
      duration: 0.5,
      ease: 'back.out(1.4)',
    },
    0,
  )

  return
}

/* ------------------------------------------------------------------ *
 * 交互反馈
 * ------------------------------------------------------------------ */

/** 通用上浮入场，供动态出现的元素使用（如"停止响铃"按钮、错误条） */
export function riseIn(el: HTMLElement): void {
  if (reducedMotion()) {
    gsap.set(el, { opacity: 1 })
    return
  }
  gsap.from(el, { opacity: 0, y: 14, scale: 0.96, duration: 0.42, ease: EASE.back })
}

/**
 * 提示条：滑入后停留，再淡出
 *
 * 返回的 timeline 在出场动画播完后才 resolve，调用方据此移除元素，
 * 不要另起定时器，否则时长对不上会在淡出播完前被摘掉。
 * 用 fromTo 而非 from，避免挂载首帧先以最终状态闪一下。
 */
export function toastIn(el: HTMLElement, holdMs = 1400): gsap.core.Timeline {
  // xPercent 负责水平居中，与 CSS 的 left:50% 配合
  gsap.set(el, { xPercent: -50 })
  const tl = gsap.timeline()

  if (reducedMotion()) {
    tl.set(el, { opacity: 1, y: 0, scale: 1 }).to(el, {
      opacity: 0,
      delay: holdMs / 1000,
      duration: 0.2,
    })
    return tl
  }

  tl.fromTo(
    el,
    { opacity: 0, y: 18, scale: 0.94 },
    { opacity: 1, y: 0, scale: 1, duration: 0.4, ease: EASE.back },
  ).to(el, { opacity: 0, y: 8, duration: 0.3, ease: 'power2.in' }, `+=${holdMs / 1000}`)

  return tl
}

/** 进度条填充：用 scaleX 从 0 展开到 1 */
export function growBar(el: HTMLElement): void {
  if (reducedMotion()) {
    gsap.set(el, { scaleX: 1 })
    return
  }
  gsap.fromTo(
    el,
    { scaleX: 0 },
    { scaleX: 1, duration: 0.7, ease: EASE.out, overwrite: true },
  )
}
