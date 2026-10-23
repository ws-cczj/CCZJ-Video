export type MotionStyle = 'calm' | 'balanced' | 'lively'
export interface MotionOptions { style: MotionStyle; speed: number; intensity: number; pages: boolean; overlays: boolean; feedback: boolean; stagger: boolean }
export const DEFAULT_MOTION: Readonly<MotionOptions> = { style: 'balanced', speed: 100, intensity: 60, pages: true, overlays: true, feedback: true, stagger: true }
export const TIMINGS = { fast: 140, normal: 220, slow: 360, emphasis: 480, carousel: 420 } as const
export function normalizeMotionOptions(raw: unknown): MotionOptions {
  const value = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, unknown> : {}
  const number = (key: 'speed' | 'intensity', min: number, max: number) => typeof value[key] === 'number' && Number.isFinite(value[key]) ? Math.round(Math.max(min, Math.min(max, value[key] as number))) : DEFAULT_MOTION[key]
  const bool = (key: 'pages' | 'overlays' | 'feedback' | 'stagger') => typeof value[key] === 'boolean' ? value[key] as boolean : DEFAULT_MOTION[key]
  return { style: value.style === 'calm' || value.style === 'lively' ? value.style : 'balanced', speed: number('speed', 60, 160), intensity: number('intensity', 0, 100), pages: bool('pages'), overlays: bool('overlays'), feedback: bool('feedback'), stagger: bool('stagger') }
}
export function motionVariables(options: MotionOptions): Record<string, string> {
  const factor = options.style === 'calm' ? 1.12 : options.style === 'lively' ? 1.08 : 1
  const variables: Record<string, string> = {}
  for (const [name, ms] of Object.entries(TIMINGS)) variables[`--cczj-motion-${name}`] = `${Math.round(ms * factor * 100 / options.speed)}ms`
  const intensity = options.intensity / 100 * (options.style === 'calm' ? .4 : options.style === 'lively' ? 1.4 : 1)
  variables['--cczj-motion-distance'] = `${+(10 * intensity).toFixed(2)}px`
  variables['--cczj-motion-enter-scale'] = String(1 - .035 * intensity)
  variables['--cczj-motion-stagger'] = options.stagger ? '35ms' : '0ms'
  const enter = options.style === 'lively' ? 'cubic-bezier(.2,1.15,.3,1)' : 'cubic-bezier(.16,1,.3,1)'
  variables['--cczj-motion-ease-enter'] = enter
  variables['--cczj-motion-ease-emphasis'] = enter
  return variables
}
