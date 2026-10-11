<script setup lang="ts">
/**
 * 时长输入组件：以「时 : 分 : 秒」三段式设置
 *
 * 每个单位独立输入，支持滚轮与上下按钮微调，回车或失焦时提交。
 */
import { computed, onUnmounted, ref } from 'vue'
import gsap from 'gsap'
import { EASE, reducedMotion } from '../composables/motion'

const props = withDefaults(
  defineProps<{
    modelValue: number
    label?: string
    minSeconds?: number
  }>(),
  {
    label: '',
    minSeconds: 1,
  },
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: number): void
  (e: 'change', value: number): void
}>()

const MAX_H = 23
const MAX_M = 59
const MAX_S = 59

// 拆解为时分秒
const h = computed(() => Math.floor(props.modelValue / 3600))
const m = computed(() => Math.floor((props.modelValue % 3600) / 60))
const s = computed(() => props.modelValue % 60)

/** 各单位的 DOM 引用，用于播放弹跳动画 */
const unitRefs = ref<Record<'h' | 'm' | 's', HTMLElement | null>>({
  h: null,
  m: null,
  s: null,
})

function setUnitRef(unit: 'h' | 'm' | 's') {
  return (el: any) => {
    unitRefs.value[unit] = el as HTMLElement | null
  }
}

/** 单位化到总秒数，并保证不低于下限 */
function combine(hh: number, mm: number, ss: number): number {
  return Math.max(props.minSeconds, hh * 3600 + mm * 60 + ss)
}

/** 数值更新时给对应单位一次弹跳 */
function bumpUnit(unit: 'h' | 'm' | 's') {
  const el = unitRefs.value[unit]
  if (!el || reducedMotion()) return
  gsap.fromTo(
    el,
    { scale: 1 },
    { scale: 1.08, duration: 0.14, ease: EASE.backSoft, yoyo: true, repeat: 1, overwrite: true },
  )
}

/** 提交变更并触发弹跳反馈 */
function commit(unit: 'h' | 'm' | 's', raw: number, max: number) {
  let v = Math.floor(Number(raw))
  if (!Number.isFinite(v) || v < 0) v = 0
  if (v > max) v = max

  const next =
    unit === 'h'
      ? combine(v, m.value, s.value)
      : unit === 'm'
        ? combine(h.value, v, s.value)
        : combine(h.value, m.value, v)

  emit('update:modelValue', next)
  emit('change', next)
  bumpUnit(unit)
}

/** 用上下按钮微调 */
function nudge(unit: 'h' | 'm' | 's', delta: number) {
  if (unit === 'h') {
    commit('h', Math.min(MAX_H, Math.max(0, h.value + delta)), MAX_H)
  } else if (unit === 'm') {
    commit('m', Math.min(MAX_M, Math.max(0, m.value + delta)), MAX_M)
  } else {
    commit('s', Math.min(MAX_S, Math.max(0, s.value + delta)), MAX_S)
  }
}

/** 滚轮调节：向上增、向下减 */
function onWheel(unit: 'h' | 'm' | 's', e: WheelEvent) {
  e.preventDefault()
  nudge(unit, e.deltaY < 0 ? 1 : -1)
}

/** 回车提交并失焦 */
function onKeydown(unit: 'h' | 'm' | 's', max: number, e: KeyboardEvent) {
  if (e.key !== 'Enter') return
  const el = e.target as HTMLInputElement
  commit(unit, Number(el.value), max)
  el.blur()
}

/** 输入框变更时提交 */
function onChange(unit: 'h' | 'm' | 's', max: number, e: Event) {
  const el = e.target as HTMLInputElement
  commit(unit, Number(el.value), max)
}

onUnmounted(() => {
  Object.values(unitRefs.value).forEach((el) => el && gsap.killTweensOf(el))
})
</script>

