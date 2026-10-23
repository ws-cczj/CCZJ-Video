import { afterEach, describe, expect, it, vi } from 'vitest'
import { interceptMethod } from '../src/plugins/interceptors'
import { PluginWorkCancelled, waitForPluginWork } from '../src/plugins/pendingWork'
afterEach(() => vi.useRealTimers())
describe('plugin interception and pending lifecycle', () => {
  it('removes a middle interceptor while preserving this and arguments', () => {
    const target = { value: 2, fn(multiplier: number) { return this.value * multiplier }, other: () => 4 }
    const original = target.fn
    const a = interceptMethod(target, 'fn', (next, ...args) => Number(next(...args)) + 10)
    const b = interceptMethod(target, 'fn', (next, ...args) => Number(next(...args)) * 3)
    const c = interceptMethod(target, 'fn', (next, ...args) => Number(next(...args)) + 1)
    expect(target.fn(5)).toBe(61)
    b()
    expect(target.fn(5)).toBe(21)
    a()
    expect(target.fn(5)).toBe(11)
    c()
    expect(target.fn).toBe(original)
  })
  it('does not overwrite a method replaced independently of the plugin API', () => {
    const target = { fn: () => 1 }
    const undo = interceptMethod(target, 'fn', next => Number(next()) + 1)
    target.fn = () => 3
    undo()
    expect(target.fn()).toBe(3)
  })
  it('keeps interception on other methods while rebuilding a finished chain', () => {
    const target = { a: () => 1, b: () => 2 }
    const a = interceptMethod(target, 'a', next => Number(next()) + 1)
    const b = interceptMethod(target, 'b', next => Number(next()) + 1)
    a()
    const nextB = interceptMethod(target, 'b', next => Number(next()) + 1)
    b()
    expect(target.b()).toBe(3)
    nextB()
    expect(target.b()).toBe(2)
  })
  it('cancels a hung setup and handles its eventual rejection', async () => {
    vi.useFakeTimers()
    const controller = new AbortController()
    let reject!: (error: Error) => void
    const setup = new Promise<void>((_resolve, no) => { reject = no })
    const pending = waitForPluginWork(setup, controller.signal)
    controller.abort()
    await expect(pending).rejects.toBeInstanceOf(PluginWorkCancelled)
    reject(new Error('late setup error'))
    await Promise.resolve()
    expect(vi.getTimerCount()).toBe(0)
  })
  it('bounds setup waiting and clears the timeout on success', async () => {
    vi.useFakeTimers()
    const pending = waitForPluginWork(new Promise<void>(() => {}), new AbortController().signal)
    const rejected = expect(pending).rejects.toThrow('30 seconds')
    await vi.advanceTimersByTimeAsync(30000)
    await rejected
    expect(await waitForPluginWork(Promise.resolve(42), new AbortController().signal)).toBe(42)
    expect(vi.getTimerCount()).toBe(0)
  })
})
