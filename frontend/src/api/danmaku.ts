import { DanmakuBinding, DanmakuBind, DanmakuEpisodes, DanmakuSearch, DanmakuUnbind, DanmakuWindow, DanmakuMatch } from './app'
import { response } from './response'
import { object as record,text as string,integer as checkedInteger } from './decode'

export interface DanmakuItem { id: string; timeMs: number; text: string; mode: number; color: number; fontSize: number }
export interface DanmakuEpisode { provider: string; seriesId: string; episodeId: string; aid: string; title: string; number: string; durationMs: number; pageUrl: string }
export interface DanmakuCandidate { provider: string; ref: string; title: string; year: string; kind: string }
export interface DanmakuBindingValue { localKey: string; episode: DanmakuEpisode; offsetMs: number; confirmed: boolean }
export interface DanmakuWindowValue { items: DanmakuItem[]; startMs: number; endMs: number; provider: string; episodeId: string; cached: boolean; stale: boolean; warnings: string[]; skipped: number }
export interface DanmakuMatchRequest {localKey:string;title:string;year:string;kind:string;episodeLabel:string;durationMs:number;positionMs:number;freshDays:number}
export interface DanmakuMatchAttempt {provider:string;status:string;code:string;retryAt:number;stage?:string;detail?:string;elapsedMs?:number}
export interface DanmakuMatchResult {binding:DanmakuBindingValue|null;window:DanmakuWindowValue|null;attempts:DanmakuMatchAttempt[];traceId?:string}
function integer(value: unknown,min=0,max=86400000):number{return checkedInteger(value,min,max)}
function ordinaryMode(value: unknown): number { const mode = integer(value, 1, 5); if (![1, 4, 5].includes(mode)) throw new Error('Unsupported danmaku mode'); return mode }
function episode(value: unknown): DanmakuEpisode { const v = record(value); return { provider: string(v.provider), seriesId: string(v.seriesId), episodeId: string(v.episodeId), aid: string(v.aid), title: string(v.title), number: string(v.number), durationMs: integer(v.durationMs), pageUrl: string(v.pageUrl) } }
export function decodeBinding(value: unknown): DanmakuBindingValue | null { if (value == null) return null; const v = record(value); if (v.confirmed !== true) throw new Error('Unconfirmed danmaku binding'); return { localKey: string(v.localKey), episode: episode(v.episode), offsetMs: integer(v.offsetMs, -3600000, 3600000), confirmed: true } }
export function decodeWindow(value: unknown): DanmakuWindowValue {
  const v = record(value); if (!Array.isArray(v.items) || v.items.length > 12000 || !Array.isArray(v.warnings) || typeof v.cached !== 'boolean' || typeof v.stale !== 'boolean') throw new Error('Invalid danmaku window')
  const items = v.items.map(value => { const row = record(value); const text = string(row.text); if (text.length > 2000) throw new Error('Oversized danmaku text'); return { id: string(row.id), timeMs: integer(row.timeMs), text, mode: ordinaryMode(row.mode), color: integer(row.color, 0, 0xffffff), fontSize: integer(row.fontSize, 0, 1000) } })
  return { items, startMs: integer(v.startMs, 0, 90000000), endMs: integer(v.endMs, 0, 90000000), provider: string(v.provider), episodeId: string(v.episodeId), cached: v.cached, stale: v.stale, warnings: v.warnings.map(string), skipped: integer(v.skipped, 0, 1000000) }
}
export function decodeMatch(value:unknown):DanmakuMatchResult {
 const v=record(value)
 if(!Array.isArray(v.attempts)||v.attempts.length>3)throw new Error('Invalid match attempts')
 const binding=decodeBinding(v.binding),window=v.window==null?null:decodeWindow(v.window)
 if(!!binding!==!!window||(binding&&window&&(binding.episode.provider!==window.provider||binding.episode.episodeId!==window.episodeId)))throw new Error('Mismatched comment pool')
 const attempts=v.attempts.map(value=>{const row=record(value);const provider=string(row.provider),status=string(row.status)
  if(!['bilibili','qq','iqiyi'].includes(provider)||!['not_used','available','cancelled','no_match','cooling_down','error'].includes(status))throw new Error('Invalid match outcome')
  const stage=row.stage===undefined?'':string(row.stage),detail=row.detail===undefined?'':string(row.detail)
  if(stage.length>32||detail.length>2000)throw new Error('Oversized match diagnostic')
  return {provider,status,code:string(row.code),retryAt:integer(row.retryAt,0,4102444800),...(row.stage===undefined?{}:{stage}),...(row.detail===undefined?{}:{detail}),...(row.elapsedMs===undefined?{}:{elapsedMs:integer(row.elapsedMs,0,3600000)})}
 })
 if(new Set(attempts.map(row=>row.provider)).size!==attempts.length)throw new Error('Duplicate match provider')
 if(binding&&!attempts.some(row=>row.provider===binding.episode.provider&&row.status==='available'))throw new Error('Unverified match provider')
 const traceId=v.traceId===undefined?'':string(v.traceId)
 if(traceId.length>100)throw new Error('Invalid match trace')
 return {binding,window,attempts,...(v.traceId===undefined?{}:{traceId})}
}
export const danmakuAPI = {
  match:(request:DanmakuMatchRequest)=>response(()=>DanmakuMatch(request),decodeMatch),
  binding: (key: string) => response(()=>DanmakuBinding(key),decodeBinding),
  search: (provider: string, query: string) => response(()=>DanmakuSearch(provider,query), values=> { if (!Array.isArray(values)) throw new Error('Invalid candidates'); return values.map(value => { const v = record(value); return { provider: string(v.provider), ref: string(v.ref), title: string(v.title), year: string(v.year), kind: string(v.kind) } }) }),
  episodes: (provider: string, ref: string) => response(()=>DanmakuEpisodes(provider,ref), values=> { if (!Array.isArray(values) || values.length > 5000) throw new Error('Invalid episodes'); return values.map(episode) }),
  bind: (value: DanmakuBindingValue) => response(()=>DanmakuBind(value),value=>value),
  unbind: (key: string) => response(()=>DanmakuUnbind(key),value=>value),
  window: (key: string, positionMs: number, freshDays: number) => response(()=>DanmakuWindow(key,Math.round(positionMs),freshDays),decodeWindow),
}
