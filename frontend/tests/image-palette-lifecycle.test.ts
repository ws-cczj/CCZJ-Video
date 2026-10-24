import { afterEach, expect, it, vi } from 'vitest'
import { pickColorFromImage } from '../src/utils/imagePalette'

vi.mock('../src/stores/theme', () => ({ hslToHex: vi.fn() }))
afterEach(() => vi.unstubAllGlobals())

it('cancels an unfinished image decoder and releases its source', async () => {
  let image: { src: string; decode: () => Promise<void> } | undefined
  vi.stubGlobal('Image', class {
    src = ''
    decode = () => new Promise<void>(() => {})
    constructor() { image = this }
  })
  const controller = new AbortController()
  const reading = pickColorFromImage('https://example.invalid/wallpaper.png', controller.signal)
  expect(image?.src).toBe('https://example.invalid/wallpaper.png')
  controller.abort()
  expect(await reading).toBeNull()
  expect(image?.src).toBe('')
})

it('does not allocate an image for an already closed editor', async () => {
  const create = vi.fn()
  vi.stubGlobal('Image', create)
  const controller = new AbortController()
  controller.abort()
  expect(await pickColorFromImage('wallpaper.png', controller.signal)).toBeNull()
  expect(create).not.toHaveBeenCalled()
})
