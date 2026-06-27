/**
 * 应用级事件定义
 * ===============
 * 集中声明前端所有跨组件 / 跨模块的事件类型，
 * 通过 createAppEventHub() 创建全局单例。
 *
 * 事件命名约定: 模块:动作 (如 collect:start, player:play)
 */

import { Event, type EventMap } from './Event'

/* ---------- 事件类型映射 ---------- */
export interface AppEventMap extends EventMap {
  // ===== 应用生命周期 =====
  'app:ready': () => void
  'app:settings-changed': (key: string, value: string) => void

  // ===== 采集模块 =====
  'collect:start': (sourceKey: string, mode: string) => void
  'collect:done': (sourceKey: string, error?: string) => void
  'collect:progress': (sourceKey: string, current: number, total: number) => void
  'collect:log': (sourceKey: string, message: string) => void

  // ===== 下载模块 =====
  'download:start': (taskId: string, url: string) => void
  'download:done': (taskId: string) => void
  'download:error': (taskId: string, error: string) => void
  'download:progress': (taskId: string, downloaded: number, total: number) => void

  // ===== 播放器 =====
  'player:play': () => void
  'player:pause': () => void
  'player:stop': () => void
  'player:ended': () => void
  'player:error': (message: string) => void
  'player:timeupdate': (currentTime: number, duration: number) => void
  'player:fullscreen': (isFullscreen: boolean) => void
  'player:volume': (volume: number) => void
  'player:rate': (rate: number) => void
  'player:episode-change': (direction: 'prev' | 'next') => void

  // ===== 收藏 =====
  'favorite:toggled': (vodId: string, isFavorite: boolean) => void
  'favorite:sync': () => void

  // ===== 主题 =====
  'theme:changed': (themeName: string) => void

  // ===== 路由 / 导航 =====
  'nav:scroll-top': () => void
  'nav:back': () => void

  // ===== 全局 UI =====
  'ui:error': (title: string, message: string) => void
  'ui:confirm': (message: string, callback: (ok: boolean) => void) => void
  'ui:sidebar-toggle': () => void
}

/* ---------- AppEvent 子类 ---------- */
export class AppEvent extends Event<AppEventMap> {
  // 语义化的 emit 便捷方法（可选使用，也可直接用 emit）

  /** 通知设置变更 */
  settingsChanged(key: string, value: string): void {
    this.emit('app:settings-changed', key, value)
  }

  /** 通知采集完成 */
  collectDone(sourceKey: string, error?: string): void {
    this.emit('collect:done', sourceKey, error)
  }

  /** 通知播放器错误 */
  playerError(message: string): void {
    this.emit('player:error', message)
  }

  /** 通知收藏变更 */
  favoriteToggled(vodId: string, isFavorite: boolean): void {
    this.emit('favorite:toggled', vodId, isFavorite)
  }

  /** 通知主题变更 */
  themeChanged(themeName: string): void {
    this.emit('theme:changed', themeName)
  }

  /** 推送全局错误提示 */
  uiError(title: string, message: string): void {
    this.emit('ui:error', title, message)
  }
}

/* ---------- 类型约束导出 ---------- */

/**
 * 仅暴露 on/off/once/emit 等业务方法，隐藏内部实现
 * 使用方式: const hub: AppEventTypes = createAppEventHub()
 */
type EventMethods = Omit<AppEvent, keyof Event>
declare class AppEventType extends AppEvent {
  on<K extends keyof AppEventMap>(event: K, listener: AppEventMap[K]): this
  once<K extends keyof AppEventMap>(event: K, listener: AppEventMap[K]): this
  off<K extends keyof AppEventMap>(event: K, listener: AppEventMap[K]): this
}
export type AppEventTypes = Omit<AppEventType, 'emit'> & {
  emit<K extends keyof AppEventMap>(event: K, ...args: Parameters<AppEventMap[K]>): void
}

/** 工厂函数 */
export function createAppEventHub(): AppEvent {
  return new AppEvent()
}
