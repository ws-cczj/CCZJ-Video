import { shallowRef } from 'vue'
import { modeSessions } from '../modes/sessionRuntime'

type StartupPhase = 'modules_ready' | 'runtime' | 'locale' | 'motion' | 'mode' | 'mode_shell' | 'theme' | 'vue_mount' | 'first_screen' | 'first_render_opportunity' | 'image_decode' | 'splash_reveal' | 'splash_closed'
interface StartupEvent { phase: StartupPhase; at_ms: number; duration_ms?: number; failed?: boolean }
export const startupRevision = shallowRef(0)
const events: StartupEvent[] = []

/** Startup events are kept even before the user enables a resource recording. */
export function markStartup(phase: StartupPhase, duration?: number, failed = false): void {
  if (events.length >= 32 || events.some(event => event.phase === phase)) return
  events.push({ phase, at_ms: Math.round(performance.now()), ...(duration === undefined ? {} : {duration_ms:Math.round(duration)}), ...(failed ? {failed:true} : {}) })
  startupRevision.value++
}

export async function startupTask<T>(phase: StartupPhase, work: () => Promise<T>): Promise<T> {
  const started = performance.now()
  try { const result = await work(); markStartup(phase, performance.now() - started); return result }
  catch (error) { markStartup(phase, performance.now() - started, true); throw error }
}

export function startupSnapshot(mode?:'video'|'novel'|'comic') {
  let native: Record<string, number> | null = null
  try {
    const host = (globalThis as {window?:{wails?:{startupState?:()=>string}}}).window?.wails
    if (host?.startupState) {
      const value:unknown=JSON.parse(host.startupState())
      const fields=['activity_started_ms','go_ready_ms','page_started_ms','page_committed_ms','page_finished_ms']
      if(value&&typeof value==='object'&&!Array.isArray(value)) {
        const source=value as Record<string,unknown>
        if(fields.every(field=>typeof source[field]==='number'&&Number.isFinite(source[field])&&Number(source[field])>=0)) {
          native=Object.fromEntries(fields.map(field=>[field,Number(source[field])]))
        }
      }
    }
  } catch { /* An older host must remain usable; coverage reports this gap. */ }
  const first=events.find(event=>event.phase==='first_screen'),splash=events.find(event=>event.phase==='splash_closed')
  return { scope:'current_document', mode_activation:modeSessions.diagnostics(mode), first_screen_within_splash:first&&splash?first.at_ms<=splash.at_ms:null, time_origin_ms:performance.timeOrigin, native, events:events.map(event=>({...event})),
    paint_entries:performance.getEntriesByType?.('paint').map(entry=>({name:entry.name,at_ms:Math.round(entry.startTime)})) ?? [] }
}

