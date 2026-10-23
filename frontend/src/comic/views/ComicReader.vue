<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { leaveReader } from '../../reading/navigation'
import { useI18n } from 'vue-i18n'
import type { ComponentPublicInstance } from 'vue'
import { useAsyncResource } from '../../reading/asyncResource'
import ReaderSheet from '../../reading/ReaderSheet.vue'
import { useReadingBrightness } from '../../reading/useReadingBrightness'
import Icon from '../../components/Icon.vue'
import ComicPanel from '../components/ComicPanel.vue'
import ComicSourceSwitch from '../components/ComicSourceSwitch.vue'
import ComicReaderPreferences from '../components/ComicReaderPreferences.vue'
import ComicLoadState from '../components/ComicLoadState.vue'
import { useComicState } from '../state'
import { comicReaderSession, type ComicEpisode } from '../reader/session'
import { loadComicSeries, refreshComicToc } from '../data/repository'
import type { ComicChapter } from '../domain'
import ChapterTools from '../../reading/ChapterTools.vue'
import BackToTop from '../../components/BackToTop.vue'
import { Spinner } from '../../components/ui'
import { createFullscreenPlatform } from '../../platform/fullscreen'
import { isMobileRuntime } from '../../api/runtime'
import { progressChapter } from '../reader/progress'
import { useComicPages } from '../reader/useComicPages'
import { refreshChapterPages, refreshComicContext } from '../../api/library'
import { normalizeApiError } from '../../api/app'

const route = useRoute(), router = useRouter(), { t } = useI18n(), state = useComicState()
const itemId = computed(()=>Number(route.params.seriesId)), ord = computed(()=>Number(route.params.episode))
const session=comicReaderSession()
function location(){return {itemId:itemId.value,chapterId:0,remoteId:typeof route.query.episodeRemoteId==='string'?route.query.episodeRemoteId:'',ord:ord.value,first:false}}
const readerRoot=ref<HTMLElement|null>(null),immersive=ref(false),immersionBusy=ref(false),fullscreen=createFullscreenPlatform()
const directory=ref<ComicChapter[]>([]),directoryLoaded=ref(false),directoryLoading=ref(false),descending=ref(false),chapterQuery=ref(''),tocError=ref('')
let tocRequest:ReturnType<typeof loadComicSeries>|ReturnType<typeof refreshComicContext>|null=null
const viewport = ref<HTMLElement | null>(null)
const page = ref(0), offset = ref(0), chrome = ref(false), sheet = ref<'' | 'chapters' | 'settings' | 'sources'>(''), hint = ref(''), notices = ref(true)
const pager = useComicPages()
const { pages: pageTable, generation: pageToken } = pager
const strip = computed(() => state.preferences.flow === 'strip')
const dim = useReadingBrightness(toRef(state, 'preferences'))

const { status, data, error, retry, run } = useAsyncResource<ComicEpisode>({
  diagnosticLabel:'comic.reader',
  load: () => session.read(location()),
  isEmpty: episode => episode.pages.length === 0,
  onData: episode => {
    restore(episode)
    if(episode.count-episode.index>3||directoryLoading.value)return
    void tocRequest?.cancel()
    const id=episode.chapter.id,pending=refreshComicContext(location());tocRequest=pending
    void pending.then(view=>{if(tocRequest===pending&&!disposed&&data.value?.chapter.id===id)data.value={...data.value,previous:view.previous,next:view.next,count:view.count,index:view.index}},()=>{}).finally(()=>{if(tocRequest===pending)tocRequest=null})
  },
})

