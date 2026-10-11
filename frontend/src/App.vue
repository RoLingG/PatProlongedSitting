<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import gsap from 'gsap'
import { EventsOn, EventsOff } from '../wailsjs/runtime/runtime'
import {
  GetConfig,
  SaveConfig,
  StartTimer,
  PauseTimer,
  ResumeTimer,
  StopTimer,
  GetTimerState,
  StopRing,
  SnoozeRing,
  PickSoundFile,
  ReadSoundFile,
} from '../wailsjs/go/main/App'
import { main } from '../wailsjs/go/models'
import { AudioPlayer } from './composables/audio'
import Portrait from './components/Portrait.vue'
import Segmented from './components/Segmented.vue'
import DurationInput from './components/DurationInput.vue'
import {
  EASE,
  growBar,
  playIntro,
  playViewEnter,
  playViewLeave,
  popMascot,
  popPhase,
  pulseNumber,
  reducedMotion,
  riseIn,
  startBreathe,
  stopBreathe,
  toastIn,
  tweenBarFill,
} from './composables/motion'
import {
  type Config,
  type TimerState,
  type PhaseEndPayload,
  type Phase,
  asPhase,
  defaultConfig,
  formatClock,
  humanDuration,
} from './types/types'

// ---- 状态 ----
const cfg = ref<Config>(defaultConfig())
const state = ref<TimerState>(
  main.TimerState.createFrom({
    phase: 'idle',
    remainingSec: 0,
    totalSec: 0,
    running: false,
    paused: false,
    completed: 0,
    remindCount: 0,
    ringing: false,
  }),
)

const soundName = ref('内置提示音')
const saved = ref(false)
const errMsg = ref('')
/** 当前视图：主页 / 设置页 */
const view = ref<'home' | 'settings'>('home')

const player = new AudioPlayer()
let soundData: ArrayBuffer | null = null

// ---- DOM 引用（GSAP 动画目标） ----
const homeEl = ref<HTMLElement | null>(null)
const settingsEl = ref<HTMLElement | null>(null)
const clockCard = ref<HTMLElement | null>(null)
const timeEl = ref<HTMLElement | null>(null)
const errEl = ref<HTMLElement | null>(null)
const toastEl = ref<HTMLElement | null>(null)
const sitBar = ref<HTMLElement | null>(null)
const standBar = ref<HTMLElement | null>(null)

/** GSAP 上下文：视图内的补间集中回收，避免切换后残留 */
let homeCtx: gsap.Context | null = null
let settingsCtx: gsap.Context | null = null
/** 当前正在播放的「已保存」提示条动画，用于打断上一次未播完的 */
let toastTl: gsap.core.Timeline | null = null

// ---- 派生 ----
const phase = computed<Phase>(() => asPhase(state.value.phase))
const isSitting = computed(() => phase.value === 'sitting')
const isStanding = computed(() => phase.value === 'standing')
const running = computed(() => state.value.running)
const paused = computed(() => state.value.paused)

const progress = computed(() =>
  state.value.totalSec > 0 ? 1 - Math.max(0, state.value.remainingSec) / state.value.totalSec : 0,
)

// 剩余比例，即进度条长度。待机时 totalSec 为 0，直接返回 0
const remainingRatio = computed(() => {
  if (state.value.totalSec <= 0) return 0
  return 1 - progress.value
})

const phaseText = computed(() =>
  isSitting.value ? '久坐中' : isStanding.value ? '站立中' : '准备开始',
)

const phaseHint = computed(() =>
  isSitting.value
    ? '保持坐姿，专注手头的事'
    : isStanding.value
      ? '起身活动，放松肩颈'
      : '设置好时长后，点击开始',
)

// 阶段强调色，供 GSAP 补间使用，须为可插值的 hex 字面量。
// 待机沿用久坐的玫瑰，不必为"还没开始"再占一个色相
const THEME = {
  sit: '#e05a78',
  stand: '#4a7a5e',
} as const

/** 当前阶段的强调色 */
const accent = computed(() => (isStanding.value ? THEME.stand : THEME.sit))

/** 久坐 / 站立时长在总循环中的占比，用于进度条 */
const sitRatio = computed(() => {
  const total = cfg.value.sitSeconds + cfg.value.standSeconds
  return total > 0 ? cfg.value.sitSeconds / total : 0.5
})

const endModeOptions = [
  { value: 'forever', label: '一直提醒' },
  { value: 'count', label: '提醒 N 次后停止' },
]

const ringModeOptions = [
  { value: 'times', label: '响 N 次' },
  { value: 'loop', label: '循环到手动停止' },
]

// ---- 生命周期 ----
onMounted(async () => {
  homeCtx = gsap.context(() => playIntro(homeEl.value!), homeEl.value!)
  settingsCtx = gsap.context(() => {}, settingsEl.value!)

  try {
    cfg.value = await GetConfig()
    state.value = await GetTimerState()
    refreshSoundName()
  } catch (e) {
    errMsg.value = '初始化失败：' + String(e)
  }

  EventsOn('timer:tick', (s: TimerState) => {
    state.value = s
  })
  EventsOn('timer:phaseEnd', (p: PhaseEndPayload) => {
    void handlePhaseEnd(p)
  })
  EventsOn('ring:stop', () => {
    player.stop()
    state.value = main.TimerState.createFrom({ ...state.value, ringing: false })
  })

  player.onFinished = () => {
    void StopRing()
  }

  window.addEventListener('keydown', onKeydown)
  await nextTick()
  // 进度条初值直接用 GSAP 落位，避免首帧从 0 闪一下
  const fill = clockCard.value?.querySelector<HTMLElement>('.clock-bar-fill')
  if (fill) tweenBarFill(fill, remainingRatio.value)
})

