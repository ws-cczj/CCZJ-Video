/** Pure sRGB conversion, perceptual tones and readable foreground selection. */
export function clamp255(n: number): number {
  if (n < 0) return 0
  if (n > 255) return 255
  return Math.round(n)
}

export function hexToRgb(hex: string): [number, number, number] {
  const h = (hex || '#000000').replace('#', '')
  const full = h.length === 3 ? h.split('').map(c => c + c).join('') : h
  return [
    parseInt(full.substring(0, 2), 16),
    parseInt(full.substring(2, 4), 16),
    parseInt(full.substring(4, 6), 16),
  ]
}

export function rgbToHex(r: number, g: number, b: number): string {
  const to = (n: number) => clamp255(n).toString(16).padStart(2, '0')
  return `#${to(r)}${to(g)}${to(b)}`
}

/** 读取 `#rgb` / `#rrggbb` / `rgb()` / `rgba()` 三通道，方便对半透明表面算对比度。 */
export function readRgb(color: string): [number, number, number] {
  const m = color.match(/rgba?\(\s*(\d+),\s*(\d+),\s*(\d+)/)
  if (m) return [Number(m[1]), Number(m[2]), Number(m[3])]
  return hexToRgb(color)
}

/** 存进编辑器的表面色必须是纯色：rgba 串绑到 `<input type="color">` 会显示成黑色。 */
export function toSolidHex(color: string): string {
  const [r, g, b] = readRgb(color)
  return rgbToHex(r, g, b)
}

export function hexToRgba(hex: string, alpha: number): string {
  const [r, g, b] = hexToRgb(hex)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

// 往白/亮方向混合 amount ∈ [0,1]
export function lighten(hex: string, amount: number): string {
  const [r, g, b] = hexToRgb(hex)
  return rgbToHex(r + (255 - r) * amount, g + (255 - g) * amount, b + (255 - b) * amount)
}

// 往黑/暗方向混合 amount ∈ [0,1]
export function darken(hex: string, amount: number): string {
  const [r, g, b] = hexToRgb(hex)
  return rgbToHex(r * (1 - amount), g * (1 - amount), b * (1 - amount))
}

// 计算颜色亮度（0-255），用于判断对比度
export function luminance(hex: string): number {
  const [r, g, b] = hexToRgb(hex)
  return 0.299 * r + 0.587 * g + 0.114 * b
}

export function hslToHex(hue: number, saturation: number, lightness: number): string {
  const h = ((hue % 360) + 360) % 360
  const s = Math.max(0, Math.min(100, saturation)) / 100
  const l = Math.max(0, Math.min(100, lightness)) / 100
  const c = (1 - Math.abs(2 * l - 1)) * s
  const x = c * (1 - Math.abs((h / 60) % 2 - 1))
  const m = l - c / 2
  let r = 0, g = 0, b = 0
  if (h < 60) [r, g, b] = [c, x, 0]
  else if (h < 120) [r, g, b] = [x, c, 0]
  else if (h < 180) [r, g, b] = [0, c, x]
  else if (h < 240) [r, g, b] = [0, x, c]
  else if (h < 300) [r, g, b] = [x, 0, c]
  else [r, g, b] = [c, 0, x]
  return rgbToHex((r + m) * 255, (g + m) * 255, (b + m) * 255)
}

export function relativeLuminance(color: string): number {
  const [r, g, b] = readRgb(color).map((value) => {
    const channel = value / 255
    return channel <= 0.04045 ? channel / 12.92 : Math.pow((channel + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

export function contrastRatio(a: string, b: string): number {
  const [lighter, darker] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x)
  return (lighter + 0.05) / (darker + 0.05)
}


function linear(channel:number):number { const c=channel/255;return c<=.04045?c/12.92:((c+.055)/1.055)**2.4 }
function encoded(channel:number):number { return 255*(channel<=.0031308?12.92*channel:1.055*channel**(1/2.4)-.055) }
function oklch(color:string):[number,number,number] {
  const [r,g,b]=readRgb(color).map(linear)
  const l=Math.cbrt(.4122214708*r+.5363325363*g+.0514459929*b)
  const m=Math.cbrt(.2119034982*r+.6806995451*g+.1073969566*b)
  const q=Math.cbrt(.0883024619*r+.2817188376*g+.6299787005*b)
  const a=1.9779984951*l-2.428592205*m+.4505937099*q
  const v=.0259040371*l+.7827717662*m-.808675766*q
  return [.2104542553*l+.793617785*m-.0040720468*q,Math.hypot(a,v),Math.atan2(v,a)]
}
/** Retain hue and reduce chroma until the tone fits the sRGB output gamut. */
export function perceptualTone(seed:string,lightness:number,maxChroma=.07):string {
  const [,chroma,hue]=oklch(seed)
  let c=Math.min(chroma,maxChroma)
  const L=Math.max(0,Math.min(1,lightness))
  for(let step=0;step<28;step++){
    const a=c*Math.cos(hue),b=c*Math.sin(hue)
    const l=(L+.3963377774*a+.2158037573*b)**3
    const m=(L-.1055613458*a-.0638541728*b)**3
    const q=(L-.0894841775*a-1.291485548*b)**3
    const rgb=[4.0767416621*l-3.3077115913*m+.2309699292*q,-1.2684380046*l+2.6097574011*m-.3413193965*q,-.0041960863*l-.7034186147*m+1.707614701*q]
    if(rgb.every(v=>v>=-1e-7&&v<=1.0000001)||step===27)return rgbToHex(...rgb.map(encoded) as [number,number,number])
    c*=.75
  }
  return rgbToHex(0,0,0)
}
/** Prefer a restrained hue-related foreground, then fall back to neutral contrast. */
export function contrastText(background:string):string {
  const dark=contrastRatio(background,'#000000')>=contrastRatio(background,'#ffffff')
  for(const L of dark?[.25,.2,.15,.1]:[.96,.98,.995]){
    const candidate=perceptualTone(background,L,dark?.065:.025)
    if(contrastRatio(background,candidate)>=4.5)return candidate
  }
  return dark?'#000000':'#ffffff'
}
/** Keep a role's hue while making its label readable on known soft surfaces. */
export function readableForeground(seed:string,surfaces:readonly string[],minimum=4.5):string {
  const score=(color:string)=>Math.min(...surfaces.map(surface=>contrastRatio(surface,color)))
  if(!surfaces.length||score(seed)>=minimum)return seed
  const towardsDark=score('#000000')>=score('#ffffff')
  const lightness=oklch(seed)[0]
  for(let step=1;step<=34;step++){
    const color=perceptualTone(seed,lightness+(towardsDark?-1:1)*step*.03,.2)
    if(score(color)>=minimum)return color
  }
  return towardsDark?'#000000':'#ffffff'
}
/** Composite a known translucent surface; wallpaper pixels are not sampled. */
export function compositeColor(surface:string,base:string):string {
  const match=surface.match(/^rgba\([^,]+,[^,]+,[^,]+,\s*([\d.]+)\s*\)$/)
  if(!match)return surface
  const alpha=Math.max(0,Math.min(1,Number(match[1])))
  const front=readRgb(surface),back=readRgb(base)
  return rgbToHex(...front.map((v,i)=>v*alpha+back[i]*(1-alpha)) as [number,number,number])
}
export function windowActionColors(primary:string,surface:string):{fill:string;text:string} {
  const dark=relativeLuminance(surface)<.35
  const fill=perceptualTone(primary,dark?.32:.9,.035)
  return {fill,text:contrastText(fill)}
}

/** 色相角（0-359）；无彩色（灰/黑/白）返回 -1。 */
export function hueOf(color: string): number {
  const [r, g, b] = hexToRgb(color).map((v) => v / 255)
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const delta = max - min
  if (delta === 0) return -1
  let hue: number
  if (max === r) hue = 60 * (((g - b) / delta) % 6)
  else if (max === g) hue = 60 * ((b - r) / delta + 2)
  else hue = 60 * ((r - g) / delta + 4)
  return ((hue % 360) + 360) % 360
}
