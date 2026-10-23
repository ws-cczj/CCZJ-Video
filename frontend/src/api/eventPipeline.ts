import { object,text,number,each } from './decode'

const textFields=['task_id','operation_id','source_key','requestId','keyword','stage','error','message','reason','mode','scope','data_dir','schema_version']
const numberFields=['downloaded','total','speed_bps','current','page']
const known=new Set(['update:download:progress','download:progress','collect:progress','collect:done','collect:log','collect:page','search:progress','app:ready','cache:invalidate','plugin:filedrop'])
type Observation='received'|'malformed'|'coalesced'|'filtered'
const observations=new Map<string,Record<Observation,number>>()
export function recordEvent(mode:string,event:Observation){if(!['video','novel','comic'].includes(mode))return;const row=observations.get(mode)??{received:0,malformed:0,coalesced:0,filtered:0};row[event]++;observations.set(mode,row)}
export function eventSnapshot(mode?:string){const rows=mode?[observations.get(mode)]:[...observations.values()];return rows.reduce<Record<Observation,number>>((total,row)=>{if(row)for(const name of ['received','malformed','coalesced','filtered'] as const)total[name]+=row[name];return total},{received:0,malformed:0,coalesced:0,filtered:0})}
export function decodeEvent(name:string,value:unknown):unknown{
 if(!known.has(name))return value
 if(name==='plugin:filedrop'){each(value,text,256);if(!Array.isArray(value))throw new TypeError('Invalid dropped files');return value}
 const row=object(value)
 for(const key of textFields)if(row[key]!==undefined)text(row[key])
 // HTTP uses -1 for an unknown content length, including update mirror fallback.
 for(const key of numberFields)if(row[key]!==undefined)number(row[key],key==='total'&&(name==='update:download:progress'||name==='download:progress')?-1:0)
 for(const key of ['vod_ids','names'])if(row[key]!==undefined)each(row[key],text,10000)
 if(name==='download:progress')text(row.task_id)
 if(name.startsWith('collect:'))text(row.source_key)
 return value
}

/** Owns scheduled progress delivery. Completion and failure flush pending work
 * before immediate delivery; disposing a subscription discards queued updates. */
export function eventDelivery<T>(name:string,deliver:(value:T)=>void,schedule:(work:()=>void)=>()=>void,accept:(value:T)=>boolean=()=>true,observe:(value:Observation)=>void=()=>{},decode?:(value:unknown)=>T){
 const pending=new Map<string,T>();let stop:(()=>void)|undefined,disposed=false
 function flush(){stop?.();stop=undefined;const batch=[...pending.values()];pending.clear();if(!disposed)for(const value of batch){if(accept(value))deliver(value);else observe('filtered')}}
 function receive(raw:unknown){
  if(disposed)return
  observe('received')
  let value:T;try{value=decode?decode(raw):decodeEvent(name,raw) as T}catch{observe('malformed');return}
  if(!accept(value)){observe('filtered');return}
  const row=value as Record<string,unknown>|null
  const progress=name==='download:progress'||name==='update:download:progress'||name==='collect:progress'
  const terminal=row&&typeof row.status==='string'&&!['queued','downloading'].includes(row.status)
  if(!progress||terminal){flush();deliver(value);return}
  const key=String(row?.task_id??row?.source_key??'')
  if(!pending.has(key)&&pending.size>=128){flush()}
  if(pending.has(key))observe('coalesced')
  pending.set(key,value)
  if(!stop)stop=schedule(()=>{stop=undefined;flush()})
 }
 return {receive,flush,dispose(){disposed=true;stop?.();stop=undefined;pending.clear()}}
}
