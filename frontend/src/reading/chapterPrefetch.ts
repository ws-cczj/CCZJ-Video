type Request<T> = Promise<T> & { cancel?: () => unknown }

/** Two adjacent navigation buffers own both the context and content request.
 * Taking a buffer transfers cancellation to the active reader request. */
export function chapterPrefetch<T>(load: (id: number) => Request<T>, prepared?: (value:T)=>void) {
  const pending = new Map<number, { request:Request<T>; expires:number }>()
  function release(entry:{request:Request<T>}):void {
    try { void Promise.resolve(entry.request.cancel?.()).catch(() => {}) } catch { /* Already settled. */ }
  }
  function clear() {
    for (const entry of pending.values()) release(entry)
    pending.clear()
  }
  function take(id:number):Request<T>|undefined {
    const entry=pending.get(id)
    if(!entry)return
    pending.delete(id)
    if(entry.expires<=Date.now()){release(entry);return}
    return entry.request
  }
  function update(chapters: readonly { id: number; available: boolean }[], at: number) {
    const wanted=new Set<number>()
    for(const direction of [-1,1]){for(let i=at+direction;i>=0&&i<chapters.length;i+=direction){const row=chapters[i];if(row?.available){wanted.add(row.id);break}}}
    for(const [id,entry] of pending)if(!wanted.has(id)||entry.expires<=Date.now()){release(entry);pending.delete(id)}
    for(const id of wanted){
      if(pending.has(id))continue
      const entry={request:load(id),expires:Date.now()+60_000}
      pending.set(id,entry)
      void entry.request.then(value=>{
        if(pending.get(id)===entry){entry.expires=Date.now()+60_000;prepared?.(value)}
      },()=>{if(pending.get(id)===entry)pending.delete(id)})
    }
  }
  return {update,clear,take}
}
