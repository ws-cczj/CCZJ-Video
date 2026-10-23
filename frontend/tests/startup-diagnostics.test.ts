import { afterEach, expect, it, vi } from 'vitest'
import { markStartup, startupSnapshot, startupTask } from '../src/diagnostics/startup'

afterEach(()=>vi.unstubAllGlobals())

it('preserves startup events before resource recording is enabled and snapshots are detached',()=>{
  markStartup('modules_ready')
  markStartup('modules_ready')
  const events=startupSnapshot().events
  expect(events.filter(event=>event.phase==='modules_ready')).toHaveLength(1)
  events.length=0
  expect(startupSnapshot().events).not.toHaveLength(0)
})

it('keeps unavailable or malformed native timings unknown',()=>{
  vi.stubGlobal('window',{wails:{startupState:()=>JSON.stringify({go_ready_ms:'fast'})}})
  expect(startupSnapshot().native).toBeNull()
  const native={activity_started_ms:1000,go_ready_ms:20,page_started_ms:25,page_committed_ms:40,page_finished_ms:50}
  vi.stubGlobal('window',{wails:{startupState:()=>JSON.stringify(native)}})
  expect(startupSnapshot().native).toEqual(native)
})

it('records a failed startup task without swallowing its failure',async()=>{
  await expect(startupTask('theme',()=>Promise.reject(new Error('settings unavailable')))).rejects.toThrow('settings unavailable')
  expect(startupSnapshot().events.find(event=>event.phase==='theme')).toMatchObject({failed:true,duration_ms:expect.any(Number)})
})

it('observes data preparation within the splash interval without making it a dismissal condition',async()=>{
 await startupTask('first_screen',async()=>{})
 markStartup('splash_closed')
 expect(startupSnapshot().first_screen_within_splash).toBe(true)
})
