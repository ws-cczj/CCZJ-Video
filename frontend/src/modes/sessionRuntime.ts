import { readSessionString, writeSessionString, removeSession } from '../platform/storage'
import { createModeSessions } from './modeSessions'

export const modeSessions = createModeSessions({ read: readSessionString, write: writeSessionString, remove: removeSession })