const total = computed(() => data.value?.pages.length ?? 0)
const chapters = computed(() => directory.value.filter(row=>!row.isVolume).sort((a,b)=>a.ord-b.ord))
const visibleChapters=computed(()=>chapters.value.filter(chapter=>!chapterQuery.value.trim()||`${chapter.ord+1} ${chapter.title}`.toLocaleLowerCase().includes(chapterQuery.value.trim().toLocaleLowerCase())).sort((a,b)=>descending.value?b.ord-a.ord:a.ord-b.ord))
const nextChapter=computed(()=>data.value?.next??null),previousChapter=computed(()=>data.value?.previous??null)
const following = computed(() => !!data.value && state.isFollowed(state.keyOf(data.value.item)))
const warnings = computed(() => data.value?.warnings ?? [])
/** 翻页模式只挂窗口内那几页，当前页用 v-show 切换，其余留在 DOM 里预取。 */
const visiblePages = computed(() => {
  const list: number[] = []
  for (let index = 0; index < total.value; index++) if (pager.inWindow(index)) list.push(index)
  return list
})
const activePages = computed(() => new Set(visiblePages.value))
let pageRefresh:ReturnType<typeof refreshChapterPages>|null=null
async function retryPage(index:number):Promise<void>{
  if(!data.value||pageRefresh)return
  const chapterId=data.value.chapter.id,at=page.value,within=offset.value
  session.invalidate(data.value.cacheIdentity)
  const request=refreshChapterPages(chapterId);pageRefresh=request
  try{
    const refreshed=await request
    if(disposed||data.value?.chapter.id!==chapterId)return
    data.value={...data.value,pages:refreshed.pages.filter((row):row is NonNullable<typeof row>=>!!row),fromCache:false,warnings:refreshed.warnings}
    pager.reset(data.value.pages.length,at);page.value=pager.current.value;offset.value=within
    await nextTick();if(!disposed)seek(page.value,within)
    pager.retry(index)
  }catch(raw){if(!disposed)notify(normalizeApiError(raw).message)}finally{if(pageRefresh===request)pageRefresh=null}
}

let pinEnd=false
let ready = false, disposed = false, timer: ReturnType<typeof setTimeout> | undefined, hintTimer: ReturnType<typeof setTimeout> | undefined, resize: ResizeObserver | undefined, frame = 0, suppress = false, pointer: { id: number; x: number; y: number; horizontal: boolean } | null = null

function save(): void {
  clearTimeout(timer)
  const loaded = data.value
  if (!ready || !loaded) return
  state.remember(loaded.item, { ord: loaded.chapter.ord, id: loaded.chapter.id, remoteId: loaded.chapter.remoteId }, page.value, offset.value)
}

function schedule(): void { clearTimeout(timer); timer = setTimeout(save, 500) }

function notify(text: string): void { hint.value = text; clearTimeout(hintTimer); hintTimer = setTimeout(() => hint.value = '', 1600) }

/** 新话到了：先把上一话的页状态整表作废（代号加一），再按进度落位。 */
function restore(loaded: ComicEpisode): void {
  const count = loaded.pages.length
  const saved = state.progress[state.keyOf(loaded.item)]
  const resumed = route.query.sourceSwitch==='1' ? null : saved && progressChapter([loaded.chapter], saved) ? saved : null
  const atEnd=route.query.position==='end'
  const requested = route.query.position === 'end' ? count - 1 : resumed ? resumed.page : 0
  pinEnd=atEnd
  notices.value = true
  // Clamp against the incoming chapter, not the previous (initially empty)
  // page table. Clamping before reset silently discarded every saved position.
  pager.reset(count, requested,loaded.ratios)
  const at = pager.current.value
  page.value = at
  offset.value = atEnd ? 1 : resumed ? resumed.offset : 0
  ready = true
  void nextTick().then(() => { if (!disposed) {seek(at,offset.value);lastWheelEvent=performance.now();if(atEnd){const query={...route.query};delete query.position;void router.replace({path:route.path,query})}} })
}

function pageReady(index:number,gen:number,ratio:number):void {
 if(gen!==pageToken.value||!data.value)return
 pager.succeed(index,gen,ratio);data.value.ratios[index]=ratio;session.rememberRatio(data.value.cacheIdentity,index,ratio)
 if(pinEnd&&strip.value)void nextTick().then(()=>{if(viewport.value&&pinEnd)viewport.value.scrollTop=viewport.value.scrollHeight})
}

