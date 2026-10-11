/**
 * 类型定义与工具函数
 *
 * 数据结构以后端生成的 wailsjs/go/models.ts 为准，
 * 这里只做窄化和补充前端专有类型，避免手写定义与后端漂移。
 */
import { main } from '../../wailsjs/go/models'

export type Config = main.ConfigData
export type TimerState = main.TimerState

/** 计时阶段，对后端 string 的窄化 */
export type Phase = 'sitting' | 'standing' | 'idle'
/** 单次提醒的响铃模式 */
export type RemindMode = 'times' | 'loop'
/** 整体提醒的结束模式 */
export type EndMode = 'forever' | 'count'

/** 后端 timer:phaseEnd 事件推送的载荷 */
export interface PhaseEndPayload {
  ended: 'sitting' | 'standing'
  next: string
  title: string
  message: string
  soundPath: string
  volume: number
  mode: string
  repeatTimes: number
  state: TimerState
}

/** 将后端 phase 字符串窄化为 Phase */
export function asPhase(v: string): Phase {
  if (v === 'sitting' || v === 'standing') return v
  return 'idle'
}

/** 构造默认配置，用于前端在加载完成前的占位 */
export function defaultConfig(): Config {
  return main.ConfigData.createFrom({
    sitSeconds: 45 * 60,
    standSeconds: 5 * 60,
    soundPath: '',
    volume: 0.7,
    mode: 'loop',
    repeatTimes: 3,
    endMode: 'forever',
    remindCount: 10,
    autoStart: false,
  })
}

/** 把秒数格式化为 mm:ss（超过一小时则 h:mm:ss） */
export function formatClock(totalSec: number): string {
  const s = Math.max(0, Math.floor(totalSec))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) {
    return `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
  }
  return `${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}

/**
 * 把秒数转成人类可读的紧凑时长，用于设置摘要展示。
 * 例：5400 → "1.5h"，1500 → "25m"，90 → "1m30s"
 */
export function humanDuration(totalSec: number): string {
  const s = Math.max(0, Math.floor(totalSec))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60

  if (h > 0 && m === 0 && sec === 0) return `${h}h`
  if (h > 0 && sec === 0) return `${h}h${m}m`
  if (h > 0) return `${h}h${m}m`
  if (m > 0 && sec === 0) return `${m}m`
  if (m > 0) return `${m}m${sec}s`
  return `${sec}s`
}

/** 分段控件的单个选项 */
export interface SegmentOption {
  value: string
  label: string
}
