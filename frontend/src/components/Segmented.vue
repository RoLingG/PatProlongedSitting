<script setup lang="ts">
/**
 * 分段控件：带滑动指示器的单选组
 *
 * 指示器位移由 GSAP 补间，带轻微回弹。
 */
import { computed, ref, watch, onMounted } from 'vue'
import gsap from 'gsap'
import { EASE, reducedMotion } from '../composables/motion'
import type { SegmentOption } from '../types/types'

const props = withDefaults(
  defineProps<{
    options: SegmentOption[]
    modelValue: string
    /** 无障碍组名 */
    name?: string
  }>(),
  { name: 'segment' },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

const root = ref<HTMLElement | null>(null)

/** 当前选中项的索引，用于计算指示器位置 */
const index = computed(() => {
  const i = props.options.findIndex((o) => o.value === props.modelValue)
  return i < 0 ? 0 : i
})

/** 指示器的宽度（一半或三分之一，取决于选项数） */
const widthPercent = computed(() => 100 / props.options.length)

/**
 * 指示器位移。
 * 用百分比 translate 而非像素，窗口缩放时无需重算。
 */
function place(animated: boolean) {
  const thumb = root.value?.querySelector<HTMLElement>('.thumb')
  if (!thumb) return
  const target = {
    xPercent: index.value * 100,
    width: `${widthPercent.value}%`,
  }
  if (!animated || reducedMotion()) {
    gsap.set(thumb, target)
    return
  }
  gsap.to(thumb, { ...target, duration: 0.42, ease: EASE.back, overwrite: true })
}

onMounted(() => place(false))
watch(() => [props.modelValue, props.options.length], () => place(true))

function select(v: string) {
  if (v === props.modelValue) return
  emit('update:modelValue', v)
}

/** 左右方向键切换，并让焦点跟随 */
function onKey(e: KeyboardEvent) {
  if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
  e.preventDefault()
  const dir = e.key === 'ArrowRight' ? 1 : -1
  const next = (index.value + dir + props.options.length) % props.options.length
  select(props.options[next].value)
  const el = e.currentTarget as HTMLElement
  el.querySelectorAll<HTMLButtonElement>('button')[next]?.focus()
}
</script>

<template>
  <div ref="root" class="segmented" role="radiogroup" :aria-label="name" @keydown="onKey">
    <!-- 滑动指示器 -->
    <div class="thumb"></div>

    <button
      v-for="opt in options"
      :key="opt.value"
      type="button"
      role="radio"
      :aria-checked="modelValue === opt.value"
      :class="{ active: modelValue === opt.value }"
      @click="select(opt.value)"
    >
      {{ opt.label }}
    </button>
  </div>
</template>

<style scoped>
.segmented {
  position: relative;
  display: flex;
  padding: 4px;
  background: var(--surface-card);
  border-radius: var(--r-pill);
  gap: 4px;
}

/* 选中态：墨色实心 pill（Clay 的 button-primary 语汇） */
.thumb {
  position: absolute;
  top: 4px;
  left: 4px;
  height: calc(100% - 8px);
  background: var(--primary);
  border-radius: var(--r-pill);
  pointer-events: none;
  /* 初始宽度由 GSAP 设置，这里兜底避免首帧闪动 */
  width: 50%;
}

button {
  position: relative;
  z-index: 1;
  flex: 1;
  padding: 8px 10px;
  font-size: 12px;
  font-weight: 600;
  font-family: inherit;
  color: var(--muted);
  background: transparent;
  border: none;
  border-radius: var(--r-pill);
  cursor: pointer;
  white-space: nowrap;
  transition: color var(--t-base) var(--ease-standard);
}

button:hover {
  color: var(--body-strong);
}

button.active {
  color: var(--on-primary);
}

button:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 2px;
}
</style>