function seek(next: number, within = 0): void {
  if (!data.value) return
  const at = pager.clamp(next)
  page.value = at
  offset.value = within
  pager.seek(at)
  if (strip.value && viewport.value) {
    const el = viewport.value.querySelectorAll<HTMLElement>('.comic-panel')[at]
    if (el) viewport.value.scrollTop = (pinEnd||within===1) && at===total.value-1 ? viewport.value.scrollHeight : el.offsetTop + within * el.offsetHeight
  }
  schedule()
}

function selectPage(next:number):void{pinEnd=false;seek(next)}

function openChapter(next: number): void {
  const chapter = chapters.value.find(entry => entry.ord === next)
  sheet.value=''
  if (!chapter || chapter.id === data.value?.chapter.id) return
  save()
  if (chapter) void router.replace(state.readPath(itemId.value, chapter.ord, chapter.remoteId))
}

function step(delta: number, end = false): void {
  if(!ready||status.value!=='ready')return
  const chapter = delta < 0 ? previousChapter.value : nextChapter.value
  if (!chapter) { notify(t(delta < 0 ? 'comic.firstEpisode' : 'comic.lastEpisode')); return }
  save()
  const path = state.readPath(itemId.value, chapter.ord, chapter.remoteId)
  void router.replace(end ? `${path}${path.includes('?') ? '&' : '?'}position=end` : path)
}

function turn(direction: number): void {
  pinEnd=false
  if (sheet.value || !data.value || !ready || status.value!=='ready') return
  if(strip.value&&viewport.value){const el=viewport.value,end=el.scrollHeight-el.clientHeight
    if(direction>0&&el.scrollTop>=end-1)step(1)
    else if(direction<0&&el.scrollTop<=1)step(-1,true)
    else el.scrollBy({top:direction*el.clientHeight*.85,behavior:'auto'})
    return
  }
  const next = page.value + direction
  if (next < 0) step(-1, true)
  else if (next >= total.value) step(1)
  else seek(next)
}

function scroll(): void {
  if (!ready || !strip.value || !viewport.value || !total.value) return
  const root = viewport.value, panels = Array.from(root.querySelectorAll<HTMLElement>('.comic-panel'))
  let at = 0
  for (let i = 0; i < panels.length; i++) { if (panels[i].offsetTop <= root.scrollTop + root.clientHeight * .25) at = i; else break }
  page.value = at
  offset.value = panels[at] ? Math.max(0, Math.min(1, (root.scrollTop - panels[at].offsetTop) / panels[at].offsetHeight)) : 0
  pager.seek(at)
  schedule()
}

function down(e: PointerEvent): void { if (e.pointerType==='touch' || !e.isPrimary || e.button !== 0 || strip.value || sheet.value) return; pointer = { id: e.pointerId, x: e.clientX, y: e.clientY, horizontal: false }; suppress = false }
function move(e: PointerEvent): void { if (!pointer || pointer.id !== e.pointerId) return; const dx = e.clientX - pointer.x, dy = e.clientY - pointer.y; if (Math.abs(dx) > 12 && Math.abs(dx) > Math.abs(dy) * 1.3) { pointer.horizontal = true; e.preventDefault() } }
function up(e: PointerEvent): void { if (!pointer || pointer.id !== e.pointerId) return; const start = pointer; pointer = null; if (start.horizontal) { suppress = true; if (Math.abs(e.clientX - start.x) > 40) turn((e.clientX < start.x ? 1 : -1) * (state.preferences.direction === 'rtl' ? -1 : 1)) } }
function cancelPointer(): void { if (pointer?.horizontal) suppress = true; pointer = null }

function tap(e: MouseEvent): void {
  if (suppress) { suppress = false; return }
  if ((e.target as Element).closest('a,button,input')) return
  const rect = viewport.value?.getBoundingClientRect(); if (!rect) return
  const x = (e.clientX - rect.left) / rect.width
  if (!strip.value && (x < .25 || x > .75)) { turn((x < .25 ? -1 : 1) * (state.preferences.direction === 'rtl' ? -1 : 1)); return }
  chrome.value = !chrome.value
}

