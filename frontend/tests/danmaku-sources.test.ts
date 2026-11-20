import { expect, it } from 'vitest'
import { decodeSourceSettings, sourceEnabled } from '../src/player/danmaku/sources'
import { builtinDanmakuSources } from '../src/constants/danmakuSources'

it('preserves the built-in defaults and selects another enabled default',()=>{
 expect(decodeSourceSettings('')).toEqual({defaultProvider:'bilibili',disabled:[]})
 const value=decodeSourceSettings('{"defaultProvider":"qq","disabled":["qq","qq"]}')
 expect(value).toEqual({defaultProvider:'bilibili',disabled:['qq']});expect(sourceEnabled(value,'qq')).toBe(false)
 expect(decodeSourceSettings(JSON.stringify({ disabled: builtinDanmakuSources.map(source => source.id) })).defaultProvider).toBe('')
})

it('rejects malformed settings rather than silently re-enabling sources',()=>{
 for(const value of ['null','[]','{"disabled":null}','{"defaultProvider":null}','{"defaultProvider":"external"}','{"disabled":["external"]}']) expect(()=>decodeSourceSettings(value)).toThrow()
})