onUnmounted(() => {
  EventsOff('timer:tick')
  EventsOff('timer:phaseEnd')
  EventsOff('ring:stop')
  window.removeEventListener('keydown', onKeydown)
  player.dispose()
  homeCtx?.revert()
  settingsCtx?.revert()
  if (breatheTarget) stopBreathe(breatheTarget)
  toastTl?.kill()
  toastTl = null
})

// ---- 动效驱动 ----
/** 秒数变化时给一次跳动反馈 */
watch(
  () => state.value.remainingSec,
  () => {
    if (timeEl.value) pulseNumber(timeEl.value)
  },
)

/** 阶段切换：卡片回弹 + 进度条"翻篇"脉冲 */
watch(phase, async (next, prev) => {
  if (next === prev) return
  await nextTick()
  if (clockCard.value) popPhase(clockCard.value)
})

/** 进度条宽度：交给 GSAP 补间，不用 CSS transition */
watch(
  () => remainingRatio.value,
  (r) => {
    const fill = clockCard.value?.querySelector<HTMLElement>('.clock-bar-fill')
    if (fill) tweenBarFill(fill, r)
  },
)

/** 响铃状态：卡片脉动，退出时复原 */
let breatheTarget: HTMLElement | null = null
watch(
  () => state.value.ringing,
  async (ringing) => {
    await nextTick()
    if (ringing && clockCard.value) {
      breatheTarget = clockCard.value
      startBreathe(clockCard.value)
    } else if (breatheTarget) {
      stopBreathe(breatheTarget)
      breatheTarget = null
    }
  },
)

/** 错误条出现时落入 */
watch(errMsg, async (v) => {
  if (!v) return
  await nextTick()
  if (errEl.value) riseIn(errEl.value)
})

/** 设置页的时长占比条随配置增长（仅在设置页可见时才动画） */
watch(
  () => [cfg.value.sitSeconds, cfg.value.standSeconds],
  async () => {
    if (view.value !== 'settings') return
    await nextTick()
    if (sitBar.value) growBar(sitBar.value)
    if (standBar.value) growBar(standBar.value)
  },
)

// ---- 提醒处理 ----
async function handlePhaseEnd(p: PhaseEndPayload) {
  try {
    const data = await loadSound(p.soundPath)
    await player.play({
      data,
      volume: p.volume,
      mode: p.mode === 'times' ? 'times' : 'loop',
      repeatTimes: p.repeatTimes,
    })
  } catch (e) {
    console.error('播放提示音失败', e)
  }
}

async function loadSound(path: string): Promise<ArrayBuffer | null> {
  if (!path) return null
  if (soundData) return soundData
  const bytes = await ReadSoundFile(path)
  if (!bytes || bytes.length === 0) return null
  const buf = toArrayBuffer(bytes)
  soundData = buf
  return buf
}

function toArrayBuffer(bytes: any): ArrayBuffer {
  if (bytes instanceof ArrayBuffer) return bytes
  if (typeof bytes === 'string') {
    const bin = atob(bytes)
    const out = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
    return out.buffer
  }
  if (Array.isArray(bytes)) {
    return new Uint8Array(bytes).buffer
  }
  throw new Error('无法识别的音频数据格式')
}

function refreshSoundName() {
  soundName.value = cfg.value.soundPath ? baseName(cfg.value.soundPath) : '内置提示音'
  soundData = null
}

function baseName(p: string): string {
  const parts = p.split(/[\\/]/)
  return parts[parts.length - 1] || p
}

// ---- 操作 ----

// 按下反馈由 CSS 的 :active 处理，不用 GSAP 补间
async function toggleRun() {
  if (!running.value) {
    await StartTimer()
  } else if (paused.value) {
    await ResumeTimer()
  } else {
    await PauseTimer()
  }
}

async function resetTimer() {
  await StopTimer()
}

async function stopRing() {
  player.stop()
  await StopRing()
}

// 点击整卡停止响铃，仅在响铃时响应
function onCardClick() {
  if (!state.value.ringing) return
  void stopRing()
}

/** 键盘等效点击：回车与空格 */
function onCardKeydown(e: KeyboardEvent) {
  if (!state.value.ringing) return
  if (e.key !== 'Enter' && e.key !== ' ') return
  e.preventDefault()
  void stopRing()
}

// 右键整卡延后 5 分钟，延后是低频操作，不在卡片上占按钮位
function onCardContextMenu(e: MouseEvent) {
  if (!state.value.ringing) return
  e.preventDefault()
  void snoozeRing()
}

async function snoozeRing() {
  player.stop()
  await SnoozeRing()
}

async function pickSound() {
  const p = await PickSoundFile()
  if (p) {
    cfg.value.soundPath = p
    refreshSoundName()
    await persist()
  }
}

async function clearSound() {
  // 换音源前先停掉可能正在进行的试听，否则会听到旧音频
  player.stop()
  previewState.value = 'idle'
  cfg.value.soundPath = ''
  refreshSoundName()
  await persist()
}

// 试听状态机。不能读 player 的内部字段，因为它是普通对象，改它不触发 Vue 更新。
// 各分支须依据引擎返回值改状态，音频播完时 pause 返回 false
const previewState = ref<'idle' | 'playing' | 'paused'>('idle')

