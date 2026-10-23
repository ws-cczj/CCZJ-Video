import { expect, it, vi } from 'vitest'
vi.mock('../src/api/app', () => ({}))
import { decodeMatch } from '../src/api/danmaku'

const binding = { localKey: 'video:source:1', confirmed: true, offsetMs: 0, episode: { provider: 'qq', episodeId: 'video01', seriesId: '', aid: '', title: 'sample', number: '1', durationMs: 120000, pageUrl: '' } }
const window = { provider: 'qq', episodeId: 'video01', items: [], warnings: [], startMs: 0, endMs: 75000, cached: true, stale: false, skipped: 0 }
const available = { provider: 'qq', status: 'available', code: '', retryAt: 0 }
it('decodes one verified pool or no pool with source cooldowns', () => {
  expect(decodeMatch({ binding, window, attempts: [available] }).binding).toEqual(binding)
  const attempts = [{ provider: 'bilibili', status: 'cooling_down', code: 'NO_WORK_MATCH', retryAt: 1792454400 }]
  expect(decodeMatch({ binding: null, window: null, attempts }).attempts).toEqual(attempts)
})
it('rejects mismatched identities, duplicate sources and fabricated availability', () => {
  for (const value of [
    { binding, window: null, attempts: [available] },
    { binding, window: { ...window, episodeId: 'other' }, attempts: [available] },
    { binding, window, attempts: [available, available] },
    { binding, window, attempts: [] },
    { binding, window, attempts: [{ ...available, status: 'cooling_down' }] },
    { binding: null, window: null, attempts: [{ ...available, provider: 'external' }] },
    { binding: null, window: null, attempts: [{ ...available, retryAt: NaN }] },
    { binding: null, window: null, attempts: [{ ...available, stage: 3 }] },
    { binding: null, window: null, attempts: [{ ...available, elapsedMs: -1 }] },
    { binding: null, window: null, attempts: [{ ...available, detail: 'x'.repeat(2001) }] },
  ]) expect(() => decodeMatch(value)).toThrow()
})
it('retains failure diagnostics and the correlation ID for a cooled source', () => {
  const attempt={provider:'qq',status:'cooling_down',code:'WORK_YEAR_MISMATCH',retryAt:1792454400,stage:'work_cache',detail:'cached_evidence=true; year_rejected=1',elapsedMs:4}
  expect(decodeMatch({binding:null,window:null,attempts:[attempt],traceId:'match-123'})).toEqual({binding:null,window:null,attempts:[attempt],traceId:'match-123'})
})
