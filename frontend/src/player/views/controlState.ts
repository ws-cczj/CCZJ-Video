/** Presentation consumes live values; media and native resources stay in the host. */
export interface PlaybackControlState {
  readonly playing: boolean
  readonly current: number
  readonly duration: number
  readonly canPip: boolean
  readonly canFullscreen: boolean
  readonly canCast: boolean
  readonly pip: boolean
  readonly fullscreen: boolean
  readonly speed: number
  readonly volume: number
  readonly muted: boolean
  readonly hasPrev: boolean
  readonly hasNext: boolean
}
export interface DesktopPlaybackCommands {
  video(): HTMLVideoElement | null
  play(): void
  previous(): void
  next(): void
  fullscreen(): void | Promise<void>
  mute(): void
  volume(event: Event): void
  speed(value: number): void
  interaction(): void
}

export type MobilePanel = 'more' | 'speed' | 'episodes' | 'cast'
export interface MobileChromeHandle {
  readonly pinned: boolean
  readonly menuOpen: boolean
  readonly category: 'quality' | 'playback' | 'tools'
  toggle(panel: MobilePanel): void
  close(panel?: MobilePanel | 'quick'): void
}
