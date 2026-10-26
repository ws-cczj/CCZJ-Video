import { ProbePlayLines as probe } from './app'
import { boolean, integer, list, object, text } from './decode'
import { response } from './response'

export interface PlayLineQuality {
  index:number; name:string; ep_num:number; ok:boolean; width:number; height:number; bandwidth:number;code?:string;retry_after_seconds?:number
}

export function ProbePlayLines(request:Parameters<typeof probe>[0]) {
  return response(()=>probe(request),value=>list(value,item=>{
    const row=object(item)
    return {index:integer(row.index),name:text(row.name),ep_num:integer(row.ep_num),ok:boolean(row.ok),
      width:integer(row.width,0,32768),height:integer(row.height,0,32768),bandwidth:integer(row.bandwidth),
      code:row.code===undefined?undefined:text(row.code),retry_after_seconds:row.retry_after_seconds===undefined?undefined:integer(row.retry_after_seconds,0,86400)} satisfies PlayLineQuality
  },32))
}
