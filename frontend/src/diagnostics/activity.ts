import { shallowRef } from 'vue'
import { eventSnapshot } from '../api/eventPipeline'
import { requestPipelines } from '../api/requestPipelines'

type Outcome = 'success' | 'failed' | 'cancelled'
interface Operation { name:string; started:number; active:number; success:number; failed:number; cancelled:number; total_ms:number; max_ms:number; last_code:string }
interface ReadingState { ready:boolean; failed:boolean; pending:number }
export const activityRevision = shallowRef(0)
const operationNames=new Set([
 'reading.request',
 ...['novel','comic'].flatMap(mode=>[`${mode}.catalog`,`${mode}.reader`,`${mode}.search`,`reading.${mode}.hydrate`,`reading.${mode}.save`]),
 ...['video','novel','comic'].map(mode=>`${mode}.transport`),
])

/** Bounded numeric observations, with no URLs, content, book keys or error bodies. */
export function createActivityDiagnostics(clock:()=>number = ()=>performance.now()) {
  const operations = new Map<string,Operation>()
  const reading = new Map<string,ReadingState>()
  function begin(name:string) {
    if (!operationNames.has(name)) return (_outcome:Outcome,_code='')=>{}
    let row=operations.get(name)
    if(!row) {row={name,started:0,active:0,success:0,failed:0,cancelled:0,total_ms:0,max_ms:0,last_code:''};operations.set(name,row)}
    row.started++;row.active++;activityRevision.value++
    const began=clock();let finished=false
    return (outcome:Outcome,code='')=>{
      if(finished)return
      finished=true
      const duration=Math.max(0,clock()-began)
      row.active--;row[outcome]++;row.total_ms+=duration;row.max_ms=Math.max(row.max_ms,duration)
      row.last_code=/^[A-Z_]{1,48}$/.test(code)?code:''
      activityRevision.value++
    }
  }
  return {begin,
    readingState(kind:'novel'|'comic',state:ReadingState){reading.set(kind,{...state});activityRevision.value++},
    snapshot(mode?:string){const belongs=(name:string)=>!mode||name.startsWith(mode+'.')||name.startsWith('reading.'+mode+'.')||((mode==='novel'||mode==='comic')&&name==='reading.request');return {scope:'current_document',pipelines:requestPipelines.snapshot(),events:eventSnapshot(mode),operations:[...operations.values()].filter(row=>belongs(row.name)).map(row=>({...row,avg_ms:row.started-row.active?Math.round(row.total_ms/(row.started-row.active)):null,max_ms:Math.round(row.max_ms)})),reading:[...reading].filter(([kind])=>!mode||kind===mode).map(([kind,state])=>({kind,...state}))}},
  }
}
export const activityDiagnostics=createActivityDiagnostics()
