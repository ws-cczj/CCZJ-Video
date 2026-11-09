<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import '../controlPalette.css'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { Button, Select, NumberField } from '../../components/ui'
import { danmakuTypefaceStatus, ensureDanmakuTypeface } from './typeface'
import DanmakuPreview from './DanmakuPreview.vue'
import DanmakuRange from './DanmakuRange.vue'
import { defaultDanmakuPreferences, type DanmakuPreferences } from './preferences'
import { useDanmaku } from './useDanmaku'
import DanmakuCanvas from './DanmakuCanvas.vue'
import { builtinDanmakuSources } from './sources'
const props=defineProps<{mediaKey:string;mediaTitle:string;mediaYear?:string;mediaKind?:string;episodeLabel:string;current:number;duration:number;playing:boolean;fullscreen:boolean;mobile:boolean;host?:HTMLElement;video:()=>HTMLVideoElement|null}>()
const emit=defineEmits<{prepared:[key:string,status:string]}>()
const {t,te}=useI18n()
const {preferences,preferencesReady,sourcesReady,boundSourceEnabled,binding,window:comments,error,busy,status,cooldownUntil,attempts,traceId,retry}=useDanmaku({key:()=>props.mediaKey,current:()=>props.current,title:()=>props.mediaTitle,year:()=>props.mediaYear||'',kind:()=>props.mediaKind||'',episode:()=>props.episodeLabel,duration:()=>props.duration})
const advanced=ref(false)
const opened=ref(false),panel=ref<HTMLElement>(),panelLeft=ref('16px'),viewportWidth=ref(640)
watch(()=>[opened.value,preferences.value.enabled,preferences.value.fontFamily],([open,enabled,family])=>{if((open||enabled)&&family==='sans')void ensureDanmakuTypeface()},{immediate:true})
const items=computed(()=>comments.value?.items??[]),enabled=computed(()=>preferences.value.enabled)
const sourceName=computed(()=>builtinDanmakuSources.find(source=>source.id===binding.value?.episode.provider)?.name||'')
function reason(code:string):string {
 if(code==='EPISODE_SPECIAL_REJECTED')return t('danmaku.specialVersionReason')
 return te(`danmaku.reasons.${code}`)?t(`danmaku.reasons.${code}`):t('danmaku.reasonUnknown')
}
type RangeKey='area'|'opacity'|'fontSize'|'speed'|'density'|'rows'
const ranges=computed(()=>[
 {key:'area' as RangeKey,label:t('danmaku.area'),min:25,max:100,step:5,factor:100,value:Math.round(preferences.value.area*100),unit:'%'},
 {key:'opacity' as RangeKey,label:t('danmaku.opacity'),min:10,max:100,step:1,factor:100,value:Math.round(preferences.value.opacity*100),unit:'%'},
 {key:'fontSize' as RangeKey,label:t('danmaku.fontSize'),min:50,max:200,step:1,factor:100/24,value:Math.round(preferences.value.fontSize/24*100),unit:'%'},
 {key:'speed' as RangeKey,label:t('danmaku.speed'),min:50,max:180,step:5,factor:1,value:Math.round(preferences.value.speed),unit:'%'},
 {key:'density' as RangeKey,label:t('danmaku.density'),min:25,max:100,step:5,factor:100,value:Math.round(preferences.value.density*100),unit:'%'},
 {key:'rows' as RangeKey,label:t('danmaku.rowLimit'),min:2,max:24,step:1,factor:1,value:preferences.value.rows,unit:''},
])
const basicRanges=computed(()=>ranges.value.slice(0,4))
function rangeDisplay(range:typeof ranges.value[number]):string {
 if(range.key!=='speed')return range.value+range.unit
 const speed=range.value
 return t(speed<70?'danmaku.speedSlowest':speed<90?'danmaku.speedSlow':speed<=110?'danmaku.speedNormal':speed<150?'danmaku.speedFast':'danmaku.speedFastest')
}
async function changePage(value:boolean):Promise<void>{
 advanced.value=value;hoverOpened=false
 await nextTick()
 if(opened.value){panel.value?.querySelector<HTMLElement>('.cczj-danmaku-body')?.scrollTo(0,0);panel.value?.querySelector<HTMLButtonElement>('header button')?.focus({preventScroll:true})}
}
function adjustRange(key:RangeKey,value:number,factor:number):void{if(preferencesReady.value)preferences.value[key]=value/factor}
const fontOptions=computed(()=>[{value:'sans',label:t('danmaku.fontSans')},{value:'serif',label:t('danmaku.fontSerif')},{value:'mono',label:t('danmaku.fontMono')}])
const weightOptions=computed(()=>[400,500,600,700].map(value=>({value,label:t(`danmaku.weight${value}`)})))
const styleOptions=computed(()=>[{value:'stroke',label:t('danmaku.styleStroke')},{value:'shadow',label:t('danmaku.styleShadow')},{value:'none',label:t('danmaku.styleNone')}])
function setFont(value:string|number):void{preferences.value.fontFamily=value==='serif'?'serif':value==='mono'?'mono':'sans'}
function setWeight(value:string|number):void{preferences.value.fontWeight=[400,500,600,700].includes(Number(value))?Number(value) as DanmakuPreferences['fontWeight']:500}
function setStyle(value:string|number):void{preferences.value.textStyle=value==='shadow'?'shadow':value==='none'?'none':'stroke'}
function resetAppearance():void{
 const {enabled,freshDays}=preferences.value
 preferences.value={...defaultDanmakuPreferences,enabled,freshDays}
}
let previousFocus:HTMLElement|null=null
let anchor:HTMLElement|undefined,resizeObserver:ResizeObserver|undefined
let closeTimer:ReturnType<typeof setTimeout>|undefined,hoverOpened=false
function keepOpen(){clearTimeout(closeTimer)}
function scheduleClose(){keepOpen();if(hoverOpened)closeTimer=setTimeout(()=>close(false),220)}
function leavePanel(event:PointerEvent){if(event.pointerType==='mouse'&&!event.buttons)scheduleClose()}
function placePanel(){
 const bounds=props.host?.getBoundingClientRect()||{left:0,width:document.documentElement.clientWidth}
 viewportWidth.value=bounds.width
 const center=anchor?.isConnected?anchor.getBoundingClientRect().left+anchor.getBoundingClientRect().width/2-bounds.left:bounds.width/2
 const width=Math.min(340,Math.max(0,bounds.width-32))
 panelLeft.value=Math.max(16,Math.min(bounds.width-width-16,center-width/2))+'px'
}
async function open(element?:HTMLElement,hover=false){keepOpen();if(opened.value&&hover)return;hoverOpened=hover;anchor=element;previousFocus=hover?null:element||document.activeElement as HTMLElement|null;advanced.value=false;placePanel();opened.value=true;resizeObserver?.disconnect();resizeObserver=new ResizeObserver(placePanel);resizeObserver.observe(props.host||document.documentElement);await nextTick();if(!hover&&opened.value)panel.value?.querySelector<HTMLButtonElement>('header button')?.focus({preventScroll:true})}
function close(restoreFocus=true){keepOpen();if(!opened.value)return;opened.value=false;resizeObserver?.disconnect();if(restoreFocus&&previousFocus?.isConnected)previousFocus.focus({preventScroll:true})}
function toggle(){close(false);if(preferencesReady.value)preferences.value.enabled=!preferences.value.enabled}
function back():void{if(advanced.value)void changePage(false);else close()}
watch(enabled,value=>{if(!value)close()})
function onKey(event:KeyboardEvent){
 if(event.key==='Escape'){event.preventDefault();if(advanced.value)void changePage(false);else close();return}
 if(event.key!=='Tab')return
 const targets=Array.from(panel.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),summary')??[]).filter(element=>element.getBoundingClientRect().width)
 const first=targets[0],last=targets[targets.length-1]
 if(event.shiftKey&&document.activeElement===first){event.preventDefault();last?.focus()}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first?.focus()}
}
watch(()=>props.mediaKey,()=>close());watch(()=>props.fullscreen,()=>close())
// Missing/disabled/failed sources also settle preparation; none blocks playback.
watch(status,value=>{if(!['loading','matching','waiting','unbound'].includes(value))emit('prepared',props.mediaKey,value)},{immediate:true})
onBeforeUnmount(()=>{keepOpen();resizeObserver?.disconnect();if(opened.value&&previousFocus?.isConnected)previousFocus.focus({preventScroll:true})})
defineExpose({open,close,back,scheduleClose,toggle,ready:preferencesReady,engaged:opened,enabled,status})
</script>
<template>
 <DanmakuCanvas v-if="preferences.enabled&&sourcesReady&&boundSourceEnabled&&binding" :items="items" :offset="binding.offsetMs" :preferences="preferences" :current="current" :playing="playing" :video="video" />
 <Teleport :to="host||'body'">
  <Transition name="cczj-danmaku-panel">
   <div v-if="opened" class="cczj-danmaku-mask" :style="{'--dm-panel-left':panelLeft}" :class="{'cczj-danmaku-portrait':mobile&&!fullscreen,'cczj-danmaku-touch':mobile}" @click.self="close()" @pointerdown.stop @wheel.stop @click.stop @dblclick.stop @keydown.stop="onKey">
    <section ref="panel" class="cczj-danmaku-panel cczj-player-dark-controls" role="dialog" :aria-modal="mobile&&!fullscreen?true:undefined" :aria-label="t('danmaku.settings')" @pointerenter="keepOpen" @pointerleave="leavePanel" @focusin="keepOpen">
     <header>
      <Button v-if="advanced" variant="text" icon :aria-label="t('common.back')" @click="changePage(false)"><Icon name="chevron-left" :size="18" /></Button>
      <strong>{{t(advanced?'danmaku.advancedSettings':'danmaku.settings')}}</strong>
      <Button variant="text" icon :aria-label="t('common.close')" @click="close()"><Icon name="close" :size="18" /></Button>
     </header>
     <div class="cczj-danmaku-body">
      <fieldset :disabled="!preferencesReady" class="cczj-danmaku-appearance">
       <template v-if="!advanced">
        <legend>{{t('danmaku.blockTypes')}}</legend>
        <div class="cczj-danmaku-types">
         <Button v-for="item in ([{key:'scrolling',icon:'danmaku',label:'scrolling'},{key:'fixed',icon:'panels',label:'fixedType'},{key:'colored',icon:'palette',label:'colored'}] as const)" :key="item.key" variant="text" :aria-pressed="!preferences[item.key]" :aria-label="t('danmaku.blockType',{type:t('danmaku.'+item.label)})" @click="preferences[item.key]=!preferences[item.key]">
          <span class="cczj-danmaku-type-icon"><Icon :name="item.icon" :size="22"/><Icon v-if="!preferences[item.key]" class="cczj-danmaku-block-mark" name="close" :size="14"/></span>{{t('danmaku.'+item.label)}}
         </Button>
        </div>
        <DanmakuRange v-for="range in basicRanges" :key="range.key" v-bind="{label:range.label,value:range.value,min:range.min,max:range.max,step:range.step}" :display="rangeDisplay(range)" @change="adjustRange(range.key,$event,range.factor)" />
       </template>
       <template v-else>
        <DanmakuPreview :viewport-width="viewportWidth" :preferences="preferences" :text="t('danmaku.previewText')" />
        <p v-if="preferences.fontFamily==='sans'&&danmakuTypefaceStatus==='failed'" class="cczj-danmaku-error" role="status">{{t('danmaku.fontFallback')}} <Button variant="text" @click="ensureDanmakuTypeface">{{t('common.retry')}}</Button></p>
        <div v-for="range in ranges" :key="range.key" class="cczj-danmaku-number-row">
         <span>{{range.label}}</span><div class="cczj-flex cczj-items-center cczj-gap-2"><NumberField :model-value="range.value" :ariaLabel="range.label" :min="range.min" :max="range.max" :step="1" @update:model-value="adjustRange(range.key,$event,range.factor)" /><span>{{range.unit}}</span></div>
        </div>
        <div class="cczj-danmaku-select"><span>{{t('danmaku.fontFamily')}}</span><Select :model-value="preferences.fontFamily" :options="fontOptions" :ariaLabel="t('danmaku.fontFamily')" inline inline-drop="up" @update:model-value="setFont" /></div>
        <div class="cczj-danmaku-select"><span>{{t('danmaku.fontWeight')}}</span><Select :model-value="preferences.fontWeight" :options="weightOptions" :ariaLabel="t('danmaku.fontWeight')" inline inline-drop="up" @update:model-value="setWeight" /></div>
        <div class="cczj-danmaku-select"><span>{{t('danmaku.textStyle')}}</span><Select :model-value="preferences.textStyle" :options="styleOptions" :ariaLabel="t('danmaku.textStyle')" inline inline-drop="up" @update:model-value="setStyle" /></div>
        <DanmakuRange v-if="preferences.textStyle==='stroke'" :label="t('danmaku.strokeWidth')" :value="preferences.strokeWidth" :min="0" :max="4" :step="0.25" :display="Number(preferences.strokeWidth.toFixed(2))+' px'" @change="preferences.strokeWidth=$event" />
        <Button class="cczj-danmaku-reset" variant="secondary" @click="resetAppearance">{{t('danmaku.resetAppearance')}}</Button>
       </template>
      </fieldset>
      <Button v-if="!advanced" class="cczj-danmaku-advanced-link" variant="text" :disabled="!preferencesReady" @click="changePage(true)"><span>{{t('danmaku.advancedSettings')}}</span><Icon name="chevron-right" :size="16"/></Button>
      <p v-if="status!=='ready'" class="cczj-danmaku-status" role="status">{{t(`danmaku.states.${status}`)}}</p>
      <template v-if="advanced">
      <div class="cczj-danmaku-availability" role="status"><strong v-if="binding">{{sourceName}} · {{episodeLabel}}</strong><span v-if="comments">{{t('danmaku.fragmentCount',{count:comments.items.length})}} · {{t(comments.cached?'danmaku.fromCache':'danmaku.fromSource')}}</span><p>{{t('danmaku.autoHint')}}</p></div>
      <details v-if="attempts.length" class="cczj-danmaku-cache cczj-danmaku-match-details"><summary>{{t('danmaku.matchDetails')}}</summary><p v-for="attempt in attempts" :key="attempt.provider"><strong>{{builtinDanmakuSources.find(source=>source.id===attempt.provider)?.name}} · {{t(`danmaku.attempts.${attempt.status}`)}}</strong><span v-if="attempt.code" class="cczj-danmaku-match-reason">{{reason(attempt.code)}} <code>{{attempt.code}}</code></span><small v-if="attempt.stage">{{t('danmaku.matchStage')}}: {{te(`danmaku.stages.${attempt.stage}`)?t(`danmaku.stages.${attempt.stage}`):attempt.stage}} · {{attempt.elapsedMs||0}} ms</small><small v-if="attempt.detail">{{attempt.detail}}</small><small v-if="attempt.retryAt">{{t('danmaku.matchRetryAt',{time:new Date(attempt.retryAt*1000).toLocaleString()})}}</small></p><p v-if="traceId">{{t('danmaku.matchLogId')}}<code>{{traceId}}</code></p><p>{{t('danmaku.matchLogHint')}}</p></details>
      </template>
      <p v-if="error" class="cczj-danmaku-error" role="alert">{{error}} <Button variant="text" :disabled="busy" @click="retry">{{t('common.retry')}}</Button></p>
      <p v-if="cooldownUntil" class="cczj-danmaku-hint">{{t('danmaku.retryAfter',{time:new Date(cooldownUntil).toLocaleTimeString()})}}</p>
      <p v-if="comments?.stale" class="cczj-danmaku-hint">{{t('danmaku.stale')}}</p>
      <p v-if="comments?.warnings.includes('cache_write_failed')" class="cczj-danmaku-error">{{t('danmaku.cacheFailed')}}</p>
     </div>
    </section>
   </div>
  </Transition>
 </Teleport>
</template>
<style scoped src="../../styles/components/player-danmaku.css"></style>