/** 试听 / 暂停 / 继续：同一个按钮，按当前状态决定行为 */
async function togglePreview() {
  if (previewState.value === 'playing') {
    // 只有引擎确认暂停成功才切状态，否则说明已经播完，直接回 idle
    previewState.value = player.pause() ? 'paused' : 'idle'
    return
  }
  if (previewState.value === 'paused') {
    previewState.value = player.resume() ? 'playing' : 'idle'
    return
  }

  // idle → 从头播一次
  try {
    const data = await loadSound(cfg.value.soundPath)
    previewState.value = 'playing'
    await player.preview(data, cfg.value.volume)
    // 播放自然结束才会走到这；若中途被 stop/暂停，状态已由对应操作改写
    if (previewState.value === 'playing') previewState.value = 'idle'
  } catch (e) {
    previewState.value = 'idle'
    errMsg.value = '试听失败：' + String(e)
  }
}

/** 停止试听并回到初始态 */
function stopPreview() {
  player.stop()
  previewState.value = 'idle'
}

// 保存配置并弹一次「已保存」提示条。
// 元素移除交给动画的 onComplete，不要另起定时器，否则时长对不上会在淡出前被摘掉。
// 连续操作会多次进入这里，先杀掉上一次未播完的 timeline
async function persist() {
  try {
    cfg.value = await SaveConfig(cfg.value)
    saved.value = true
    errMsg.value = ''
    await nextTick()

    if (toastEl.value) {
      toastTl?.kill()
      toastTl = toastIn(toastEl.value)
      toastTl.eventCallback('onComplete', () => {
        saved.value = false
        toastTl = null
      })
    } else {
      // 拿不到元素时兜底，避免提示条永远留在界面上
      setTimeout(() => (saved.value = false), 1800)
    }
  } catch (e) {
    errMsg.value = '保存失败：' + String(e)
  }
}

/** 时长变更（来自 DurationInput） */
async function onSitChange(v: number) {
  cfg.value.sitSeconds = v
  await persist()
}

async function onStandChange(v: number) {
  cfg.value.standSeconds = v
  await persist()
}

/**
 * 恢复默认时长：只动久坐与站立两项。
 *
 * 特意不从 defaultConfig() 整体覆盖 cfg —— 那样会把用户选的提示音、
 * 音量、响铃模式一并冲掉，而按钮的文案只承诺"恢复默认时长"。
 * 写明常量而不是引用 defaultConfig()，是为了让这里改的是"时长默认值"，
 * 将来若 defaultConfig() 的字段增删，也不会意外影响这个按钮。
 */
const DEFAULT_SIT_SECONDS = 45 * 60
const DEFAULT_STAND_SECONDS = 5 * 60

async function resetDurations() {
  cfg.value.sitSeconds = DEFAULT_SIT_SECONDS
  cfg.value.standSeconds = DEFAULT_STAND_SECONDS
  await persist()
  // 两张时长卡上的占比条重新生长一次，给一个"已重置"的视觉确认
  await nextTick()
  if (sitBar.value) growBar(sitBar.value)
  if (standBar.value) growBar(standBar.value)
}

/** 打开设置页：主页向左退场，设置页自右入场 */
async function openSettings() {
  if (view.value === 'settings') return
  const home = homeEl.value
  const settings = settingsEl.value
  if (!home || !settings) return

  playViewLeave(home, 'left')
  view.value = 'settings'
  await nextTick()
  settingsCtx?.revert()
  settingsCtx = gsap.context(() => playViewEnter(settings, 'right'), settings)
  // 时长占比条随入场一起生长
  if (sitBar.value) growBar(sitBar.value)
  if (standBar.value) growBar(standBar.value)
}

/** 返回主页：设置页向右退场，主页自左归来 */
async function backHome() {
  if (view.value === 'home') return
  const home = homeEl.value
  const settings = settingsEl.value
  if (!home || !settings) return

  playViewLeave(settings, 'right')
  view.value = 'home'
  await nextTick()
  homeCtx?.revert()
  homeCtx = gsap.context(() => playViewEnter(home, 'left'), home)
}

/** ESC 在设置页时返回主页 */
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && view.value === 'settings') {
    backHome()
  }
}

/** 音量滑块：拖动实时生效，松手保存 */
function onVolumeInput() {
  player.setVolume(cfg.value.volume)
}

/** 提醒方式 / 响铃模式（Segmented 以 update:modelValue 回传） */
async function onEndModeUpdate(v: string) {
  cfg.value.endMode = v
  await persist()
}

async function onRingModeUpdate(v: string) {
  cfg.value.mode = v
  await persist()
}
</script>

