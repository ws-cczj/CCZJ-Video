import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { useSubtitles } from '../src/player/useSubtitles'
import { findActiveCue, parseSubtitle } from '../src/utils/subtitles'

vi.mock('../src/locales', () => ({ tr: (key: string) => key }))

const text = (word: string) => `1\n00:00:00,000 --> 00:00:10,000\n${word}`
function deferred() {
  let resolve!: (text: string) => void
  let reject!: (error: Error) => void
  const promise = new Promise<string>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function choose(name: string, read: () => Promise<string>, size = 100): Event {
  return { target: { value: name, files: [{ name, size, text: read }] } } as unknown as Event
}
const scopes: ReturnType<typeof effectScope>[] = []
function setup() {
  const scope = effectScope()
  scopes.push(scope)
  const subtitles = scope.run(() => useSubtitles({ current: ref(1), fileInput: ref(null) }))!
  return { scope, subtitles }
}
afterEach(() => { scopes.splice(0).forEach(scope => scope.stop()) })

describe('local subtitle input boundaries', () => {
  it('keeps the most recently selected file when reads finish out of order', async () => {
    const { subtitles } = setup()
    const old = deferred()
    const pending = subtitles.onSubtitleFileChosen(choose('old.srt', () => old.promise))
    await subtitles.onSubtitleFileChosen(choose('new.srt', async () => text('new')))
    old.resolve(text('old'))
    await pending
    expect(subtitles.subtitleName.value).toBe('new.srt')
    expect(subtitles.activeSubtitle.value).toBe('new')
  })
  it('does not resurrect subtitles cleared during a read or episode switch', async () => {
    const { subtitles } = setup()
    const read = deferred()
    const pending = subtitles.onSubtitleFileChosen(choose('old.srt', () => read.promise))
    subtitles.clearSubtitle()
    read.resolve(text('old'))
    await pending
    expect(subtitles.subtitleCues.value).toHaveLength(0)
  })
  it('ignores a late error belonging to an earlier file', async () => {
    const { subtitles } = setup()
    const read = deferred()
    const pending = subtitles.onSubtitleFileChosen(choose('old.srt', () => read.promise))
    await subtitles.onSubtitleFileChosen(choose('new.srt', async () => text('new')))
    read.reject(new Error('read failed'))
    await pending
    expect(subtitles.subtitleError.value).toBe('')
  })
  it('discards file results after the player scope is disposed', async () => {
    const { subtitles, scope } = setup()
    const read = deferred()
    const pending = subtitles.onSubtitleFileChosen(choose('old.srt', () => read.promise))
    scope.stop()
    read.resolve(text('old'))
    await pending
    expect(subtitles.subtitleCues.value).toHaveLength(0)
  })
  it('rejects an oversized local file before allocating its text', async () => {
    const { subtitles } = setup()
    const read = vi.fn(async () => text('huge'))
    await subtitles.onSubtitleFileChosen(choose('huge.srt', read, 100 * 1024 * 1024))
    expect(read).not.toHaveBeenCalled()
    expect(subtitles.subtitleError.value).not.toBe('')
  })
  it('renders markup as plain subtitle content', () => {
    const cues = parseSubtitle(text('<img src=x onerror=alert(1)><b>safe</b>'))
    expect(cues[0].text).toBe('safe')
  })
  it('finds a long cue after several shorter overlapping cues have ended', () => {
    const cues = [{ start: 0, end: 100, text: 'long' }, ...[1, 2, 3, 4].map(start => ({ start, end: start + 1, text: 'short' }))]
    expect(findActiveCue(cues, 20)?.text).toBe('long')
  })
})
