import { Storage } from './storage'
import { Settings } from './settings'
import { Legacy } from './legacy'
import { Keys } from './keys'

/** Explicit, immutable key catalog. Resource owners stay in their domain modules. */
export const CCZJ = Object.freeze({ Storage, Settings, Legacy, Keys })
