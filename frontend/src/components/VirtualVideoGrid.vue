<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useViewActivity } from '../composables/useViewActivity'
import VideoCard from './VideoCard.vue'
import type { Video } from '../types'

const props = defineProps<{ items: Video[]; catalogLabel?: string; inCatalog?: boolean }>()
const emit = defineEmits<{ select: [video: Video] }>()
const root = ref<HTMLElement>()
const active = useViewActivity()
const first = ref(0), last = ref(24), columns = ref(2), rowHeight = ref(300), gap = ref(12)
const heights = shallowRef(new Map<number,number>())
const offsets = computed(() => {
  const result=[0]
  for(let row=0;row<Math.ceil(props.items.length/columns.value);row++)result.push(result[row]+(heights.value.get(row)??rowHeight.value)+gap.value)
  return result
})
const focused = shallowRef<Video>()
const ranges = computed(() => {
  const segments=[{from:first.value,to:Math.min(last.value,props.items.length)}]
  const index=focused.value?props.items.indexOf(focused.value):-1
  if(index>=0&&(index<first.value||index>=last.value))segments.push({from:Math.floor(index/columns.value)*columns.value,to:Math.min(props.items.length,(Math.floor(index/columns.value)+1)*columns.value)})
  segments.sort((a,b)=>a.from-b.from)
  let cursor=0
  return segments.map(segment=>{
    const row=Math.floor(segment.from/columns.value), before=offsets.value[row]-offsets.value[cursor]
    cursor=Math.ceil(segment.to/columns.value)
    return {...segment,before,items:props.items.slice(segment.from,segment.to)}
  })
})
const after = computed(() => offsets.value.at(-1)!-offsets.value[Math.ceil((ranges.value.at(-1)?.to??0)/columns.value)])
let host: HTMLElement | undefined, observer: ResizeObserver | undefined, frame: number | undefined, binding = 0, measuredWidth = 0
async function keyboard(event:KeyboardEvent):Promise<void>{
  if(event.key!=='Tab'||!focused.value)return
  const index=props.items.indexOf(focused.value)+(event.shiftKey?-1:1)
  const item=props.items[index]
  if(!item||root.value?.querySelector(`[data-virtual-index="${index}"]`))return
  event.preventDefault();focused.value=item
  await nextTick()
  root.value?.querySelector<HTMLElement>(`[data-virtual-index="${index}"]`)?.focus()
}
function schedule(): void { if (frame === undefined) frame = requestAnimationFrame(measure) }
function measure(): void {
  frame = undefined
  const grid = root.value
  if (!grid || !active.value || !host) return
  const style = getComputedStyle(grid)
  if(Math.abs(grid.clientWidth-measuredWidth)>1){measuredWidth=grid.clientWidth;heights.value=new Map()}
  const count = style.gridTemplateColumns.split(/\s+/).filter(Boolean).length
  if (count > 0 && style.gridTemplateColumns !== 'none' && columns.value!==count) { columns.value = count; heights.value=new Map() }
  gap.value = Number.parseFloat(style.rowGap) || 0
  const card = grid.querySelector<HTMLElement>('.video-card')
  if (card && !heights.value.size) rowHeight.value = Math.max(1, card.offsetHeight)
  const measured=new Map<number,number>()
  for(const node of Array.from(grid.querySelectorAll<HTMLElement>('[data-virtual-index]'))){
    const row=Math.floor(Number(node.dataset.virtualIndex)/columns.value)
    measured.set(row,Math.max(measured.get(row)||0,node.offsetHeight))
  }
  const next=new Map(heights.value)
  let changed=false
  for(const [row,height] of measured)if(Math.abs((next.get(row)||0)-height)>.5){next.set(row,height);changed=true}
  if(changed)heights.value=next
  const top = host.getBoundingClientRect().top - grid.getBoundingClientRect().top
  const find=(value:number)=>{let lo=0,hi=offsets.value.length-1;while(lo<hi){const mid=(lo+hi+1)>>1;if(offsets.value[mid]<=value)lo=mid;else hi=mid-1}return lo}
  const begin=Math.max(0,find(top)-3),end=find(top+host.clientHeight)+4
  first.value = Math.min(begin*columns.value,Math.max(0,Math.floor((props.items.length-1)/columns.value)*columns.value))
  last.value = Math.min(props.items.length, Math.max(first.value+columns.value,end*columns.value))
}
function stop(): void {
  binding++
  host?.removeEventListener('scroll', schedule)
  observer?.disconnect(); observer = undefined
  window.removeEventListener('resize', schedule)
  if (frame !== undefined) cancelAnimationFrame(frame)
  frame = undefined; host = undefined
}
watch([root, active], async () => {
  stop()
  const token = binding
  await nextTick()
  if (token !== binding || !root.value || !active.value) return
  // Reuse the page's scroll owner, including the short-drama theatre.
  let parent = root.value.parentElement
  while (parent && !/auto|scroll/.test(getComputedStyle(parent).overflowY)) parent = parent.parentElement
  host = parent || undefined
  if (!host) return
  host.addEventListener('scroll', schedule, { passive: true })
  window.addEventListener('resize', schedule)
  observer = new ResizeObserver(schedule)
  observer.observe(root.value); observer.observe(host)
  schedule()
}, { flush: 'post', immediate: true })
watch(() => props.items, () => { focused.value = undefined; heights.value=new Map(); schedule() })
watch(() => props.items.length, schedule)
onBeforeUnmount(stop)
</script>

<template>
  <div ref="root" class="virtual-video-grid" @keydown.capture="keyboard" @focusout="if(!root?.contains($event.relatedTarget as Node)) focused=undefined; schedule()">
    <template v-for="(range,rangeIndex) in ranges" :key="rangeIndex">
    <div v-if="range.before" class="virtual-video-spacer" :style="{height: Math.max(0,range.before-gap)+'px'}" aria-hidden="true" />
    <VideoCard v-for="(item,index) in range.items" :key="`${item.source_key || ''}:${item.global_id || item.vod_id}`" :video="item" :data-virtual-index="range.from+index"
      :in-catalog="inCatalog ?? item.in_catalog" :catalog-label="catalogLabel"
      @focusin="focused=item" @click="emit('select',item)">
      <template v-if="$slots.meta" #meta><slot name="meta" :item="item" /></template>
    </VideoCard>
    </template>
    <div v-if="after" class="virtual-video-spacer" :style="{height: Math.max(0,after-gap)+'px'}" aria-hidden="true" />
  </div>
</template>

<style scoped>
.virtual-video-grid { overflow-anchor: none; }
.virtual-video-spacer { grid-column: 1 / -1; @apply cczj-pointer-events-none; }
</style>
