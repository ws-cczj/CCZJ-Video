import type { DanmakuItem } from '../../api/danmaku'
import type { DanmakuPreferences } from './preferences'
import { danmakuFont, danmakuTextPadding, paintDanmaku } from './textStyle'
import type { DanmakuDraw } from './timeline'

interface TextBitmap {
  canvas: HTMLCanvasElement
  originY: number
}

const maxPixels = 2_000_000
const maxEntries = 120

/** Only the current overlay's visible comments can retain rasterized text. */
export class DanmakuTextBitmaps {
  private readonly entries = new Map<DanmakuItem, TextBitmap>()
  private readonly retained = new Set<DanmakuItem>()
  private pixels = 0

  retain(rows: DanmakuDraw[]): void {
    this.retained.clear()
    for (const row of rows) this.retained.add(row.item)
    for (const [item, bitmap] of this.entries) {
      if (!this.retained.has(item)) {
        this.entries.delete(item)
        this.release(bitmap)
      }
    }
    this.retained.clear()
  }

  clear(): void {
    for (const bitmap of this.entries.values()) this.release(bitmap)
    this.entries.clear()
  }

  private release({ canvas }: TextBitmap): void {
    this.pixels -= canvas.width * canvas.height
    canvas.width = 0
    canvas.height = 0
  }

  draw(context: CanvasRenderingContext2D, row: DanmakuDraw, scaleX: number, scaleY: number, preferences: DanmakuPreferences): void {
    const { item, fontSize } = row
    const x = Math.round(row.x * scaleX)
    const y = Math.round(row.y * scaleY)
    const bitmap = this.entries.get(item) ?? this.create(context, row, scaleX, scaleY, preferences)
    if (bitmap) {
      context.drawImage(bitmap.canvas, x, y - bitmap.originY)
      return
    }
    context.setTransform(scaleX, 0, 0, scaleY, 0, 0)
    paintDanmaku(context, item.text, x / scaleX + row.textInset, y / scaleY, fontSize, item.color, preferences)
    context.resetTransform()
  }

  private create(context: CanvasRenderingContext2D, row: DanmakuDraw, scaleX: number, scaleY: number, preferences: DanmakuPreferences): TextBitmap | undefined {
    if (this.entries.size >= maxEntries || this.pixels >= maxPixels) return
    const { item, fontSize } = row
    context.font = danmakuFont(preferences, fontSize)
    const metrics = context.measureText(item.text)
    const padding = danmakuTextPadding(preferences, fontSize)
    const originY = Math.ceil((Math.max(fontSize, metrics.actualBoundingBoxAscent || 0) + padding) * scaleY)
    const width = Math.ceil(row.width * scaleX)
    const height = originY + Math.ceil((Math.max(fontSize * .25, metrics.actualBoundingBoxDescent || 0) + padding) * scaleY)
    if (width > 4096 || this.pixels + width * height > maxPixels) return

    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const brush = canvas.getContext('2d')
    if (!brush) return
    brush.setTransform(scaleX, 0, 0, scaleY, 0, 0)
    paintDanmaku(brush, item.text, row.textInset, originY / scaleY, fontSize, item.color, preferences)

    const bitmap = { canvas, originY }
    this.entries.set(item, bitmap)
    this.pixels += width * height
    return bitmap
  }
}