let wheelTotal=0,lastWheel=0,lastWheelEvent=0
function wheel(e:WheelEvent):void {
 if(!state.preferences.wheelPaging||sheet.value||e.ctrlKey||!ready||status.value!=='ready')return
 pinEnd=false
 const el=viewport.value;if(!el)return
 if(strip.value){
  if(!e.deltaY)return
  const boundary=e.deltaY>0?el.scrollTop>=el.scrollHeight-el.clientHeight-1:el.scrollTop<=1
  if(!boundary){lastWheelEvent=performance.now();return}
  if(performance.now()-lastWheelEvent<180){lastWheelEvent=performance.now();return}
 }
 e.preventDefault()
 const now=performance.now();if(now-lastWheelEvent>180)wheelTotal=0
 lastWheelEvent=now;if(now-lastWheel<220)return
 const delta=Math.abs(e.deltaX)>Math.abs(e.deltaY)?e.deltaX:e.deltaY
 wheelTotal+=delta*(e.deltaMode===1?16:e.deltaMode===2?el.clientHeight:1)
 if(Math.abs(wheelTotal)>=45){if(strip.value)step(wheelTotal>0?1:-1,wheelTotal<0);else turn(wheelTotal>0?1:-1);wheelTotal=0;lastWheel=now}
}
let touchStart:{x:number;y:number;top:boolean;bottom:boolean}|null=null
function touchDown(e:TouchEvent):void {
 pinEnd=false
 const el=viewport.value;touchStart=null
 if(e.touches.length!==1||sheet.value||!ready||!el||(e.target instanceof Element&&e.target.closest('button,a,input')))return
 const point=e.touches[0]!
 touchStart={x:point.clientX,y:point.clientY,top:el.scrollTop<=1,bottom:el.scrollTop>=el.scrollHeight-el.clientHeight-1};suppress=false
}
function touchUp(e:TouchEvent):void {
 const start=touchStart;touchStart=null
 if(!start||e.changedTouches.length!==1)return
 const point=e.changedTouches[0]!,dx=point.clientX-start.x,dy=point.clientY-start.y
 if(Math.abs(dy)>60&&Math.abs(dy)>Math.abs(dx)*1.3){suppress=true;if(!strip.value)turn(dy<0?1:-1);else if(start.bottom&&dy<0)step(1);else if(start.top&&dy>0)step(-1,true)}
 else if(!strip.value&&Math.abs(dx)>40&&Math.abs(dx)>Math.abs(dy)*1.3){suppress=true;turn((dx<0?1:-1)*(state.preferences.direction==='rtl'?-1:1))}
}
function key(e: KeyboardEvent): void {
 if(e.defaultPrevented||e.isComposing||e.ctrlKey||e.altKey||e.metaKey||sheet.value||(e.target instanceof Element&&e.target.closest('input,textarea,select,[contenteditable="true"],[role="slider"]')))return
 if(e.key==='Escape'){e.preventDefault();if(immersive.value)void toggleImmersion();else chrome.value=!chrome.value;return}
 if(e.code==='KeyF'){e.preventDefault();void toggleImmersion();return}
 if((e.target instanceof Element&&e.target.closest('button,a'))&&e.code==='Space')return
 if(!state.preferences.keyboardPaging)return
 const horizontal=e.code===state.preferences.previousKey||e.code===state.preferences.nextKey
 const direction=horizontal?(e.code===state.preferences.nextKey?1:-1)*(state.preferences.direction==='rtl'?-1:1):['PageDown','ArrowDown','Space'].includes(e.code)?(e.shiftKey&&e.code==='Space'?-1:1):['PageUp','ArrowUp'].includes(e.code)?-1:0
 if(direction){e.preventDefault();turn(direction)}
}
async function toggleImmersion():Promise<void>{
 if(immersionBusy.value)return
 immersionBusy.value=true
 try{if(immersive.value){await fullscreen.exit(document.documentElement,()=>!disposed);immersive.value=false;delete document.body.dataset.readingImmersive}
 else{immersive.value=true;chrome.value=false;document.body.dataset.readingImmersive='1';await nextTick();const entered=await fullscreen.enter(document.documentElement,()=>!disposed);if(disposed){await fullscreen.release(document.documentElement,()=>true);return}if(!entered)throw new Error(t('novel.immersionUnavailable'))}}
 catch(raw){immersive.value=false;delete document.body.dataset.readingImmersive;notify(normalizeApiError(raw).message)}finally{immersionBusy.value=false}
}
function fullscreenChanged(){if(fullscreen.kind==='element'&&!fullscreen.browserActive(document.documentElement)){immersive.value=false;delete document.body.dataset.readingImmersive}}
async function loadDirectory(refresh=false):Promise<void>{
 if(directoryLoading.value)return
 directoryLoading.value=true;tocError.value=''
 void tocRequest?.cancel()
 const id=itemId.value,pending=refresh?refreshComicToc(id):loadComicSeries(id);tocRequest=pending
 try{const view=await pending;if(!disposed&&tocRequest===pending&&itemId.value===id){directory.value=view.chapters;directoryLoaded.value=true;tocError.value=view.tocError
  const current=data.value,rows=view.chapters.filter(row=>!row.isVolume).sort((a,b)=>a.ord-b.ord),at=rows.findIndex(row=>row.id===current?.chapter.id)
  if(current&&at>=0)data.value={...current,index:at,count:rows.length,previous:rows.slice(0,at).reverse().find(row=>row.available)??null,next:rows.slice(at+1).find(row=>row.available)??null}
 }}catch(raw){if(!disposed&&tocRequest===pending)tocError.value=normalizeApiError(raw).message}finally{if(tocRequest===pending)tocRequest=null;directoryLoading.value=false}
}
async function locateChapter(id:number){chapterQuery.value='';await nextTick();document.querySelector(`.comic-reader-episodes [data-chapter-id="${id}"]`)?.scrollIntoView({block:'center',behavior:'auto'})}
function locateNumber(n:number){const chapter=chapters.value[n-1];if(chapter)void locateChapter(chapter.id)}
watch(sheet,value=>{if(value==='chapters'&&!directoryLoaded.value)void loadDirectory()})
watch(()=>[route.params.seriesId,route.params.episode,route.query.episodeRemoteId],()=>{save();ready=false;sheet.value='';void pageRefresh?.cancel();pageRefresh=null;void tocRequest?.cancel();tocRequest=null;void run()},{immediate:true})

