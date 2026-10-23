import { getActivePinia } from 'pinia'
import { useApplicationMode } from '../modes/applicationMode'
// Early bootstrap observations may precede Pinia; the default mode is video.
export function diagnosticMode(){return getActivePinia()?useApplicationMode().mode:'video'}
