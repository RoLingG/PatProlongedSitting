/**
 * 音频播放引擎
 *
 * 基于 Web Audio API 而非 audio 标签，以获得精确的音量控制、
 * 无缝循环和更干净的停止。
 */

export type PlayMode = 'times' | 'loop'

export interface PlayOptions {
  /** 音频数据；为空时使用内置合成提示音 */
  data?: ArrayBuffer | null
  /** 音量 0~1 */
  volume: number
  /** 播放模式 */
  mode: PlayMode
  /** mode 为 times 时的播放次数 */
  repeatTimes: number
  /** 每次播放结束后的间隔（秒），避免连续急促 */
  gapSeconds?: number
}

/** 内置默认提示音：一段柔和的「叮—咚」双音 */
function synthesizeDefaultBuffer(ctx: AudioContext): AudioBuffer {
  const duration = 0.9
  const rate = ctx.sampleRate
  const length = Math.floor(duration * rate)
  const buffer = ctx.createBuffer(1, length, rate)
  const ch = buffer.getChannelData(0)

  // 两个正弦音叠加，带指数衰减，模拟柔和提示音
  const tones = [
    { freq: 880, start: 0.0, dur: 0.35, gain: 0.5 },
    { freq: 660, start: 0.32, dur: 0.5, gain: 0.45 },
  ]

  for (const t of tones) {
    const s0 = Math.floor(t.start * rate)
    const s1 = Math.min(length, Math.floor((t.start + t.dur) * rate))
    for (let i = s0; i < s1; i++) {
      const local = (i - s0) / rate
      const env = Math.exp(-local * 7)
      ch[i] += Math.sin(2 * Math.PI * t.freq * local) * env * t.gain
    }
  }

  // 防止削波
  let peak = 0
  for (let i = 0; i < length; i++) peak = Math.max(peak, Math.abs(ch[i]))
  if (peak > 0.99) {
    const k = 0.99 / peak
    for (let i = 0; i < length; i++) ch[i] *= k
  }

  return buffer
}

export class AudioPlayer {
  private ctx: AudioContext | null = null
  private gain: GainNode | null = null

  /** 当前正在播放的 source，用于停止 */
  private active: AudioBufferSourceNode | null = null
  /** 已解码的音频缓冲，按数据缓存 */
  private buffer: AudioBuffer | null = null
  private bufferSourceData: ArrayBuffer | null = null

  /** 递增的播放代次，用于取消旧的播放计划 */
  private generation = 0
  private timer: number | null = null

  // AudioBufferSourceNode 一旦 start 就只能 stop，没有暂停，也没有
  // 当前播放位置属性，因此位置需自行累计：playedSec 记已播秒数，
  // startedAt 记本次续播的起点。pausedAt 非 null 时其值即为续播起点
  private pausedAt: number | null = null
  private playedSec = 0
  private startedAt = 0

  /** 续播器，由 playOnce 注册，resume 调用它从断点重新开始这一遍播放 */
  private resumer: ((offset: number) => void) | null = null

  private ensureContext(): AudioContext {
    if (!this.ctx) {
      const Ctor = window.AudioContext || (window as any).webkitAudioContext
      this.ctx = new Ctor()
      this.gain = this.ctx.createGain()
      this.gain.connect(this.ctx.destination)
    }
    if (this.ctx.state === 'suspended') {
      void this.ctx.resume()
    }
    return this.ctx
  }

  /** 将 ArrayBuffer 解码为 AudioBuffer，带缓存 */
  private async resolveBuffer(data?: ArrayBuffer | null): Promise<AudioBuffer> {
    const ctx = this.ensureContext()

    if (!data) {
      if (!this.buffer || this.bufferSourceData !== null) {
        this.buffer = synthesizeDefaultBuffer(ctx)
        this.bufferSourceData = null
      }
      return this.buffer
    }

    if (this.buffer && this.bufferSourceData === data) {
      return this.buffer
    }

    // decodeAudioData 会「转移」传入的 ArrayBuffer，必须传副本
    const copy = data.slice(0)
    const decoded = await ctx.decodeAudioData(copy)
    this.buffer = decoded
    this.bufferSourceData = data
    return decoded
  }

