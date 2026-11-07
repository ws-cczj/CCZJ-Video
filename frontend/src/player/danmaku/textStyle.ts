import type { DanmakuPreferences } from './preferences'
const families = {
  sans: '"CCZJ Danmaku Sans", "Source Han Sans CN", "Noto Sans CJK SC", "Microsoft YaHei", "PingFang SC", sans-serif',
  serif: '"Noto Serif CJK SC", "Songti SC", SimSun, serif',
  mono: '"Noto Sans Mono CJK SC", Consolas, monospace',
} as const
export function danmakuFontSize(width:number,size:number):number { return size * Math.min(1,Math.max(.75,width/640)) }
export function danmakuFont(preferences: DanmakuPreferences, size: number): string {
  return `${preferences.fontWeight} ${size}px ${families[preferences.fontFamily]}`
}
/** Measurement, live drawing and the settings preview share identical typography. */
export function paintDanmaku(context: CanvasRenderingContext2D, text: string, x: number, y: number, size: number, color: number, preferences: DanmakuPreferences): void {
  const scale = size / preferences.fontSize
  const luminance = ((color >> 16 & 255) * .2126 + (color >> 8 & 255) * .7152 + (color & 255) * .0722) / 255
  context.font = danmakuFont(preferences, size)
  context.globalAlpha = preferences.opacity
  context.textBaseline = 'alphabetic'
  context.lineJoin = 'round'
  context.fillStyle = `#${color.toString(16).padStart(6, '0')}`
  context.strokeStyle = luminance < .2 ? 'rgba(255,255,255,.85)' : 'rgba(0,0,0,.9)'
  context.shadowColor = 'rgba(0,0,0,.9)'
  context.shadowBlur = preferences.textStyle === 'shadow' ? 3 * scale : 0
  context.shadowOffsetX = 0
  context.shadowOffsetY = preferences.textStyle === 'shadow' ? scale : 0
  if (preferences.textStyle === 'stroke' && preferences.strokeWidth > 0) {
    context.lineWidth = preferences.strokeWidth * scale
    context.strokeText(text, x, y)
  }
  context.fillText(text, x, y)
}
/** Bound the backing store while retaining sharp text on high-density phones. */
export function danmakuPixelRatio(width: number, height: number): number {
  return Math.min(3, globalThis.devicePixelRatio || 1, Math.sqrt(8_000_000 / Math.max(1, width * height)))
}
