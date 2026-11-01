<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, toRaw, toRef, watch, type ComponentPublicInstance } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { leaveReader } from '../../reading/navigation'
import { useI18n } from 'vue-i18n'
import { useAsyncResource } from '../../reading/asyncResource'
import { refKey, refOf, refreshNovelContext, refreshChapterText } from '../../api/library'
import { chapterPrefetch } from '../../reading/chapterPrefetch'
import { loadNovelBook, refreshNovelToc } from '../data/novelLibrary'
import { novelReaderSession, type NovelReaderData } from '../reader/session'
import type { NovelChapter } from '../domain'
import NovelSourceSwitch from '../components/NovelSourceSwitch.vue'
import { useNovelReading } from '../readingState'
import { useNovelPages } from '../reader/useNovelPages'
import { useNovelPageTurn, type NovelPageTurn } from '../reader/useNovelPageTurn'
import { measuredPageCount, pageFromRatio, ratioFromPage } from '../reader/pagination'
import { progressChapter } from '../reader/progress'
import { orderChapterEntries } from '../reader/catalog'
import { useReadingBrightness } from '../../reading/useReadingBrightness'
import ReaderSheet from '../../reading/ReaderSheet.vue'
import NovelReaderPreferences from '../components/NovelReaderPreferences.vue'
import Icon from '../../components/Icon.vue'
import BackToTop from '../../components/BackToTop.vue'
import NovelChapterTools from '../../reading/ChapterTools.vue'
import { createFullscreenPlatform } from '../../platform/fullscreen'
import { isMobileRuntime } from '../../api/runtime'
import { normalizeApiError } from '../../api/app'
import { Button, Spinner, MotionTransition } from '../../components/ui'
import NovelChapterText from '../components/NovelChapterText.vue'
import { useMotionStore } from '../../stores/motion'
import { TIMINGS } from '../../motion/preferences'
import { createReadingSystemBars } from '../../platform/readingSystemBars'