  /** 开始播放。返回的 Promise 在播放结束或被停止后 resolve */
  async play(opts: PlayOptions): Promise<void> {
    this.stop()

    const ctx = this.ensureContext()
    const buffer = await this.resolveBuffer(opts.data)

    if (!this.gain) return
    this.gain.gain.value = Math.max(0, Math.min(1, opts.volume))

    const myGen = ++this.generation
    const gap = (opts.gapSeconds ?? 0.4) * 1000
    // 新一轮播放：位置归零，且不在暂停态
    this.pausedAt = null
    this.playedSec = 0

    // 单次播放。暂停时不 resolve，只有真正播完才 resolve；
    // 续播由 resumer 重新 start 并挂 onended，故外层 Promise 始终有效
    const playOnce = (fromSec = 0): Promise<void> => {
      return new Promise<void>((resolve) => {
        if (myGen !== this.generation) {
          resolve()
          return
        }

        const startSource = (offset: number) => {
          const src = ctx.createBufferSource()
          src.buffer = buffer
          src.connect(this.gain!)
          src.onended = () => {
            // 被 stop() 打断时不 resolve，让外层通过 generation 判断退出
            if (myGen !== this.generation) {
              resolve()
              return
            }
            // 暂停中不 resolve —— 等 resume 之后继续
            if (this.pausedAt !== null) return
            resolve()
          }
          this.active = src
          this.startedAt = ctx.currentTime
          const clamped = Math.min(offset, Math.max(0, buffer.duration - 0.01))
          // 本次续播从第几秒开始，就把它记为当前的累计位置
          this.playedSec = clamped
          src.start(0, clamped)
        }

        // 记录续播方式，供 resume() 调用
        this.resumer = startSource
        startSource(fromSec)
      })
    }

    const sleep = (ms: number): Promise<void> => {
      return new Promise<void>((resolve) => {
        this.timer = window.setTimeout(() => {
          this.timer = null
          resolve()
        }, ms)
      })
    }

    if (opts.mode === 'loop') {
      // 循环模式：不断播放，直到 stop() 被调用
      while (myGen === this.generation) {
        await playOnce()
        if (myGen !== this.generation) break
        await sleep(gap)
      }
      return
    }

    // 次数模式：播放固定次数
    const times = Math.max(1, Math.floor(opts.repeatTimes))
    for (let i = 0; i < times; i++) {
      if (myGen !== this.generation) break
      await playOnce()
      if (myGen !== this.generation) break
      if (i < times - 1) {
        await sleep(gap)
      }
    }

    if (myGen === this.generation) {
      // 播放自然结束，通知外部
      this.onFinished?.()
    }
  }

  /** 试听：固定播放一次 */
  async preview(data: ArrayBuffer | null, volume: number): Promise<void> {
    await this.play({ data, volume, mode: 'times', repeatTimes: 1 })
  }

  /**
   * 暂停播放，把已播时长结算进 playedSec 并停掉 source
   *
   * @returns 是否真的暂停。音频已播完时返回 false，
   *          调用方须据此决定要不要切到"暂停中"状态
   */
  pause(): boolean {
    if (!this.ctx || !this.active || this.pausedAt !== null) return false

    // 结算：起点 + 这一段已经播了多久
    this.playedSec += this.ctx.currentTime - this.startedAt
    // 同一个值同时充当"续播起点"与"暂停标记"
    this.pausedAt = this.playedSec

    // 停掉 source；generation 不动，否则 resume 会被当成新一轮播放
    try {
      this.active.onended = null
      this.active.stop()
    } catch {
      // 已停止的 source 再次 stop 会抛错，忽略
    }
    this.active = null

    // 清掉循环间隙里的等待定时器，避免它在暂停期间又触发下一轮
    if (this.timer !== null) {
      window.clearTimeout(this.timer)
      this.timer = null
    }
    return true
  }

  /**
   * 从暂停处继续播放，复用 playOnce 注册的续播器起新 source
   *
   * @returns 是否真的恢复了播放
   */
  resume(): boolean {
    if (this.pausedAt === null || !this.resumer) return false

    const offset = this.pausedAt
    // 清掉暂停标记；playedSec 保持为这个位置，由 startSource 接管
    this.pausedAt = null

    // 续播器会重新挂 onended；此时 pausedAt 已清空，
    // 所以播完时 onended 里的判断会放行并 resolve
    this.resumer(offset)
    return true
  }

  /** 停止当前播放并取消后续计划 */
  stop(): void {
    this.generation++
    this.pausedAt = null
    this.playedSec = 0
    this.resumer = null
    if (this.timer !== null) {
      window.clearTimeout(this.timer)
      this.timer = null
    }
    if (this.active) {
      try {
        this.active.onended = null
        this.active.stop()
      } catch {
        // 已停止的 source 再次 stop 会抛错，忽略
      }
      this.active = null
    }
  }

  /** 实时调整音量（播放中亦可） */
  setVolume(v: number): void {
    if (this.gain) {
      this.gain.gain.value = Math.max(0, Math.min(1, v))
    }
  }

  /** 播放自然结束时的回调（手动停止不会触发） */
  onFinished: (() => void) | null = null

  /** 释放资源 */
  dispose(): void {
    this.stop()
    if (this.ctx) {
      void this.ctx.close()
      this.ctx = null
    }
    this.gain = null
    this.buffer = null
    this.bufferSourceData = null
  }
}
