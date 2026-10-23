<script setup lang="ts">
import './player/presentation.css'
import { ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { DiscoverCastDevices, PlayCastVideo, ControlCastVideo } from '../api/app'
import { createPlayerPlatform } from '../player/platform'
const platform=createPlayerPlatform()
const props = defineProps<{ open: boolean; url: string; title: string; position: number; host?:HTMLElement }>()
const emit = defineEmits<{ close: []; connected: [] }>()
const { t } = useI18n()
const devices = ref<{ id: string; name: string }[]>([])
const scanning = ref(false), busy = ref(false), error = ref('')
const selected = ref<{ id: string; name: string } | null>(null)
const paused = ref(false)
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
  scanning.value = true; error.value = ''; platform.cast.multicast(true)
  try {
    discovery = DiscoverCastDevices()
    const result = await discovery
    if (!disposed&&token===scanGeneration) devices.value = Array.isArray(result)?result:[]
  } catch (e) { if (!disposed&&token===scanGeneration) error.value = String(e) }
  finally { if(token===scanGeneration){platform.cast.multicast(false); scanning.value = false; discovery = undefined} }
}
async function connect(device: { id: string; name: string }): Promise<void> {
  if (busy.value) return
  busy.value = true; error.value = ''
  try { await PlayCastVideo(device.id, props.url, props.title); if (!disposed) { selected.value = device; paused.value = false; emit('connected') } }
  catch (e) { if (!disposed) error.value = String(e) }
  finally { busy.value = false }
}
async function control(action: string): Promise<void> {
  if (!selected.value || busy.value) return
  busy.value = true; error.value = ''
  try {
    await ControlCastVideo(selected.value.id, action, action === 'Seek' ? props.position : 0)
    if (action === 'Stop') selected.value = null
    else if (action !== 'Seek') paused.value = action === 'Pause'
  } catch (e) { if (!disposed) error.value = String(e) }
  finally { busy.value = false }
}
watch(() => props.open, value => { if (!value) stopDiscovery();else if (!selected.value) void scan() })
watch(() => props.url, () => { selected.value = null; paused.value = false })
onBeforeUnmount(() => { disposed = true; stopDiscovery() })
</script>
<template>
 <Teleport :to="host || 'body'">
  <div v-if="open" class="cczj-mobile-cast" role="dialog" aria-modal="true" :aria-label="t('mobile.cast')" @click.stop @pointerdown.stop @wheel.stop>
    <div class="cczj-mobile-cast-heading"><strong><Icon name="cast" :size="20" />{{ t('mobile.cast') }}</strong><button :aria-label="t('common.close')" @click="emit('close')"><Icon name="close" :size="20" /></button></div>
    <p>{{ t('mobile.castHint') }}</p>
    <p v-if="error" class="cczj-mobile-cast-error" role="alert">{{ error }}</p>
    <template v-if="selected">
      <strong>{{ selected.name }}</strong><p role="status">{{ t('mobile.castSent') }}</p>
      <div class="cczj-mobile-cast-actions"><button :disabled="busy" @click="control(paused ? 'Play' : 'Pause')">{{ t(paused ? 'player.play' : 'player.pause') }}</button><button :disabled="busy" @click="control('Stop')">{{ t('mobile.castStop') }}</button><button :disabled="busy || position <= 0" @click="control('Seek')">{{ t('mobile.castSeek') }}</button></div>
    </template>
    <template v-else>
      <button class="cczj-mobile-cast-refresh" :disabled="scanning || busy" @click="scan"><Icon name="refresh" :size="16" />{{ scanning ? t('mobile.castScanning') : t('common.refresh') }}</button>
      <div class="cczj-mobile-cast-list"><button v-for="device in devices" :key="device.id" :disabled="busy" @click="connect(device)"><Icon name="monitor" :size="22" /><span>{{ device.name }}</span><Icon name="chevron-right" :size="18" /></button></div>
      <p v-if="!scanning && !devices.length">{{ t('mobile.castEmpty') }}</p>
      <p v-if="busy" role="status">{{ t('mobile.castConnecting') }}</p>
    </template>
  </div>
 </Teleport>
</template>
