import { createRenderer, h, nextTick, ref } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
const mocks=vi.hoisted(()=>({get:vi.fn(),set:vi.fn(()=>Promise.resolve()),binding:vi.fn(),window:vi.fn(),search:vi.fn(),episodes:vi.fn(),bind:vi.fn(),unbind:vi.fn(),match:vi.fn()}))
vi.mock('../src/api/app',async()=>({...await vi.importActual('../src/api/errors'),GetSetting:mocks.get,SetSetting:mocks.set}))
vi.mock('../src/api/danmaku',()=>({danmakuAPI:mocks}))
import { useDanmaku } from '../src/player/danmaku/useDanmaku'
type Node={parent:Node|null;children:Node[]}
const node=():Node=>({parent:null,children:[]})
const renderer=createRenderer<Node,Node>({createElement:node,createText:node,createComment:node,insert:(child,parent)=>{parent.children.push(child);child.parent=parent},remove:()=>{},setText:()=>{},setElementText:()=>{},parentNode:child=>child.parent,nextSibling:()=>null,patchProp:()=>{}})
let unmount:(()=>void)|undefined
function mount(current=()=>0){let state!:ReturnType<typeof useDanmaku>;const app=renderer.createApp({setup(){state=useDanmaku({key:()=> 'video:source:episode',current});return()=>h('div')}});app.mount(node());unmount=()=>app.unmount();return state}
function mountAuto(key=ref('video:source:episode'),current=ref(0),year=ref('2023')){let state!:ReturnType<typeof useDanmaku>;const app=renderer.createApp({setup(){state=useDanmaku({key:()=>key.value,current:()=>current.value,title:()=> 'sample',year:()=>year.value,kind:()=> '电视剧',episode:()=> '第01集',duration:()=>120});return()=>h('div')}});app.mount(node());unmount=()=>app.unmount();return state}
afterEach(()=>{unmount?.();unmount=undefined;vi.clearAllMocks()})
const binding={localKey:'video:source:episode',confirmed:true,offsetMs:0,episode:{provider:'qq',episodeId:'c4102g9a01t',aid:'',seriesId:'',title:'sample',number:'1',durationMs:120000,pageUrl:''}}
it('waits for persisted disabled preferences before fetching, and never saves untouched defaults',async()=>{
 let finish!:(value:string)=>void;mocks.get.mockReturnValue(new Promise(resolve=>{finish=resolve}));mocks.binding.mockResolvedValue(binding)
 const state=mount();await nextTick();await nextTick();expect(mocks.window).not.toHaveBeenCalled()
 finish('{"enabled":false}');await vi.waitFor(()=>expect(state.preferencesReady.value).toBe(true));expect(mocks.window).not.toHaveBeenCalled()
 unmount?.();unmount=undefined;expect(mocks.set).not.toHaveBeenCalled()
})
it('closing discovery cancels its actual request and rejects a late result',async()=>{
 mocks.get.mockResolvedValue('{"enabled":false}');mocks.binding.mockResolvedValue(null)
 let finish!:(value:unknown[])=>void;const cancel=vi.fn();const pending=Object.assign(new Promise<unknown[]>(resolve=>{finish=resolve}),{cancel});mocks.search.mockReturnValue(pending)
 const state=mount();await vi.waitFor(()=>expect(state.preferencesReady.value).toBe(true))
 const done=state.search('sample');state.cancelDiscovery();expect(cancel).toHaveBeenCalledOnce()
 finish([{provider:'qq',ref:'old',title:'old',year:'',kind:''}]);await done;expect(state.candidates.value).toEqual([])
})

const emptyWindow={provider:'qq',episodeId:'c4102g9a01t',items:[],startMs:0,endMs:75000,cached:true,stale:false,warnings:[],skipped:0}
function persistedSources(raw='') { mocks.get.mockImplementation((key:string)=>Promise.resolve(key==='danmaku_sources_v1'?raw:''));mocks.binding.mockResolvedValue(binding) }

