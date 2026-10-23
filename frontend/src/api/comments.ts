import { GetDoubanComments as binding } from './app'
import { response } from './response'
import { required,object,integer,text,each } from './decode'

export function GetDoubanComments(request:Parameters<typeof binding>[0]){
 return response(()=>binding(request),value=>{
  const result=required(value)
  integer(result.total);integer(result.page,1);integer(result.total_pages,1)
  each(result.comments,value=>{const row=object(value);text(row.id);text(row.content);text(row.username);integer(row.rating,0,5);integer(row.votes)},1000)
  return result
 })
}
