import { Storage } from './storage'

/** Keep historical separators and prefixes: existing checkpoints depend on them. */
export const Keys = Object.freeze({
  videoRefresh: (source: string, vodID: string) => `${Storage.videoRefreshPrefix}${source}_${vodID}`,
  bootBackground: (mode: 'video' | 'novel' | 'comic') => `cczj.boot.background.${mode}.v1`,
  pluginStorage: (packID: string, key: string) => `plugin:${packID}:${key}`,
  playbackTime: (key: string) => key.startsWith(Storage.playbackTimePrefix) ? key : `${Storage.playbackTimePrefix}${key}`,
  legacyPlaybackTime: (key: string) => `${Storage.playbackTimePrefix}${key}`,
})