function back(e: Event): void { if (chrome.value && !sheet.value) { e.preventDefault(); e.stopImmediatePropagation(); chrome.value = false } }

function onViewport(el: Element | ComponentPublicInstance | null): void {
  const node=el instanceof HTMLElement?el:null
  if(viewport.value===node)return
  viewport.value=node
  resize?.disconnect()
  resize = undefined
  if (!viewport.value) return
  resize = new ResizeObserver(() => {
    if (!ready || !strip.value) return
    const at = page.value, pos = offset.value
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => { if (!disposed) seek(at, pos) })
  })
  resize.observe(viewport.value)
}

watch(strip, async () => {
  const at = page.value, pos = offset.value
  ready = false
  await nextTick()
  if (disposed) return
  seek(at, pos)
  ready = true
  save()
})

onMounted(async () => {
  document.addEventListener('fullscreenchange',fullscreenChanged)
  window.addEventListener('keydown', key); window.addEventListener('pagehide', save); window.addEventListener('cczj:android-overlay-back', back)
  await nextTick()
  // 数据在挂载之前就回来时，restore 里的落位找不到 viewport，这里补一次。
  if (!disposed && ready) seek(page.value, offset.value)
})

onBeforeUnmount(() => {
  void pageRefresh?.cancel();pageRefresh=null
  save(); disposed = true;session.clear();void tocRequest?.cancel();tocRequest=null
  if(immersive.value)void fullscreen.exit(document.documentElement,()=>true).catch(()=>{})
  delete document.body.dataset.readingImmersive;document.removeEventListener('fullscreenchange',fullscreenChanged); clearTimeout(timer); clearTimeout(hintTimer); cancelAnimationFrame(frame); resize?.disconnect()
  window.removeEventListener('keydown', key); window.removeEventListener('pagehide', save); window.removeEventListener('cczj:android-overlay-back', back)
})
</script>
<template>
  <div v-if="data&&(status==='ready'||status==='loading')" ref="readerRoot" class="comic-reader" :aria-busy="status==='loading'" :class="{ 'comic-reader-pages': !strip, 'comic-reader-immersive':immersive }">
    <div :ref="onViewport" class="comic-reader-viewport" @scroll.passive="scroll" @wheel="wheel" @touchstart.passive="touchDown" @touchend.passive="touchUp" @touchcancel.passive="touchStart=null" @pointerdown="down" @pointermove="move" @pointerup="up" @pointercancel="cancelPointer" @lostpointercapture="cancelPointer" @click="tap">
      <div v-if="strip" class="comic-strip">
        <ComicPanel v-for="(item, index) in data.pages" :key="`${pageToken}-${index}`" :url="item.url" :alt="t('comic.reader.pageAlt', { n: index + 1 })" :status="pageTable[index]?.status ?? 'queued'" :active="activePages.has(index)" :number="index + 1" :ratio="pageTable[index]?.ratio ?? 0" :nonce="pageTable[index]?.nonce ?? 0" :generation="pageToken" mode="strip" @ready="(gen, ratio) => pageReady(index, gen, ratio)" @failed="gen => pager.fail(index, gen)" @retry="retryPage(index)" />
    <div class="comic-episode-end">
          <p>{{ t('comic.episodeEnd') }}</p>
        <button v-if="nextChapter" @click.stop="step(1)">{{ t('comic.nextEpisode') }}<Icon name="chevron-right" :size="18" /></button>
          <button v-else @click.stop="leaveReader(router, 'comic', `/comic/series/${itemId}`)">{{ t('comic.backSeries') }}</button>
        </div>
      </div>
      <template v-else>
        <ComicPanel v-for="index in visiblePages" v-show="index === page" :key="`${pageToken}-${index}`" :url="data.pages[index].url" :alt="t('comic.reader.pageAlt', { n: index + 1 })" :status="pageTable[index]?.status ?? 'queued'" :active="activePages.has(index)" :number="index + 1" :nonce="pageTable[index]?.nonce ?? 0" :generation="pageToken" mode="page" @ready="(gen, ratio) => pageReady(index, gen, ratio)" @failed="gen => pager.fail(index, gen)" @retry="retryPage(index)" />
