import { beforeEach, describe, expect, it, vi } from 'vitest'
import { webcrypto, createHash } from 'node:crypto'
import { createScriptTrust, scriptDigest } from '../src/plugins/scriptTrust'

beforeEach(() => vi.stubGlobal('crypto', webcrypto))

function setup(saved = '') {
  let raw = saved
  const ask = vi.fn(async () => true)
  const save = vi.fn(async (next: string) => { raw = next })
  const trust = createScriptTrust({ load: async () => raw, save, ask })
  return { trust, ask, save, raw: () => raw }
}

describe('script execution trust', () => {
  it('handles package IDs that coincide with Object prototype names', async () => {
    const { trust, ask, raw } = setup()
    const digest = await scriptDigest(['script'])
    expect(await trust.requireTrust('constructor', 'Constructor', digest)).toBe(true)
    expect(ask).toHaveBeenCalledOnce()
    const restarted = setup(raw())
    expect(await restarted.trust.requireTrust('constructor', 'Constructor', digest, '', false)).toBe(true)
    expect(restarted.ask).not.toHaveBeenCalled()
  })
  it('never prompts or grants new scripts during startup, then asks on enabling', async () => {
    const { trust, ask, save } = setup()
    const digest = await scriptDigest(['script'])
    expect(await trust.requireTrust('new', 'New', digest, '', false)).toBe(false)
    expect(ask).not.toHaveBeenCalled()
    expect(save).not.toHaveBeenCalled()
    expect(await trust.requireTrust('new', 'New', digest)).toBe(true)
    expect(await trust.requireTrust('new', 'New', digest, '', false)).toBe(true)
    expect(ask).toHaveBeenCalledOnce()
  })
  it('remembers consent for the package across updates and restarts', async () => {
    const { trust, ask, raw } = setup()
    const first = await scriptDigest(['manifest', 'style', 'script'])
    const changed = await scriptDigest(['manifest', 'style', 'changed script'])
    expect(await trust.requireTrust('pack', 'Pack', first)).toBe(true)
    expect(await trust.requireTrust('pack', 'Pack', first)).toBe(true)
    expect(ask).toHaveBeenCalledOnce()
    expect(await trust.requireTrust('pack', 'Pack', changed)).toBe(true)
    expect(ask).toHaveBeenCalledOnce()
    const restarted = setup(raw())
    expect(await restarted.trust.requireTrust('pack', 'Pack', changed)).toBe(true)
    expect(restarted.ask).not.toHaveBeenCalled()
  })

  it('remembers denial across restarts until revoked', async () => {
    const { trust, ask, raw } = setup()
    const digest = await scriptDigest(['script'])
    ask.mockResolvedValue(false)
    expect(await trust.requireTrust('pack', 'Pack', digest)).toBe(false)
    const restarted = setup(raw())
    expect(await restarted.trust.requireTrust('pack', 'Pack', digest)).toBe(false)
    expect(restarted.ask).not.toHaveBeenCalled()
    await restarted.trust.revoke('pack')
    expect(await restarted.trust.requireTrust('pack', 'Pack', digest)).toBe(true)
    expect(restarted.ask).toHaveBeenCalledOnce()
  })

  it('only trusts exact embedded bytes, including builtin manifest and styles', async () => {
    const { trust, ask } = setup()
    const embedded = await scriptDigest(['manifest', 'style', 'script'])
    expect(await trust.requireTrust('logs-panel', 'Logs', embedded, embedded)).toBe(true)
    expect(ask).not.toHaveBeenCalled()
    const modified = await scriptDigest(['manifest', 'malicious style', 'script'])
    await trust.requireTrust('logs-panel', 'Logs', modified, embedded)
    expect(ask).toHaveBeenCalledOnce()
  })

  it('does not grant execution when consent cannot be saved', async () => {
    const { trust, save } = setup()
    const digest = await scriptDigest(['script'])
    save.mockRejectedValue(new Error('disk full'))
    await expect(trust.requireTrust('pack', 'Pack', digest)).rejects.toThrow('disk full')
    expect(trust.records.value.pack).toBeUndefined()
  })

  it('queues concurrent prompts and persists every decision without losing entries', async () => {
    const { trust, ask, raw } = setup()
    const digest = await scriptDigest(['script'])
    await Promise.all([trust.requireTrust('one', 'One', digest), trust.requireTrust('two', 'Two', digest)])
    expect(ask.mock.calls.map(call => call[0])).toEqual(['one', 'two'])
    expect(Object.keys(JSON.parse(raw()))).toEqual(['one', 'two'])
  })

  it.each(['{broken', '{"pack":{"digest":"bad","allowed":true}}'])('never restores malformed consent: %s', async raw => {
    const { trust, ask } = setup(raw)
    await trust.requireTrust('pack', 'Pack', await scriptDigest(['script']))
    expect(ask).toHaveBeenCalledOnce()
  })

  it('uses the same UTF-8 SHA-256 framing as the backend', async () => {
    const parts = ['声明', '样式', '脚本']
    const framed = parts.map(part => `${Buffer.byteLength(part, 'utf8')}:${part}`).join('')
    expect(await scriptDigest(parts)).toBe(createHash('sha256').update(framed, 'utf8').digest('hex'))
  })

  it('preserves file boundaries even if files contain NUL bytes', async () => {
    expect(await scriptDigest(['a\0b', 'c'])).not.toBe(await scriptDigest(['a', 'b\0c']))
  })
})
