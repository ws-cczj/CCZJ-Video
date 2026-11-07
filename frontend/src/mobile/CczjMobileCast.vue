<script setup lang="ts">
import './player/presentation.css'
import { recordTimeline } from '../diagnostics/session'
import { ref, computed, watch, useSlots, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { Button, Select } from '../components/ui'
import CczjMobilePlayerDrawer from './CczjMobilePlayerDrawer.vue'
import CczjMobileCastRemote from './CczjMobileCastRemote.vue'
import { DiscoverCastDevices, DiscoverCastDevicesOnInterfaces, PlayCastVideoWithOptions, ControlCastVideo, ReleaseCastSession, GetCastStatus, normalizeApiError } from '../api/app'
import { createPlayerPlatform } from '../player/platform'
import { createRequestScope } from '../api/requestScope'
import type { CastPlaybackState } from '../player/views/controlState'
import type { PlaybackCheckpoint } from '../player/playbackSession'
const platform=createPlayerPlatform()
const props = defineProps<{ open: boolean; url: string; mediaKey: string; continuation?:PlaybackCheckpoint&{key:string}; title: string; position: number; host?:HTMLElement; fullscreen?:boolean }>()
const emit = defineEmits<{ open: []; close: []; exitFullscreen: []; connected: []; stopped: [position?: number]; status: [value: CastPlaybackState] }>()
const { t } = useI18n()
const slots=useSlots(),routesOpen=ref(false)
const panelRoot=ref<HTMLElement>(),routesBody=ref<HTMLElement>()
let sourceReplacement=false,restorePaused=false,sourceResume:number|undefined,consumedContinuation:typeof props.continuation
async function openRoutes():Promise<void> {
 if(busy.value||!slots.lines)return
 trace('routes_opened');routesOpen.value=true;emit('open')
 await nextTick()
 if(!disposed&&routesOpen.value)routesBody.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({preventScroll:true})
}
function closeRoutes():void {
 const hadRoutes=routesOpen.value;routesOpen.value=false
 if(hadRoutes)void nextTick(()=>{if(!disposed&&props.open)panelRoot.value?.querySelector<HTMLButtonElement>('[data-cast-action="lines"]')?.focus({preventScroll:true})})
}
function back():boolean {if(!routesOpen.value)return false;closeRoutes();return true}
const devices = ref<{ id: string; name: string }[]>([])
const scanning = ref(false), busy = ref(false), error = ref('')
const announced=ref(false),clockVerified=ref(false),userConfirmed=ref(false)
const transportChoice=ref('relay'),fromStart=ref(true)
const transportOptions=computed(()=>[{value:'relay',label:t('mobile.castViaPhone')},{value:'direct',label:t('mobile.castDirect')}])
let castSession='',acceptRequest='',parentRequest=''
function trace(name:string,data:Record<string,number|boolean>={}):void {recordTimeline('ui.lifecycle',{name:`cast.${name}`,cast_session_id:castSession,backend_request_id:acceptRequest,parent_id:parentRequest,data})}
function resetConfirmation():void {clockVerified.value=false;userConfirmed.value=false}
function observePlayback(value:NonNullable<typeof remote.value>):void {
 clockVerified.value=value.seek_ready===true
 if(value.state==='STOPPED'||value.state==='NO_MEDIA_PRESENT'||value.media_state==='other'||value.relay_health==='failed'||value.relay_health==='revoked'||value.transport_status==='ERROR_OCCURRED')userConfirmed.value=false
}
const stopFailed=ref(false)
let initialPosition: number | undefined
let lastPosition: number | undefined
const pendingSeek=ref<number>()
let seekStarted=0
const selected = ref<{ id: string; name: string } | null>(null)
const paused = ref(false)
const remote=ref<{state:string;transport_status:string;position:number;duration:number;has_position:boolean;media_state?:string;cast_session_id:string;seek_ready:boolean;confirmation_state:string;relay_health:string}|null>(null)
const confirmationExpired=ref(false)
let attemptStarted=0
const statusError=ref(''),statusScope=createRequestScope(),controlScope=createRequestScope()
let statusTimer:ReturnType<typeof setTimeout>|undefined,statusRevision=0,sessionRevision=0
const playbackVerified=computed(()=>clockVerified.value||userConfirmed.value)
const needsConfirmation=computed(()=>!userConfirmed.value&&(!clockVerified.value||!remote.value?.has_position))
const remoteLabel=computed(()=>{
 if(remote.value?.relay_health==='failed'||remote.value?.relay_health==='revoked')return t('mobile.castRelayError')
 if(remote.value?.transport_status==='ERROR_OCCURRED')return t('mobile.castReceiverError')
 if(remote.value?.media_state==='other')return t('mobile.castDifferentMedia')
 if(needsConfirmation.value&&(remote.value?.state==='PLAYING'||remote.value?.state==='PAUSED_PLAYBACK'))return t(confirmationExpired.value?'mobile.castVerifyPlayback':'mobile.castChecking')
 if(userConfirmed.value&&remote.value?.state==='PLAYING')return t('mobile.castUserConfirmed')
 return t(remote.value?.state==='PLAYING'?'mobile.castPlaying':remote.value?.state==='PAUSED_PLAYBACK'?'mobile.castPaused':remote.value?.state==='STOPPED'||remote.value?.state==='NO_MEDIA_PRESENT'?'mobile.castNotPlaying':'mobile.castChecking')
})
const receiverActive=computed(()=>remote.value?.transport_status!=='ERROR_OCCURRED'&&remote.value?.relay_health!=='failed'&&remote.value?.relay_health!=='revoked'&&(remote.value?.state==='PLAYING'||remote.value?.state==='PAUSED_PLAYBACK'))
const canConfirmPlayback=computed(()=>receiverActive.value&&remote.value?.media_state!=='other'&&needsConfirmation.value)
const remoteView=computed(()=>({title:props.title,device:selected.value?.name||'',status:error.value||statusError.value||(pendingSeek.value!==undefined?t('mobile.castSeeking'):busy.value?t('mobile.castConnecting'):remoteLabel.value),attention:!!error.value||!!statusError.value||!receiverActive.value||needsConfirmation.value||pendingSeek.value!==undefined,paused:paused.value,busy:busy.value,active:receiverActive.value,position:pendingSeek.value??remote.value?.position??0,duration:remote.value?.duration||0,hasPosition:clockVerified.value&&!!remote.value?.has_position}))
function confirmPlayback(manual=false):void {
 if(!receiverActive.value||remote.value?.media_state==='other')return
 if(manual){initialPosition=undefined;userConfirmed.value=true;trace('confirmed_manual')}
 if(!clockVerified.value&&!userConfirmed.value)return
 if(!announced.value){trace(manual?'handoff_manual':'confirmed_clock');announced.value=true;emit('connected')}
}
function stopStatus():void {statusRevision++;clearTimeout(statusTimer);statusTimer=undefined;statusScope.cancel()}
async function refreshStatus():Promise<void>{
 stopStatus()
 if(disposed||document.hidden||!selected.value)return
 const token=statusRevision,id=selected.value.id
 try{
  const value=await statusScope.run(()=>GetCastStatus(id))
  if(token!==statusRevision||disposed||selected.value?.id!==id)return
  if(value.cast_session_id!==castSession){const session=castSession;void ReleaseCastSession(id,session).catch(()=>{});finishSession(lastPosition);error.value=t('mobile.castUnconfirmed');return}
  if(value.media_state!=='matched')value.has_position=false
  remote.value=value;statusError.value=''
  if(pendingSeek.value!==undefined){
    if(value.has_position&&Math.abs(value.position-pendingSeek.value)<=2){trace('seek_confirmed');pendingSeek.value=undefined}
    else if(performance.now()-seekStarted>=8000){trace('seek_timeout');pendingSeek.value=undefined;statusError.value=t('mobile.castSeekUnconfirmed')}
  }
  if(!confirmationExpired.value&&performance.now()-attemptStarted>=30000&&!clockVerified.value)trace('confirmation_timeout')
  confirmationExpired.value=performance.now()-attemptStarted>=30000
  paused.value=value.state==='PAUSED_PLAYBACK'||value.state==='STOPPED'||value.state==='NO_MEDIA_PRESENT'
  // A renderer can still report the previous video's PLAYING state after SetURI.
  if(value.media_state==='other'&&announced.value){announced.value=false;emit('stopped',lastPosition)}
  observePlayback(value)
  if(clockVerified.value&&value.has_position)confirmPlayback()
  if(announced.value){
    if(clockVerified.value&&value.has_position)lastPosition=value.position
    emit('status',{device:selected.value!.name,state:value.state,position:value.position,duration:value.duration,hasPosition:clockVerified.value&&value.has_position&&initialPosition===undefined&&pendingSeek.value===undefined&&!restorePaused})
    if(value.state==='STOPPED'||value.state==='NO_MEDIA_PRESENT'){announced.value=false;emit('stopped',lastPosition)}
    else if(initialPosition!==undefined&&value.state==='PLAYING'&&value.has_position&&clockVerified.value){
      const target=initialPosition;initialPosition=undefined
      if(target>0){trace('seek_resume',{target_seconds:target});void control('Seek',target)}
    }
    else if(restorePaused&&playbackVerified.value&&pendingSeek.value===undefined&&initialPosition===undefined){
      restorePaused=false
      if(value.state==='PLAYING'){trace('pause_restored');void control('Pause')}
    }
  }
 }catch(error){if(token===statusRevision&&!disposed&&normalizeApiError(error).code!=='CANCELLED'){remote.value=null;clockVerified.value=false;if(!statusError.value)trace('status_failed');statusError.value=t('mobile.castUnconfirmed')}}
 finally{if(token===statusRevision&&!disposed&&!document.hidden&&selected.value)statusTimer=setTimeout(()=>{void refreshStatus()},3000)}
}
let disposed = false
let discovery: ReturnType<typeof DiscoverCastDevices> | undefined
let scanGeneration=0
function stopDiscovery():void {
  scanGeneration++
  const pending=discovery;discovery=undefined
  try{void Promise.resolve(pending?.cancel?.()).catch(()=>{})}catch{/* Already settled. */}
  scanning.value=false;platform.cast.multicast(false)
}
async function scan(): Promise<void> {
  if (scanning.value || disposed || !props.open) return
  const token=++scanGeneration
  scanning.value = true; error.value = ''
  try {
    platform.cast.multicast(true)
    let network:ReturnType<typeof platform.cast.interfaces>
    try{network=platform.cast.interfaces()}catch{error.value=t('mobile.castNetworkUnavailable');return}
    if(network && !network.interfaces.length){error.value=t('mobile.castNetworkRequired');return}
    discovery = network ? DiscoverCastDevicesOnInterfaces(network.interfaces) : DiscoverCastDevices()
    const result = await discovery
    if (!disposed&&token===scanGeneration) devices.value = Array.isArray(result)?result:[]
  } catch (e) { if (!disposed&&token===scanGeneration) error.value = normalizeApiError(e).message }
  finally { if(token===scanGeneration){platform.cast.multicast(false); scanning.value = false; discovery = undefined} }
}
async function connect(device: { id: string; name: string }): Promise<void> {
  if (busy.value) return
  const token=sessionRevision
  parentRequest=acceptRequest
  if(!sourceReplacement)restorePaused=false
  if(sourceReplacement)trace('source_replaced',{resume_available:sourceResume!==undefined})
  stopDiscovery()
  stopStatus()
  busy.value = true; error.value = '';pendingSeek.value=undefined;remote.value=null;statusError.value='';stopFailed.value=false
  resetConfirmation();if(announced.value)emit('status',{device:device.name,state:'TRANSITIONING',position:0,duration:0,hasPosition:false});trace(selected.value?'resend':'send');confirmationExpired.value=false;attemptStarted=performance.now()
  const resumePosition=announced.value ? lastPosition ?? props.position : props.position
  initialPosition=sourceReplacement?sourceResume:!fromStart.value&&Number.isFinite(resumePosition)?Math.max(0,resumePosition):undefined
  lastPosition=undefined
  try { const receipt=await controlScope.run(()=>PlayCastVideoWithOptions(device.id, props.url, props.title,{transport:transportChoice.value})); if(disposed||token!==sessionRevision){void ReleaseCastSession(device.id,receipt.cast_session_id).catch(()=>{});return} if (!disposed&&token===sessionRevision) { castSession=receipt.cast_session_id;acceptRequest=receipt.request_id;trace('command_accepted');selected.value = device; paused.value = false;sourceReplacement=false;sourceResume=undefined;routesOpen.value=false;trace('remote_opened');emit('open');void refreshStatus() } }
  catch (e) { if (!disposed&&token===sessionRevision&&normalizeApiError(e).code!=='CANCELLED') {initialPosition=undefined;error.value = normalizeApiError(e).message;trace('send_failed')} }
  finally { if(token===sessionRevision)busy.value = false }
}
async function changeTransport(value:string|number):Promise<void>{
 transportChoice.value=value==='direct'?'direct':'relay'
 if(selected.value&&!busy.value)await connect(selected.value)
}
async function chooseDevice():Promise<void> {
 if(busy.value)return
 if(selected.value){
   await control('Stop')
   if(selected.value)return
 }
 resetConfirmation();cancelControls();stopStatus();selected.value=null;remote.value=null;statusError.value='';error.value='';announced.value=false
 void scan()
}
async function systemCast():Promise<void> {
  if(selected.value){await control('Stop');if(selected.value)return}
  stopDiscovery()
  resetConfirmation();cancelControls();stopStatus();selected.value=null;remote.value=null;announced.value=false
  try { if(platform.cast.openSystem()){emit('close');return} }
  catch { /* Keep the panel usable when an OEM settings activity is unavailable. */ }
  error.value=t('mobile.castSystemUnavailable')
}
async function control(action: string, target = 0): Promise<void> {
  if (!selected.value || busy.value) return
  if(action==='Seek'&&(!announced.value||!clockVerified.value||!remote.value?.has_position||!receiverActive.value))return
  const token=sessionRevision,id=selected.value.id
  stopStatus()
  busy.value = true; error.value = ''
  try {
    // Capture the receiver clock before Stop; many receivers reset it to zero.
    let position=lastPosition
    if(action==='Stop'){
      try{const latest=await controlScope.run(()=>GetCastStatus(id));if(announced.value&&playbackVerified.value&&latest.cast_session_id===castSession&&latest.media_state==='matched'&&latest.seek_ready&&latest.has_position)position=latest.position}catch{/* Last confirmed receiver position remains usable. */}
      if(disposed||token!==sessionRevision)return
    }
    const seconds=action==='Seek'?Math.max(0,Math.min(target,remote.value?.duration||7*24*3600)):0
    trace(`control_${action.toLowerCase()}`,{target_seconds:seconds});
    if(action==='Seek'){pendingSeek.value=seconds;seekStarted=performance.now()}
    await controlScope.run(()=>ControlCastVideo(id, action, seconds))
    if(disposed||token!==sessionRevision)return
    if (action === 'Stop') finishSession(position)
    else {if (action !== 'Seek')paused.value = action === 'Pause';void refreshStatus()}
  } catch (e) { if (!disposed&&token===sessionRevision&&normalizeApiError(e).code!=='CANCELLED') {if(action==='Seek'){trace('seek_rejected');pendingSeek.value=undefined}error.value = action==='Seek' ? t('mobile.castSeekFailed') : normalizeApiError(e).message;if(action==='Stop')stopFailed.value=true;void refreshStatus()} }
  finally { if(token===sessionRevision)busy.value = false }
}
function finishSession(position?:number):void {
 sourceReplacement=false;restorePaused=false;sourceResume=undefined;routesOpen.value=false
 trace('session_ended');castSession='';acceptRequest='';parentRequest='';resetConfirmation();stopStatus();selected.value=null;remote.value=null;announced.value=false;pendingSeek.value=undefined;initialPosition=undefined;lastPosition=undefined;stopFailed.value=false;statusError.value='';error.value='';emit('stopped',position)
}
async function disconnect():Promise<void> {
 const id=selected.value?.id,session=castSession
 cancelControls()
 if(id){try{await ReleaseCastSession(id,session)}catch{trace('disconnect_failed');error.value=t('mobile.castUnconfirmed');stopFailed.value=true;return}}
 finishSession(lastPosition)
}
function cancelControls():void{sessionRevision++;controlScope.cancel();busy.value=false}
// The drawer owns discovery, not the receiver session. Closing it must not
// cancel an accepted handoff or prevent its confirmation.
watch(() => props.open, value => { if (!value){stopDiscovery();closeRoutes()}else if (!selected.value&&!busy.value) void scan();else if(!busy.value&&!error.value) void refreshStatus() },{immediate:true})
watch(() => [props.url,props.mediaKey] as const, ([url,key],[oldURL,oldKey]) => {
 const device=selected.value
 const handoff=props.continuation!==consumedContinuation&&props.continuation?.key===key
 const sameEpisode=!!key&&(key===oldKey||handoff)
 if(handoff)consumedContinuation=props.continuation
 const position=pendingSeek.value??initialPosition??sourceResume??(clockVerified.value&&remote.value?.has_position?remote.value.position:undefined)
 const wasPaused=restorePaused||remote.value?.state==='PAUSED_PLAYBACK'
 cancelControls();stopStatus()
 if(device&&url&&oldURL&&sameEpisode){
   sourceReplacement=true;sourceResume=position;restorePaused=wasPaused
   void connect(device)
   return
 }
 if(device)void ReleaseCastSession(device.id,castSession).catch(()=>{})
 finishSession();paused.value=false
})
function visibility():void{trace(document.hidden?'background':'foreground');if(document.hidden){stopStatus();stopDiscovery()}else if(selected.value&&!busy.value&&!error.value)void refreshStatus()}
onMounted(()=>document.addEventListener('visibilitychange',visibility))
onBeforeUnmount(() => { const id=selected.value?.id;trace('unmount');disposed = true;cancelControls();if(id)void ReleaseCastSession(id,castSession).catch(()=>{});stopStatus(); stopDiscovery();document.removeEventListener('visibilitychange',visibility) })
defineExpose({back})
</script>
<template>
 <Teleport v-if="selected && host && !open" :to="host">
  <CczjMobileCastRemote :layout="fullscreen ? 'fullscreen' : 'compact'" :has-lines="!!$slots.lines" v-bind="remoteView" @lines="openRoutes" @exit-fullscreen="emit('exitFullscreen')" @play="control(paused ? 'Play' : 'Pause')" @seek="control('Seek',$event)" @stop="control('Stop')" @change="emit('open');chooseDevice()" @expand="emit('open')" />
 </Teleport>
 <CczjMobilePlayerDrawer class="cczj-mobile-playback-drawer" :class="{'cczj-mobile-cast-remote-mask':!!selected}" :solid="true" :expanded="!!selected" :close-label="selected ? t('mobile.castCollapse') : undefined" :initial-height="60" :host="host" :open="open" :fullscreen="!!fullscreen" :title="selected ? title : t('mobile.cast')" @close="routesOpen ? closeRoutes() : emit('close')">
  <template v-if="selected" #header>
    <Button variant="text" icon :aria-label="t(routesOpen?'common.back':'mobile.castCollapse')" @click="routesOpen ? closeRoutes() : emit('close')"><Icon name="chevron-left" :size="24" /></Button>
    <strong class="cczj-flex-1 cczj-min-w-0 cczj-truncate cczj-font-normal">{{ routesOpen ? t('mobile.castLines') : title }}</strong>
    <Button variant="text" icon :disabled="busy" :aria-label="t('mobile.castStop')" @click="control('Stop')"><Icon name="power" :size="24" /></Button>
  </template>
  <div v-if="open" ref="panelRoot" class="cczj-mobile-cast" :class="{'cczj-mobile-cast-connected':!!selected}">
    <div v-if="!routesOpen" class="cczj-mobile-cast-actions">
      <Select :model-value="transportChoice" :options="transportOptions" :ariaLabel="t('mobile.castTransport')" :disabled="busy" inline @update:model-value="changeTransport" />
      <Button variant="text" :disabled="busy" :aria-pressed="fromStart" @click="fromStart=!fromStart">{{t(fromStart?'mobile.castFromStart':'mobile.castResumeNext')}}</Button>
    </div>
    <div v-if="routesOpen" ref="routesBody" class="cczj-mobile-cast-routes cczj-flex-1 cczj-min-h-0 cczj-overflow-y-auto"><slot name="lines" :close="closeRoutes" /></div>
    <template v-else-if="selected">
      <CczjMobileCastRemote layout="page" :has-lines="!!$slots.lines" v-bind="remoteView" @lines="openRoutes" @play="control(paused ? 'Play' : 'Pause')" @seek="control('Seek',$event)" @stop="control('Stop')" @change="chooseDevice" />
      <p v-if="confirmationExpired && needsConfirmation" class="cczj-mobile-drawer-hint">{{t('mobile.castRetryHint')}}</p>
      <div v-if="error || statusError || !receiverActive || stopFailed || canConfirmPlayback" class="cczj-mobile-cast-actions"><Button v-if="canConfirmPlayback" :disabled="busy" @click="confirmPlayback(true)">{{t('mobile.castConfirmPlayback')}}</Button><Button variant="text" :disabled="busy" @click="connect(selected)">{{t('mobile.castRetry')}}</Button><Button v-if="platform.cast.systemSupported()" variant="text" :disabled="busy" @click="systemCast">{{t('mobile.castSystem')}}</Button><Button v-if="stopFailed" variant="text" :disabled="busy" @click="disconnect">{{t('mobile.castDisconnect')}}</Button><p v-if="stopFailed">{{t('mobile.castDisconnectHint')}}</p></div>
    </template>
    <template v-else>
      <p v-if="error" class="cczj-mobile-cast-error" role="alert">{{ error }}</p>
      <p>{{t('mobile.castHint')}}</p>
      <div class="cczj-mobile-cast-heading"><strong>{{t('mobile.castDevices')}}</strong><Button variant="text" size="sm" :disabled="scanning || busy" @click="scan">{{ scanning ? t('mobile.castScanning') : t('common.refresh') }}</Button></div>
      <div class="cczj-mobile-cast-list"><Button v-for="device in devices" :key="device.id" variant="primary" :disabled="busy" @click="connect(device)"><Icon name="monitor" :size="22" /><span>{{ device.name }}</span><Icon name="chevron-right" :size="18" /></Button></div>
      <div v-if="!scanning && !devices.length && !error" class="cczj-mobile-cast-empty"><Icon name="cast" :size="30"/><p>{{ t('mobile.castEmpty') }}</p></div>
      <p v-if="busy" role="status">{{ t('mobile.castConnecting') }}</p>
      <Button v-if="platform.cast.systemSupported()" variant="text" size="sm" :disabled="busy" @click="systemCast">{{t('mobile.castSystem')}}</Button>
      <p v-if="platform.cast.systemSupported()">{{t('mobile.castSystemHint')}}</p>
      <details class="cczj-mobile-cast-help"><summary>{{t('mobile.castHelp')}}</summary><p>{{t('mobile.castHelpText')}}</p></details>
    </template>
  </div>
 </CczjMobilePlayerDrawer>
</template>
