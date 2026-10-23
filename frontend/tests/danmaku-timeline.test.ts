import { expect, it } from 'vitest'
import { DanmakuTimeline } from '../src/player/danmaku/timeline'
import { decodeDanmakuPreferences } from '../src/player/danmaku/preferences'
import type { DanmakuItem } from '../src/api/danmaku'
const item=(id:string,timeMs:number,mode=1):DanmakuItem=>({id,timeMs,mode,text:id,color:0xffffff,fontSize:25})
const preferences=()=>decodeDanmakuPreferences(null)
it('uses media time, preserves visible comments during a window refresh, and skips old comments on seek',()=>{
 const line=new DanmakuTimeline(()=>100),p=preferences()
 line.setItems([item('a',0),item('b',5000)],0)
 expect(line.tick(0,600,400,p)[0]?.x).toBe(600)
 expect(line.tick(500,600,400,p)[0]?.x).toBe(550)
 line.setItems([item('a',0),item('b',5000),item('c',20000)],0)
 const visible=line.tick(500,600,400,p)
 expect(visible).toHaveLength(1);expect(visible[0]?.x).toBe(550)
 expect(line.tick(20000,600,400,p).map(row=>row.item.id)).toEqual(['c'])
 expect(line.tick(100,600,400,p).map(row=>row.item.id)).toEqual(['a'])
})
it('applies delay once and bounds lanes under a dense timestamp',()=>{
 const line=new DanmakuTimeline(()=>100),p=preferences();p.rows=2
 line.setItems(Array.from({length:2000},(_,i)=>item(String(i),1000)),5000)
 expect(line.tick(5900,600,400,p)).toEqual([])
 expect(line.tick(6000,600,400,p)).toHaveLength(2)
 p.enabled=false;expect(line.tick(6100,600,400,p)).toEqual([])
})
it('clamps persisted cache and rendering preferences at runtime',()=>{
 const p=decodeDanmakuPreferences(JSON.stringify({freshDays:0,opacity:4,rows:100,speed:-1}))
 expect(p.freshDays).toBe(1);expect(p.opacity).toBe(1);expect(p.rows).toBe(12);expect(p.speed).toBe(50)
 expect(decodeDanmakuPreferences('{bad')).toEqual(preferences())
})
it('applies rolling, fixed and colored filters without rewriting source items',()=>{
 const line=new DanmakuTimeline(()=>100),p=preferences()
 const rows=[item('rolling',0),item('fixed',0,5),{...item('colored',0,4),color:0xff0000}]
 line.setItems(rows,0);expect(line.tick(0,600,400,p)).toHaveLength(3)
 p.scrolling=false;p.colored=false
 expect(line.tick(1,600,400,p).map(row=>row.item.id)).toEqual(['fixed'])
 expect(rows[2].color).toBe(0xff0000)
})
