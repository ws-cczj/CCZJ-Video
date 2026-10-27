import { expect, it, vi } from 'vitest'
import { backdropEdgeColor } from '../src/platform/backdropSystemBars'
vi.mock('../src/stores/theme',()=>({hslToHex:vi.fn()}))


it('uses the visible crop rather than the entire wallpaper average',()=>{
  const image={width:5,height:2,data:new Uint8ClampedArray([
    255,0,0,255, 0,255,0,255, 0,0,255,255, 255,255,255,255, 0,0,0,255,
    10,10,10,255, 20,20,20,255, 30,30,30,255, 40,40,40,255, 50,50,50,255,
  ])}
  expect(backdropEdgeColor(image,{width:500,height:200,viewWidth:100,x:-200,y:0},[])).toBe('#0000ff')
  expect(backdropEdgeColor(image,{width:500,height:200,viewWidth:100,x:-200,y:-100},[])).toBe('#1e1e1e')
  expect(backdropEdgeColor(image,{width:500,height:200,viewWidth:100,x:-400,y:0},[255,255,255,.5])).toBe('#808080')
})
it('composites transparent wallpaper over the actual theme canvas',()=>{
  expect(backdropEdgeColor({width:1,height:1,data:new Uint8ClampedArray([0,0,0,0])},{width:100,height:100,viewWidth:100,x:0,y:0,background:'#102030'},[])).toBe('#102030')
})
