/** Reader-owned hot copies. Persistent content belongs to the Go blob cache;
 * keys include its source revision and durable chapter identity. */
export function readingContentCache<T>(limits:{entries:number;bytes:number;ttl:number}) {
 const entries=new Map<string,{value:T;bytes:number;expires:number}>()
 let bytes=0,generation=0
 function remove(key:string){const old=entries.get(key);if(old)bytes-=old.bytes;entries.delete(key)}
 function clear(){generation++;entries.clear();bytes=0}
 function get(key:string):T|undefined {
  const saved=entries.get(key)
  if(!saved)return
  if(saved.expires<=Date.now()){remove(key);return}
  entries.delete(key);entries.set(key,saved)
  return saved.value
 }
 function put(key:string,value:T,size:number,started:number) {
  if(!key||started!==generation||size>limits.bytes)return
  remove(key);entries.set(key,{value,bytes:size,expires:Date.now()+limits.ttl});bytes+=size
  while(entries.size>limits.entries||bytes>limits.bytes)remove(entries.keys().next().value!)
 }
 return {get,put,remove,clear,generation:()=>generation}
}
