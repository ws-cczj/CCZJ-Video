import { expect, it } from 'vitest'
import { createModeSessions } from '../src/modes/modeSessions'

it('persists reading settings routes and independent group scroll across documents',()=>{
 const f=fixture(),sessions=createModeSessions(f.storage,f.now)
 sessions.rememberRoute('comic','/comic/settings?group=appearance')
 sessions.rememberScroll('comic','/comic/settings:appearance',120)
 sessions.rememberScroll('novel','/novel/settings:reading',480)
 sessions.rememberScroll('novel','/novel/settings:index',35)
 sessions.rememberScroll('video','/novel/settings:reading',999)
 sessions.prepare('comic')
 const restored=createModeSessions(f.storage,f.now)
 expect(restored.resumeRoute('comic')).toBe('/comic/settings?group=appearance')
 expect(restored.scroll('comic','/comic/settings:appearance')).toBe(120)
 expect(restored.scroll('novel','/novel/settings:reading')).toBe(480)
 expect(restored.scroll('novel','/novel/settings:index')).toBe(35)
 expect(restored.scroll('video','/novel/settings:reading')).toBe(0)
 restored.rememberRoute('comic','/comic/me')
 expect(restored.resumeRoute('comic')).toBe('/comic/settings')
})

function fixture(){const values=new Map<string,string>();let now=100000;return{storage:{read:(key:string)=>values.get(key)??null,write:(key:string,value:string)=>{values.set(key,value);return true},remove:(key:string)=>{values.delete(key)}},values,now:()=>now,advance:(ms:number)=>{now+=ms}}}
it('restores scalar view state and routes without retaining runtime resources',()=>{
 const f=fixture(),sessions=createModeSessions(f.storage,f.now)
 sessions.rememberRoute('video','/search?keyword=hello');sessions.rememberScroll('video','/search',700)
 sessions.saveView('video','search',{keyword:'hello',source:'source-a',enabled:true})
 const token=sessions.prepare('novel');expect(token).toBeTruthy()
 const novel=createModeSessions(f.storage,f.now)
 expect(novel.activate('novel',`https://wails.localhost/?application-mode=novel&mode-resume=${token}#/novel`).warm).toBe(true)
 expect(novel.view('video','search')).toEqual({keyword:'hello',source:'source-a',enabled:true})
 expect(novel.resumeRoute('video')).toBe('/search?keyword=hello')
 expect(novel.scroll('video','/search')).toBe(700)
 expect(novel.diagnostics('novel')).toMatchObject({mode:'novel',runtime_retained:false,strategy:'state_checkpoint'})
 expect(JSON.stringify(novel.diagnostics('novel'))).not.toContain('source-a')
})
it('uses a one-time, matching, short-lived resume ticket; ordinary reload is cold',()=>{
 const f=fixture(),first=createModeSessions(f.storage,f.now),token=first.prepare('comic')
 const href=`https://wails.localhost/?application-mode=comic&mode-resume=${token}#/comic`
 expect(createModeSessions(f.storage,f.now).activate('comic',href).warm).toBe(true)
 expect(createModeSessions(f.storage,f.now).activate('comic',href).warm).toBe(false)
 const old=first.prepare('comic');f.advance(120001)
 expect(createModeSessions(f.storage,f.now).activate('comic',href.replace(String(token),String(old))).warm).toBe(false)
 const wrong=first.prepare('comic')
 expect(createModeSessions(f.storage,f.now).activate('video',href.replace(String(token),String(wrong))).warm).toBe(false)
})
it('rejects cross-mode, media and malformed routes and bounds state',()=>{
 const f=fixture(),sessions=createModeSessions(f.storage,f.now)
 for(const path of ['/novel/search','//example.test','/player/a/b/0','/settings','/search\\bad'])sessions.rememberRoute('video',path)
 expect(sessions.resumeRoute('video')).toBe('/')
 sessions.saveView('video','search',{long:'x'.repeat(2000),ok:1,bad:NaN})
 expect(sessions.view('video','search')).toEqual({ok:1})
 expect(sessions.prepare('comic')).toBeTruthy()
 const snapshot=[...f.values.values()].find(value=>value.includes('"modes"'))!
 expect(snapshot.length).toBeLessThan(65536)
 f.values.set('cczj.mode.sessions.v1','{"version":1,"modes":{"video":{"at":100000,"route":"https://evil.test"}}}')
 expect(createModeSessions(f.storage,f.now).resumeRoute('video')).toBe('/')
})
it('fails safely when session storage is unavailable',()=>{
 const sessions=createModeSessions({read:()=>null,write:()=>false,remove:()=>{}})
 expect(sessions.prepare('novel')).toBeUndefined()
 expect(sessions.activate('novel','https://wails.localhost/?mode-resume=x#/novel').warm).toBe(false)
})
it('caps each mode checkpoint even with many maximum-size inputs and URLs',()=>{
 const f=fixture(),sessions=createModeSessions(f.storage,f.now)
 for(let page=0;page<30;page++){
  sessions.rememberScroll('video','/search?q='+('a'.repeat(1900))+page,page*100)
  sessions.saveView('video','view'+page,Object.fromEntries(Array.from({length:24},(_,i)=>['field'+i,'x'.repeat(512)])))
 }
 expect(sessions.diagnostics('video').checkpoint_bytes).toBeLessThanOrEqual(16384)
 expect(sessions.prepare('novel')).toBeTruthy()
})
