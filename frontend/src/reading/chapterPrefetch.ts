type Request = Promise<unknown> & { cancel?: () => unknown }

/** Adjacent reads share the backend cache and are released on navigation. */
export function chapterPrefetch(load: (id: number) => Request) {
  const pending = new Map<number, Request>()
  function clear() {
    for (const request of pending.values()) { try { void Promise.resolve(request.cancel?.()).catch(() => {}) } catch {} }
    pending.clear()
  }
  function update(chapters: readonly { id: number; available: boolean }[], at: number) {
    const wanted=new Set<number>()
    for(const direction of [-1,1]){for(let i=at+direction;i>=0&&i<chapters.length;i+=direction){const row=chapters[i];if(row?.available){wanted.add(row.id);break}}}
    for(const [id,request] of pending)if(!wanted.has(id)){try{void Promise.resolve(request.cancel?.()).catch(()=>{})}catch{};pending.delete(id)}
    for(const id of wanted){if(pending.has(id))continue;const request=load(id);pending.set(id,request);void request.then(()=>{},()=>{}).finally(()=>{if(pending.get(id)===request)pending.delete(id)})}
  }
  return {update,clear}
}
