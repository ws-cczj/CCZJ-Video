import type { Video, Episode, PlayLine } from '../../types'
import type { SourceOption, SourceProbe } from './useSourceCatalog'

/** A stable read model: progress mutations are observed by the panel, rather
 * than rebuilding the page and the media host on every timeupdate. */
export interface PlayerPanelModel {
  readonly details: {
    readonly video: Video | null
    readonly overview: string
    readonly sourceKey: string
    readonly vodId: string
    readonly globalId: number
    readonly lines: PlayLine[]
    readonly activeLineIndex: number
  }
  readonly catalog: {
    readonly items: SourceOption[]
    readonly activeKey: string
    readonly readyCount: number
    readonly probingCount: number
    readonly loading: boolean
    readonly label: (probe: SourceProbe) => string
    readonly selectable: (source: SourceOption) => boolean
  }
  readonly episodes: {
    readonly items: Episode[]
    readonly current: number
    readonly currentNumber: number
    readonly ascending: boolean
    readonly originalIndex: (index: number) => number
    readonly percent: (index: number) => number
    readonly watched: (index: number) => boolean
    readonly label: (index: number, episode: Episode) => string
  }
}
