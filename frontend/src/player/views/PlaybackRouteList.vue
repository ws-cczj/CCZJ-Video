<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '../../components/ui'
import type { PlayLine } from '../../types'
import type { SourceOption } from '../page/useSourceCatalog'
import { declaredQualityTier } from '../streamQuality'

const props=defineProps<{sources:SourceOption[];activeKey:string;activeLine:number;lines:PlayLine[];busy?:boolean;mobile?:boolean;dark?:boolean;selectable:(source:SourceOption)=>boolean}>()
const emit=defineEmits<{choose:[source:string,line?:number]}>()
const {t}=useI18n()
const rows=computed(()=>props.sources.flatMap(source=>{
  const known=source.source_key===props.activeKey?props.lines:[]
  const lines=known.length?known.map(line=>({index:line.index,name:line.name,quality:source.lines?.find(item=>item.index===line.index)})):
    source.lines?.length?source.lines.map(quality=>({index:quality.index,name:quality.name,quality})):[{index:undefined,name:'',quality:undefined}]
  return lines.map(line=>({source,...line,tier:line.quality?.ok?declaredQualityTier(line.quality.width,line.quality.height):0}))
}).sort((a,b)=>b.tier-a.tier||(b.quality?.bandwidth||0)-(a.quality?.bandwidth||0)))
function active(row:typeof rows.value[number]):boolean{return row.source.source_key===props.activeKey&&row.index===props.activeLine}
function quality(row:typeof rows.value[number]):string {
  if(row.tier)return `${row.tier}P`
  if(row.source.qualityState==='limited'||row.quality?.code==='RATE_LIMITED')return t('playLine.qualityLimited')
  if(row.quality&&!row.quality.ok)return t('playLine.qualityFailed')
  if(row.source.probe==='probing')return t('player.probeProbing')
  if(row.source.probe==='missing'||row.source.probe==='failed')return t(row.source.probe==='missing'?'player.probeMissing':'player.probeFailed')
  return t(row.source.qualityState==='pending'?'playLine.probingQuality':row.source.qualityState==='failed'?'playLine.qualityFailed':'playLine.unknownQuality')
}
function badge(row:typeof rows.value[number]):string {
  if(row.tier)return `${row.tier}P`
  if(row.source.probe==='missing')return t('player.probeMissing')
  if(row.source.qualityState==='limited'||row.quality?.code==='RATE_LIMITED')return t('playLine.limitedShort')
  if(row.source.probe==='failed')return t('player.probeFailed')
  if(row.source.qualityState==='failed'||(row.quality&&!row.quality.ok))return t('playLine.unmeasuredShort')
  return t(row.source.probe==='probing'||row.source.qualityState==='pending'?'playLine.probingShort':'playLine.unknownShort')
}
function description(row:typeof rows.value[number]):string {
  return row.tier&&row.quality?t('playLine.declaredQuality',{quality:`${row.tier}P`,width:row.quality.width,height:row.quality.height}):quality(row)
}
</script>
<template>
  <div class="playback-routes cczj-flex cczj-flex-col cczj-gap-3 cczj-min-w-0 cczj-min-h-0 cczj-overflow-y-auto cczj-overflow-x-hidden cczj-p-2" :class="{'playback-routes-mobile':mobile,'playback-routes-dark':dark||mobile}" role="group" :aria-label="t('playLine.switch')">
    <Button v-for="row in rows" :key="`${row.source.source_key}:${row.index??'pending'}`" block variant="ghost" class="route-item cczj-flex-shrink-0 cczj-text-left"
      :class="{active:active(row)}" :aria-pressed="active(row)" :data-source="row.source.source_key" :data-line="row.index"
      :disabled="busy||active(row)||(row.source.source_key!==activeKey&&!selectable(row.source))"
      :title="description(row)" :aria-label="[row.source.name,row.name,description(row)].filter(Boolean).join(' · ')" @click="emit('choose',row.source.source_key,row.index)">
      <span class="route-content cczj-grid cczj-w-full cczj-min-w-0 cczj-gap-2">
        <span class="route-label cczj-truncate">{{row.source.name}}</span>
        <span class="route-quality cczj-text-sm cczj-font-semibold cczj-whitespace-nowrap">{{badge(row)}}</span>
        <small v-if="row.name" class="route-name cczj-truncate cczj-text-xs cczj-font-normal">{{row.name}}</small>
      </span>
    </Button>
  </div>
</template>
<style scoped>
.playback-routes {max-height:clamp(144px,30vh,252px);@apply cczj-flex-shrink-0;}
.playback-routes-dark {--route-text:#e5e9f0;--route-muted:#a9b2c0;--route-border:#ffffff26;--route-bg:#ffffff06;--route-active:#9cddff;--route-active-bg:#71d2ff26;}
.route-item {max-width:100%;min-height:52px;@apply cczj-min-w-0;line-height:1.35;color:var(--route-text,var(--text-primary));border-color:var(--route-border,var(--border));background:var(--route-bg,transparent);}
.route-item :deep(.ui-btn__content) {@apply cczj-w-full cczj-min-w-0;}
.route-content {grid-template-columns:minmax(0,1fr) auto;@apply cczj-items-center;}
.route-label {grid-column:1;@apply cczj-min-w-0;}
.route-quality {grid-column:2;grid-row:1 / span 2;padding:3px 7px;border:1px solid var(--route-border,var(--border));border-radius:6px;}
.route-name {grid-column:1;grid-row:2;color:var(--route-muted,var(--text-secondary));}
.route-item.active {border-color:var(--route-active,var(--accent));color:var(--route-active,var(--accent));background:var(--route-active-bg,var(--bg-hover));@apply cczj-opacity-100;}
.playback-routes-mobile {max-height:none;--route-text:var(--cczj-player-action-ink,#e5e9f0);--route-muted:var(--route-text);--route-bg:var(--cczj-player-action-bg,#24262b);--route-active:var(--route-text);--route-active-bg:var(--route-bg);@apply cczj-overflow-visible;}
.playback-routes-mobile .route-item.active { border-color:var(--route-text); outline:1px solid var(--route-bg); outline-offset:2px; }
</style>
