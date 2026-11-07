<script setup lang="ts">
import './player/presentation.css'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import CczjMobilePlayerControls from './CczjMobilePlayerControls.vue'
import CczjMobileFullscreenControls from './CczjMobileFullscreenControls.vue'
import CczjMobilePlayerDrawer from './CczjMobilePlayerDrawer.vue'
import CczjMobileCast from './CczjMobileCast.vue'
import type { PlaybackControlState, MobilePanel, CastPlaybackState } from '../player/views/controlState'
import type { PlaybackCheckpoint } from '../player/playbackSession'
const props=defineProps<{
  playback:PlaybackControlState; source:{readonly url:string;readonly title:string;readonly mediaKey:string;readonly continuation?:PlaybackCheckpoint&{key:string}}; host?:HTMLElement
  commands:{video:()=>HTMLVideoElement|null;play:()=>void;previous:()=>void;next:()=>void;fullscreen:()=>void|Promise<void>;pip:()=>void|Promise<void>;speed:(value:number)=>void;cast:(active:boolean,position?:number)=>void;castStatus:(value:CastPlaybackState)=>void;interaction:()=>void}
}>()
const emit=defineEmits<{toolsHost:[element:HTMLElement|null];toolsClosed:[];categoryChanged:[];quickOpened:[]}>()
const {t}=useI18n()
const panel=ref<MobilePanel|null>(null)
const cast=ref<{back:()=>boolean}|null>(null)
const menuOpen=computed(()=>panel.value==='more')
const linesOpen=computed(()=>panel.value==='lines')
const category=ref<'quality'|'playback'|'tools'>('quality')
const pinned=computed(()=>panel.value!==null)
const toolsHost=ref<HTMLElement|null>(null)
const speeds=[.5,.75,1,1.25,1.5,2,3]
function close(which?:MobilePanel|'quick'):void {
  if(which===undefined||which===panel.value||(which==='quick'&&panel.value!=='more')){
    const hadPanel=panel.value!==null
    panel.value=null
    if(hadPanel)props.commands.interaction()
  }
}
function toggle(which:MobilePanel):void {panel.value=panel.value===which?null:which;props.commands.interaction()}
function back():void {if(panel.value==='cast'&&cast.value?.back())return;close()}
function selectSpeed(value:number):void {props.commands.speed(value);close('speed');props.commands.interaction()}
watch(panel,(next,previous)=>{if(previous==='more'&&next!=='more')emit('toolsClosed');if(next&&next!=='more')emit('quickOpened')})
watch(category,()=>emit('categoryChanged'))
watch(()=>props.playback.fullscreen,()=>close())
watch(()=>props.source.url,()=>close())
watch(toolsHost,element=>emit('toolsHost',element),{flush:'post'})
onBeforeUnmount(()=>emit('toolsHost',null))
defineExpose({pinned,menuOpen,linesOpen,category,toggle,close,back})
</script>
<template>
 <div style="display:contents">
  <CczjMobilePlayerControls v-if="!playback.fullscreen" :playing="playback.playing" :pip="playback.pip" :can-pip="playback.canPip" :can-fullscreen="playback.canFullscreen" :current="playback.current" :duration="playback.duration" :has-episodes="!!$slots.episodes" :episodes-open="panel==='episodes'" @episodes-panel="toggle('episodes')" @play="commands.play();commands.interaction()" @fullscreen="commands.fullscreen()" @pip="commands.pip()"><slot name="danmaku" /></CczjMobilePlayerControls>
  <CczjMobileFullscreenControls v-else :playing="playback.playing" :pip="playback.pip" :can-pip="playback.canPip" :can-fullscreen="playback.canFullscreen" :current="playback.current" :duration="playback.duration" :has-episodes="!!$slots.episodes" :has-prev="playback.hasPrev" :has-next="playback.hasNext" :episodes-open="panel==='episodes'" :speed="playback.speed" :speed-open="panel==='speed'" @episodes-panel="toggle('episodes')" @speed-panel="toggle('speed')" @play="commands.play();commands.interaction()" @previous="commands.previous();commands.interaction()" @next="commands.next();commands.interaction()" @fullscreen="commands.fullscreen()" @pip="commands.pip()"><slot name="danmaku" /></CczjMobileFullscreenControls>
  <CczjMobilePlayerDrawer class="cczj-mobile-playback-drawer" lazy :placement="playback.fullscreen ? 'right' : 'bottom'" :host="host" :open="panel==='lines'" :fullscreen="playback.fullscreen" :title="t('playLine.switch')" @close="close('lines')"><slot name="lines" :close="()=>close('lines')" /></CczjMobilePlayerDrawer>
  <CczjMobileCast ref="cast" :host="host" :fullscreen="playback.fullscreen" :open="panel==='cast'" :url="source.url" :media-key="source.mediaKey" :continuation="source.continuation" :title="source.title" :position="playback.current" @open="panel='cast';commands.interaction()" @close="close('cast')" @exit-fullscreen="commands.fullscreen()" @connected="commands.cast(true)" @stopped="commands.cast(false,$event)" @status="commands.castStatus($event)">
   <template v-if="$slots.lines" #lines="slotProps"><slot name="lines" :close="slotProps.close" /></template>
  </CczjMobileCast>
  <CczjMobilePlayerDrawer class="cczj-mobile-playback-drawer" :placement="playback.fullscreen ? 'right' : 'bottom'" :host="host" :open="panel==='speed'" :fullscreen="playback.fullscreen" :title="t('player.playbackSpeed')" @close="close('speed')">
   <div class="cczj-mobile-speed-options" role="group" :aria-label="t('player.playbackSpeed')"><button v-for="speed in speeds" :key="speed" :aria-pressed="playback.speed===speed" @click="selectSpeed(speed)">{{speed}}x</button></div>
  </CczjMobilePlayerDrawer>
  <CczjMobilePlayerDrawer class="cczj-mobile-playback-drawer" lazy :host="host" :open="panel==='episodes'" :fullscreen="playback.fullscreen" :title="t('detail.episodes')" @close="close('episodes')"><slot name="episodes" :close="()=>close('episodes')" /></CczjMobilePlayerDrawer>
  <CczjMobilePlayerDrawer kind="more" :host="host" :open="menuOpen" :fullscreen="playback.fullscreen" :title="t('mobile.playerMore')" @close="close('more')">
   <template #navigation><nav class="cczj-mobile-player-categories" :aria-label="t('mobile.playerMore')"><button v-for="item in (['quality','playback','tools'] as const)" :key="item" :aria-pressed="category===item" @click="category=item">{{t(item==='quality'?'mobile.quality':item==='playback'?'settings.playback':'mobile.playerTools')}}</button></nav></template>
   <div ref="toolsHost" />
  </CczjMobilePlayerDrawer>
 </div>
</template>
