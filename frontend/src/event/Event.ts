/**
 * 轻量级事件发射器
 * =================
 * 参考 lx-music-desktop 的 Event 基类设计，从零实现一个
 * 类型安全、零依赖的事件发射器，替代 mitt / EventEmitter。
 *
 * 特性:
 *  - on / off / emit / once / offAll
 *  - 泛型支持：子类可传入事件映射类型获得完整类型推导
 *  - 单次监听 (once)
 *  - 按事件名移除全部监听 (offAll)
 */

type Listener = (...args: any[]) => any

/** 事件映射：key = 事件名, value = 回调签名 */
export type EventMap = Record<string, (...args: any[]) => any>

/** 默认事件映射（任意事件名，任意参数） */
type AnyEventMap = Record<string, Listener>

export class Event<E extends EventMap = AnyEventMap> {
  private _listeners = new Map<keyof E, Listener[]>()

  /** 注册监听 */
  on<K extends keyof E>(eventName: K, listener: E[K]): this {
    const list = this._listeners.get(eventName)
    if (list) {
      list.push(listener as Listener)
    } else {
      this._listeners.set(eventName, [listener as Listener])
    }
    return this
  }

  /** 移除指定监听 */
  off<K extends keyof E>(eventName: K, listener: E[K]): this {
    const list = this._listeners.get(eventName)
    if (list) {
      const idx = list.indexOf(listener as Listener)
      if (idx !== -1) list.splice(idx, 1)
      if (list.length === 0) this._listeners.delete(eventName)
    }
    return this
  }

  /** 单次监听：触发后自动移除 */
  once<K extends keyof E>(eventName: K, listener: E[K]): this {
    const wrapper = ((...args: any[]) => {
      this.off(eventName, wrapper as any)
      return (listener as Listener)(...args)
    }) as Listener
    // 在 wrapper 上挂原始引用，方便 off 按原 listener 查找
    ;(wrapper as any)._original = listener
    return this.on(eventName, wrapper as any)
  }

  /** 触发事件 */
  emit<K extends keyof E>(eventName: K, ...args: Parameters<E[K]>): void {
    const list = this._listeners.get(eventName)
    if (!list) return
    // 拷贝一份再遍历，避免 listener 内部 off 导致迭代异常
    const snapshot = list.slice()
    for (const fn of snapshot) {
      try {
        fn(...args)
      } catch (err) {
        console.error(`[Event] error in listener for "${String(eventName)}":`, err)
      }
    }
  }

  /** 移除指定事件的全部监听 */
  offAll<K extends keyof E>(eventName: K): this {
    this._listeners.delete(eventName)
    return this
  }

  /** 移除所有事件的全部监听 */
  removeAllListeners(): this {
    this._listeners.clear()
    return this
  }

  /** 获取指定事件的监听数量 */
  listenerCount<K extends keyof E>(eventName: K): number {
    return this._listeners.get(eventName)?.length ?? 0
  }
}
