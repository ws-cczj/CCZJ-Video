<script setup lang="ts">
import './player/presentation.css'
import { recordTimeline } from '../diagnostics/session'
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { Button } from '../components/ui'
import CczjMobilePlayerDrawer from './CczjMobilePlayerDrawer.vue'
import CczjMobileCastRemote from './CczjMobileCastRemote.vue'
import { DiscoverCastDevices, DiscoverCastDevicesOnInterfaces, PlayCastVideo, ControlCastVideo, ReleaseCastSession, GetCastStatus, normalizeApiError } from '../api/app'
import { createPlayerPlatform } from '../player/platform'
import { createRequestScope } from '../api/requestScope'
import { formatPlaybackTime } from '../player/videoInfoFormat'
import type { CastPlaybackState } from '../player/views/controlState'
const platform=createPlayerPlatform()
const props = defineProps<{ open: boolean; url: string; title: string; position: number; host?:HTMLElement; fullscreen?:boolean }>()
const emit = defineEmits<{ open: []; close: []; exitFullscreen: []; connected: []; stopped: [position?: number]; status: [value: CastPlaybackState] }>()
const { t } = useI18n()
const devices = ref<{ id: string; name: string }[]>([])
const scanning = ref(false), busy = ref(false), error = ref('')
const announced=ref(false),clockVerified=ref(false),userConfirmed=ref(false)
let castSession='',acceptRequest=''
function trace(name:string,data:Record<string,number|boolean>={}):void {recordTimeline('ui.lifecycle',{name:`cast.${name}`,cast_session_id:castSession,backend_request_id:acceptRequest,data})}
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
  if(value.cast_session_id!==castSession)return
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
    emit('status',{device:selected.value!.name,state:value.state,position:value.position,duration:value.duration,hasPosition:clockVerified.value&&value.has_position})
    if(value.state==='STOPPED'||value.state==='NO_MEDIA_PRESENT'){announced.value=false;emit('stopped',lastPosition)}
    else if(initialPosition!==undefined&&value.state==='PLAYING'&&value.has_position&&clockVerified.value){
      const target=initialPosition;initialPosition=undefined
      if(target>0){trace('seek_resume',{target_seconds:target});void control('Seek',target)}
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
  stopDiscovery()
  stopStatus()
  busy.value = true; error.value = '';pendingSeek.value=undefined;remote.value=null;statusError.value='';stopFailed.value=false
  resetConfirmation();if(announced.value)emit('status',{device:device.name,state:'TRANSITIONING',position:0,duration:0,hasPosition:false});trace(selected.value?'resend':'send');confirmationExpired.value=false;attemptStarted=performance.now()
  const resumePosition=announced.value ? lastPosition ?? props.position : props.position
  initialPosition=Number.isFinite(resumePosition)?Math.max(0,resumePosition):undefined
  lastPosition=undefined
  try { const receipt=await controlScope.run(()=>PlayCastVideo(device.id, props.url, props.title)); if(disposed||token!==sessionRevision){void ReleaseCastSession(device.id,receipt.cast_session_id).catch(()=>{});return} if (!disposed&&token===sessionRevision) { castSession=receipt.cast_session_id;acceptRequest=receipt.request_id;trace('command_accepted');selected.value = device; paused.value = false;void refreshStatus() } }
  catch (e) { if (!disposed&&token===sessionRevision&&normalizeApiError(e).code!=='CANCELLED') error.value = normalizeApiError(e).message }
  finally { if(token===sessionRevision)busy.value = false }
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
 trace('session_ended');castSession='';acceptRequest='';resetConfirmation();stopStatus();selected.value=null;remote.value=null;announced.value=false;pendingSeek.value=undefined;initialPosition=undefined;lastPosition=undefined;stopFailed.value=false;statusError.value='';error.value='';emit('stopped',position)
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
watch(() => props.open, value => { if (!value) stopDiscovery();else if (!selected.value&&!busy.value) void scan();else if(!busy.value) void refreshStatus() },{immediate:true})
watch(() => props.url, () => {const id=selected.value?.id;cancelControls();if(id)void ReleaseCastSession(id,castSession).catch(()=>{});finishSession();paused.value=false})
function visibility():void{trace(document.hidden?'background':'foreground');if(document.hidden){stopStatus();stopDiscovery()}else if(selected.value&&!busy.value)void refreshStatus()}
onMounted(()=>document.addEventListener('visibilitychange',visibility))
onBeforeUnmount(() => { const id=selected.value?.id;trace('unmount');disposed = true;cancelControls();if(id)void ReleaseCastSession(id,castSession).catch(()=>{});stopStatus(); stopDiscovery();document.removeEventListener('visibilitychange',visibility) })
</script>
<template>
 <Teleport v-if="announced && host && !open" :to="host">
  <CczjMobileCastRemote :layout="fullscreen ? 'fullscreen' : 'compact'" v-bind="remoteView" @exit-fullscreen="emit('exitFullscreen')" @play="control(paused ? 'Play' : 'Pause')" @seek="control('Seek',$event)" @stop="control('Stop')" @change="emit('open');chooseDevice()" @expand="emit('open')" />
 </Teleport>
 <CczjMobilePlayerDrawer class="cczj-mobile-playback-drawer" :class="{'cczj-mobile-cast-remote-mask':announced}" :solid="true" :expanded="announced" :close-label="announced ? t('mobile.castCollapse') : undefined" :initial-height="60" :host="host" :open="open" :fullscreen="!!fullscreen" :title="announced ? title : t('mobile.cast')" @close="emit('close')">
  <template v-if="announced" #header>
    <Button variant="text" icon :aria-label="t('mobile.castCollapse')" @click="emit('close')"><Icon name="chevron-left" :size="24" /></Button>
    <strong class="cczj-flex-1 cczj-min-w-0 cczj-truncate cczj-font-normal">{{ title }}</strong>
    <Button variant="text" icon :disabled="busy" :aria-label="t('mobile.castStop')" @click="control('Stop')"><Icon name="power" :size="24" /></Button>
  </template>
  <div v-if="open" class="cczj-mobile-cast" :class="{'cczj-mobile-cast-connected':announced}">
    <template v-if="selected && announced">
      <CczjMobileCastRemote layout="page" v-bind="remoteView" @play="control(paused ? 'Play' : 'Pause')" @seek="control('Seek',$event)" @stop="control('Stop')" @change="chooseDevice" />
      <div v-if="error || statusError || !receiverActive || stopFailed || canConfirmPlayback" class="cczj-mobile-cast-actions"><Button v-if="canConfirmPlayback" :disabled="busy" @click="confirmPlayback(true)">{{t('mobile.castConfirmPlayback')}}</Button><Button variant="text" :disabled="busy" @click="connect(selected)">{{t('mobile.castRetry')}}</Button><Button v-if="stopFailed" variant="text" :disabled="busy" @click="disconnect">{{t('mobile.castDisconnect')}}</Button><p v-if="stopFailed">{{t('mobile.castDisconnectHint')}}</p></div>
    </template>
    <template v-else-if="selected">
      <p v-if="error" class="cczj-mobile-cast-error" role="alert">{{ error }}</p>
      <div class="cczj-mobile-cast-session"><Icon name="cast" :size="28"/><strong>{{ selected.name }}</strong><p role="status">{{ statusError || remoteLabel }}</p><p v-if="remote?.has_position">{{formatPlaybackTime(remote.position)}} / {{formatPlaybackTime(remote.duration)}}</p></div>
      <p v-if="confirmationExpired">{{t('mobile.castRetryHint')}}</p>
      <div class="cczj-mobile-cast-actions"><Button :disabled="busy" variant="text" @click="control('Stop')">{{ t('mobile.castStop') }}</Button><Button v-if="stopFailed" variant="text" :disabled="busy" @click="disconnect">{{t('mobile.castDisconnect')}}</Button><p v-if="stopFailed">{{t('mobile.castDisconnectHint')}}</p></div>
      <div class="cczj-mobile-cast-actions"><Button v-if="canConfirmPlayback" :disabled="busy" @click="confirmPlayback(true)">{{t('mobile.castConfirmPlayback')}}</Button><Button :disabled="busy" @click="connect(selected)">{{t('mobile.castRetry')}}</Button><Button variant="text" :disabled="busy" @click="chooseDevice">{{t('mobile.castChangeDevice')}}</Button><Button v-if="platform.cast.systemSupported()" variant="text" :disabled="busy" @click="systemCast">{{t('mobile.castSystem')}}</Button></div>
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
