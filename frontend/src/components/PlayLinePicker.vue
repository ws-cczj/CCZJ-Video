<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ProbePlayLines, type PlayLineQuality } from '../api/playback'
import { createRequestScope } from '../api/requestScope'
import { useGeneration } from '../composables/useGeneration'
import { useSourceStore } from '../stores/source'
import { Button } from './ui'
import PlaybackRouteList from '../player/views/PlaybackRouteList.vue'
import type { PlayLine } from '../types'
import type { SourceOption } from '../player/page/useSourceCatalog'

const props=defineProps<{lines:PlayLine[];modelValue:number;sourceKey:string;vodId:string;globalId?:number;epNum?:number}>()
const emit=defineEmits<{'update:modelValue':[index:number]}>()
const {t}=useI18n(),sources=useSourceStore(),scope=createRequestScope(),generation=useGeneration()
const results=ref<PlayLineQuality[]>([]),state=ref<'pending'|'ready'|'failed'>('pending')
const options=computed<SourceOption[]>(()=>[{source_key:props.sourceKey,name:sources.sources.find(s=>s.source_key===props.sourceKey)?.name||t('playLine.title'),probe:'local',vod_id:props.vodId,lines:results.value,qualityState:state.value}])
async function probe():Promise<void>{
  scope.cancel();const seq=generation.begin();results.value=[];state.value='pending'
  if(!props.sourceKey||!props.vodId||!props.lines.length)return
  try{
    const value=await scope.run(()=>ProbePlayLines({source_key:props.sourceKey,vod_id:props.vodId,global_id:props.globalId||0,ep_num:props.epNum||0}))
    if(generation.isCurrent(seq)){results.value=value;state.value='ready'}
  }catch{if(generation.isCurrent(seq))state.value='failed'}
}
watch(()=>[props.sourceKey,props.vodId,props.globalId,props.epNum,props.lines],()=>{void probe()},{immediate:true})
onBeforeUnmount(()=>{generation.invalidate();scope.cancel()})
function choose(_source:string,line?:number):void{const index=props.lines.findIndex(item=>item.index===line);if(index>=0&&index!==props.modelValue)emit('update:modelValue',index)}
</script>
<template>
  <div v-if="lines.length" class="line-picker cczj-flex cczj-flex-col cczj-gap-4">
    <PlaybackRouteList :sources="options" :active-key="sourceKey" :active-line="lines[modelValue]?.index??-1" :lines="lines" :selectable="()=>false" @choose="choose" />
    <Button v-if="state==='failed'" size="sm" variant="text" @click="probe">{{t('playLine.retryQuality')}}</Button>
  </div>
</template>
