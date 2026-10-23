<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRef, watch, type ComponentPublicInstance } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { leaveReader } from '../../reading/navigation'
import { useI18n } from 'vue-i18n'
import { useAsyncResource } from '../../reading/asyncResource'
import { refKey, refOf, refreshNovelContext } from '../../api/library'
import { chapterPrefetch } from '../../reading/chapterPrefetch'
import { loadNovelBook, refreshNovelToc } from '../data/novelLibrary'
import { novelReaderSession, type NovelReaderData } from '../reader/session'
import type { NovelChapter } from '../domain'
import NovelSourceSwitch from '../components/NovelSourceSwitch.vue'
import { useNovelReading } from '../readingState'
import { useNovelPages } from '../reader/useNovelPages'
import { pageFromRatio } from '../reader/pagination'
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
import { Button, Spinner } from '../../components/ui'

const route=useRoute(),router=useRouter(),{t}=useI18n(),reading=useNovelReading()
const readerRoot=ref<HTMLElement|null>(null),immersive=ref(false),immersionBusy=ref(false)
const fullscreen=createFullscreenPlatform()
const itemId=computed(()=>Number(route.params.bookId))
const chapterParam=computed(()=>String(route.params.chapter))
const session=novelReaderSession()
const prefetch=chapterPrefetch(id=>session.read({itemId:itemId.value,chapterId:id,remoteId:'',ord:0,first:false}))
let tocRequest:ReturnType<typeof refreshNovelContext>|ReturnType<typeof loadNovelBook>|null=null
const directory=ref<NovelChapter[]>([]),directoryLoaded=ref(false),directoryLoading=ref(false)
function location() {
 const id=itemId.value,param=chapterParam.value
 const record=Object.values(reading.progress).find(row=>row.itemId===id&&String(row.chapterId)===param)
 return {itemId:id,chapterId:Number(param)||0,remoteId:record?.chapterRemoteId??'',ord:record?.chapterOrd??-1,first:param==='first'}
}
const resource=useAsyncResource<NovelReaderData>({
 diagnosticLabel:'novel.reader',
 load:()=>session.read(location()),
 isEmpty:result=>!result.item,
 onData:result=>{
  if(result.item&&result.chapter?.available&&chapterParam.value!==String(result.chapter.id)){
   void router.replace({path:`/novel/read/${result.item.id}/${result.chapter.id}`,query:route.query});return
  }
  void reflow(initialPosition())
  if(!result.item||!result.chapter||!result.text)return
  prefetch.update([result.previous,result.chapter,result.next].filter((row):row is NovelChapter=>!!row),result.previous?1:0)
  if(result.count-result.index>3)return
  const currentChapter=result.chapter.id,currentItem=result.item.id
  const pending=refreshNovelContext(location());tocRequest=pending
  void pending.then(view=>{
   if(tocRequest===pending&&!disposed&&data.value?.chapter?.id===currentChapter&&data.value?.item?.id===currentItem)resource.data.value={...view,text:result.text}
  },()=>{}).finally(()=>{if(tocRequest===pending)tocRequest=null})
 },
})
const data=computed(()=>resource.data.value)
const book=computed(()=>data.value?.item??null)
const chapter=computed(()=>data.value?.chapter??null)
const text=computed(()=>data.value?.text??null)
const chapters=computed(()=>directory.value.filter(row=>!row.isVolume))
const total=computed(()=>data.value?.count??0)
const index=computed(()=>data.value?.index??-1)
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
 directoryLoading.value=true;manualRefreshing.value=refresh;tocError.value=''
 void tocRequest?.cancel()
 const pending=refresh?refreshNovelToc(id):loadNovelBook(id);tocRequest=pending
 try{
  const view=await pending
  if(!disposed&&tocRequest===pending&&itemId.value===id&&view){
   directory.value=view.chapters;directoryLoaded.value=true;tocError.value=view.tocError
   const current=data.value,rows=view.chapters.filter(row=>!row.isVolume),at=rows.findIndex(row=>row.id===current?.chapter?.id)
   if(current&&at>=0)resource.data.value={...current,index:at,count:rows.length,previous:rows.slice(0,at).reverse().find(row=>row.available)??null,next:rows.slice(at+1).find(row=>row.available)??null}
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
function bindViewport(el:Element|ComponentPublicInstance|null):void { viewport.value=el instanceof HTMLElement?el:null }
function bindTrack(el:Element|ComponentPublicInstance|null):void { track.value=el instanceof HTMLElement?el:null }
const paged=computed(()=>reading.preferences.flow==='paged')
const pages=useNovelPages(viewport,track,()=>paged.value),chrome=ref(false),panel=ref<''|'chapters'|'preferences'|'sources'>(''),ratio=ref(0)
const dim=useReadingBrightness(toRef(reading,'preferences'))
const readerStyle=computed(()=>({'--reading-size':reading.preferences.size+'px','--reading-leading':reading.preferences.leading,'--reading-margin':reading.preferences.margin+'px','--reading-font':reading.preferences.serif ? '"Noto Serif CJK SC","Songti SC",SimSun,serif' : 'system-ui,sans-serif'}))
const percent=computed(()=>total.value>0&&index.value>=0?Math.round((index.value+ratio.value)/total.value*100):0)
const slider=computed(()=>Math.round(ratio.value*1000))
const clock=ref(''),hint=ref('')
let ready=false,saveTimer:ReturnType<typeof setTimeout>|undefined,clockTimer:ReturnType<typeof setInterval>|undefined,hintTimer:ReturnType<typeof setTimeout>|undefined,layoutFrame=0,disposed=false
let pointer:{id:number;x:number;y:number;horizontal:boolean;top:boolean;bottom:boolean}|null=null,suppressClick=false
function leaveToBook():void { leaveReader(router, 'novel', `/novel/book/${itemId.value}`) }
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
  if(!ready||!current?.item||!current.chapter||!current.chapter.available||!current.text)return
  reading.remember(shelfKey.value,{itemId:current.item.id,title:current.item.title,chapterOrd:current.chapter.ord,chapterId:current.chapter.id,chapterRemoteId:current.chapter.remoteId,ratio:ratio.value})
}
function notify(message:string):void { hint.value=message;clearTimeout(hintTimer);hintTimer=setTimeout(()=>hint.value='',1800) }
function go(next:NovelChapter|null,end=false):void {
 if(!ready||resource.status.value!=='ready')return
 if(next?.available){save();void router.replace(`/novel/read/${itemId.value}/${next.id}${end?'?position=end':''}`)}
 else notify(t(end?'novel.firstChapter':'novel.lastChapter'))
}
function jumpTo(chapterId:number):void {
 const target=chapters.value.find(item=>item.id===chapterId)
 panel.value=''
 if(target&&target.id!==chapter.value?.id)go(target)
}
function turn(direction:number):void {
 if(panel.value||!ready||resource.status.value!=='ready')return
 if(!paged.value&&viewport.value){
  const el=viewport.value,end=Math.max(0,el.scrollHeight-el.clientHeight)
  if(direction>0&&el.scrollTop>=end-1)go(nextChapter.value)
  else if(direction<0&&el.scrollTop<=1)go(previousChapter.value,true)
  else el.scrollBy({top:direction*el.clientHeight*.85,behavior:'auto'})
  return
 }
 const next=pages.page.value+direction
 if(next<0)go(previousChapter.value,true)
 else if(next>=pages.count.value)go(nextChapter.value)
 else pages.select(next)
}
function seek(value:number):void {
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
async function reflow(position=ratio.value):Promise<void> {
 ready=false;await nextTick();cancelAnimationFrame(layoutFrame)
 layoutFrame=requestAnimationFrame(()=>{
  if(disposed)return
  lastWheelEvent=performance.now()
  if(paged.value){pages.measure(position);ratio.value=pages.ratio.value}
  else if(viewport.value){viewport.value.scrollTop=position*Math.max(0,viewport.value.scrollHeight-viewport.value.clientHeight);ratio.value=position}
  ready=true;save()
  // Entering the previous chapter at its end is a one-time navigation hint.
  // Leaving it in the URL would override subsequent saved progress on reload.
  if(route.query.position==='end')void router.replace({query:{...route.query,position:undefined}})
 })
}
watch(()=>[reading.preferences.size,reading.preferences.leading,reading.preferences.margin,reading.preferences.serif,reading.preferences.flow],()=>{if(data.value)void reflow(ratio.value)})
// 切章走 run：generation 会丢弃迟到的旧章响应，否则慢网络下会出现
// 「正文是上一章、标题是这一章」。
watch(itemId,()=>{prefetch.clear();session.clear();directory.value=[];directoryLoaded.value=false})
watch([itemId,chapterParam],()=>{save();void tocRequest?.cancel();tocRequest=null;directoryLoading.value=false;ready=false;ratio.value=0;panel.value='';void resource.run()},{immediate:true})
watch(panel,value=>{if(value==='chapters'&&!directoryLoaded.value)void loadDirectory()})
function down(event:PointerEvent):void {
 if(event.pointerType==='touch'||!event.isPrimary||event.button!==0||panel.value||!ready)return
 pointer={id:event.pointerId,x:event.clientX,y:event.clientY,horizontal:false,top:(viewport.value?.scrollTop??0)<=1,bottom:!!viewport.value&&viewport.value.scrollTop>=viewport.value.scrollHeight-viewport.value.clientHeight-1};suppressClick=false
}
function move(event:PointerEvent):void {
 if(!pointer||pointer.id!==event.pointerId)return
 const dx=event.clientX-pointer.x,dy=event.clientY-pointer.y
 if(paged.value&&Math.abs(dx)>12&&Math.abs(dx)>Math.abs(dy)*1.3){pointer.horizontal=true;event.preventDefault()}
}
function up(event:PointerEvent):void {
 if(!pointer||pointer.id!==event.pointerId)return
 const start=pointer;pointer=null
 if(start.horizontal){suppressClick=true;if(Math.abs(event.clientX-start.x)>40)turn(event.clientX<start.x?1:-1)}
 else if(Math.abs(event.clientY-start.y)>60&&Math.abs(event.clientY-start.y)>Math.abs(event.clientX-start.x)*1.3){
  suppressClick=true
  if(paged.value)turn(event.clientY<start.y?1:-1)
  else if(start.bottom&&event.clientY<start.y)go(nextChapter.value)
  else if(start.top&&event.clientY>start.y)go(previousChapter.value,true)
 }
}
function cancel():void { if(pointer?.horizontal)suppressClick=true;pointer=null }
// Native vertical scrolling sends pointercancel before the finger is released.
// Touch endpoints survive that cancellation without preventing normal scrolling.
let touchStart:{x:number;y:number;top:boolean;bottom:boolean}|null=null
function touchDown(event:TouchEvent):void {
 const el=viewport.value
 touchStart=null
 if(event.touches.length!==1||panel.value||!ready||!el||(event.target instanceof Element&&event.target.closest('button,a,input')))return
 const point=event.touches[0]!
 touchStart={x:point.clientX,y:point.clientY,top:el.scrollTop<=1,bottom:el.scrollTop>=el.scrollHeight-el.clientHeight-1};suppressClick=false
}
function touchUp(event:TouchEvent):void {
 const start=touchStart;touchStart=null
 if(!start||event.changedTouches.length!==1||window.getSelection()?.toString())return
 const point=event.changedTouches[0]!,dx=point.clientX-start.x,dy=point.clientY-start.y
 if(Math.abs(dy)>60&&Math.abs(dy)>Math.abs(dx)*1.3){
  suppressClick=true
  if(paged.value)turn(dy<0?1:-1)
  else if(start.bottom&&dy<0)go(nextChapter.value)
  else if(start.top&&dy>0)go(previousChapter.value,true)
 }else if(paged.value&&Math.abs(dx)>40&&Math.abs(dx)>Math.abs(dy)*1.3){suppressClick=true;turn(dx<0?1:-1)}
}
function tap(event:MouseEvent):void {
 if(suppressClick){suppressClick=false;return}
 if((event.target as HTMLElement).closest('a,button,input')||window.getSelection()?.toString())return
 const rect=viewport.value?.getBoundingClientRect();if(!rect)return
 const x=(event.clientX-rect.left)/rect.width
 if(paged.value&&(x<.25||x>.75)){turn(x<.25?-1:1);return}
 chrome.value=!chrome.value
}
let wheelTotal=0,lastWheel=0,lastWheelEvent=0
function wheel(event:WheelEvent):void{
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
onMounted(()=>{
 document.addEventListener('fullscreenchange',fullscreenChanged);window.addEventListener('keydown',keyboard);window.addEventListener('pagehide',save);window.addEventListener('cczj:android-overlay-back',back)
 tick();clockTimer=setInterval(tick,60000)
})
onBeforeUnmount(()=>{save();disposed=true;if(immersive.value)void fullscreen.exit(document.documentElement,()=>true).catch(()=>{});delete document.body.dataset.readingImmersive;document.removeEventListener('fullscreenchange',fullscreenChanged);prefetch.clear();session.clear();void tocRequest?.cancel();tocRequest=null;clearTimeout(saveTimer);clearTimeout(hintTimer);clearInterval(clockTimer);cancelAnimationFrame(layoutFrame);window.removeEventListener('keydown',keyboard);window.removeEventListener('pagehide',save);window.removeEventListener('cczj:android-overlay-back',back)})
</script>
<template>
 <div v-if="(resource.status.value === 'ready' || resource.status.value === 'loading') && book && chapter && chapter.available && text" ref="readerRoot" class="novel-reader" :aria-busy="resource.status.value==='loading'" :class="{'novel-reader-paged':paged,'novel-reader-immersive':immersive}" :data-reading-theme="reading.preferences.theme" :style="readerStyle">
  <button v-if="!chrome" class="novel-reader-exit novel-icon-button" @click.stop="leaveReader(router, 'novel', `/novel/book/${book.id}`)" :aria-label="t('common.back')"><Icon name="chevron-left" :size="22" /></button>
  <button v-if="!isMobileRuntime() && !chrome" class="novel-reader-immersion novel-icon-button" :aria-label="t(immersive?'novel.exitImmersion':'novel.enterImmersion')" :aria-pressed="immersive" :disabled="immersionBusy" @click="toggleImmersion"><Icon :name="immersive?'exit-fullscreen':'fullscreen'" :size="20"/></button>
  <div class="novel-reading-running-head"><span>{{ chapter.title }}</span></div>
  <div :ref="bindViewport" class="novel-reader-scroll novel-page-viewport" @scroll.passive="onScroll" @touchstart.passive="touchDown" @touchend.passive="touchUp" @touchcancel.passive="touchStart=null" @wheel="wheel" @pointerdown="down" @pointermove="move" @pointerup="up" @pointercancel="cancel" @lostpointercapture="cancel" @click="tap">
   <article :ref="bindTrack" class="novel-reading-paper" :class="{'novel-pagination-track':paged}" :style="paged?{transform:pages.transform.value}:undefined">
    <h1>{{ chapter.title }}</h1>
    <div v-if="text.warnings && text.warnings.length" class="novel-reading-warnings" role="note"><p v-for="(warning,i) in text.warnings" :key="i"><Icon name="alert-triangle" :size="14" />{{ warning }}</p></div>
     <p v-for="(paragraph,i) in text.paragraphs" :key="i" class="novel-reading-paragraph">{{ paragraph }}</p>
    <nav v-if="!paged" class="novel-scroll-chapters"><Button :disabled="!previousChapter" @click.stop="go(previousChapter,true)">{{t('novel.previousChapter')}}</Button><Button :disabled="!nextChapter" @click.stop="go(nextChapter)">{{t('novel.nextChapter')}}</Button></nav>
   </article>
  </div>
  <div class="novel-reading-status"><span>{{ clock }}</span><span v-if="paged">{{ pages.page.value+1 }} / {{ pages.count.value }}</span><span v-else>{{ Math.round(ratio*100) }}%</span><span>{{ t('novel.bookProgress',{n:percent}) }}</span></div>
  <header class="novel-reader-top" :class="{'novel-chrome-hidden':!chrome}"><button @click.stop="leaveReader(router, 'novel', `/novel/book/${book.id}`)" class="novel-icon-button" :aria-label="t('common.back')"><Icon name="chevron-left" :size="22" /></button><span>{{ book.title }}</span><button v-if="!isMobileRuntime()" class="novel-icon-button" :aria-label="t(immersive?'novel.exitImmersion':'novel.enterImmersion')" :aria-pressed="immersive" :disabled="immersionBusy" @click="toggleImmersion"><Icon :name="immersive?'exit-fullscreen':'fullscreen'" :size="20"/></button><button class="novel-icon-button" :aria-label="t(reading.inShelf(shelfKey)?'novel.removeShelf':'novel.addShelf')" :aria-pressed="reading.inShelf(shelfKey)" @click="reading.toggleShelf(book)"><Icon :name="reading.inShelf(shelfKey)?'check':'bookmark'" :size="21" /></button><button class="novel-icon-button" :aria-label="t('novel.readingSettings')" @click="panel='preferences'"><Icon name="more" :size="22" /></button></header>
  <div class="novel-reader-bottom" :class="{'novel-chrome-hidden':!chrome}"><div class="novel-reader-seek"><button :disabled="!previousChapter" @click="go(previousChapter,true)">{{ t('novel.previousChapter') }}</button><input type="range" min="0" max="1000" :value="slider" :aria-label="t('novel.chapterProgress')" @input="seek(Number(($event.target as HTMLInputElement).value)/1000)"/><button :disabled="!nextChapter" @click="go(nextChapter)">{{ t('novel.nextChapter') }}</button></div><nav><button @click="panel='chapters'"><Icon name="list" :size="22" /><span>{{ t('novel.chapters') }}</span></button><button @click="reading.setPreferences({theme:reading.preferences.theme==='night'?'paper':'night'})"><Icon :name="reading.preferences.theme==='night'?'sun':'moon'" :size="22" /><span>{{ t(reading.preferences.theme==='night'?'novel.dayMode':'novel.themes.night') }}</span></button><button @click="panel='sources'"><Icon name="database" :size="22"/><span>{{ t('sourceRouting.switchNovel') }}</span></button><button @click="panel='preferences'"><span class="novel-reader-aa">Aa</span><span>{{ t('novel.readingSettings') }}</span></button></nav></div>
  <ReaderSheet v-if="panel" :title="t(panel==='sources'?'sourceRouting.switchNovel':panel==='chapters'?'novel.chapters':'novel.readingSettings')" @close="panel=''"><NovelSourceSwitch v-if="panel==='sources' && chapter" :item-id="itemId" :chapter-id="chapter.id" @leaving="save"/><NovelReaderPreferences v-else-if="panel==='preferences'" /><template v-else><p class="novel-directory-heading">{{ book.title }}<span>{{ t('novel.chapterCount',{n:total}) }}</span></p><NovelChapterTools v-model:query="chapterSearch" v-model:descending="descending" :count="chapters.length" :current="!!chapter" :refreshable="true" :refreshing="manualRefreshing" @current="chapter && locateChapter(chapter.id)" @locate="locateNumber" @latest="chapters.length && locateChapter(chapters[chapters.length-1].id)" @refresh="refreshDirectory"/><p v-if="tocError" role="status">{{tocError}}</p><Spinner v-if="directoryLoading && !directoryLoaded" :label="t('common.loading')"/><ol class="novel-chapter-list"><li v-for="entry in visibleEntries" :data-chapter-id="entry.chapter.id" :key="entry.chapter.id" :class="{'novel-chapter-volume':entry.chapter.isVolume,'novel-chapter-gone':!entry.chapter.isVolume&&!entry.chapter.available}"><strong v-if="entry.chapter.isVolume" class="novel-volume-title">{{ entry.chapter.title }}</strong><button v-else :disabled="!entry.chapter.available" :aria-current="chapter?.id===entry.chapter.id?'page':undefined" @click="jumpTo(entry.chapter.id)"><strong>{{ entry.chapter.title }}</strong><small v-if="!entry.chapter.available">{{ t('novel.chapterUnavailable') }}</small><Icon v-else-if="chapter && entry.chapter.id===chapter.id" name="volume" :size="16" /></button></li></ol><BackToTop compact target-selector=".novel-reader .reader-sheet-content"/></template></ReaderSheet>
  <div v-if="hint" class="novel-reading-hint" role="status">{{ hint }}</div><div class="novel-reader-dim" :style="{opacity:dim}" />
 </div>
  <div v-else-if="resource.status.value === 'ready' && chapter && !chapter.available" class="novel-empty"><h1>{{ t('novel.chapterUnavailableTitle') }}</h1><p>{{ t('novel.chapterUnavailableHint') }}</p><button class="novel-button" @click="leaveToBook">{{ t('novel.backBook') }}</button></div>
  <div v-else-if="resource.status.value === 'empty'" class="novel-empty"><h1>{{ t('novel.bookMissing') }}</h1><button class="novel-button" @click="leaveToExplore">{{ t('novel.explore') }}</button></div>
  <div v-else-if="resource.status.value === 'ready'" class="novel-empty"><h1>{{ t('novel.chapterMissing') }}</h1><button class="novel-button" @click="leaveToBook">{{ t('novel.backBook') }}</button></div>
  <div v-else-if="resource.status.value === 'error'" class="novel-empty"><h1>{{ resource.error.value?.message || t('common.unknownError') }}</h1><div class="novel-empty-actions"><button class="novel-button" @click="resource.retry()">{{ t('common.retry') }}</button><button class="novel-button" @click="leaveToBook">{{ t('novel.backBook') }}</button><button class="novel-button" @click="panel='sources'">{{ t('sourceRouting.switchNovel') }}</button></div><ReaderSheet v-if="panel==='sources' && resource.status.value==='error'" :title="t('sourceRouting.switchNovel')" @close="panel=''"><NovelSourceSwitch :item-id="itemId" :chapter-id="Number(chapterParam)||0" @leaving="save"/></ReaderSheet></div>
 <div v-else class="novel-empty"><Spinner :label="t('common.loading')" /></div>
</template>
<style src="../styles/reader.css"></style>
