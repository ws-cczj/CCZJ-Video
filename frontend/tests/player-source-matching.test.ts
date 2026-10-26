import { expect, it, vi } from 'vitest'
vi.mock('../src/api/app',()=>({}))
import { findBestMatch } from '../src/player/page/sourceMatching'

it('never treats companion documentaries or a different season as a playable copy',()=>{
  const companion={vod_id:'34189',vod_name:'绝命毒师 第四季 幕后纪实'}
  const otherSeason={vod_id:'2',vod_name:'绝命毒师第二季'}
  expect(findBestMatch([companion,otherSeason],'绝命毒师第四季')).toBeNull()
  const correct={vod_id:'4',vod_name:'绝命毒师 第四季'}
  expect(findBestMatch([companion,correct],'绝命毒师第四季')).toBe(correct)
})

it('accepts differences in whitespace and release-quality suffixes',()=>{
  const row={vod_id:'4',vod_name:'绝命毒师 第四季_1080P_'}
  expect(findBestMatch([row],'绝命毒师第四季')).toBe(row)
})
