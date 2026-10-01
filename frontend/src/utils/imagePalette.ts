import { hslToHex } from '../stores/theme'

/**
 * 从背景图里挑一个能当主题色的颜色。
 *
 * 直接取平均色会把「宣纸白 + 淡墨」这类低饱和图算成一片灰，灰做主色等于没主色；
 * 取像素众数又会被大面积背景带跑。所以先按色相分桶、用饱和度加权挑最鲜明的那一簇，
 * 只在整张图都无彩色时才退回墨色。
 */

/** 采样最长边像素数：够统计出主色，又不会在 4K 壁纸上卡住界面。 */
const SAMPLE_SIZE = 96
/** 低于此饱和度视为纸白/墨黑/灰，不参与主色投票。 */
const MIN_SATURATION = 0.18
/**
 * 全图平均饱和度的下限：低于此值这张图就是黑白/水墨，鲜艳的那一小簇只是点缀。
 * 饱和度加权天生偏爱少数高饱和像素 —— 水墨图里一簇粉花只占 3%（全图均值 0.05）
 * 却能投出主色，而四张彩色主题图的均值都在 0.6 以上，所以用整图色量做闸门。
 */
const MONOCHROME_SATURATION = 0.15
/** 过暗的像素色相失真、过亮的几乎没有信息，都排除掉。 */
const MIN_LIGHTNESS = 0.12
const MAX_LIGHTNESS = 0.92
/** 色相分桶宽度（度）。12° 能把邻近的暖色并成一簇，又不会把红和橙混成一桶。 */
const HUE_BIN = 12
/** 成品主题色的可用区间：再艳会刺眼，再淡压在深色底上看不出是主题色。 */
const OUT_SATURATION: [number, number] = [0.34, 0.72]
const OUT_LIGHTNESS: [number, number] = [0.30, 0.52]

interface Bin {
  weight: number
  sin: number
  cos: number
  saturation: number
  lightness: number
}

function clamp(value: number, [lo, hi]: [number, number]): number {
  return Math.min(hi, Math.max(lo, value))
}

function toHsl(r: number, g: number, b: number): [number, number, number] {
  const rn = r / 255
  const gn = g / 255
  const bn = b / 255
  const max = Math.max(rn, gn, bn)
  const min = Math.min(rn, gn, bn)
  const delta = max - min
  const lightness = (max + min) / 2
  if (delta === 0) return [0, 0, lightness]
  const saturation = delta / (1 - Math.abs(2 * lightness - 1))
  let hue: number
  if (max === rn) hue = 60 * (((gn - bn) / delta) % 6)
  else if (max === gn) hue = 60 * ((bn - rn) / delta + 2)
  else hue = 60 * ((rn - gn) / delta + 4)
  return [((hue % 360) + 360) % 360, saturation, lightness]
}

async function readPixels(src: string): Promise<Uint8ClampedArray | null> {
  const img = new Image()
  img.src = src
  try {
    await img.decode()
  } catch {
    return null
  }
  const longest = Math.max(img.naturalWidth, img.naturalHeight)
  if (!longest) return null
  const scale = Math.min(1, SAMPLE_SIZE / longest)
  const w = Math.max(1, Math.round(img.naturalWidth * scale))
  const h = Math.max(1, Math.round(img.naturalHeight * scale))
  const canvas = document.createElement('canvas')
  canvas.width = w
  canvas.height = h
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  if (!ctx) return null
  ctx.drawImage(img, 0, 0, w, h)
  // 远程图未开 CORS 时画布会被污染，这里读不出像素 —— 当作取色失败，不抛错。
  try {
    return ctx.getImageData(0, 0, w, h).data
  } catch {
    return null
  }
}

/** 按亮度上限筛像素，返回它们的平均 RGB；一个都不剩时返回 null。 */
function averageColor(pixels: Uint8ClampedArray, maxLightness: number): [number, number, number] | null {
  let r = 0
  let g = 0
  let b = 0
  let count = 0
  for (let i = 0; i < pixels.length; i += 4) {
    if (pixels[i + 3] < 250) continue
    if (toHsl(pixels[i], pixels[i + 1], pixels[i + 2])[2] > maxLightness) continue
    r += pixels[i]
    g += pixels[i + 1]
    b += pixels[i + 2]
    count += 1
  }
  if (!count) return null
  return [r / count, g / count, b / count]
}

/**
 * 整张图都没有彩色时，用图里的墨色当主色。
 * 优先取暗部（真正的墨迹）；通篇浅灰的图没有暗部，就退到全图均值再压暗。
 */
function inkFallback(pixels: Uint8ClampedArray): string | null {
  const tone = averageColor(pixels, 0.35) ?? averageColor(pixels, 0.9)
  if (!tone) return null
  const [hue, saturation, lightness] = toHsl(tone[0], tone[1], tone[2])
  // 墨平均出来常常贴近纯黑，抬一档才能看出按钮轮廓。
  return hslToHex(hue, clamp(saturation, [0.05, 0.35]) * 100, clamp(lightness * 1.6, [0.18, 0.32]) * 100)
}

/**
 * 取背景图的主色。返回 `#rrggbb`；读不到像素（图损坏、跨域污染画布）时返回 null。
 */
export async function pickColorFromImage(src: string): Promise<string | null> {
  const pixels = await readPixels(src)
  if (!pixels) return null

  const bins = new Map<number, Bin>()
  let total = 0
  let saturationSum = 0
  for (let i = 0; i < pixels.length; i += 4) {
    if (pixels[i + 3] < 250) continue
    const [hue, saturation, lightness] = toHsl(pixels[i], pixels[i + 1], pixels[i + 2])
    total += 1
    saturationSum += saturation
    if (saturation < MIN_SATURATION || lightness < MIN_LIGHTNESS || lightness > MAX_LIGHTNESS) continue
    // 饱和度平方：让鲜艳的少数像素盖过大片近灰；中间调比极明极暗都更可信。
    const weight = saturation * saturation * (1 - Math.abs(lightness - 0.5) * 1.2)
    const bin = Math.floor(hue / HUE_BIN)
    const acc = bins.get(bin) || { weight: 0, sin: 0, cos: 0, saturation: 0, lightness: 0 }
    acc.weight += weight
    // 色相是环度量，直接求平均会在 359°/1° 处算出 180°，所以走 sin/cos 向量均值。
    acc.sin += Math.sin((hue * Math.PI) / 180) * weight
    acc.cos += Math.cos((hue * Math.PI) / 180) * weight
    acc.saturation += saturation * weight
    acc.lightness += lightness * weight
    bins.set(bin, acc)
  }

  if (!total || saturationSum / total < MONOCHROME_SATURATION) return inkFallback(pixels)

  let best: Bin | null = null
  for (const acc of bins.values()) {
    if (!best || acc.weight > best.weight) best = acc
  }
  if (!best || best.weight <= 0) return inkFallback(pixels)

  const hue = ((Math.atan2(best.sin, best.cos) * 180) / Math.PI + 360) % 360
  return hslToHex(
    hue,
    clamp(best.saturation / best.weight, OUT_SATURATION) * 100,
    clamp(best.lightness / best.weight, OUT_LIGHTNESS) * 100,
  )
}