const route=useRoute(),router=useRouter(),{t}=useI18n(),reading=useNovelReading()
const readerRoot=ref<HTMLElement|null>(null),immersive=ref(false),immersionBusy=ref(false)
const fullscreen=createFullscreenPlatform()
const systemBars=createReadingSystemBars()
const itemId=computed(()=>Number(route.params.bookId))
const chapterParam=computed(()=>String(route.params.chapter))
const session=novelReaderSession()
const frameData=shallowRef<NovelReaderData[]>([])
const frameElements=new Map<number,HTMLElement>(),frameOffsets=new Map<number,string>()
const framePositions=new Map<number,{page:number;count:number}>()
const adjacentLayouts=new Map<number,{signature:string;text:NovelReaderData['text'];count:number}>()
const prefetch=chapterPrefetch(id=>session.read({itemId:itemId.value,chapterId:id,remoteId:'',ord:0,first:false}),stageChapter)
function stageChapter(result:NovelReaderData):void {
 const current=toRaw(resource.data.value)
 if(disposed||!current?.chapter||!result.chapter?.available||!result.text||result.item?.id!==current.item?.id)return
 const ids=new Set([current.previous?.id,current.chapter.id,current.next?.id])
 const saved=new Map(frameData.value.filter(row=>ids.has(row.chapter?.id)).map(row=>[row.chapter!.id,row]))
 saved.set(current.chapter.id,current)
 if(ids.has(result.chapter.id))saved.set(result.chapter.id,result)
 frameData.value=Array.from(saved.values())
 for(const id of frameOffsets.keys())if(!ids.has(id))frameOffsets.delete(id)
 for(const id of framePositions.keys())if(!ids.has(id))framePositions.delete(id)
}
let tocRequest:ReturnType<typeof refreshNovelContext>|ReturnType<typeof loadNovelBook>|null=null
const directory=ref<NovelChapter[]>([]),directoryLoaded=ref(false),directoryLoading=ref(false)
function location() {
 const id=itemId.value,param=chapterParam.value
 const record=Object.values(reading.progress).find(row=>row.itemId===id&&String(row.chapterId)===param)
 return {itemId:id,chapterId:Number(param)||0,remoteId:record?.chapterRemoteId??'',ord:record?.chapterOrd??-1,first:param==='first'}
}
const resource=useAsyncResource<NovelReaderData>({
 diagnosticLabel:'novel.reader',
 load:()=>{const target=location();return prefetch.take(target.chapterId)??session.read(target)},
 isEmpty:result=>!result.item,
 onData:result=>{
  stageChapter(result)
  if(result.item&&result.chapter?.available&&chapterParam.value!==String(result.chapter.id)){
   void router.replace({path:`/novel/read/${result.item.id}/${result.chapter.id}`,query:route.query});return
  }
  void reflow(initialPosition())
  if(!result.item||!result.chapter||!result.text)return
  queueBackground(result)
 },
})
let backgroundFrame=0,backgroundGeneration=0
function cancelBackground():void {backgroundGeneration++;cancelAnimationFrame(backgroundFrame)}
function queueBackground(result:NovelReaderData):void {
 cancelBackground()
 const generation=backgroundGeneration
 // Let the current chapter finish layout and paint before adjacent IPC/refresh work.
 backgroundFrame=requestAnimationFrame(()=>{backgroundFrame=requestAnimationFrame(()=>{
  if(disposed||generation!==backgroundGeneration||data.value?.chapter?.id!==result.chapter?.id)return
  if(result.text?.stale)void updateText(true)
  prefetch.update([result.previous,result.chapter,result.next].filter((row):row is NovelChapter=>!!row),result.previous?1:0)
  if(result.count-result.index>3)return
  const currentChapter=result.chapter?.id,currentItem=result.item?.id
  const pending=refreshNovelContext(location());tocRequest=pending
  void pending.then(view=>{
   if(tocRequest===pending&&!disposed&&data.value?.chapter?.id===currentChapter&&data.value?.item?.id===currentItem){
    prefetch.clear();resource.data.value={...view,text:result.text};stageChapter(resource.data.value)
   }
  },()=>{}).finally(()=>{if(tocRequest===pending)tocRequest=null})
 })})
}
const data=computed(()=>resource.data.value)
const book=computed(()=>data.value?.item??null)
const chapter=computed(()=>data.value?.chapter??null)
const text=computed(()=>data.value?.text??null)
const chapters=computed(()=>directory.value.filter(row=>!row.isVolume))
const total=computed(()=>data.value?.count??0)
const shelfKey=computed(()=>book.value?refKey(refOf(book.value)):'')
const chapterSearch=ref(''),descending=ref(false)
const entries=computed(()=>{let number=0;return directory.value.map(item=>({chapter:item,number:item.isVolume?0:++number}))})
const visibleEntries=computed(()=>{const query=chapterSearch.value.trim().toLowerCase();return orderChapterEntries(entries.value,descending.value).filter(row=>!query||row.chapter.title.toLowerCase().includes(query)||String(row.number)===query)})
function locateChapter(id:number):void{chapterSearch.value='';void nextTick(()=>readerRoot.value?.querySelector<HTMLElement>(`[data-chapter-id="${id}"]`)?.scrollIntoView({block:'center',behavior:'auto'}))}
function locateNumber(number:number):void{const row=entries.value.find(row=>row.number===number&&!row.chapter.isVolume);if(row)locateChapter(row.chapter.id)}
const manualRefreshing=ref(false),tocError=ref('')
async function loadDirectory(refresh=false):Promise<void>{
 if(directoryLoading.value||!book.value)return
 const id=book.value.id
 if(refresh)prefetch.clear()
 directoryLoading.value=true;manualRefreshing.value=refresh;tocError.value=''
 void tocRequest?.cancel()
 const pending=refresh?refreshNovelToc(id):loadNovelBook(id);tocRequest=pending
 try{
  const view=await pending
  if(!disposed&&tocRequest===pending&&itemId.value===id&&view){
   directory.value=view.chapters;directoryLoaded.value=true;tocError.value=view.tocError
   const current=data.value,rows=view.chapters.filter(row=>!row.isVolume),at=rows.findIndex(row=>row.id===current?.chapter?.id)
   if(current&&at>=0){resource.data.value={...current,index:at,count:rows.length,previous:rows.slice(0,at).reverse().find(row=>row.available)??null,next:rows.slice(at+1).find(row=>row.available)??null};stageChapter(resource.data.value);queueBackground(resource.data.value)}
  }
 }
 catch(error){if(!disposed&&tocRequest===pending)tocError.value=normalizeApiError(error).message}
 finally{if(tocRequest===pending)tocRequest=null;directoryLoading.value=false;manualRefreshing.value=false}
}
function refreshDirectory():void{void loadDirectory(true)}
async function toggleImmersion():Promise<void>{
 if(immersionBusy.value)return
 immersionBusy.value=true
 try{
  if(immersive.value){await fullscreen.exit(document.documentElement,()=>!disposed);immersive.value=false;delete document.body.dataset.readingImmersive}
  else{immersive.value=true;chrome.value=false;document.body.dataset.readingImmersive='1';await nextTick();const entered=await fullscreen.enter(document.documentElement,()=>!disposed);if(disposed){await fullscreen.release(document.documentElement,()=>true);return}if(!entered)throw new Error(t('novel.immersionUnavailable'))}
 }catch(error){immersive.value=false;delete document.body.dataset.readingImmersive;notify(normalizeApiError(error).message)}
 finally{immersionBusy.value=false}
}
function fullscreenChanged():void{if(fullscreen.kind==='element'&&!fullscreen.browserActive(document.documentElement)){immersive.value=false;delete document.body.dataset.readingImmersive}}
const nextChapter=computed(()=>data.value?.next??null),previousChapter=computed(()=>data.value?.previous??null)
const viewport=ref<HTMLElement|null>(null),track=ref<HTMLElement|null>(null)
function bindViewport(el:Element|ComponentPublicInstance|null,id:number):void {if(id===chapter.value?.id)viewport.value=el instanceof HTMLElement?el:null}
function bindTrack(el:Element|ComponentPublicInstance|null,id:number):void {if(id===chapter.value?.id)track.value=el instanceof HTMLElement?el:null}
function bindFrame(el:Element|ComponentPublicInstance|null,id:number):void {if(el instanceof HTMLElement)frameElements.set(id,el);else frameElements.delete(id)}
const paged=computed(()=>reading.preferences.flow==='paged')
const motion=useMotionStore()
function turnDuration():number {return !motion.allows('pages')?0:motion.tokenMs('--cczj-motion-normal')||TIMINGS.normal}
const pages=useNovelPages(viewport,track,()=>paged.value),chrome=ref(false),panel=ref<''|'chapters'|'preferences'|'sources'|'recovery'>(''),ratio=ref(0)
const pageTurnHost=ref<HTMLElement|null>(null)
const pageTurn=useNovelPageTurn(pageTurnHost,turnDuration,preparePageTurn,direction=>go(direction>0?nextChapter.value:previousChapter.value,direction<0),error=>notify(normalizeApiError(error).message))
let coverNavigation:{chapter:number;resolve:(success:boolean)=>void}|null=null
function finishCoverNavigation(success:boolean):void {const pending=coverNavigation;coverNavigation=null;pending?.resolve(success)}
function commitPageChapter(target:NovelChapter,end:boolean):Promise<boolean>{
 return new Promise(resolve=>{finishCoverNavigation(false);coverNavigation={chapter:target.id,resolve};go(target,end)})
}
function pageSnapshot(entry:NovelReaderData,number:number,count:number):HTMLElement|null {
 const frame=frameElements.get(entry.chapter?.id??0)
 if(!frame)return null
 const clone=frame.cloneNode(true) as HTMLElement
 const body=clone.querySelector<HTMLElement>('.novel-reading-paper'),view=frame.querySelector<HTMLElement>('.novel-page-viewport')
 if(!body||!view)return null
 body.style.transform=`translate3d(${-number*(view.clientWidth+40)}px,0,0)`
 const counter=clone.querySelector('[data-page-counter]'),progress=clone.querySelector('[data-book-progress]')
 if(counter)counter.textContent=`${number+1} / ${count}`
 if(progress)progress.textContent=t('novel.bookProgress',{n:entry.count>0?Math.round((entry.index+ratioFromPage(number,count))/entry.count*100):0})
 return clone
}
function adjacentLayout(entry:NovelReaderData,end:boolean):{page:number;count:number}|null {
 const frame=frameElements.get(entry.chapter?.id??0),view=frame?.querySelector<HTMLElement>('.novel-page-viewport'),body=frame?.querySelector<HTMLElement>('.novel-reading-paper')
 if(!view||!body||view.clientWidth<1||view.clientHeight<1)return null
 const signature=[entry.cacheIdentity,view.clientWidth,view.clientHeight,reading.preferences.size,reading.preferences.leading,reading.preferences.margin,reading.preferences.serif,immersive.value].join('|')
 const saved=adjacentLayouts.get(entry.chapter!.id)
 if(saved?.signature===signature&&saved.text===entry.text)return {page:end?saved.count-1:0,count:saved.count}
 body.style.columnWidth=view.clientWidth+'px';body.style.columnGap='40px'
 const count=measuredPageCount(body.scrollWidth,view.clientWidth,40)
 adjacentLayouts.set(entry.chapter!.id,{signature,text:entry.text,count})
 for(const id of adjacentLayouts.keys())if(!frameData.value.some(row=>row.chapter?.id===id))adjacentLayouts.delete(id)
 return {page:end?count-1:0,count}
}
function preparePageTurn(direction:number):NovelPageTurn|null {
 const current=data.value
 if(!current?.chapter||!current.text||!ready)return null
 const from=pageSnapshot(current,pages.page.value,pages.count.value)
 if(!from)return null
 const number=pages.page.value+direction
 if(number>=0&&number<pages.count.value)return {current:from,target:pageSnapshot(current,number,pages.count.value),commit:()=>{pages.select(number);return true}}
 const neighbor=direction>0?nextChapter.value:previousChapter.value
 const prepared=frameData.value.find(row=>!!row.chapter&&row.chapter.id===neighbor?.id&&row.text&&row.chapter.available)
 const layout=prepared?adjacentLayout(prepared,direction<0):null
 return {current:from,target:prepared&&layout?pageSnapshot(prepared,layout.page,layout.count):null,commit:()=>neighbor?commitPageChapter(neighbor,direction<0):false}
}
function frameCounter(entry:NovelReaderData):string {
 const saved=entry.chapter?.id===chapter.value?.id?{page:pages.page.value,count:pages.count.value}:framePositions.get(entry.chapter!.id)
 return `${(saved?.page??0)+1} / ${saved?.count??1}`
}
function framePercent(entry:NovelReaderData):number {
 const saved=framePositions.get(entry.chapter!.id),position=entry.chapter?.id===chapter.value?.id?ratio.value:saved?ratioFromPage(saved.page,saved.count):0
 return entry.count>0?Math.round((entry.index+position)/entry.count*100):0
}
const recoveryReason=computed(()=>{
 const code=resource.error.value?.code
 return ['PERMISSION_DENIED','AUTHENTICATION_REQUIRED'].includes(code??'')?'restricted':code==='NOT_FOUND'?'missing':['CORRUPT','UNSUPPORTED'].includes(code??'')?'invalid':'temporary'
})
const failedChapter=computed(()=>resource.status.value==='error'?(Number(chapterParam.value)||0):(chapter.value?.id??0))
function returnCurrent():void {if(book.value&&chapter.value)void router.replace(`/novel/read/${book.value.id}/${chapter.value.id}`)}
const chapterMoving=ref(false),waiting=ref(false)
let waitTimer:ReturnType<typeof setTimeout>|undefined,chapterAnimations:Animation[]=[],transitionFrom=0,transitionDirection=0
let scrollFrame=0
function stopScroll():void {cancelAnimationFrame(scrollFrame);scrollFrame=0}
function scrollPage(direction:number):void {
 const el=viewport.value
 if(!el)return
 stopScroll()
 const from=el.scrollTop,to=Math.max(0,Math.min(el.scrollHeight-el.clientHeight,from+direction*el.clientHeight*.85)),duration=turnDuration(),started=performance.now()
 if(!duration){el.scrollTop=to;return}
 const tick=(now:number)=>{
  if(disposed||viewport.value!==el)return
  const progress=Math.min(1,(now-started)/duration)
  el.scrollTop=from+(to-from)*(1-Math.pow(1-progress,3))
  scrollFrame=progress<1?requestAnimationFrame(tick):0
 }
 scrollFrame=requestAnimationFrame(tick)
}
function stopChapterMotion():void {for(const animation of chapterAnimations)animation.cancel();chapterAnimations=[];chapterMoving.value=false}
function animateChapter():void {
 const from=frameElements.get(transitionFrom),to=frameElements.get(chapter.value?.id??0),direction=transitionDirection
 if(from&&from===to)return
 transitionFrom=0;transitionDirection=0
 const duration=turnDuration()
 if(paged.value){
  if(pageTurn.committing.value||!from||!to||!direction)return
  const previous=frameData.value.find(row=>row.chapter?.id===Number(from.dataset.chapter)),current=data.value,saved=previous?framePositions.get(previous.chapter!.id):undefined
  if(previous&&current&&saved){
   const oldPage=pageSnapshot(previous,saved.page,saved.count),newPage=pageSnapshot(current,pages.page.value,pages.count.value)
   if(oldPage&&newPage){
    // The new chapter is already mounted. Preserve the real old paper while
    // its temporary replacement warms, otherwise the target flashes first.
    from.style.visibility='visible';to.style.visibility='hidden'
    const releaseBase=()=>{from.style.removeProperty('visibility');to.style.removeProperty('visibility')}
    if(!pageTurn.turn(direction,{current:oldPage,target:newPage,commit:()=>true,releaseBase}))releaseBase()
   }
  }
  return
 }
 if(!from||!to||!direction||!duration)return
 stopChapterMotion();chapterMoving.value=true
 const axis='Y'
 chapterAnimations=[from.animate([{visibility:'visible',transform:'translate'+axis+'(0)'},{visibility:'visible',transform:`translate${axis}(${-direction*100}%)`}],{duration,easing:'cubic-bezier(.2,.65,.3,1)'}),to.animate([{transform:`translate${axis}(${direction*100}%)`},{transform:'translate'+axis+'(0)'}],{duration,easing:'cubic-bezier(.2,.65,.3,1)'})]
 const running=chapterAnimations
 void Promise.allSettled(running.map(animation=>animation.finished)).then(()=>{if(chapterAnimations===running){chapterAnimations=[];chapterMoving.value=false}})
}
function frameStyle(row:NovelReaderData):Record<string,string>|undefined {return paged.value?{transform:row.chapter?.id===chapter.value?.id?pages.transform.value:frameOffsets.get(row.chapter!.id)||'translate3d(0px,0px,0px)'}:undefined}
const dim=useReadingBrightness(toRef(reading,'preferences'))
const readerStyle=computed(()=>({'--reading-size':reading.preferences.size+'px','--reading-leading':reading.preferences.leading,'--reading-margin':reading.preferences.margin+'px','--reading-font':reading.preferences.serif ? '"Noto Serif CJK SC","Songti SC",SimSun,serif' : 'system-ui,sans-serif'}))
const slider=computed(()=>Math.round(ratio.value*1000))
const clock=ref(''),hint=ref('')
let ready=false,saveTimer:ReturnType<typeof setTimeout>|undefined,clockTimer:ReturnType<typeof setInterval>|undefined,hintTimer:ReturnType<typeof setTimeout>|undefined,layoutFrame=0,disposed=false
let pointer:{id:number;x:number;y:number;horizontal:boolean;top:boolean;bottom:boolean}|null=null,suppressClick=false
function leaveToBook():void { void router.replace(`/novel/book/${itemId.value}`) }
function leaveToExplore():void { leaveReader(router, 'novel', '/novel') }
function initialPosition():number {
  if(route.query.sourceSwitch==='1')return 0
  if(route.query.position==='end')return 1
  const current=data.value
  if(current?.chapter){
    const record=reading.progress[shelfKey.value]
    if(record&&progressChapter([current.chapter],record)?.id===current.chapter.id)return record.ratio
  }
  return 0
}
function save():void {
  clearTimeout(saveTimer)
  const current=data.value
  if(!ready||resource.status.value!=='ready'||!current?.item||!current.chapter||!current.chapter.available||!current.text)return
  reading.remember(shelfKey.value,{itemId:current.item.id,title:current.item.title,chapterOrd:current.chapter.ord,chapterId:current.chapter.id,chapterRemoteId:current.chapter.remoteId,ratio:ratio.value})
}
function notify(message:string):void { hint.value=message;clearTimeout(hintTimer);hintTimer=setTimeout(()=>hint.value='',1800) }
const textUpdating=ref(false),textUpdateError=ref('')
let textUpdateRequest:ReturnType<typeof refreshChapterText>|undefined,textUpdateGeneration=0,textAttempt:{chapter:number;at:number}|null=null
function cancelTextUpdate():void {textUpdateGeneration++;void textUpdateRequest?.cancel();textUpdateRequest=undefined;textUpdating.value=false;textUpdateError.value=''}
async function updateText(automatic=false):Promise<void>{
 const current=data.value
 if(!current?.chapter||!current.text||textUpdating.value||pageTurn.active.value||chapterMoving.value||resource.status.value!=='ready')return
 if(automatic&&textAttempt?.chapter===current.chapter.id&&Date.now()-textAttempt.at<300000)return
 textAttempt={chapter:current.chapter.id,at:Date.now()}
 const token=++textUpdateGeneration
 textUpdating.value=true;textUpdateError.value=''
 const request=refreshChapterText(current.chapter.id);textUpdateRequest=request
 try{
  const updated=await request
  if(disposed||token!==textUpdateGeneration||data.value?.chapter?.id!==current.chapter.id||data.value?.cacheIdentity!==current.cacheIdentity)return
  session.remember(current.cacheIdentity,updated)
  // A settled fetch may arrive during a drag; wait for that page to hand off
  // before changing any of its underlying column fragments.
  while(pageTurn.active.value||chapterMoving.value){
   await new Promise<void>(resolve=>requestAnimationFrame(()=>resolve()))
   if(disposed||token!==textUpdateGeneration)return
  }
  const changed=JSON.stringify(current.text.paragraphs)!==JSON.stringify(updated.paragraphs)||JSON.stringify(current.text.warnings)!==JSON.stringify(updated.warnings)
  const anchor=changed?visibleTextAnchor():null
  const paragraphs=Array.from(track.value?.querySelectorAll('.novel-reading-paragraph')??[])
  const index=anchor?paragraphs.indexOf(anchor.node.parentElement!):-1
  const position=ratio.value
  resource.data.value={...data.value!,text:updated};stageChapter(resource.data.value);adjacentLayouts.delete(current.chapter.id)
  if(changed){
   await nextTick()
   const node=index>=0?track.value?.querySelectorAll('.novel-reading-paragraph')[index]?.firstChild:null
   await reflow(position,node instanceof Text?{node,offset:anchor!.offset}:null)
  }
  if(!automatic)notify(t('novel.textRefreshed'))
 }catch(error){
  if(!disposed&&token===textUpdateGeneration){textUpdateError.value=t('novel.textRefreshFailed');if(!automatic)notify(normalizeApiError(error).message)}
 }finally{if(token===textUpdateGeneration){textUpdating.value=false;textUpdateRequest=undefined}}
}
function go(next:NovelChapter|null,end=false):void {
 if(chapterMoving.value||!ready||resource.status.value!=='ready'){finishCoverNavigation(false);return}
 if(next?.available){
  save()
  transitionFrom=chapter.value?.id??0;transitionDirection=end||next.ord<(chapter.value?.ord??next.ord)?-1:1
  if(paged.value&&chapter.value){frameOffsets.set(chapter.value.id,pages.transform.value);framePositions.set(chapter.value.id,{page:pages.page.value,count:pages.count.value})}
  void router.replace(`/novel/read/${itemId.value}/${next.id}${end?'?position=end':''}`).then(failure=>{if(failure)finishCoverNavigation(false)},error=>{finishCoverNavigation(false);notify(normalizeApiError(error).message)})
 }
 else notify(t(end?'novel.firstChapter':'novel.lastChapter'))
}
function jumpTo(chapterId:number):void {
 const target=chapters.value.find(item=>item.id===chapterId)
 panel.value=''
 if(target&&target.id!==chapter.value?.id)go(target)
}
function turn(direction:number):void {
 if(chapterMoving.value||pageTurn.active.value||panel.value||!ready||resource.status.value!=='ready')return
 if(!paged.value&&viewport.value){
  const el=viewport.value,end=Math.max(0,el.scrollHeight-el.clientHeight)
  if(direction>0&&el.scrollTop>=end-1)go(nextChapter.value)
  else if(direction<0&&el.scrollTop<=1)go(previousChapter.value,true)
  else scrollPage(direction)
  return
 }
 if(!pageTurn.turn(direction))go(direction>0?nextChapter.value:previousChapter.value,direction<0)
}
function seek(value:number):void {
 if(pageTurn.committing.value)return
 pageTurn.cancel();finishCoverNavigation(false)
 transitionFrom=0;transitionDirection=0
 stopScroll()
 ratio.value=Math.max(0,Math.min(1,value))
 if(paged.value)pages.select(pageFromRatio(ratio.value,pages.count.value))
 else if(viewport.value)viewport.value.scrollTop=ratio.value*Math.max(0,viewport.value.scrollHeight-viewport.value.clientHeight)
}
function onScroll():void {
 if(!ready||paged.value||!viewport.value)return
 const el=viewport.value;ratio.value=el.scrollHeight>el.clientHeight?el.scrollTop/(el.scrollHeight-el.clientHeight):0
}
watch(pages.ratio,value=>{if(ready&&paged.value)ratio.value=value})
watch(ratio,()=>{if(ready){clearTimeout(saveTimer);saveTimer=setTimeout(save,500)}})
// Keep the visible text through typography changes; chapter entry still uses saved progress.
function visibleTextAnchor(): { node: Text; offset: number } | null {
 const el=viewport.value,body=track.value
 if(!el||!body||ratio.value<=0)return null
 const box=el.getBoundingClientRect()
 // Read geometry rather than hit-testing: the settings mask covers the text.
 for(const paragraph of Array.from(body.querySelectorAll<HTMLElement>('.novel-reading-paragraph'))){
  if(!Array.from(paragraph.getClientRects()).some(r=>r.right>box.left&&r.left<box.right&&r.bottom>box.top&&r.top<box.bottom))continue
  const node=paragraph.firstChild
  if(!(node instanceof Text)||!node.length)continue
  const range=document.createRange()
  let low=0,high=node.length-1
  while(low<high){
   const middle=Math.floor((low+high)/2)
   range.setStart(node,middle);range.setEnd(node,middle+1)
   const rect=range.getClientRects()[0]
   if(rect&&(rect.right<=box.left||rect.left<box.right&&rect.bottom<=box.top))low=middle+1
   else high=middle
  }
  return {node,offset:low}
 }
 return null
}
async function reflow(position=ratio.value,anchor:ReturnType<typeof visibleTextAnchor>=null):Promise<void> {
 stopScroll();ready=false;await nextTick();cancelAnimationFrame(layoutFrame)
 layoutFrame=requestAnimationFrame(()=>{
  if(disposed){finishCoverNavigation(false);return}
  lastWheelEvent=performance.now()
  if(paged.value){pages.measure(position);ratio.value=pages.ratio.value}
  else if(viewport.value){viewport.value.scrollTop=position>0?position*Math.max(0,viewport.value.scrollHeight-viewport.value.clientHeight):0;ratio.value=position}
  if(anchor?.node.isConnected&&viewport.value&&track.value?.contains(anchor.node)){
   const range=document.createRange();range.setStart(anchor.node,Math.min(anchor.offset,anchor.node.length));range.setEnd(anchor.node,Math.min(anchor.offset+1,anchor.node.length))
   const target=range.getClientRects()[0],box=viewport.value.getBoundingClientRect()
   if(target){
    if(paged.value){const stride=viewport.value.clientWidth+40;pages.select(Math.floor((target.left-box.left+pages.page.value*stride)/stride));ratio.value=pages.ratio.value}
    else{const el=viewport.value;el.scrollTop+=target.top-box.top;ratio.value=el.scrollHeight>el.clientHeight?el.scrollTop/(el.scrollHeight-el.clientHeight):0}
   }
  }
  ready=true;save();animateChapter()
  if(coverNavigation)finishCoverNavigation(coverNavigation.chapter===chapter.value?.id&&!!text.value)
  // Entering the previous chapter at its end is a one-time navigation hint.
  // Leaving it in the URL would override subsequent saved progress on reload.
  if(route.query.position==='end')void router.replace({query:{...route.query,position:undefined}})
  else if(route.query.sourceSwitch==='1')void router.replace({query:{...route.query,sourceSwitch:undefined}})
 })
}
watch(()=>[reading.preferences.size,reading.preferences.leading,reading.preferences.margin,reading.preferences.serif,reading.preferences.flow,immersive.value],()=>{pageTurn.cancel();finishCoverNavigation(false);transitionFrom=0;transitionDirection=0;if(data.value)void reflow(ratio.value,visibleTextAnchor())})
// 切章走 run：generation 会丢弃迟到的旧章响应，否则慢网络下会出现
// 「正文是上一章、标题是这一章」。
watch(itemId,()=>{finishCoverNavigation(false);pageTurn.cancel();save();resource.data.value=null;transitionFrom=0;transitionDirection=0;prefetch.clear();session.clear();frameData.value=[];frameOffsets.clear();framePositions.clear();directory.value=[];directoryLoaded.value=false})
watch([itemId,chapterParam],()=>{cancelTextUpdate();adjacentLayouts.clear();if(coverNavigation?.chapter!==Number(chapterParam.value)){finishCoverNavigation(false);pageTurn.cancel()}save();stopScroll();cancelBackground();stopChapterMotion();void tocRequest?.cancel();tocRequest=null;directoryLoading.value=false;ready=false;if(!data.value?.text)ratio.value=0;panel.value='';void resource.run()},{immediate:true})
watch(resource.status,status=>{clearTimeout(waitTimer);waiting.value=false;if(status==='loading')waitTimer=setTimeout(()=>waiting.value=true,180);if(status==='error'){finishCoverNavigation(false);if(text.value)panel.value='recovery'}},{flush:'sync'})
watch(panel,value=>{if(value&&!pageTurn.committing.value)pageTurn.cancel();if(value==='chapters'&&!directoryLoaded.value)void loadDirectory()})
function down(event:PointerEvent):void {
 if(event.pointerType!=='touch')stopScroll()
 if(event.pointerType==='touch'||!event.isPrimary||event.button!==0||panel.value||chapterMoving.value||pageTurn.active.value||!ready||(event.target instanceof Element&&event.target.closest('button,a,input')))return
 pointer={id:event.pointerId,x:event.clientX,y:event.clientY,horizontal:false,top:(viewport.value?.scrollTop??0)<=1,bottom:!!viewport.value&&viewport.value.scrollTop>=viewport.value.scrollHeight-viewport.value.clientHeight-1};suppressClick=false
 readerRoot.value?.setPointerCapture(event.pointerId)
}
function move(event:PointerEvent):void {
 if(!pointer||pointer.id!==event.pointerId)return
 const dx=event.clientX-pointer.x,dy=event.clientY-pointer.y
 if(paged.value&&Math.abs(dx)>12&&Math.abs(dx)>Math.abs(dy)*1.3){pointer.horizontal=true;event.preventDefault();pageTurn.drag(dx)}
}
function up(event:PointerEvent):void {
 if(!pointer||pointer.id!==event.pointerId)return
 const start=pointer;pointer=null
 if(readerRoot.value?.hasPointerCapture(event.pointerId))readerRoot.value.releasePointerCapture(event.pointerId)
 if(start.horizontal){suppressClick=true;pageTurn.release(!!window.getSelection()?.toString())}
 else if(Math.abs(event.clientY-start.y)>60&&Math.abs(event.clientY-start.y)>Math.abs(event.clientX-start.x)*1.3){
  suppressClick=true
  if(paged.value)turn(event.clientY<start.y?1:-1)
  else if(start.bottom&&event.clientY<start.y)go(nextChapter.value)
  else if(start.top&&event.clientY>start.y)go(previousChapter.value,true)
 }
}
function cancel():void { if(pointer?.horizontal){suppressClick=true;pageTurn.release(true)}pointer=null }
// Native vertical scrolling sends pointercancel before the finger is released.
// Touch endpoints survive that cancellation without preventing normal scrolling.
let touchStart:{x:number;y:number;top:boolean;bottom:boolean;horizontal:boolean}|null=null
function touchDown(event:TouchEvent):void {
 stopScroll()
 const el=viewport.value
 touchStart=null
 if(event.touches.length!==1){pageTurn.release(true);return}
 if(panel.value||chapterMoving.value||pageTurn.active.value||!ready||!el||(event.target instanceof Element&&event.target.closest('button,a,input')))return
 const point=event.touches[0]!
 touchStart={x:point.clientX,y:point.clientY,top:el.scrollTop<=1,bottom:el.scrollTop>=el.scrollHeight-el.clientHeight-1,horizontal:false};suppressClick=false
}
function touchMove(event:TouchEvent):void {
 if(!paged.value||!touchStart)return
 if(event.touches.length!==1||window.getSelection()?.toString()){pageTurn.release(true);return}
 const point=event.touches[0]!,dx=point.clientX-touchStart.x,dy=point.clientY-touchStart.y
 if(Math.abs(dx)>12&&Math.abs(dx)>Math.abs(dy)*1.3){touchStart.horizontal=true;suppressClick=true;pageTurn.drag(dx)}
}
function touchCancel():void {touchStart=null;pageTurn.release(true)}
function touchUp(event:TouchEvent):void {
 const start=touchStart;touchStart=null
 if(!start||event.changedTouches.length!==1)return
 if(window.getSelection()?.toString()){pageTurn.release(true);return}
 if(paged.value&&start.horizontal){suppressClick=true;pageTurn.release();return}
 const point=event.changedTouches[0]!,dx=point.clientX-start.x,dy=point.clientY-start.y
 if(Math.abs(dy)>60&&Math.abs(dy)>Math.abs(dx)*1.3){
  suppressClick=true
  if(paged.value)turn(dy<0?1:-1)
  else if(start.bottom&&dy<0)go(nextChapter.value)
  else if(start.top&&dy>0)go(previousChapter.value,true)
 }else if(paged.value&&Math.abs(dx)>40&&Math.abs(dx)>Math.abs(dy)*1.3){suppressClick=true;turn(dx<0?1:-1)}
 else pageTurn.release(true)
}
function tap(event:MouseEvent):void {
 if(panel.value||pageTurn.active.value)return
 if(suppressClick){suppressClick=false;return}
 if((event.target as HTMLElement).closest('a,button,input')||window.getSelection()?.toString())return
 const rect=viewport.value?.getBoundingClientRect();if(!rect)return
 const x=(event.clientX-rect.left)/rect.width
 if(x<.25||x>.75){turn(x<.25?-1:1);return}
 chrome.value=!chrome.value
}
let wheelTotal=0,lastWheel=0,lastWheelEvent=0
function wheel(event:WheelEvent):void{
 stopScroll()
 if(!reading.preferences.wheelPaging||panel.value||event.ctrlKey||!ready||resource.status.value!=='ready')return
 if(!paged.value){
  const el=viewport.value;if(!el||!event.deltaY)return
  const boundary=event.deltaY>0?el.scrollTop>=el.scrollHeight-el.clientHeight-1:el.scrollTop<=1
  if(!boundary){lastWheelEvent=performance.now();return}
  // Finish the last screen first. Momentum from the same gesture must not skip
  // its remaining lines or immediately turn back after restoring a chapter.
  if(performance.now()-lastWheelEvent<180){lastWheelEvent=performance.now();return}
 }
 event.preventDefault()
 const now=performance.now()
 if(now-lastWheelEvent>180)wheelTotal=0
 lastWheelEvent=now
 if(now-lastWheel<220)return
 const delta=Math.abs(event.deltaX)>Math.abs(event.deltaY)?event.deltaX:event.deltaY
 wheelTotal+=delta*(event.deltaMode===1?16:event.deltaMode===2?viewport.value?.clientHeight??600:1)
 if(Math.abs(wheelTotal)>=45){turn(wheelTotal>0?1:-1);wheelTotal=0;lastWheel=now}
}
function keyboard(event:KeyboardEvent):void {
 if(event.defaultPrevented||event.isComposing||event.ctrlKey||event.metaKey||event.altKey||panel.value||(event.target instanceof Element&&event.target.closest('input,textarea,select,[contenteditable="true"],[role="slider"]')))return
 if(event.key==='Escape'){event.preventDefault();if(immersive.value)void toggleImmersion();else chrome.value=!chrome.value;return}
 if(event.code==='KeyF'){event.preventDefault();void toggleImmersion();return}
 if(!reading.preferences.keyboardPaging)return
 const forward=event.code===reading.preferences.nextKey||event.code==='PageDown'||event.code==='Space'&&!event.shiftKey
 const backward=event.code===reading.preferences.previousKey||event.code==='PageUp'||event.code==='Space'&&event.shiftKey
 if((event.target instanceof Element&&event.target.closest('button,a'))&&event.code==='Space')return
 if(forward||backward){event.preventDefault();turn(backward?-1:1)}
}
function back(event:Event):void { if(chrome.value&&!panel.value){event.preventDefault();event.stopImmediatePropagation();chrome.value=false} }
function tick():void { clock.value=new Date().toLocaleTimeString([],{hour:'2-digit',minute:'2-digit',hour12:false}) }
function fontsChanged():void {
 adjacentLayouts.clear();pageTurn.cancel();finishCoverNavigation(false)
 if(data.value&&ready)void reflow(ratio.value,visibleTextAnchor())
}
onMounted(()=>{
 document.fonts?.addEventListener('loadingdone',fontsChanged)
 systemBars.enter()
 document.addEventListener('fullscreenchange',fullscreenChanged);window.addEventListener('keydown',keyboard);window.addEventListener('pagehide',save);window.addEventListener('cczj:android-overlay-back',back)
 tick();clockTimer=setInterval(tick,60000)
})
onBeforeUnmount(()=>{document.fonts?.removeEventListener('loadingdone',fontsChanged);cancelTextUpdate();adjacentLayouts.clear();systemBars.release();save();finishCoverNavigation(false);pageTurn.cancel();disposed=true;clearTimeout(waitTimer);stopScroll();stopChapterMotion();cancelBackground();if(immersive.value)void fullscreen.exit(document.documentElement,()=>true).catch(()=>{});delete document.body.dataset.readingImmersive;document.removeEventListener('fullscreenchange',fullscreenChanged);prefetch.clear();session.clear();void tocRequest?.cancel();tocRequest=null;clearTimeout(saveTimer);clearTimeout(hintTimer);clearInterval(clockTimer);cancelAnimationFrame(layoutFrame);window.removeEventListener('keydown',keyboard);window.removeEventListener('pagehide',save);window.removeEventListener('cczj:android-overlay-back',back)})
</script>
<template>
 <div v-if="['ready','loading','error'].includes(resource.status.value) && book && chapter && chapter.available && text" ref="readerRoot" class="novel-reader" :aria-busy="resource.status.value==='loading'" :class="{'novel-reader-paged':paged,'novel-reader-immersive':immersive}" :data-reading-theme="reading.preferences.theme" :style="readerStyle" @touchstart.passive="touchDown" @touchmove.passive="touchMove" @touchend.passive="touchUp" @touchcancel.passive="touchCancel" @wheel="wheel" @pointerdown="down" @pointermove="move" @pointerup="up" @pointercancel="cancel" @lostpointercapture="cancel" @click="tap">
  <Button v-if="resource.status.value==='error'&&!panel" class="novel-reader-recovery-entry" @click.stop="panel='recovery'">{{ t('sourceSupply.readingBlocked') }}</Button>
  <button v-if="!isMobileRuntime() && !chrome" class="novel-reader-immersion novel-icon-button" :aria-label="t(immersive?'novel.exitImmersion':'novel.enterImmersion')" :aria-pressed="immersive" :disabled="immersionBusy" @click="toggleImmersion"><Icon :name="immersive?'exit-fullscreen':'fullscreen'" :size="20"/></button>
  <div v-if="waiting" class="novel-reading-hint cczj-flex cczj-items-center cczj-gap-4" role="status"><Spinner size="sm" inline />{{ t('novelUpgrade.loadingChapter') }}</div>
  <section v-for="entry in frameData" :key="entry.chapter!.id" :ref="el=>bindFrame(el,entry.chapter!.id)" class="novel-chapter-frame" :class="{'novel-chapter-current':entry.chapter!.id===chapter.id}" :data-chapter="entry.chapter!.id" :aria-hidden="entry.chapter!.id!==chapter.id" :inert="entry.chapter!.id!==chapter.id ? true : undefined">
   <div class="novel-reading-running-head" :class="{'novel-reading-running-head-desktop':!isMobileRuntime()}">
    <Button v-if="!chrome" variant="text" class="novel-reader-exit" :aria-label="t('common.back')" :aria-disabled="pageTurn.active.value" @click.stop="!pageTurn.active.value && leaveReader(router, 'novel', `/novel/book/${book.id}`)"><Icon name="chevron-left" :size="16"/></Button>
    <span class="novel-running-title">{{ entry.chapter!.title }}</span>
   </div>
   <div :ref="el=>bindViewport(el,entry.chapter!.id)" class="novel-reader-scroll novel-page-viewport" @scroll.passive="entry.chapter!.id===chapter.id && onScroll()">
    <article :ref="el=>bindTrack(el,entry.chapter!.id)" class="novel-reading-paper" :class="{'novel-pagination-track':paged}" :style="frameStyle(entry)"><NovelChapterText :chapter="entry.chapter!" :text="entry.text!" /></article>
   </div>
   <div class="novel-reading-status"><span>{{ clock }}</span><span v-if="paged" data-page-counter>{{ frameCounter(entry) }}</span><span v-else>{{ entry.chapter!.id===chapter.id?Math.round(ratio*100):0 }}%</span><span data-book-progress>{{ t('novel.bookProgress',{n:framePercent(entry)}) }}</span></div>
  </section>
  <div ref="pageTurnHost" class="novel-page-turn-host" aria-hidden="true" inert />
  <header class="novel-reader-top" :class="{'novel-chrome-hidden':!chrome}" :inert="!chrome ? true : undefined" :aria-hidden="!chrome"><button @click.stop="leaveReader(router, 'novel', `/novel/book/${book.id}`)" class="novel-icon-button" :aria-label="t('common.back')"><Icon name="chevron-left" :size="22" /></button><span>{{ book.title }}</span><button v-if="!isMobileRuntime()" class="novel-icon-button" :aria-label="t(immersive?'novel.exitImmersion':'novel.enterImmersion')" :aria-pressed="immersive" :disabled="immersionBusy" @click="toggleImmersion"><Icon :name="immersive?'exit-fullscreen':'fullscreen'" :size="20"/></button><button class="novel-icon-button" :aria-label="t(reading.inShelf(shelfKey)?'novel.removeShelf':'novel.addShelf')" :aria-pressed="reading.inShelf(shelfKey)" @click="reading.toggleShelf(book)"><Icon :name="reading.inShelf(shelfKey)?'check':'bookmark'" :size="21" /></button><button class="novel-icon-button" :aria-label="t('novel.readingSettings')" @click="panel='preferences'"><Icon name="more" :size="22" /></button></header>
  <div class="novel-reader-bottom" :class="{'novel-chrome-hidden':!chrome}" :inert="!chrome ? true : undefined" :aria-hidden="!chrome"><div class="novel-reader-seek"><button @click="turn(-1)">{{ t('novelUpgrade.previousPage') }}</button><input type="range" min="0" max="1000" :value="slider" :aria-label="t('novel.chapterProgress')" @input="seek(Number(($event.target as HTMLInputElement).value)/1000)"/><button @click="turn(1)">{{ t('novelUpgrade.nextPage') }}</button></div><nav><button @click="panel='chapters'"><Icon name="list" :size="22" /><span>{{ t('novel.chapters') }}</span></button><button @click="reading.setPreferences({theme:reading.preferences.theme==='night'?(reading.preferences.dayTheme??'paper'):'night'})"><Icon :name="reading.preferences.theme==='night'?'sun':'moon'" :size="22" /><span>{{ t(reading.preferences.theme==='night'?'novel.dayMode':'novel.themes.night') }}</span></button><button @click="panel='sources'"><Icon name="database" :size="22"/><span>{{ t('sourceRouting.switchNovel') }}</span></button><button @click="panel='preferences'"><span class="novel-reader-aa">Aa</span><span>{{ t('novel.readingSettings') }}</span></button></nav></div>
  <MotionTransition preset="sheet" appear><ReaderSheet v-if="panel" :title="t(panel==='sources'?'sourceRouting.switchNovel':panel==='recovery'?'sourceSupply.readingBlocked':panel==='chapters'?'novel.chapters':'novel.readingSettings')" @close="panel=''"><NovelSourceSwitch v-if="panel==='sources' && chapter" :item-id="itemId" :chapter-id="failedChapter" @leaving="save"/><section v-else-if="panel==='recovery'" class="novel-reading-recovery"><p role="alert">{{ resource.error.value?.message }}</p><p>{{ t(`sourceSupply.recovery.${recoveryReason}`) }}</p><div class="cczj-flex cczj-flex-wrap cczj-gap-6"><Button v-if="recoveryReason==='temporary'" @click="panel='';resource.retry()">{{ t('common.retry') }}</Button><Button variant="primary" @click="panel='sources'">{{ t('sourceSupply.otherSources') }}</Button><Button variant="text" @click="returnCurrent">{{ t('sourceSupply.returnCurrent') }}</Button></div></section><div v-else-if="panel==='preferences'"><div class="cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4"><Button :loading="textUpdating" @click="updateText()">{{ t('novel.refreshText') }}</Button><Button v-if="textUpdating" variant="text" @click="cancelTextUpdate">{{ t('common.cancel') }}</Button></div><p v-if="textUpdateError" role="status">{{ textUpdateError }}</p><p v-else-if="text?.stale">{{ t('novel.cachedTextStale') }}</p><NovelReaderPreferences /></div><template v-else><p class="novel-directory-heading">{{ book.title }}<span>{{ t('novel.chapterCount',{n:total}) }}</span></p><NovelChapterTools v-model:query="chapterSearch" v-model:descending="descending" :count="chapters.length" :current="!!chapter" :refreshable="true" :refreshing="manualRefreshing" @current="chapter && locateChapter(chapter.id)" @locate="locateNumber" @latest="chapters.length && locateChapter(chapters[chapters.length-1].id)" @refresh="refreshDirectory"/><p v-if="tocError" role="status">{{tocError}}</p><Spinner v-if="directoryLoading && !directoryLoaded" :label="t('common.loading')"/><ol class="novel-chapter-list"><li v-for="entry in visibleEntries" :data-chapter-id="entry.chapter.id" :key="entry.chapter.id" :class="{'novel-chapter-volume':entry.chapter.isVolume,'novel-chapter-gone':!entry.chapter.isVolume&&!entry.chapter.available}"><strong v-if="entry.chapter.isVolume" class="novel-volume-title">{{ entry.chapter.title }}</strong><button v-else :disabled="!entry.chapter.available" :aria-current="chapter?.id===entry.chapter.id?'page':undefined" @click="jumpTo(entry.chapter.id)"><strong>{{ entry.chapter.title }}</strong><small v-if="!entry.chapter.available">{{ t('novel.chapterUnavailable') }}</small><Icon v-else-if="chapter && entry.chapter.id===chapter.id" name="volume" :size="16" /></button></li></ol><BackToTop compact target-selector=".novel-reader .reader-sheet-content"/></template></ReaderSheet></MotionTransition>
  <div v-if="hint" class="novel-reading-hint" role="status">{{ hint }}</div><div class="novel-reader-dim" :style="{opacity:dim}" />
 </div>
  <div v-else-if="resource.status.value === 'ready' && chapter && !chapter.available" class="novel-empty"><h1>{{ t('novel.chapterUnavailableTitle') }}</h1><p>{{ t('novel.chapterUnavailableHint') }}</p><button class="novel-button" @click="leaveToBook">{{ t('novel.backBook') }}</button></div>
  <div v-else-if="resource.status.value === 'empty'" class="novel-empty"><h1>{{ t('novel.bookMissing') }}</h1><button class="novel-button" @click="leaveToExplore">{{ t('novel.explore') }}</button></div>
  <div v-else-if="resource.status.value === 'ready'" class="novel-empty"><h1>{{ t('novel.chapterMissing') }}</h1><button class="novel-button" @click="leaveToBook">{{ t('novel.backBook') }}</button></div>
  <div v-else-if="resource.status.value === 'error'" class="novel-empty"><h1>{{ resource.error.value?.message || t('common.unknownError') }}</h1><p>{{ t(`sourceSupply.recovery.${recoveryReason}`) }}</p><div class="novel-empty-actions"><button v-if="recoveryReason==='temporary'" class="novel-button" @click="resource.retry()">{{ t('common.retry') }}</button><button class="novel-button" @click="leaveToBook">{{ t('novel.backBook') }}</button><button class="novel-button" @click="panel='sources'">{{ t('sourceRouting.switchNovel') }}</button></div><MotionTransition preset="sheet" appear><ReaderSheet v-if="panel==='sources' && resource.status.value==='error'" :title="t('sourceRouting.switchNovel')" @close="panel=''"><NovelSourceSwitch :item-id="itemId" :chapter-id="Number(chapterParam)||0" @leaving="save"/></ReaderSheet></MotionTransition></div>
 <div v-else class="novel-empty"><Spinner :label="t('common.loading')" /></div>
</template>
<style src="../styles/reader.css"></style>