<template>
  <div class="app" :class="{ sitting: isSitting, standing: isStanding, running }">
    <!-- ================= 主页 ================= -->
    <main ref="homeEl" class="page home" :class="{ active: view === 'home', ringing: state.ringing }">
      <!-- ===== 顶部：品牌 + 设置入口 ===== -->
      <header class="hero" data-anim="header">
        <div class="hero-main">
          <div class="hero-text">
            <h1>久坐提醒</h1>
            <p class="sub">{{ phaseHint }}</p>
          </div>
          <button class="icon-btn" aria-label="打开设置" @click="openSettings">
            <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
              <path
                d="M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
              />
              <path
                d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1 1.55V21a2 2 0 1 1-4 0v-.09A1.7 1.7 0 0 0 8.9 19.3a1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 4.6 15a1.7 1.7 0 0 0-1.55-1H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.6 8.9a1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.7 1.7 0 0 0 8.9 4.6h.09A1.7 1.7 0 0 0 10 3.05V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1 1.55"
                fill="none"
                stroke="currentColor"
                stroke-width="1.8"
                stroke-linecap="round"
              />
            </svg>
          </button>
        </div>
      </header>

      <div v-if="errMsg" ref="errEl" class="alert" role="alert">{{ errMsg }}</div>

      <!-- ===== 计时卡 ===== -->
      <section class="stage">
        <!-- 计时卡：响铃时可点击停止 -->
        <div
          ref="clockCard"
          class="clock-card"
          :data-phase="phase"
          data-anim="clock"
          :class="{ actionable: state.ringing }"
          :role="state.ringing ? 'button' : undefined"
          :tabindex="state.ringing ? 0 : undefined"
          :aria-label="state.ringing ? '停止响铃（右键延后 5 分钟）' : undefined"
          @click="onCardClick"
          @keydown="onCardKeydown"
          @contextmenu="onCardContextMenu"
          :title="state.ringing ? '点击停止响铃 · 右键延后 5 分钟' : undefined"
        >
          <span class="clock-badge">
            <span class="badge-dot"></span>
            {{ phaseText }}
          </span>

          <div class="clock-body">
            <!-- 吉祥物 -->
            <div class="mascot-slot">
              <Portrait :phase="phase" :ringing="state.ringing" :size="168" />
            </div>

            <div class="clock-readout">
              <div ref="timeEl" class="clock-time">{{ formatClock(state.remainingSec) }}</div>

              <!-- 进度条 -->
              <div class="clock-bar">
                <span class="clock-bar-fill"></span>
              </div>
            </div>
          </div>
        </div>

        <!-- 循环统计 -->
        <div class="stats" data-anim="stats">
          <div class="stat">
            <span class="stat-num">{{ state.completed }}</span>
            <span class="stat-label">完成循环</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat">
            <span class="stat-num">{{ state.remindCount }}</span>
            <span class="stat-label">已提醒</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat">
            <span class="stat-num">{{ humanDuration(cfg.sitSeconds) }}</span>
            <span class="stat-label">久坐时长</span>
          </div>
          <div class="stat-divider"></div>
          <div class="stat">
            <span class="stat-num">{{ humanDuration(cfg.standSeconds) }}</span>
            <span class="stat-label">站立时长</span>
          </div>
        </div>
      </section>

      <!-- ===== 主操作 ===== -->
      <section class="controls" data-anim="controls">
        <button class="btn primary" @click="toggleRun">
          <template v-if="!running">开始</template>
          <template v-else-if="paused">继续</template>
          <template v-else>暂停</template>
        </button>

        <button class="btn secondary" :disabled="!running" @click="resetTimer">重置</button>
      </section>
    </main>

    <!-- ================= 设置页 ================= -->
    <section
      ref="settingsEl"
      class="page settings"
      :class="{ active: view === 'settings' }"
      :aria-hidden="view !== 'settings'"
    >
      <header class="settingsHead" data-stagger>
        <button class="back-btn" aria-label="返回主页" @click="backHome">
          <span class="arrow">←</span>
          <span>返回</span>
        </button>
        <h2 class="settingsTitle">设置</h2>
      </header>

      <div class="settingsContent">
        <!-- 左栏：时间设置（两张饱和色功能卡）+ 提示音 -->
        <div class="col">
          <div class="group time-group" data-stagger>
            <div class="phase-card sitting-card">
              <div class="phase-card-head">
                <span class="phase-dot"></span>
                <span class="phase-card-label">久坐</span>
                <span class="phase-card-value">{{ humanDuration(cfg.sitSeconds) }}</span>
              </div>
              <div class="phase-bar">
                <span
                  ref="sitBar"
                  class="phase-bar-fill"
                  :style="{ width: (sitRatio * 100).toFixed(1) + '%' }"
                ></span>
              </div>
              <DurationInput
                :model-value="cfg.sitSeconds"
                :min-seconds="1"
                @change="onSitChange"
              />
            </div>

            <div class="phase-card standing-card">
              <div class="phase-card-head">
                <span class="phase-dot"></span>
                <span class="phase-card-label">站立</span>
                <span class="phase-card-value">{{ humanDuration(cfg.standSeconds) }}</span>
              </div>
              <div class="phase-bar">
                <span
                  ref="standBar"
                  class="phase-bar-fill"
                  :style="{ width: ((1 - sitRatio) * 100).toFixed(1) + '%' }"
                ></span>
              </div>
              <DurationInput
                :model-value="cfg.standSeconds"
                :min-seconds="1"
                @change="onStandChange"
              />
            </div>

            <div class="phase-card sound-card" data-stagger>
              <div class="phase-card-head">
                <span class="phase-dot"></span>
                <span class="phase-card-label">提示音</span>
                <span class="phase-card-value">{{ Math.round(cfg.volume * 100) }}%</span>
              </div>

              <!-- 文件名 -->
              <div class="sound-name-row">
                <span class="sound-name" :title="cfg.soundPath || '内置提示音'">{{ soundName }}</span>
              </div>

              <div class="sound-actions">
                <button class="btn tiny" @click="pickSound">选择</button>
                <!-- 试听 / 暂停 / 继续，三种状态共用一个按钮 -->
                <button class="btn tiny" @click="togglePreview">
                  {{ previewState === 'playing' ? '暂停' : previewState === 'paused' ? '继续' : '试听' }}
                </button>
                <button class="btn tiny" :disabled="previewState === 'idle'" @click="stopPreview">
                  停止
                </button>
                <button class="btn tiny ghost" :disabled="!cfg.soundPath" @click="clearSound">
                  默认
                </button>
              </div>

              <div class="slider-field">
                <label for="vol">音量</label>
                <input
                    id="vol"
                    v-model.number="cfg.volume"
                    type="range"
                    min="0"
                    max="1"
                    step="0.01"
                    @input="onVolumeInput"
                    @change="persist"
                />
                <span class="vol-num">{{ Math.round(cfg.volume * 100) }}%</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 右栏：提醒方式 + 启动 + 循环概览 -->
        <div class="col">
          <div class="group" data-stagger>
            <h3 class="group-title">提醒方式</h3>
            <Segmented
              name="提醒方式"
              :options="endModeOptions"
              :model-value="cfg.endMode"
              @update:model-value="onEndModeUpdate"
            />

            <!-- 固定高度插槽，兜住二选一显示的两块，避免卡片高度跳变 -->
            <div class="field-slot">
              <div v-if="cfg.endMode === 'count'" class="inline-field">
                <span>提醒</span>
                <input
                  v-model.number="cfg.remindCount"
                  type="number"
                  min="1"
                  max="999"
                  @change="persist"
                />
                <span>次后自动停止</span>
              </div>
              <p v-else class="group-note">
                每次到达时长都会响铃提醒，直到你手动重置
              </p>
            </div>
          </div>

          <div class="group" data-stagger>
            <h3 class="group-title">启动</h3>
            <label class="switch-field">
              <input v-model="cfg.autoStart" type="checkbox" @change="persist" />
              <span class="track"><span class="knob"></span></span>
              <span class="switch-text">开机自动启动</span>
            </label>

            <!-- 重置时长 -->
            <div class="reset-row">
              <div class="reset-text">
                <span class="reset-label">重置时长</span>
                <span class="reset-hint">久坐 45 分钟 · 站立 5 分钟</span>
              </div>
              <button class="btn tiny" @click="resetDurations">恢复</button>
            </div>
          </div>

          <div class="group recap-group" data-stagger>
            <h3 class="group-title">循环概览</h3>

            <!-- 久坐与站立的循环占比 -->
            <div class="ratio-bar" :aria-label="`久坐占 ${Math.round(sitRatio * 100)}%`">
              <span
                class="ratio-seg sit"
                :style="{ width: (sitRatio * 100).toFixed(1) + '%' }"
              ></span>
              <span
                class="ratio-seg stand"
                :style="{ width: ((1 - sitRatio) * 100).toFixed(1) + '%' }"
              ></span>
            </div>

            <div class="recap">
              <div class="recap-row">
                <span class="recap-dot sit"></span>
                <span class="recap-key">久坐</span>
                <span class="recap-val">{{ humanDuration(cfg.sitSeconds) }}</span>
              </div>
              <div class="recap-arrow">↓ 提醒并起身</div>
              <div class="recap-row">
                <span class="recap-dot stand"></span>
                <span class="recap-key">站立</span>
                <span class="recap-val">{{ humanDuration(cfg.standSeconds) }}</span>
              </div>
              <div class="recap-arrow">↓ 提醒并坐下</div>
              <div class="recap-row">
                <span class="recap-dot loop"></span>
                <span class="recap-key">单次循环</span>
                <span class="recap-val">
                  {{ humanDuration(cfg.sitSeconds + cfg.standSeconds) }}
                </span>
              </div>
            </div>
            
          </div>
        </div>
      </div>
    </section>

    <div v-if="saved" ref="toastEl" class="toast">已保存</div>
  </div>