<template>
  <div class="duration-field">
    <span v-if="label" class="field-label">{{ label }}</span>

    <div class="units">
      <!-- 时 -->
      <div :ref="setUnitRef('h')" class="unit">
        <input
          type="number"
          min="0"
          :max="MAX_H"
          :value="h"
          :aria-label="label + ' 小时'"
          @change="(e: any) => onChange('h', MAX_H, e)"
          @keydown="(e: any) => onKeydown('h', MAX_H, e)"
          @wheel="(e: any) => onWheel('h', e)"
        />
        <div class="steppers">
          <button type="button" tabindex="-1" aria-label="增加一小时" @click="nudge('h', 1)">▲</button>
          <button type="button" tabindex="-1" aria-label="减少一小时" @click="nudge('h', -1)">▼</button>
        </div>
        <span class="unit-label">时</span>
      </div>

      <span class="colon">:</span>

      <!-- 分 -->
      <div :ref="setUnitRef('m')" class="unit">
        <input
          type="number"
          min="0"
          :max="MAX_M"
          :value="m"
          :aria-label="label + ' 分钟'"
          @change="(e: any) => onChange('m', MAX_M, e)"
          @keydown="(e: any) => onKeydown('m', MAX_M, e)"
          @wheel="(e: any) => onWheel('m', e)"
        />
        <div class="steppers">
          <button type="button" tabindex="-1" aria-label="增加一分钟" @click="nudge('m', 1)">▲</button>
          <button type="button" tabindex="-1" aria-label="减少一分钟" @click="nudge('m', -1)">▼</button>
        </div>
        <span class="unit-label">分</span>
      </div>

      <span class="colon">:</span>

      <!-- 秒 -->
      <div :ref="setUnitRef('s')" class="unit">
        <input
          type="number"
          min="0"
          :max="MAX_S"
          :value="s"
          :aria-label="label + ' 秒'"
          @change="(e: any) => onChange('s', MAX_S, e)"
          @keydown="(e: any) => onKeydown('s', MAX_S, e)"
          @wheel="(e: any) => onWheel('s', e)"
        />
        <div class="steppers">
          <button type="button" tabindex="-1" aria-label="增加一秒" @click="nudge('s', 1)">▲</button>
          <button type="button" tabindex="-1" aria-label="减少一秒" @click="nudge('s', -1)">▼</button>
        </div>
        <span class="unit-label">秒</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.duration-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.field-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--body);
}

.units {
  display: flex;
  margin-top: 10px;
  align-items: flex-start;
  justify-content: center;
  gap: 4px;
}

.unit {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
}

.colon {
  font-size: 18px;
  font-weight: 700;
  color: var(--muted-soft);
  line-height: 44px;
}

/* Clay 的 text-input：奶油底 + 发丝边框 + 12px 圆角 */
input {
  width: 52px;
  height: 44px;
  box-sizing: border-box;
  text-align: center;
  font-size: 16px;
  font-weight: 700;
  font-family: inherit;
  font-variant-numeric: tabular-nums;
  color: var(--ink);
  background: var(--canvas);
  border: 1px solid var(--hairline);
  border-radius: var(--r-md);
  outline: none;
  /* 隐藏数字输入框的原生上下箭头，改用自定义步进按钮 */
  appearance: textfield;
  -moz-appearance: textfield;
  transition:
    border-color var(--t-fast) var(--ease-standard),
    box-shadow var(--t-fast) var(--ease-standard);
}

input::-webkit-outer-spin-button,
input::-webkit-inner-spin-button {
  appearance: none;
  margin: 0;
}

.unit:hover input {
  border-color: var(--muted-soft);
}

/* text-input-focused：边框转墨色 */
input:focus {
  border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(10, 10, 10, 0.06);
}

.steppers {
  display: flex;
  gap: 3px;
}

.steppers button {
  width: 26px;
  height: 18px;
  padding: 0;
  font-size: 8px;
  line-height: 1;
  color: var(--muted);
  background: var(--surface-card);
  border: none;
  border-radius: var(--r-xs);
  cursor: pointer;
  transition:
    background var(--t-fast) var(--ease-standard),
    color var(--t-fast) var(--ease-standard);
}

.steppers button:hover {
  background: var(--primary);
  color: var(--on-primary);
}

.steppers button:active {
  background: var(--primary-active);
}

.unit-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--muted-soft);
  letter-spacing: 1px;
}
</style>
