import { danmakuFontSize } from './textStyle'
import type { DanmakuItem } from '../../api/danmaku'
import type { DanmakuPreferences } from './preferences'
export interface DanmakuDraw { item: DanmakuItem; x: number; y: number; width: number; fontSize: number }
interface Active { item: DanmakuItem; born: number; lane: number; width: number; fontSize: number }
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
  constructor(private measure: (text: string, size: number) => number) { }
  setItems(items: DanmakuItem[], offset: number) {
    this.items = [...items].sort((a, b) => a.timeMs - b.timeMs)
    if (offset !== this.offset) { this.last = -Infinity; this.active = [] }
    this.offset = offset
    // Replacing the look-ahead window must not clear comments already on screen
    // or replay the current timestamp. A seek/layout change is handled in tick.
    this.cursor = Number.isFinite(this.last) ? lowerBound(this.items, this.last - this.offset + 1) : 0
  }
  invalidateLayout() { this.layout = ''; this.active = []; this.last = -Infinity }
  clear() { this.items = []; this.active = []; this.cursor = 0; this.last = -Infinity }
  tick(time: number, width: number, height: number, p: DanmakuPreferences): DanmakuDraw[] {
    const font = danmakuFontSize(width,p.fontSize)
    const rowHeight = Math.ceil(font * 1.35 + (p.textStyle === 'stroke' ? p.strokeWidth : 0))
    const top = 12, bottom = Math.min(height * p.area, height - 48) - 8
    const rows = Math.max(0, Math.min(p.rows, Math.floor((bottom - top) / rowHeight)))
    // A viewport-relative shared velocity keeps equal-speed collision checks valid.
    const velocity = Math.max(1, width / 8) * p.speed / 100
    const yOf = (mode: number, lane: number) => mode === 4 ? bottom - 4 - lane * rowHeight : top + font + lane * rowHeight
    const layout = `${width}:${height}:${font}:${p.area}:${rows}:${p.speed}:${p.fixed}:${p.scrolling}:${p.colored}:${p.fontFamily}:${p.fontWeight}:${p.textStyle}:${p.strokeWidth}:${p.density}`
    if (!p.enabled || !rows || !width || !height) { this.active = []; this.last = -Infinity; return [] }
    if (layout !== this.layout || time < this.last || time - this.last > 1000) { this.active = []; this.cursor = lowerBound(this.items, time - this.offset - 200); this.layout = layout }
    this.last = time
    const xOf = (a: Active) => a.item.mode === 1 ? width - (time - a.born) * velocity / 1000 : (width - a.width) / 2
    this.active = this.active.filter(a => a.item.mode === 1 ? xOf(a) + a.width > 0 : time - a.born < 4500)
    while (this.cursor < this.items.length && this.items[this.cursor].timeMs + this.offset <= time) {
      const item = this.items[this.cursor++], born = item.timeMs + this.offset
      if (time - born > 800 || (!p.fixed && item.mode !== 1) || (!p.scrolling && item.mode === 1) || (!p.colored && item.color !== 0xffffff) || ![1, 4, 5].includes(item.mode)) continue
      if (this.active.length >= Math.max(1, Math.floor(120 * p.density))) continue
      const size = font, widthOf = Math.min(12000, this.measure(item.text, size))
      let lane = -1
      for (let i = 0; i < rows; i++) {
        const occupied = this.active.some(a => Math.abs(yOf(a.item.mode, a.lane) - yOf(item.mode, i)) < rowHeight && (item.mode !== 1 || a.item.mode !== 1 || width - (xOf(a) + a.width) < 18 + (1 - p.density) * 80))
        if (!occupied) { lane = i; break }
      }
      if (lane >= 0) this.active.push({ item, born, lane, width: widthOf, fontSize: size })
    }
    return this.active.map(a => ({ item: a.item, x: xOf(a), y: yOf(a.item.mode, a.lane), width: a.width, fontSize: a.fontSize }))
  }
}
