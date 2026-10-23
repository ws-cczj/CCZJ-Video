import {expect,it} from 'vitest'
import {measuredPageCount,pageFromRatio,ratioFromPage} from '../src/novel/reader/pagination'
import {normalizeReadingPreferences} from '../src/novel/domain'
it('counts measured columns without a blank last page at fractional widths',()=>{
 expect(measuredPageCount(306,306,40)).toBe(1)
 expect(measuredPageCount(998,306,40)).toBe(3)
 expect(measuredPageCount(998.4,306,40)).toBe(3)
 expect(measuredPageCount(1344,306,40)).toBe(4)
 expect(measuredPageCount(0,0,40)).toBe(1)
})
it('restores relative progress after font and viewport changes and clamps corrupt positions',()=>{
 const ratio=ratioFromPage(4,9);expect(pageFromRatio(ratio,17)).toBe(8)
 expect(ratioFromPage(0,1)).toBe(0);expect(pageFromRatio(NaN,9)).toBe(0);expect(pageFromRatio(8,9)).toBe(8)
 expect(ratioFromPage(-8,9)).toBe(0);expect(ratioFromPage(100,9)).toBe(1)
})
it('migrates the original reader preferences and refuses unsupported layout values',()=>{
 expect(normalizeReadingPreferences({size:22,theme:'night',serif:false})).toMatchObject({size:22,theme:'night',serif:false,flow:'scroll',leading:1.85,margin:24,systemBrightness:true})
 expect(normalizeReadingPreferences({size:NaN,leading:20,margin:-1,brightness:NaN,flow:'invalid'})).toMatchObject({size:20,leading:1.85,margin:24,brightness:.8,flow:'scroll'})
 expect(normalizeReadingPreferences({flow:'paged'}).flow).toBe('paged')
})