</template>

<style scoped>
/* ===== 页面容器 ===== */
.app {
  /* 两个视图重叠在同一格，用 grid 而非 absolute，最小高度跟随内容撑开 */
  display: grid;
  width: 100%;
  min-height: 100vh;
  overflow-x: hidden;
}

/* 未激活的页面不可见也不可交互，可见性由 GSAP 的 autoAlpha 控制 */
.page {
  grid-area: 1 / 1;
  overflow-x: hidden;
  opacity: 0;
  visibility: hidden;
  pointer-events: none;
}

.page.active {
  opacity: 1;
  visibility: visible;
  pointer-events: auto;
}

.home {
  --stage-w: min(100%, 420px);
  max-width: 620px;
  width: 100%;
  margin: 0 auto;
  padding: 34px 28px 44px;
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.settings {
  background: var(--canvas);
}

/* ===== 顶部 ===== */
.hero {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.hero-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.hero-text {
  min-width: 0;
}

h1 {
  margin: 0 0 3px;
  font-family: var(--font-display);
  font-size: 28px;
  font-weight: 600;
  line-height: 1.15;
  letter-spacing: -0.9px;
  color: var(--ink);
}

.sub {
  margin: 0;
  font-size: 13px;
  line-height: 1.45;
  color: var(--muted);
}

/* 设置入口 */
.icon-btn {
  flex: none;
  width: 42px;
  height: 42px;
  display: grid;
  place-items: center;
  color: var(--ink);
  background: var(--canvas);
  border: 1px solid var(--hairline);
  border-radius: var(--r-md);
  cursor: pointer;
  transition:
    background var(--t-fast) var(--ease-standard),
    transform var(--t-fast) var(--ease-out);
}

.icon-btn:hover {
  background: var(--surface-card);
  border-color: var(--surface-strong);
}

.icon-btn:active {
  transform: scale(0.94);
}

/* 错误条 */
.alert {
  padding: 10px 14px;
  font-size: 13px;
  font-weight: 600;
  color: var(--on-primary);
  background: var(--theme);
  border-radius: var(--r-md);
}

/* ===== 计时区 ===== */
.stage {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

/* 计时卡 */
.clock-card {
  --accent: var(--theme);

  position: relative;
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 26px;
  padding: 34px 32px 32px;
  box-sizing: border-box;
  border-radius: var(--r-xl);
  background: var(--theme-surface);
  box-shadow: var(--card-edge);
  transition: background 0.5s var(--ease-standard);
}

.clock-card[data-phase='standing'] {
  --accent: var(--theme-alt);
}

/* 响铃态：整卡染上强调色 */
.home.ringing .clock-card {
  --accent: var(--theme);
  background: var(--theme);
  box-shadow: none;
}

/* 阶段标签 */
.clock-badge {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 5px 13px 5px 11px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.2px;
  border-radius: var(--r-pill);
  background: var(--canvas);
  color: var(--body-strong);
  transition:
    background 0.5s var(--ease-standard),
    color 0.5s var(--ease-standard);
}

.badge-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
  transition: background var(--t-slow) var(--ease-standard);
}

/* 响铃时文字反白，与卡底同步 */
.home.ringing .clock-badge {
  background: rgba(255, 255, 255, 0.2);
  color: var(--on-primary);
}

.home.ringing .badge-dot {
  background: var(--on-primary);
}

/* ---- 表盘 ---- */
.clock-body {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
}

/* 头图 */
.mascot-slot {
  position: relative;
  display: grid;
  place-items: center;
}

/* ---- 读数 ---- */
.clock-readout {
  width: var(--clock-width);
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 12px;
}

/* 展示级数字 */
.clock-time {
  font-family: var(--font-display);
  font-size: var(--clock-size);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  letter-spacing: var(--clock-tracking);
  color: var(--ink);
  line-height: 1;
  text-align: center;
  transition: color var(--t-slow) var(--ease-standard);
}

/* 进度条 */
.clock-bar {
  position: relative;
  height: 8px;
  border-radius: var(--r-pill);
  background: rgba(60, 45, 25, 0.1);
  transition: background var(--t-slow) var(--ease-standard);
}

.clock-bar-fill {
  display: block;
  height: 100%;
  width: 0;
  border-radius: var(--r-pill);
  background: var(--accent);
  transition: background var(--t-slow) var(--ease-standard);
}

/* 响铃时 */
.home.ringing .clock-bar {
  background: rgba(255, 255, 255, 0.28);
}

.home.ringing .clock-bar-fill {
  background: var(--on-primary);
}

.home.ringing .clock-time {
  color: var(--on-primary);
}

.home.ringing .clock-card.actionable {
  cursor: pointer;
}

/* 统计条 */
.stats {
  box-sizing: border-box;
  display: flex;
  align-items: center;
  padding: 14px 8px;
  background: var(--surface-card);
  border-radius: var(--r-lg);
  box-shadow: var(--card-edge);
}

.stat {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
}

.stat-num {
  font-family: var(--font-display);
  font-size: 18px;
  font-weight: 600;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.3px;
}

.stat-label {
  font-size: 10px;
  font-weight: 600;
  color: var(--muted-soft);
  letter-spacing: 0.4px;
}

.stat-divider {
  width: 1px;
  height: 24px;
  background: var(--surface-strong);
}

/* ===== 主操作按钮 ===== */
.controls {
  display: flex;
  align-items: stretch;
  gap: 12px;
}

.btn {
  position: relative;
  flex: 1 1 0;
  min-width: 0;
  box-sizing: border-box;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: 52px;
  padding: 0 20px;
  font-size: 15px;
  font-weight: 600;
  font-family: inherit;
  color: var(--ink);
  background: transparent;
  border: 1px solid var(--hairline);
  border-radius: var(--r-xl);
  cursor: pointer;
  /* 立体感 */
  box-shadow: 0 3px 0 var(--btn-edge);
  transition:
    background var(--t-fast) var(--ease-standard),
    border-color var(--t-fast) var(--ease-standard),
    color var(--t-fast) var(--ease-standard),
    box-shadow var(--t-fast) var(--ease-standard),
    transform var(--t-fast) var(--ease-out);
}

.btn:active:not(:disabled) {
  transform: translateY(3px);
  box-shadow: 0 0 0 var(--btn-edge);
}

.btn:disabled {
  color: var(--muted-soft);
  background: transparent;
  border-color: var(--hairline-soft);
  box-shadow: 0 3px 0 var(--surface-soft);
  cursor: not-allowed;
}

/* 主操作按钮，三个动作共用同一套配色 */
.btn.primary {
  color: var(--ink);
  background: var(--surface-card);
  border-color: transparent;
  box-shadow: 0 3px 0 var(--btn-edge);
}

.btn.primary:hover:not(:disabled) {
  background: var(--surface-strong);
  transform: translateY(-1px);
  box-shadow: 0 4px 0 var(--btn-edge);
}

.btn.primary:active:not(:disabled) {
  background: var(--surface-strong);
  transform: translateY(3px);
  box-shadow: 0 0 0 var(--btn-edge);
}

/* 次要按钮，与主按钮同形，只降一档填充 */
.btn.secondary {
  color: var(--body);
  background: var(--canvas);
  border-color: var(--surface-strong);
  box-shadow: 0 3px 0 var(--btn-edge);
}

.btn.secondary:hover:not(:disabled) {
  color: var(--ink);
  background: var(--surface-soft);
  border-color: var(--surface-strong);
  transform: translateY(-1px);
  box-shadow: 0 4px 0 var(--btn-edge);
}

.btn.secondary:active:not(:disabled) {
  transform: translateY(3px);
  box-shadow: 0 0 0 var(--btn-edge);
}

/* 响铃脉动圆点 */
.pulse-dot {
  width: 8px;
  height: 8px;
  background: currentColor;
  border-radius: 50%;
  animation: pulse 1.1s ease-in-out infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 0.45;
    transform: scale(0.8);
  }
  50% {
    opacity: 1;
    transform: scale(1.2);
  }
}

/* 小尺寸按钮 */
.btn.tiny {
  height: 34px;
  padding: 0 14px;
  font-size: 13px;
  border-radius: var(--r-sm);
}

/* ===== 设置页 ===== */
.settingsHead {
  display: flex;
  align-items: center;
  gap: 14px;
  max-width: 760px;
  margin: 0 auto;
  width: 100%;
  box-sizing: border-box;
  padding: 20px 28px 14px;
}

/* 返回按钮 */
.back-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 38px;
  padding: 0 18px 0 14px;
  font-size: 14px;
  font-weight: 600;
  font-family: inherit;
  color: var(--body);
  background: var(--surface-card);
  border: none;
  border-radius: var(--r-pill);
  cursor: pointer;
  transition:
    background var(--t-fast) var(--ease-standard),
    color var(--t-fast) var(--ease-standard);
}