</template>
    </div>
    <div v-if="warnings.length && notices" class="comic-reader-warnings" role="status">
      <Icon name="info" :size="17" />
      <ul><li v-for="warning in warnings" :key="warning">{{ warning }}</li></ul>
      <button type="button" :aria-label="t('common.close')" @click.stop="notices = false"><Icon name="close" :size="16" /></button>
    </div>
    <div class="comic-reader-indicator"><span>{{ data.chapter.title }}</span><span>{{ page + 1 }} / {{ total }}</span></div>
    <button v-if="!chrome" @click.stop="leaveReader(router, 'comic', `/comic/series/${itemId}`)" class="comic-reader-exit comic-icon-button" :aria-label="t('common.back')"><Icon name="chevron-left" :size="22" /></button>
    <button v-if="!isMobileRuntime()&&!chrome" class="comic-reader-immersion comic-icon-button" :aria-label="t(immersive?'novel.exitImmersion':'novel.enterImmersion')" :aria-pressed="immersive" :disabled="immersionBusy" @click="toggleImmersion"><Icon :name="immersive?'exit-fullscreen':'fullscreen'" :size="20"/></button>
    <header class="comic-reader-top" :class="{ 'comic-controls-hidden': !chrome }">
      <button @click.stop="leaveReader(router, 'comic', `/comic/series/${itemId}`)" class="comic-icon-button" :aria-label="t('common.back')"><Icon name="chevron-left" :size="22" /></button>
      <div><strong>{{ data.item.title }}</strong><small>{{ data.chapter.title }}</small></div>
      <button class="comic-icon-button" :aria-pressed="following" :aria-label="t(following ? 'comic.unfollow' : 'comic.follow')" @click="state.toggle(data.item)"><Icon :name="following ? 'check' : 'plus'" :size="22" /></button>
    </header>
    <footer class="comic-reader-bottom" :class="{ 'comic-controls-hidden': !chrome }">
      <div class="comic-reader-seek">
        <button :disabled="!previousChapter" @click="step(-1, true)">{{ t('comic.previousEpisode') }}</button>
        <input type="range" min="1" :max="total" :value="page + 1" :aria-label="t('comic.pageProgress')" @input="selectPage(Number(($event.target as HTMLInputElement).value) - 1)" />
        <button :disabled="!nextChapter" @click="step(1)">{{ t('comic.nextEpisode') }}</button>
      </div>
      <nav>
        <button @click="sheet = 'chapters'"><Icon name="list" :size="22" /><span>{{ t('comic.episodes') }}</span></button>
        <button @click="state.setPreferences({ flow: strip ? 'pages' : 'strip' })"><Icon :name="strip ? 'panels' : 'list'" :size="22" /><span>{{ t(strip ? 'comic.pages' : 'comic.strip') }}</span></button>
        <button @click="sheet = 'sources'"><Icon name="database" :size="22"/><span>{{ t('sourceRouting.switchComic') }}</span></button><button @click="sheet = 'settings'"><Icon name="settings" :size="22" /><span>{{ t('comic.readerSettings') }}</span></button>
      </nav>
    </footer>
    <ReaderSheet v-if="sheet" :title="t(sheet === 'sources' ? 'sourceRouting.switchComic' : sheet === 'chapters' ? 'comic.episodes' : 'comic.readerSettings')" @close="sheet = ''">
      <ComicSourceSwitch v-if="sheet === 'sources'" :item-id="itemId" :chapter-id="data.chapter.id" @leaving="save"/><ComicReaderPreferences v-else-if="sheet === 'settings'" />
      <template v-else>
       <ChapterTools mode="comic" v-model:query="chapterQuery" v-model:descending="descending" :count="chapters.length" :current="true" refreshable :refreshing="directoryLoading" @current="locateChapter(data.chapter.id)" @locate="locateNumber" @latest="chapters.length&&locateChapter(chapters[chapters.length-1].id)" @refresh="loadDirectory(true)"/>
       <p v-if="tocError" role="status">{{tocError}}</p><Spinner v-if="directoryLoading&&!directoryLoaded" :label="t('common.loading')"/>
       <ol class="comic-reader-episodes"><li v-for="chapter in visibleChapters" :key="chapter.id" :data-chapter-id="chapter.id"><button :aria-current="chapter.id===data.chapter.id?'page':undefined" :disabled="!chapter.available" @click="openChapter(chapter.ord)"><strong>{{chapter.title}}</strong><em v-if="!chapter.available">{{t('comic.chapterUnavailable')}}</em><Icon v-if="chapter.id===data.chapter.id" name="check" :size="18"/></button></li></ol>
       <BackToTop compact target-selector=".comic-reader .reader-sheet-content"/>
      </template>
    </ReaderSheet>
    <div v-if="hint" class="comic-reader-hint" role="status">{{ hint }}</div>
    <div class="comic-reader-dim" :style="{ opacity: dim }" />
  </div>
  <div v-else class="comic-reader comic-reader-idle">
    <ComicLoadState :status="status" :error="error" icon="panels" :title="status === 'empty' ? t('comic.reader.noPages') : t('comic.seriesMissing')" :not-found-text="t('comic.chapterUnavailable')" @retry="retry">
      <template #actions><button class="comic-button" @click="leaveReader(router, 'comic', `/comic/series/${itemId}`)">{{ t('comic.backSeries') }}</button><button class="comic-button" @click="sheet='sources'">{{ t('sourceRouting.switchComic') }}</button></template>
    </ComicLoadState>
    <ReaderSheet v-if="sheet==='sources' && !data" :title="t('sourceRouting.switchComic')" @close="sheet=''"><ComicSourceSwitch :item-id="itemId" :chapter-id="0" @leaving="save"/></ReaderSheet>
  </div>
</template>
