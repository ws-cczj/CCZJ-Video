/**
 * 事件系统入口
 * ============
 * 创建全局事件总线单例，挂载到 window.app_event 供全局访问。
 *
 * 使用方式:
 *   import { appEvent } from '@/event'
 *   // 或者
 *   window.app_event.on('player:play', () => { ... })
 *   window.app_event.emit('player:play')
 *
 * Wails 后端事件桥接:
 *   Wails 的 Events.On() 仍用于 Go ↔ JS 通信；
 *   appEvent 用于前端内部跨组件/跨模块通信。
 *   stores 中可将 Wails 事件桥接到 appEvent（见 collect.ts / download.ts）。
 */

import { AppEvent, createAppEventHub } from './appEvent'

/** 全局应用事件总线（单例） */
export const appEvent: AppEvent = createAppEventHub()

/** 初始化：挂载到 window 全局对象 */
export function registerEvents(): void {
  const w = window as any
  w.app_event = appEvent
}

// 模块加载时自动注册
registerEvents()

// 导出类型与工厂，供需要扩展时使用
export { AppEvent, createAppEventHub } from './appEvent'
export { Event } from './Event'
export type { AppEventMap } from './appEvent'
export type { EventMap } from './Event'
