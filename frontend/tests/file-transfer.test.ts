import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { readFileDataURL, readFileText } from '../src/api/fileTransfer'
import { createRequestScope } from '../src/api/requestScope'

vi.mock('../src/locales', () => ({ tr: (key: string) => key }))

class Reader {
  static opened: Reader[] = []
  result: string | null = null
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  abort = vi.fn(() => this.onabort?.())
  constructor() { Reader.opened.push(this) }
  readAsDataURL(_file: File) {}
  readAsText = vi.fn((_file: File) => {})
}

beforeEach(() => { Reader.opened = []; vi.stubGlobal('FileReader', Reader) })
afterEach(() => vi.unstubAllGlobals())

it('rejects oversized files before creating a reader', async () => {
  await expect(readFileDataURL(new File(['too large'], 'backup.json'), 4)).rejects.toMatchObject({ code: 'VALIDATION' })
  expect(Reader.opened).toHaveLength(0)
})

it('returns the complete data URL and releases reader callbacks', async () => {
  const reading = readFileDataURL(new File(['{}'], 'source.json'))
  const reader = Reader.opened[0]!
  reader.result = 'data:application/json;base64,e30='
  reader.onload?.()
  expect(await reading).toBe(reader.result)
  expect([reader.onload, reader.onerror, reader.onabort]).toEqual([null, null, null])
})

it('aborts the file when its request scope leaves and ignores a queued load callback', async () => {
  const scope = createRequestScope()
  const reading = scope.run(() => readFileDataURL(new File(['{}'], 'source.json')))
  const rejected = expect(reading).rejects.toMatchObject({ name: 'CancelError' })
  const reader = Reader.opened[0]!, queued = reader.onload
  scope.cancel()
  reader.result = 'late result'
  queued?.()
  await rejected
  expect(reader.abort).toHaveBeenCalledOnce()
  expect([reader.onload, reader.onerror, reader.onabort]).toEqual([null, null, null])
})

it('returns a storage failure instead of leaving a file read pending', async () => {
  const reading = readFileDataURL(new File(['{}'], 'backup.json'))
  const rejected = expect(reading).rejects.toMatchObject({ code: 'STORAGE' })
  Reader.opened[0]!.onerror?.()
  await rejected
})

it('reads plain source and subtitle text with the same lifecycle', async () => {
  const file = new File(['字幕正文'], 'subtitle.srt')
  const reading = readFileText(file, 1024)
  const reader = Reader.opened[0]!
  expect(reader.readAsText).toHaveBeenCalledWith(file)
  reader.result = '字幕正文'
  reader.onload?.()
  expect(await reading).toBe('字幕正文')
})

it('aborts a text read when its consumer leaves', async () => {
  const scope = createRequestScope()
  const reading = scope.run(() => readFileText(new File(['{}'], 'sources.json')))
  const rejected = expect(reading).rejects.toMatchObject({ name: 'CancelError' })
  scope.cancel()
  await rejected
  expect(Reader.opened[0]!.abort).toHaveBeenCalledOnce()
})
