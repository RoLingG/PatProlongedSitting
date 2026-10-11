<script setup lang="ts">
/**
 * 吉祥物：一张 512×512 的方图，角色画在正中的圆角方形卡片里，四角透明。
 *
 * 图本身不裁剪也不填色，只在背后铺一层装饰，即两道扩散环加一圈虚线环。
 * 装饰用环而非实心光晕，是因为环是线不是面，不会与方形图标构成第二个轮廓。
 * 动效由 CSS 驱动，与业务状态无关，因此不纳入 GSAP 的上下文管理。
 */
import { computed } from 'vue'
import mascotImg from '../assets/images/mascot.png'

const props = withDefaults(
  defineProps<{
    phase?: 'idle' | 'sitting' | 'standing'
    /** 是否正在响铃，响铃时装饰环改用白色 */
    ringing?: boolean
    /** 边长的像素值 */
    size?: number
  }>(),
  {
    phase: 'idle',
    ringing: false,
    size: 112,
  },
)

// 装饰环的色相，平时跟随阶段，响铃时改用白色以在深色卡底上保持可见
const accent = computed(() => {
  if (props.ringing) return '#ffffff'
  return props.phase === 'standing' ? '#4a7a5e' : '#e05a78'
})
</script>

<template>
  <div
    class="mascot"
    :class="[phase, { ringing }]"
    :style="{ '--size': size + 'px', '--mascot-accent': accent }"
    aria-hidden="true"
  >
    <!-- 背景装饰：两道扩散环 + 细虚线环，都在图标之下 -->
    <span class="halo"></span>
    <span class="pulse pulse-a"></span>
    <span class="pulse pulse-b"></span>
    <span class="dashed"></span>

    <img :src="mascotImg" alt="" draggable="false" />
  </div>
</template>

<style scoped>
.mascot {
  position: relative;
  width: var(--size);
  height: var(--size);
  flex: none;
  display: grid;
  place-items: center;
}

/* 底色光晕：一团极淡的阶段色，把图标从卡底上垫起来 */
.halo {
  position: absolute;
  inset: -6%;
  border-radius: 50%;
  background: radial-gradient(
    circle at 50% 45%,
    color-mix(in srgb, var(--mascot-accent) 16%, transparent),
    transparent 68%
  );
  transition: background var(--t-slow) var(--ease-standard);
}

/* 两道扩散环，错开半个周期向外扩张淡出。峰值透明度由变量控制 */
.pulse {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  border: 1.5px solid var(--mascot-accent);
  opacity: 0;
  pointer-events: none;
  animation: halo-expand 3.2s var(--ease-out) infinite;
}

.pulse-b {
  animation-delay: 1.6s;
}

@keyframes halo-expand {
  0% {
    transform: scale(0.86);
    opacity: 0;
  }
  25% {
    opacity: var(--pulse-peak, 0.28);
  }
  100% {
    transform: scale(1.22);
    opacity: 0;
  }
}

/* 细虚线环：一圈静止的装饰，同时框住扩散环的活动范围 */
.dashed {
  position: absolute;
  inset: -9%;
  border-radius: 50%;
  border: 1.5px dashed var(--mascot-accent);
  opacity: var(--dashed-opacity, 0.22);
  pointer-events: none;
  transition:
    opacity var(--t-slow) var(--ease-standard),
    border-color var(--t-slow) var(--ease-standard);
}

/* 响铃态：环改白色后对比变弱，需要更高的不透明度 */
.mascot.ringing {
  --pulse-peak: 0.5;
  --dashed-opacity: 0.42;
}

/* 完整显示位图，drop-shadow 跟随 alpha 轮廓只沿实际边缘落影 */
img {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  object-fit: contain;
  background: transparent;
  transition:
    filter var(--t-base) var(--ease-out),
    transform var(--t-base) var(--ease-out);
  filter: drop-shadow(0 2px 8px rgba(60, 45, 25, 0.16));
}

.mascot:hover img {
  transform: scale(1.05);
}

.mascot:hover .dashed {
  opacity: 0.4;
}

/* 减弱动画：波纹与虚线环全部停住，只留静态的光晕 */
@media (prefers-reduced-motion: reduce) {
  .mascot:hover img {
    transform: none;
  }

  .pulse {
    animation: none;
    opacity: 0.14;
    transform: scale(1.04);
  }

  /* 响铃态下静态环也要够清楚，否则白环在粉底上几乎看不见 */
  .mascot.ringing .pulse {
    opacity: 0.3;
  }

  .pulse-b {
    opacity: 0;
  }

  .mascot.ringing .pulse-b {
    opacity: 0;
  }
}
</style>