.back-btn:hover {
  color: var(--ink);
  background: var(--surface-strong);
}

.back-btn .arrow {
  font-size: 16px;
  line-height: 1;
  transition: transform var(--t-base) var(--ease-out);
}

.back-btn:hover .arrow {
  transform: translateX(-3px);
}

.settingsTitle {
  margin: 0;
  font-family: var(--font-display);
  font-size: 22px;
  font-weight: 500;
  letter-spacing: -0.6px;
  color: var(--ink);
}

.settingsContent {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  gap: 20px 24px;
  max-width: 760px;
  width: 100%;
  margin: 0 auto;
  box-sizing: border-box;
  padding: 8px 28px 40px;
}

@media (max-width: 820px) {
  .settingsContent {
    grid-template-columns: minmax(0, 1fr);
  }
}

/* 栏 */
.col {
  display: flex;
  flex-direction: column;
  gap: 18px;
  min-width: 0;
}

/* 栏内分组 */
.group {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 10px 20px;
  background: var(--surface-card);
  border-radius: var(--r-lg);
}

/* 时间设置组去掉分组底色，由内部阶段卡自带上色 */
.group.time-group {
  background: transparent;
  padding: 0;
  gap: 18px;
}

/* 分区标签 */
.group-title {
  margin: 0 0 4px;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 1.5px;
  text-transform: uppercase;
  color: var(--muted);
}

