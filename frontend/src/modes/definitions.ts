export const APPLICATION_MODES = {
  video: { home: '/', routePrefix: '', label: 'modes.video', icon: 'film', caption: 'VIDEO', loadShell: () => import('./VideoModeShell.vue') },
  novel: { home: '/novel', routePrefix: '/novel', label: 'modes.novel', icon: 'book-open', caption: 'READING', loadShell: () => import('../novel/NovelShell.vue') },
  comic: { home: '/comic', routePrefix: '/comic', label: 'modes.comic', icon: 'panels', caption: 'COMICS', loadShell: () => import('../comic/ComicShell.vue') },
} as const

export type ApplicationMode = keyof typeof APPLICATION_MODES
export const APPLICATION_MODE_KEYS = Object.keys(APPLICATION_MODES) as ApplicationMode[]

export function normalizeApplicationMode(value: unknown): ApplicationMode { return typeof value === 'string' && Object.prototype.hasOwnProperty.call(APPLICATION_MODES, value) ? value as ApplicationMode : 'video' }

/** The composition root chooses a strategy; individual content pages do not. */
export function modeHome(mode: ApplicationMode): string { return APPLICATION_MODES[mode].home }
export function routeMode(path: string): ApplicationMode { return APPLICATION_MODE_KEYS.find(mode => { const prefix = APPLICATION_MODES[mode].routePrefix; return prefix && (path === prefix || path.startsWith(prefix + '/')) }) ?? 'video' }

export function modeReloadURL(href: string, mode: ApplicationMode, path=modeHome(mode), resumeToken?:string): string {
  const target = new URL(href)
  // Changing the query performs a document navigation, rather than only a hash
  // navigation. Only lightweight checkpoints cross this resource boundary.
  target.searchParams.set('application-mode', mode)
  target.searchParams.delete('mode-resume')
  if(resumeToken)target.searchParams.set('mode-resume',resumeToken)
  target.hash = path
  return target.href
}
