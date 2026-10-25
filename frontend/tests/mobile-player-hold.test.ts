import { afterEach, beforeEach, expect, it, vi } from 'vitest'
const hooks = vi.hoisted(() => ({ mounted: [] as Array<() => void>, dispose: [] as Array<() => void> }))
vi.mock('vue', async original => ({ ...await original<typeof import('vue')>(), onMounted: (fn: () => void) => hooks.mounted.push(fn), onBeforeUnmount: (fn: () => void) => hooks.dispose.push(fn) }))
import { useMobilePlayerGestures } from '../src/mobile/useMobilePlayerGestures'

beforeEach(() => { vi.useFakeTimers(); vi.stubGlobal('window', new EventTarget()); vi.stubGlobal('document', Object.assign(new EventTarget(), { hidden: false })) })
afterEach(() => { hooks.dispose.splice(0).forEach(fn => fn()); hooks.mounted.length = 0; vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

function fixture(volumeSupported = true, portrait = false) {
 const video = Object.assign(new EventTarget(), { readyState: 4, currentTime: 12, duration: 120, playbackRate: 1.25, paused: false, ended: false, volume: .5 }) as HTMLVideoElement
 const root = { getBoundingClientRect: () => ({ left: 0, width: 400, height: 225 }), setPointerCapture: vi.fn() }
 const onSwipe = vi.fn()
 const gestures = useMobilePlayerGestures({ enabled: true, interaction: portrait ? 'portrait' : undefined, onSwipe, video: () => video, visible: vi.fn(), onVolume: vi.fn(), device: { volumeSupported, read: () => undefined, setVolume: vi.fn(), restoreBrightness: vi.fn() } })
 const event = (type: string, x = 200) => ({ type, isPrimary: true, button: 0, pointerId: 1, pointerType: 'touch', clientX: x, clientY: 100, currentTarget: root, target: { closest: () => null }, preventDefault: vi.fn() }) as unknown as PointerEvent
 hooks.mounted.splice(0).forEach(fn => fn())
 return { video, gestures, event, onSwipe }
}

it('portrait swipes switch titles without seeking or changing volume, and cancellation never switches', () => {
 const { video, gestures, event, onSwipe } = fixture(true, true)
 const move = (dy: number) => { const e = event('pointermove'); Object.defineProperty(e, 'clientY', {value: 100 + dy}); gestures.move(e) }
 gestures.down(event('pointerdown')); move(-80); vi.advanceTimersByTime(700); gestures.end(event('pointerup'))
 expect(onSwipe).toHaveBeenCalledWith('next')
 expect(video.currentTime).toBe(12); expect(video.volume).toBe(.5); expect(video.playbackRate).toBe(1.25)
 onSwipe.mockClear()
 gestures.down(event('pointerdown')); move(80); gestures.end(event('pointercancel'))
 expect(onSwipe).not.toHaveBeenCalled()
 gestures.down(event('pointerdown')); gestures.move(event('pointermove', 300)); gestures.end(event('pointerup'))
 expect(video.currentTime).toBe(12); expect(onSwipe).not.toHaveBeenCalled()
 gestures.down(event('pointerdown')); vi.advanceTimersByTime(700)
 expect(video.playbackRate).toBe(2)
 gestures.end(event('pointerup')); expect(video.playbackRate).toBe(1.25)
})

it('does not display a fake volume change when the host requires system volume controls', () => {
 const { video, gestures, event } = fixture(false)
 gestures.down(event('pointerdown', 350))
 const swipe = event('pointermove', 350)
 Object.defineProperty(swipe, 'clientY', { value: 40 })
 gestures.move(swipe)
 expect(gestures.kind.value).toBe('')
 expect(gestures.hud.value).toBe('')
 expect(video.volume).toBe(.5)
})

it('temporarily accelerates a stationary hold, retains its HUD and restores the original rate on release', () => {
 const { video, gestures, event } = fixture()
 const down = event('pointerdown')
 gestures.down(down)
 Object.defineProperty(down, 'currentTarget', { value: null })
 vi.advanceTimersByTime(700)
 expect(video.playbackRate).toBeGreaterThan(1.25)
 expect(gestures.kind.value).toBe('speed')
 expect(gestures.hud.value).toContain(String(video.playbackRate))
 vi.advanceTimersByTime(3000)
 expect(gestures.kind.value).toBe('speed')
 gestures.end(event('pointerup'))
 expect(video.playbackRate).toBe(1.25)
 expect(gestures.kind.value).toBe('')
 expect(gestures.consumeClick()).toBe(true)
})

it.each(['blur', 'pagehide', 'pause', 'ended', 'emptied', 'visibilitychange', 'dispose', 'lostpointercapture', 'lock'])('restores a held rate on %s', cause => {
 const { video, gestures, event } = fixture()
 gestures.down(event('pointerdown')); vi.advanceTimersByTime(700)
 expect(video.playbackRate).toBe(2)
 if (cause === 'dispose') hooks.dispose.splice(0).forEach(fn => fn())
 else if (cause === 'lock') gestures.toggleLock()
 else if (cause === 'lostpointercapture') gestures.end(event(cause))
 else if (cause === 'visibilitychange') { Object.defineProperty(document,'hidden',{value:true}); document.dispatchEvent(new Event(cause)) }
 else (['pause','ended','emptied'].includes(cause) ? video : window).dispatchEvent(new Event(cause))
 expect(video.playbackRate).toBe(1.25)
 expect(gestures.kind.value).toBe('')
 expect(vi.getTimerCount()).toBe(0)
})

it('does not accelerate controls, a paused video or a locked player', () => {
 const { video, gestures, event } = fixture()
 const control = event('pointerdown'); Object.defineProperty(control,'target',{value:{closest:()=>({})}})
 gestures.down(control); vi.advanceTimersByTime(700); expect(video.playbackRate).toBe(1.25)
 video.paused = true; gestures.down(event('pointerdown')); vi.advanceTimersByTime(700); expect(video.playbackRate).toBe(1.25)
 video.paused = false; gestures.toggleLock(); gestures.down(event('pointerdown')); vi.advanceTimersByTime(700); expect(video.playbackRate).toBe(1.25)
})

it('never boosts a short tap or a seek swipe and restores on cancellation', () => {
 const { video, gestures, event } = fixture()
 gestures.down(event('pointerdown')); vi.advanceTimersByTime(100); gestures.end(event('pointerup')); vi.advanceTimersByTime(800)
 expect(video.playbackRate).toBe(1.25)
 expect(gestures.consumeClick()).toBe(false)
 gestures.down(event('pointerdown')); gestures.move(event('pointermove', 260)); vi.advanceTimersByTime(800)
 expect(video.playbackRate).toBe(1.25)
 gestures.end(event('pointercancel'))
 expect(video.currentTime).toBe(12)
 gestures.down(event('pointerdown')); vi.advanceTimersByTime(700); gestures.end(event('pointercancel'))
 expect(video.playbackRate).toBe(1.25)
})
