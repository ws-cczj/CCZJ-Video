export type MotionStyle = 'calm' | 'balanced' | 'lively'
export type MotionTiming = keyof typeof TIMINGS
export interface MotionOptions { style: MotionStyle; speed: number; intensity: number; pages: boolean; overlays: boolean; feedback: boolean; stagger: boolean; reduceMotion:boolean; durations:Partial<Record<MotionTiming,number>> }
export const DEFAULT_MOTION: Readonly<MotionOptions> = { style: 'balanced', speed: 100, intensity: 60, pages: true, overlays: true, feedback: true, stagger: true, reduceMotion:false, durations:Object.freeze({}) }
export const TIMINGS = { fast: 140, normal: 220, slow: 360, emphasis: 480, carousel: 420, section: 800, group: 320, dismiss: 600, reveal: 1200 } as const
export function normalizeMotionOptions(raw: unknown): MotionOptions {
  const value = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, unknown> : {}
  const number = (key: 'speed' | 'intensity', min: number, max: number) => typeof value[key] === 'number' && Number.isFinite(value[key]) ? Math.round(Math.max(min, Math.min(max, value[key] as number))) : DEFAULT_MOTION[key]
  const durations:Partial<Record<MotionTiming,number>>={}
  const rawDurations=value.durations && typeof value.durations==='object'&&!Array.isArray(value.durations)?value.durations as Record<string,unknown>:{}
  for(const key of Object.keys(TIMINGS) as MotionTiming[]){const ms=rawDurations[key];if(typeof ms==='number'&&Number.isFinite(ms))durations[key]=Math.round(Math.min(2500,Math.max(100,ms)))}
  const bool = (key: 'pages' | 'overlays' | 'feedback' | 'stagger' | 'reduceMotion') => typeof value[key] === 'boolean' ? value[key] as boolean : DEFAULT_MOTION[key]
  return { style: value.style === 'calm' || value.style === 'lively' ? value.style : 'balanced', speed: number('speed', 60, 160), intensity: number('intensity', 0, 100), pages: bool('pages'), overlays: bool('overlays'), feedback: bool('feedback'), stagger: bool('stagger'),reduceMotion:bool('reduceMotion'),durations }
}
export function motionVariables(options: MotionOptions): Record<string, string> {
  const factor = options.style === 'calm' ? 1.12 : options.style === 'lively' ? 1.08 : 1
  const variables: Record<string, string> = {}
  for (const [name, ms] of Object.entries(TIMINGS)) variables[`--cczj-motion-${name}`] = `${options.durations[name as MotionTiming] ?? Math.round(ms * factor * 100 / options.speed)}ms`
  const intensity = options.intensity / 100 * (options.style === 'calm' ? .4 : options.style === 'lively' ? 1.4 : 1)
  variables['--cczj-motion-distance'] = `${+(10 * intensity).toFixed(2)}px`
  variables['--cczj-motion-enter-scale'] = String(1 - .035 * intensity)
  variables['--cczj-motion-stagger'] = options.stagger ? '35ms' : '0ms'
  const enter = options.style === 'lively' ? 'cubic-bezier(.2,1.15,.3,1)' : 'cubic-bezier(.16,1,.3,1)'
  variables['--cczj-motion-ease-enter'] = enter
  variables['--cczj-motion-ease-emphasis'] = enter
  return variables
}