it('does not fetch a disabled bound source, while preserving its binding',async()=>{
 persistedSources('{"defaultProvider":"bilibili","disabled":["qq"]}')
 const state=mount();await vi.waitFor(()=>expect(state.status.value).toBe('sourceDisabled'))
 expect(state.binding.value).toEqual(binding);expect(mocks.window).not.toHaveBeenCalled()
})

it('reuses the covered window across playback ticks and distinguishes empty content from missing segments',async()=>{
 persistedSources();mocks.window.mockResolvedValue(emptyWindow)
 const current=ref(0),state=mount(()=>current.value)
 await vi.waitFor(()=>expect(state.status.value).toBe('empty'))
 for(const time of [1,5,10,30]){current.value=time;await nextTick()}
 expect(mocks.window).toHaveBeenCalledOnce()
 mocks.window.mockResolvedValue({...emptyWindow,warnings:['segment_not_found']});state.retry()
 await vi.waitFor(()=>expect(state.status.value).toBe('missingSegment'))
})

it('cancels content reads during explicit discovery and ignores their late result',async()=>{
 persistedSources()
 let finishWindow!:(value:typeof emptyWindow)=>void,finishSearch!:(value:unknown[])=>void
 const cancel=vi.fn();mocks.window.mockReturnValueOnce(Object.assign(new Promise<typeof emptyWindow>(resolve=>{finishWindow=resolve}),{cancel})).mockResolvedValue(emptyWindow)
 mocks.search.mockReturnValue(new Promise<unknown[]>(resolve=>{finishSearch=resolve}))
 const current=ref(0),state=mount(()=>current.value)
 await vi.waitFor(()=>expect(mocks.window).toHaveBeenCalledOnce())
 const done=state.search('sample');expect(cancel).toHaveBeenCalledOnce()
 await state.search('sample');expect(mocks.search).toHaveBeenCalledOnce()
 current.value=25;await nextTick();expect(mocks.window).toHaveBeenCalledOnce()
 finishWindow({...emptyWindow,items:[]});await nextTick();expect(state.window.value).toBeNull()
 finishSearch([]);await done;expect(state.discoveryResult.value).toBe('searchEmpty')
 await vi.waitFor(()=>expect(mocks.window).toHaveBeenCalledTimes(2))
})

it('allows retrying a failed binding read instead of reporting no source forever',async()=>{
 persistedSources();mocks.binding.mockRejectedValueOnce({code:'STORAGE',message:'read failed'}).mockResolvedValue(binding);mocks.window.mockResolvedValue(emptyWindow)
 const state=mount();await vi.waitFor(()=>expect(state.status.value).toBe('error'))
 state.retry();await vi.waitFor(()=>expect(state.status.value).toBe('empty'))
 expect(mocks.binding).toHaveBeenCalledTimes(2)
})

it('does not let manual retry or playback ticks bypass a backend cooldown',async()=>{
 persistedSources();mocks.window.mockRejectedValueOnce({code:'UNAVAILABLE',message:'offline',retryAfterSeconds:330}).mockResolvedValue(emptyWindow)
 const current=ref(0),state=mount(()=>current.value)
 await vi.waitFor(()=>expect(state.status.value).toBe('error'))
 const clock=vi.spyOn(Date,'now').mockReturnValue(state.cooldownUntil.value-1)
 state.retry();current.value=1;await nextTick();expect(mocks.window).toHaveBeenCalledOnce()
 clock.mockReturnValue(state.cooldownUntil.value+1);state.retry()
 await nextTick();await nextTick();expect(mocks.window).toHaveBeenCalledTimes(2)
 clock.mockRestore()
})

it('does not block a seek to another window behind a failed window cooldown',async()=>{
 persistedSources();mocks.window.mockRejectedValueOnce({code:'UNAVAILABLE',message:'offline',retryAfterSeconds:330}).mockResolvedValue({...emptyWindow,startMs:80000,endMs:165000})
 const current=ref(0),state=mount(()=>current.value)
 await vi.waitFor(()=>expect(state.status.value).toBe('error'))
 current.value=90;await nextTick();await nextTick();expect(mocks.window).toHaveBeenCalledTimes(2)
})

