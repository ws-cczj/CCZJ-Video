import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useApplicationMode } from '../src/modes/applicationMode'
import { modeReloadURL, normalizeApplicationMode, routeMode } from '../src/modes/definitions'
import { GetApplicationModeStatus, SetSetting } from '../src/api/app'
import { reloadApplication } from '../src/api/runtime'
vi.mock('../src/api/app',()=>({GetApplicationModeStatus:vi.fn(),SetSetting:vi.fn()}))
vi.mock('../src/api/runtime',()=>({reloadApplication:vi.fn()}))
vi.mock('../src/platform/bootAppearance',()=>({prepareModeBackground:vi.fn()}))
beforeEach(()=>{setActivePinia(createPinia());vi.mocked(GetApplicationModeStatus).mockResolvedValue({mode:'video',phase:'ready'});vi.mocked(SetSetting).mockResolvedValue(undefined);vi.stubGlobal('window',{location:{href:'https://wails.localhost/?debug=1#/settings'}})})
afterEach(()=>vi.unstubAllGlobals())
it('loads one mode once, and rejects unknown mode values without losing video access',async()=>{
  vi.mocked(GetApplicationModeStatus).mockResolvedValue({mode:'novel',phase:'ready'})
  const mode=useApplicationMode();await Promise.all([mode.load(),mode.load()]);expect(mode.mode).toBe('novel');expect(GetApplicationModeStatus).toHaveBeenCalledTimes(1)
  expect(normalizeApplicationMode('bad')).toBe('video');expect(routeMode('/novel/read/book/0')).toBe('novel');expect(routeMode('/novelty')).toBe('video')
  expect(normalizeApplicationMode('comic')).toBe('comic');expect(routeMode('/comic/read/moon-mail/0')).toBe('comic');expect(routeMode('/comical')).toBe('video')
})
it('persists the mode before navigating to a new document and blocks duplicate clicks',async()=>{
  let finish:()=>void=()=>{}
  vi.mocked(SetSetting).mockImplementation(()=>new Promise<void>(resolve=>{finish=resolve}) as ReturnType<typeof SetSetting>)
  const mode=useApplicationMode();await mode.load();const switching=mode.switchMode('novel',async()=>true);await mode.switchMode('novel',async()=>true)
  await vi.waitFor(()=>expect(SetSetting).toHaveBeenCalledTimes(1));expect(reloadApplication).not.toHaveBeenCalled();finish();await switching
  expect(reloadApplication).toHaveBeenCalledWith(expect.stringMatching(/debug=1&application-mode=novel&mode-reload=[^#]+#\/novel$/),expect.any(AbortSignal))
  expect(mode.mode).toBe('video') // The existing shell stays until the document reloads.
})
it('keeps the live mode and allows retry when the database rejects the change',async()=>{
  const mode=useApplicationMode();await mode.load();vi.mocked(SetSetting).mockRejectedValueOnce(new Error('disk unavailable'))
  await mode.switchMode('novel',async()=>true);expect(mode.issue).toBe('switchFailed');expect(mode.mode).toBe('video');expect(mode.switching).toBe(false);expect(reloadApplication).not.toHaveBeenCalled()
  await mode.retry();expect(reloadApplication).toHaveBeenCalledOnce()
})
it('uses separate homes in both directions and preserves unrelated URL parameters',()=>{
  expect(modeReloadURL('https://wails.localhost/?application-mode=novel&debug=1#/novel/settings','video')).toMatch(/application-mode=video&debug=1&mode-reload=[^#]+#\/$/)
  expect(modeReloadURL('https://wails.localhost/?debug=1#/novel/settings','comic')).toMatch(/debug=1&application-mode=comic&mode-reload=[^#]+#\/comic$/)
})

it('keeps the current document and tasks when the switch warning is cancelled',async()=>{
 const mode=useApplicationMode();await mode.load();
 await mode.switchMode('novel',async()=>false);expect(SetSetting).not.toHaveBeenCalled();expect(reloadApplication).not.toHaveBeenCalled();expect(mode.switching).toBe(false)
})
