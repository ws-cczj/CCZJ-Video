/** Serialises durable setting writes and retains the newest unsaved value.
 * A successful older write cannot erase an edit queued while it was in flight. */
export function createSettingsQueue(write:(key:string,value:string)=>Promise<unknown>,changed:(pending:number,error:unknown|null)=>void=()=>{}){
 const pending=new Map<string,{value:string;revision:number}>();let revision=0,running:Promise<boolean>|undefined,lastError:unknown|null=null
 function observe(){changed(pending.size,lastError)}
 function flush():Promise<boolean>{
  if(running)return running
  running=(async()=>{while(pending.size){const [key,entry]=pending.entries().next().value!;try{await write(key,entry.value)}catch(error){lastError=error;observe();return false}if(pending.get(key)===entry)pending.delete(key);lastError=null;observe()}return true})().finally(()=>{running=undefined})
  return running
 }
 return {set(key:string,value:string){pending.set(key,{value,revision:++revision});observe();return flush()},retry:flush,pending:()=>pending.size}
}