function autoSources(){
 mocks.get.mockImplementation((key:string)=>Promise.resolve(key==='danmaku_sources_v1'?'{"defaultProvider":"qq","disabled":[]}':''))
 mocks.binding.mockResolvedValue(null);mocks.bind.mockResolvedValue(undefined);mocks.window.mockResolvedValue(emptyWindow)
 mocks.episodes.mockResolvedValue([binding.episode])
 mocks.match.mockResolvedValue({binding,window:emptyWindow,attempts:[{provider:'qq',status:'available',code:'',retryAt:0}]})
}
it('delegates metadata and current episode to one cancellable backend match, reusing its first window',async()=>{
 autoSources()
 const state=mountAuto();await vi.waitFor(()=>expect(state.status.value).toBe('empty'))
 expect(mocks.match).toHaveBeenCalledExactlyOnceWith({localKey:binding.localKey,title:'sample',year:'2023',kind:'电视剧',episodeLabel:'第01集',durationMs:120000,positionMs:0,freshDays:1})
 expect(mocks.search).not.toHaveBeenCalled();expect(mocks.episodes).not.toHaveBeenCalled();expect(mocks.bind).not.toHaveBeenCalled();expect(mocks.window).not.toHaveBeenCalled()
})
it('keeps the canvas unbound when all providers fail and retains their cooldown details',async()=>{
 autoSources();const attempts=[{provider:'qq',status:'cooling_down',code:'NO_WORK_MATCH',retryAt:1792510000}]
 mocks.match.mockResolvedValue({binding:null,window:null,attempts})
 const state=mountAuto();await vi.waitFor(()=>expect(state.status.value).toBe('noMatch'))
 state.preferences.value.opacity=.5;await nextTick()
 expect(mocks.match).toHaveBeenCalledOnce();expect(state.binding.value).toBeNull();expect(state.window.value).toBeNull();expect(state.attempts.value).toEqual(attempts)
 expect(mocks.search).not.toHaveBeenCalled();expect(mocks.episodes).not.toHaveBeenCalled();expect(mocks.bind).not.toHaveBeenCalled()
})
it('does not schedule matching after the one-day cooldown, even while playback ticks continue',async()=>{
 autoSources()
 const attempts=[{provider:'qq',status:'cooling_down',code:'NO_WORK_MATCH',retryAt:Math.floor(Date.now()/1000)+86400}]
 mocks.match.mockResolvedValue({binding:null,window:null,attempts})
 const current=ref(0),state=mountAuto(ref(binding.localKey),current)
 await vi.waitFor(()=>expect(state.status.value).toBe('noMatch'))
 vi.useFakeTimers()
 try{
  await vi.advanceTimersByTimeAsync(86401000)
  current.value=120;await nextTick()
  expect(mocks.match).toHaveBeenCalledOnce();expect(mocks.window).not.toHaveBeenCalled()
  state.preferences.value.enabled=false;await nextTick()
  state.preferences.value.enabled=true
  await vi.advanceTimersByTimeAsync(0)
  expect(mocks.match).toHaveBeenCalledTimes(2)
 }finally{vi.useRealTimers()}
})
it('does not publish a previous binding before the backend evaluates default and fallback priority',async()=>{
 autoSources();mocks.binding.mockResolvedValue({...binding,episode:{...binding.episode,number:'2'}})
 mocks.search.mockResolvedValue([{provider:'qq',ref:'cid:sample',title:'sample',year:'',kind:'series'}])
 const state=mountAuto();await vi.waitFor(()=>expect(state.binding.value?.episode.number).toBe('1'))
 expect(mocks.binding).not.toHaveBeenCalled();expect(mocks.match).toHaveBeenCalledOnce()
})
it('settles disabled availability without searching when every source is off',async()=>{
 autoSources();mocks.get.mockImplementation((key:string)=>Promise.resolve(key==='danmaku_sources_v1'?'{"defaultProvider":"","disabled":["qq","bilibili","iqiyi"]}':''))
 const state=mountAuto();await vi.waitFor(()=>expect(state.status.value).toBe('sourceDisabled'))
 expect(mocks.match).not.toHaveBeenCalled();expect(mocks.search).not.toHaveBeenCalled();expect(mocks.window).not.toHaveBeenCalled()
})
it('cancels automatic discovery when changing media and ignores the obsolete response',async()=>{
 autoSources();let finish!:(value:unknown)=>void;const cancel=vi.fn()
 const response={binding:{...binding,localKey:'second'},window:emptyWindow,attempts:[]}
 mocks.match.mockReturnValueOnce(Object.assign(new Promise<unknown>(resolve=>finish=resolve),{cancel})).mockResolvedValue(response)
 const key=ref('first'),state=mountAuto(key)
 await vi.waitFor(()=>expect(mocks.match).toHaveBeenCalledOnce())
 key.value='second';await vi.waitFor(()=>expect(state.binding.value?.localKey).toBe('second'))
 finish({...response,binding:{...binding,localKey:'first'}});await nextTick();await nextTick()
 expect(cancel).toHaveBeenCalled();expect(mocks.match).toHaveBeenCalledTimes(2);expect(state.binding.value?.localKey).toBe('second')
})
it('re-evaluates fallback after a later content failure without duplicating the initial window',async()=>{
 autoSources();const current=ref(0),state=mountAuto(ref(binding.localKey),current)
 await vi.waitFor(()=>expect(state.status.value).toBe('empty'))
 const alternate={...binding,episode:{...binding.episode,provider:'iqiyi',episodeId:'12345'}}
 mocks.window.mockRejectedValueOnce({code:'UNAVAILABLE',message:'offline'})
 mocks.match.mockResolvedValueOnce({binding:alternate,window:{...emptyWindow,provider:'iqiyi',episodeId:'12345',startMs:80000,endMs:165000},attempts:[]})
 current.value=90;await vi.waitFor(()=>expect(state.binding.value?.episode.provider).toBe('iqiyi'))
 expect(mocks.match).toHaveBeenCalledTimes(2);expect(mocks.window).toHaveBeenCalledOnce()
})
it('restarts matching when late metadata disambiguates the work and rejects the earlier response',async()=>{
 autoSources();let finish!:(value:unknown)=>void;const cancel=vi.fn()
 mocks.match.mockReturnValueOnce(Object.assign(new Promise<unknown>(resolve=>finish=resolve),{cancel})).mockResolvedValue({binding:null,window:null,attempts:[]})
 const year=ref(''),state=mountAuto(ref(binding.localKey),ref(0),year)
 await vi.waitFor(()=>expect(mocks.match).toHaveBeenCalledOnce())
 year.value='2023'
 await vi.waitFor(()=>expect(mocks.match).toHaveBeenCalledTimes(2))
 finish({binding,window:emptyWindow,attempts:[]});await nextTick();await nextTick()
 expect(cancel).toHaveBeenCalledOnce();expect(state.binding.value).toBeNull();expect(state.status.value).toBe('noMatch')
})
it('re-evaluates fallback when a later window reports a missing segment',async()=>{
 autoSources();const current=ref(0),state=mountAuto(ref(binding.localKey),current)
 await vi.waitFor(()=>expect(state.status.value).toBe('empty'))
 mocks.window.mockResolvedValueOnce({...emptyWindow,warnings:['segment_not_found']})
 mocks.match.mockResolvedValueOnce({binding:null,window:null,attempts:[]})
 current.value=90
 await vi.waitFor(()=>expect(state.status.value).toBe('noMatch'))
 expect(mocks.match).toHaveBeenCalledTimes(2);expect(state.window.value).toBeNull()
})
it('enabling comments repeats matching without starting a redundant window read',async()=>{
 autoSources();const state=mountAuto()
 await vi.waitFor(()=>expect(state.status.value).toBe('empty'))
 state.preferences.value.enabled=false;await nextTick()
 expect(state.window.value).toBeNull()
 state.preferences.value.enabled=true
 await vi.waitFor(()=>expect(mocks.match).toHaveBeenCalledTimes(2))
 expect(mocks.window).not.toHaveBeenCalled()
})
