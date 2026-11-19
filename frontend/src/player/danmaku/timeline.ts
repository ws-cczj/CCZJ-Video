import { danmakuFontSize } from './textStyle'
import type { DanmakuItem } from '../../api/danmaku'
import type { DanmakuPreferences } from './preferences'
export interface DanmakuDraw { item: DanmakuItem; x: number; y: number; width: number; fontSize: number }
interface Active { item: DanmakuItem; born: number; lane: number; width: number; fontSize: number; pausedFor: number }
export function lowerBound(items: DanmakuItem[], time: number): number { let lo = 0, hi = items.length; while (lo < hi) { const mid = (lo + hi) >>> 1; if (items[mid].timeMs < time) lo = mid + 1; else hi = mid } return lo }
/** Playback time is the only animation clock: pause, buffering and rate changes
 * need no independent CSS animation, timer or speed multiplier. */
export class DanmakuTimeline {
  private items: DanmakuItem[] = []
  private active: Active[] = []
  private cursor = 0
  private last = -Infinity
  private offset = 0
  private layout = ''
  private held: { row: Active; since: number } | undefined
  constructor(private measure: (text: string, size: number) => number) { }
  setItems(items: DanmakuItem[], offset: number) {
    this.items = [...items].sort((a, b) => a.timeMs - b.timeMs)
    if (offset !== this.offset) this.resetPosition()
    this.offset = offset
    // Replacing the look-ahead window must not clear comments already on screen
    // or replay the current timestamp. A seek/layout change is handled in tick.
    this.cursor = Number.isFinite(this.last) ? lowerBound(this.items, Math.floor(this.last - this.offset) + 1) : 0
  }
  invalidateLayout() { this.layout = '' }
  resetPosition() { this.active = []; this.held = undefined; this.last = -Infinity }
  clear() { this.items = []; this.cursor = 0; this.resetPosition() }
  hold(item?: DanmakuItem): void {
    if (this.held?.row.item === item) return
    if (this.held) this.held.row.pausedFor += this.last - this.held.since
    const row = item && this.active.find(candidate => candidate.item === item)
    this.held = row ? { row, since: this.last } : undefined
  }
  tick(time: number, width: number, height: number, p: DanmakuPreferences): DanmakuDraw[] {
    const font = danmakuFontSize(width,p.fontSize)
    const rowHeight = Math.ceil(font * 1.35 + (p.textStyle === 'stroke' ? p.strokeWidth : 0))
    const top = 12, bottom = Math.min(height * p.area, height - 48) - 8
    const rows = Math.max(0, Math.min(p.rows, Math.floor((bottom - top) / rowHeight)))
    // A viewport-relative shared velocity keeps equal-speed collision checks valid.
    const velocity = Math.max(1, width / 8) * p.speed / 100
    const yOf = (mode: number, lane: number) => mode === 4 ? bottom - 4 - lane * rowHeight : top + font + lane * rowHeight
    const layout = `${width}:${height}:${font}:${p.area}:${rows}:${p.speed}:${p.fixed}:${p.scrolling}:${p.colored}:${p.fontFamily}:${p.fontWeight}:${p.textStyle}:${p.strokeWidth}:${p.density}`
    if (!p.enabled) { this.resetPosition(); return [] }
    if (!rows || !width || !height) return []
    if (!Number.isFinite(this.last) || time < this.last || (layout === this.layout && time - this.last > 1000)) {
      this.resetPosition()
      this.cursor = lowerBound(this.items, time - this.offset - 200)
    }
    this.last = time
    const age = (a: Active) => (this.held?.row === a ? this.held.since : time) - a.born - a.pausedFor
    const xOf = (a: Active) => a.item.mode === 1 ? width - age(a) * velocity / 1000 : (width - a.width) / 2
    const shows = (item: DanmakuItem) => (item.mode === 1 ? p.scrolling : p.fixed && (item.mode === 4 || item.mode === 5)) && (p.colored || item.color === 0xffffff)
    const maxActive = Math.max(1, Math.floor(120 * p.density))
    this.active = this.active.filter(a => a.item.mode === 1 ? xOf(a) + a.width > 0 : age(a) < 4500)
    if (layout !== this.layout) {
      // Retain comments across reflow, using their original media timestamps.
      const retained: Active[] = []
      for (const row of this.active) {
        if (row.lane >= rows || !shows(row.item) || retained.length >= maxActive) continue
        row.fontSize = font
        row.width = Math.min(12000, this.measure(row.item.text, font))
        const x = xOf(row), y = yOf(row.item.mode, row.lane)
        if (x + row.width <= 0 || retained.some(other => Math.abs(y - yOf(other.item.mode, other.lane)) < rowHeight && x < xOf(other) + other.width && xOf(other) < x + row.width)) continue
        retained.push(row)
      }
      this.active = retained
      this.layout = layout
    }
    const held = this.held?.row
    if (held) {
      if (!this.active.includes(held)) this.held = undefined
      else {
        // Existing followers can catch up to a held comment. Retire the ones
        // that reach it so their text cannot overlap the interaction target.
        const left = xOf(held), right = left + held.width
        const y = yOf(held.item.mode, held.lane)
        this.active = this.active.filter(row => row === held || Math.abs(yOf(row.item.mode, row.lane) - y) >= rowHeight ||
          xOf(row) >= right || xOf(row) + row.width <= left)
      }
    }
    while (this.cursor < this.items.length && this.items[this.cursor].timeMs + this.offset <= time) {
      const item = this.items[this.cursor++], born = item.timeMs + this.offset
      if (time - born > 800 || !shows(item)) continue
      if (this.active.length >= maxActive) continue
      const size = font, widthOf = Math.min(12000, this.measure(item.text, size))
      let lane = -1
      for (let i = 0; i < rows; i++) {
        const occupied = this.active.some(a => Math.abs(yOf(a.item.mode, a.lane) - yOf(item.mode, i)) < rowHeight &&
          (a === this.held?.row || item.mode !== 1 || a.item.mode !== 1 || width - (xOf(a) + a.width) < 18 + (1 - p.density) * 80))
        if (!occupied) { lane = i; break }
      }
      if (lane >= 0) this.active.push({ item, born, lane, width: widthOf, fontSize: size, pausedFor: 0 })
    }
    return this.active.map(a => ({ item: a.item, x: xOf(a), y: yOf(a.item.mode, a.lane), width: a.width, fontSize: a.fontSize }))
  }
}