/* 固定高度插槽，兜住二选一的两块，避免卡片高度跳变 */
.field-slot {
  display: flex;
  align-items: center;
  min-height: 40px;
}

/* 分组下的说明文字 */
.group-note {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--muted);
}

/* ---- 恢复默认时长 ---- */
.reset-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--surface-strong);
}

.reset-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.reset-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--body);
}

.reset-hint {
  font-size: 11px;
  color: var(--muted-soft);
}

/* ---- 循环概览 ---- */

/* 循环占比条，两段色相与圆点一致 */
.ratio-bar {
  display: flex;
  height: 10px;
  border-radius: var(--r-pill);
  overflow: hidden;
  background: rgba(60, 45, 25, 0.08);
  gap: 2px;
}

.ratio-seg {
  display: block;
  height: 100%;
  transition: width var(--t-base) var(--ease-out);
}

.ratio-seg.sit {
  background: var(--theme);
  border-radius: var(--r-pill) 0 0 var(--r-pill);
}

.ratio-seg.stand {
  background: var(--theme-alt);
  border-radius: 0 var(--r-pill) var(--r-pill) 0;
}

.recap {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 每一步，由内容撑开 */
.recap-row {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 12px;
  background: var(--canvas);
  border-radius: var(--r-sm);
}

.recap-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}

/* 圆点按语义取色 */
.recap-dot.sit {
  background: var(--theme);
}

.recap-dot.stand {
  background: var(--theme-alt);
}

.recap-dot.loop {
  background: var(--primary);
}

.recap-key {
  flex: 1;
  font-size: 13px;
  font-weight: 600;
  color: var(--body);
}

.recap-val {
  font-family: var(--font-display);
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.3px;
}

/* 箭头缩进到与文字对齐 */
.recap-arrow {
  font-size: 11px;
  font-weight: 600;
  color: var(--muted-soft);
}

/* ---- 阶段功能卡 ---- */
.phase-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 20px;
  border-radius: var(--r-xl);
}

/* 阶段卡，归属由左侧色条与圆点承载 */
.sitting-card {
  --phase-accent: var(--theme);
  background: var(--theme-surface);
  color: var(--ink);
  border-left: 3px solid var(--phase-accent);
  box-shadow: var(--card-edge);
}

