import { recordDiagnosticEvent } from './recording'

/** Bounded per-session issue counts. No timer, URL, DOM or request is retained. */
export function createPlayerEvents(sessionId:string) {
  type Issue={operation:string;code:string;recoverable:boolean;count:number;first_at_ms:number;last_at_ms:number;reported:number}
  const issues=new Map<string,Issue>()
  let closed=false
  function event(kind:string,data:Record<string,unknown>):void {
    if(!closed)void recordDiagnosticEvent(kind,{...data,player_session_id:sessionId})
  }
  function report(row:Issue):void {
    event('player_issue',{operation:row.operation,code:row.code,recoverable:row.recoverable,
      count:row.count,first_at_ms:row.first_at_ms,last_at_ms:row.last_at_ms})
    row.reported=row.count
  }
  function issue(operation:string,code:string,recoverable:boolean):void {
    if(closed)return
    code=code.slice(0,120)
    const key=JSON.stringify([operation,code,recoverable])
    const existing=issues.get(key),now=Date.now()
    if(existing){existing.count++;existing.last_at_ms=now;return}
    if(issues.size>=32){const first=issues.entries().next().value;if(first){if(first[1].count>first[1].reported)report(first[1]);issues.delete(first[0])}}
    const row={operation,code,recoverable,count:1,first_at_ms:now,last_at_ms:now,reported:0}
    issues.set(key,row);report(row)
  }
  function snapshot(){return Array.from(issues.values(),({reported,...row})=>({...row}))}
  function close():void {for(const row of issues.values())if(row.count>row.reported)report(row);closed=true;issues.clear()}
  return {event,issue,snapshot,close}
}
