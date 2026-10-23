import { expect, it } from 'vitest'
import { decodeSourceSettings, sourceEnabled } from '../src/player/danmaku/sources'

it('preserves the built-in defaults and selects another enabled default',()=>{
 expect(decodeSourceSettings('')).toEqual({defaultProvider:'bilibili',disabled:[]})
 const value=decodeSourceSettings('{"defaultProvider":"qq","disabled":["qq","qq"]}')
 expect(value).toEqual({defaultProvider:'bilibili',disabled:['qq']});expect(sourceEnabled(value,'qq')).toBe(false)
 expect(decodeSourceSettings('{"disabled":["bilibili","qq","iqiyi"]}').defaultProvider).toBe('')
})

it('rejects malformed settings rather than silently re-enabling sources',()=>{
 for(const value of ['null','[]','{"disabled":null}','{"defaultProvider":null}','{"defaultProvider":"external"}','{"disabled":["external"]}']) expect(()=>decodeSourceSettings(value)).toThrow()
})