.standing-card {
  --phase-accent: var(--theme-alt);
  background: var(--theme-alt-surface);
  color: var(--ink);
  border-left: 3px solid var(--phase-accent);
  box-shadow: var(--card-edge);
}

/* 提示音卡，外形同阶段卡，左侧色条用墨色以区分配置与阶段 */
.sound-card {
  --phase-accent: var(--primary);
  background: var(--theme-surface);
  color: var(--ink);
  border-left: 3px solid var(--phase-accent);
  box-shadow: var(--card-edge);
}

/* 卡内文字降一档字重，与阶段卡的大标题区分 */
.sound-card .sound-name,
.sound-card .slider-field label,
.sound-card .inline-field {
  font-weight: 500;
}

/* 卡内小按钮同降一档 */
.sound-card .btn.tiny {
  font-weight: 500;
}

/* 分段控件选项同降一档 */
.sound-card :deep(.segmented button) {
  font-weight: 500;
}

.phase-card-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.phase-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--phase-accent, currentColor);
}

/* 卡标题 */
.phase-card-label {
  flex: 1;
  font-size: 15px;
  font-weight: 600;
}

.phase-card-value {
  font-family: var(--font-display);
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.5px;
  font-variant-numeric: tabular-nums;
}

/* 占比条 */
.phase-bar {
  height: 6px;
  border-radius: var(--r-pill);
  background: rgba(10, 10, 10, 0.1);
  overflow: hidden;
}

.phase-bar-fill {
  display: block;
  height: 100%;
  width: 100%;
  border-radius: var(--r-pill);
  background: var(--phase-accent, currentColor);
  transform-origin: left center;
}

/* 卡内输入控件，只需把边框调柔 */
.phase-card :deep(input) {
  border-color: transparent;
}

.phase-card :deep(.steppers button) {
  background: rgba(255, 255, 255, 0.55);
}

.phase-card :deep(.steppers button:hover) {
  background: var(--primary);
  color: var(--on-primary);
}

.inline-field {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 600;
  color: var(--body);
}

/* 文本输入框 */
.inline-field input {
  width: 50px;
  height: 24px;
  box-sizing: border-box;
  font-size: 14px;
  font-weight: 700;
  font-family: inherit;
  text-align: center;
  color: var(--ink);
  background: var(--canvas);
  border: 1px solid var(--hairline);
  border-radius: var(--r-md);
  outline: none;
  transition:
    border-color var(--t-fast) var(--ease-standard),
    box-shadow var(--t-fast) var(--ease-standard);
}

.inline-field input:focus {
  border-color: var(--primary);
  box-shadow: 0 0 0 3px rgba(10, 10, 10, 0.06);
}

/* 文件名行，独占一行 */
.sound-name-row {
  display: flex;
  min-width: 0;
}

.sound-name {
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--body);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 操作按钮行，四个按钮等分整行宽度 */
.sound-actions {
  display: flex;
  gap: 6px;
}

.sound-actions .btn.tiny {
  flex: 1 1 0;
  min-width: 0;
  padding: 0 6px;
}

/* 音量滑块 */
.slider-field {
  display: flex;
  margin-top: 10px;
  align-items: center;
  gap: 12px;
}

.slider-field label {
  min-width: 46px;
  font-size: 13px;
  font-weight: 600;
  color: var(--body);
}

input[type='range'] {
  flex: 1;
  height: 6px;
  appearance: none;
  background: var(--surface-strong);
  border-radius: var(--r-pill);
  outline: none;
}

input[type='range']::-webkit-slider-thumb {
  appearance: none;
  width: 20px;
  height: 20px;
  background: var(--primary);
  border: 3px solid var(--canvas);
  border-radius: 50%;
  cursor: pointer;
  transition: background var(--t-fast) var(--ease-standard);
}

/* 滑块的 hover 态用主题色，与按钮的第二态保持同一套语言 */
input[type='range']::-webkit-slider-thumb:hover {
  background: var(--theme);
}

.vol-num {
  min-width: 40px;
  text-align: right;
  font-size: 13px;
  font-weight: 700;
  color: var(--ink);
  font-variant-numeric: tabular-nums;
}

/* 开关 */
.switch-field {
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  user-select: none;
}

.switch-field input {
  position: absolute;
  opacity: 0;
  pointer-events: none;
}

.track {
  position: relative;
  width: 48px;
  height: 28px;
  background: var(--surface-strong);
  border-radius: var(--r-pill);
  transition: background var(--t-base) var(--ease-standard);
}

.knob {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 22px;
  height: 22px;
  background: var(--canvas);
  border-radius: 50%;
  box-shadow: var(--shadow-soft);
  transition: transform var(--t-base) var(--ease-out);
}

/* 开启态用主题色——开关是"已启用"，与主按钮的第二态同色 */
.switch-field input:checked + .track {
  background: var(--theme);
}

.switch-field input:checked + .track .knob {
  transform: translateX(20px);
}

.switch-field input:focus-visible + .track {
  box-shadow: 0 0 0 3px rgba(10, 10, 10, 0.08);
}

.switch-text {
  font-size: 13px;
  font-weight: 600;
  color: var(--body);
}

/* 保存提示：墨色 pill，避免用糖果色打扰 */
.toast {
  position: fixed;
  bottom: 24px;
  left: 50%;
  z-index: 60;
  padding: 10px 22px;
  font-size: 13px;
  font-weight: 600;
  color: var(--on-primary);
  background: var(--primary);
  border-radius: var(--r-pill);
  box-shadow: var(--shadow-card);
}
</style>
