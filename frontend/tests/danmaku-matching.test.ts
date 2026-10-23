import { describe, expect, it } from 'vitest'
import { episodeNumber, matchEpisode, normalizeWorkTitle } from '../src/player/danmaku/matching'
import type { DanmakuEpisode } from '../src/api/danmaku'
const episode = (number: string, title='正片', durationMs=1200000): DanmakuEpisode => ({provider:'bilibili',seriesId:'1',episodeId:number,aid:'',number,title,durationMs,pageUrl:''})
describe('normalized danmaku matching',()=>{
 it('keeps request identity stable despite punctuation and quality metadata',()=>{
  expect(normalizeWorkTitle('《凡人修仙传》【1080P】')).toBe(normalizeWorkTitle('凡人修仙传'))
 })
 it('preserves distinct season identities in request fingerprints',()=>{
  expect(normalizeWorkTitle('凡人修仙传第二季')).not.toBe(normalizeWorkTitle('凡人修仙传'))
  expect(normalizeWorkTitle('作品第2季')).toBe(normalizeWorkTitle('作品第二季'))
 })
 it('uses declared episode numbers instead of catalog positions',()=>{
  const target=episode('197')
  expect(matchEpisode([target,episode('1')],'第197集',1200)).toBe(target)
  expect(episodeNumber('第一百九十七集')).toBe(197)
  expect(episodeNumber('EP. 02')).toBe(2)
  expect(matchEpisode([episode('2')],'第01集')).toBeNull()
 })
 it('excludes previews but accepts a remaster or different duration with the same number',()=>{
  expect(matchEpisode([episode('1','第1集 预告')],'第01集')).toBeNull()
  const remaster=episode('1','1 凡人风起天南1重制版',1800000)
  expect(matchEpisode([remaster],'第01集',1200)).toBe(remaster)
  const original={...episode('1'),episodeId:'original'}
  expect(matchEpisode([remaster,original],'第01集',1200)).toBe(original)
  const duplicate={...episode('1'),episodeId:'another'}
  expect(matchEpisode([duplicate,original],'第01集',1200)).toBe(duplicate)
  expect(matchEpisode([original,duplicate],'第01集',1200)).toBe(duplicate)
 })
})
