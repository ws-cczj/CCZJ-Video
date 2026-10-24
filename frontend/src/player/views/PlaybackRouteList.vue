<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '../../components/ui'
import type { PlayLine } from '../../types'
import type { SourceOption } from '../page/useSourceCatalog'

const props=defineProps<{sources:SourceOption[];activeKey:string;activeLine:number;lines:PlayLine[];busy?:boolean;mobile?:boolean;selectable:(source:SourceOption)=>boolean}>()
const emit=defineEmits<{choose:[source:string,line?:number]}>()
const {t}=useI18n()
const rows=computed(()=>props.sources.flatMap(source=>{
  const known=source.source_key===props.activeKey?props.lines:[]
  const lines=known.length?known.map(line=>({index:line.index,name:line.name,quality:source.lines?.find(item=>item.index===line.index)})):
    source.lines?.length?source.lines.map(quality=>({index:quality.index,name:quality.name,quality})):[{index:undefined,name:'',quality:undefined}]
  return lines.map(line=>({source,...line}))
}).sort((a,b)=>(b.quality?.height||0)-(a.quality?.height||0)||(b.quality?.width||0)-(a.quality?.width||0)||(b.quality?.bandwidth||0)-(a.quality?.bandwidth||0)))
function active(row:typeof rows.value[number]):boolean{return row.source.source_key===props.activeKey&&row.index===props.activeLine}
function quality(row:typeof rows.value[number]):string {
  if(row.quality?.height)return `${row.quality.height}P`
  if(row.quality&&!row.quality.ok)return t('playLine.qualityFailed')
  if(row.source.probe==='probing')return t('player.probeProbing')
  if(row.source.probe==='missing'||row.source.probe==='failed')return t(row.source.probe==='missing'?'player.probeMissing':'player.probeFailed')
  return t(row.source.qualityState==='pending'?'playLine.probingQuality':row.source.qualityState==='failed'?'playLine.qualityFailed':'playLine.unknownQuality')
}
</script>
<template>
  <div class="playback-routes cczj-flex cczj-flex-wrap cczj-gap-4" :class="{'playback-routes-mobile':mobile}" role="group" :aria-label="t('playLine.switch')">
    <Button v-for="row in rows" :key="`${row.source.source_key}:${row.index??'pending'}`" variant="ghost" class="route-item"
      :class="{active:active(row)}" :aria-pressed="active(row)" :data-source="row.source.source_key" :data-line="row.index"
      :disabled="busy||active(row)||(row.source.source_key!==activeKey&&!selectable(row.source))"
      :title="row.quality?.height?t('playLine.declaredQuality',{width:row.quality.width,height:row.quality.height}):quality(row)" @click="emit('choose',row.source.source_key,row.index)">
      <span class="route-label cczj-flex cczj-items-center cczj-gap-2 cczj-flex-1 cczj-min-w-0"><span class="cczj-truncate">{{row.source.name}}</span><span class="route-quality cczj-flex-shrink-0">· {{quality(row)}}</span></span>
      <small v-if="row.name" class="route-name cczj-truncate">{{row.name}}</small>
    </Button>
  </div>
</template>
<style scoped>
.route-item {max-width:100%;min-height:44px;@apply cczj-min-w-0; border:1px solid var(--border-color); }
.route-item :deep(.ui-btn__content) {@apply cczj-min-w-0;}
.route-label {@apply cczj-min-w-0;}
.route-name {@apply cczj-text-xs cczj-font-normal cczj-opacity-70;max-width:90px;}
.route-item.active {border-color:var(--accent);color:var(--accent);background:var(--bg-hover);opacity:1;}
.playback-routes-mobile {@apply cczj-flex-col cczj-flex-nowrap;}
.playback-routes-mobile .route-item {@apply cczj-w-full cczj-justify-start;min-height:48px;color:inherit;border-color:#ffffff26;}
.playback-routes-mobile .route-item :deep(.ui-btn__content) {@apply cczj-w-full cczj-justify-between;}
.playback-routes-mobile .route-item.active {color:#9cddff;border-color:#71d2ff99;background:#71d2ff26;}
</style>
