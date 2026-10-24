import { ProbePlayLines as probe } from './app'
import { boolean, integer, list, object, text } from './decode'
import { response } from './response'

export interface PlayLineQuality {
  index:number; name:string; ep_num:number; ok:boolean; width:number; height:number; bandwidth:number
}

export function ProbePlayLines(request:Parameters<typeof probe>[0]) {
  return response(()=>probe(request),value=>list(value,item=>{
    const row=object(item)
    return {index:integer(row.index),name:text(row.name),ep_num:integer(row.ep_num),ok:boolean(row.ok),
      width:integer(row.width,0,32768),height:integer(row.height,0,32768),bandwidth:integer(row.bandwidth)} satisfies PlayLineQuality
  },32))
}
