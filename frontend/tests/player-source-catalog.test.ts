import { effectScope } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import { useSourceCatalog } from '../src/player/page/useSourceCatalog'

const calls=vi.hoisted(()=>({probe:vi.fn(),search:vi.fn(),global:vi.fn()}))
vi.mock('vue-i18n',()=>({useI18n:()=>({t:(key:string)=>key})}))
vi.mock('../src/api/playback',()=>({ProbePlayLines:calls.probe}))
vi.mock('../src/api/app',()=>({GetGlobalIdForVideo:calls.global}))
vi.mock('../src/player/page/sourceMatching',()=>({
  probeNameOf:(name:string)=>name,findBestMatch:(rows:unknown[])=>rows[0],searchSourceWithFallback:calls.search,
}))

const scopes:ReturnType<typeof effectScope>[]=[]
afterEach(()=>{scopes.splice(0).forEach(scope=>scope.stop());vi.resetAllMocks()})
function setup(remote=false, extra:{source_key:string;enabled:number|boolean}[]=[]) {
  const scope=effectScope();scopes.push(scope)
  calls.global.mockResolvedValue(0)
  return scope.run(()=>useSourceCatalog({sources:()=>[{source_key:'current'},...(remote?[{source_key:'remote'}]:[]),...extra],
    identity:()=>({sourceKey:'current',vodId:'1',name:'Title'}),episode:()=>5}))!
}
const measured=[{index:0,name:'HLS',ep_num:5,ok:true,width:1280,height:720,bandwidth:800000}]

it('retains measured routes during retry and exposes source cooldown without retry storms',async()=>{
  const catalogue=setup()
  calls.probe.mockResolvedValueOnce(measured).mockRejectedValueOnce({code:'RATE_LIMITED',message:'cooldown',retryAfterSeconds:120})
  await catalogue.buildSourceOptions()
  await catalogue.buildSourceOptions()
  expect(catalogue.sourceOptions.value[0]).toMatchObject({lines:measured,qualityState:'limited'})
  await catalogue.buildSourceOptions()
  expect(calls.probe).toHaveBeenCalledTimes(2)
})

it('retries a failed remote discovery instead of caching the failure for ten minutes',async()=>{
  const catalogue=setup(true)
  calls.probe.mockResolvedValue(measured)
  calls.search.mockRejectedValueOnce(new Error('temporary outage')).mockResolvedValueOnce([{vod_id:'remote-id'}])
  await catalogue.buildSourceOptions()
  expect(catalogue.sourceOptions.value[1]?.probe).toBe('failed')
  await catalogue.buildSourceOptions()
  expect(catalogue.sourceOptions.value[1]).toMatchObject({probe:'remote',qualityState:'ready',vod_id:'remote-id'})
  expect(calls.search).toHaveBeenCalledTimes(2)
})

it('does not probe disabled alternatives or restart an active batch on repeated retry clicks',async()=>{
  const catalogue=setup(false,[{source_key:'disabled',enabled:0}])
  let finish!:(value:typeof measured)=>void
  calls.probe.mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
  const pending=catalogue.buildSourceOptions()
  await vi.waitFor(()=>expect(calls.probe).toHaveBeenCalledOnce())
  await catalogue.buildSourceOptions()
  expect(calls.probe).toHaveBeenCalledOnce()
  expect(catalogue.sourceOptions.value.map(row=>row.source_key)).toEqual(['current'])
  expect(catalogue.refreshing.value).toBe(true)
  finish(measured);await pending
  expect(catalogue.refreshing.value).toBe(false)
})

it('keeps a measured resolution when a single route returns its persisted cooldown',async()=>{
  const catalogue=setup()
  calls.probe.mockResolvedValueOnce(measured).mockResolvedValueOnce([{...measured[0],ok:false,width:0,height:0,code:'RATE_LIMITED',retry_after_seconds:301}])
  await catalogue.buildSourceOptions();await catalogue.buildSourceOptions()
  expect(catalogue.sourceOptions.value[0]).toMatchObject({qualityState:'limited',lines:[{ok:true,width:1280,height:720,code:'RATE_LIMITED'}]})
  await catalogue.buildSourceOptions()
  expect(calls.probe).toHaveBeenCalledTimes(2)
})
