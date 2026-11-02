<script setup lang="ts" generic="T">
import { computed, nextTick, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useViewActivity } from '../composables/useViewActivity'

const props = withDefaults(defineProps<{
  items: readonly T[]; itemKey: (item:T,index:number)=>string|number
  estimatedHeight?:number; initialCount?:number; locateIndex?:number; enabled?:boolean
}>(), { estimatedHeight:44, initialCount:30, enabled:true })
defineSlots<{ default(props:{item:T;index:number}):unknown }>()
const root=ref<HTMLElement>()
const active=useViewActivity(()=>props.enabled)
const first=ref(0),last=ref(props.initialCount),columns=ref(3),rowHeight=ref(props.estimatedHeight),gap=ref(0)
const heights=shallowRef(new Map<number,number>())
const measuredRowHeight=ref(props.estimatedHeight)
const focused=ref<number>()
const offsets=computed(()=>{
  const rows=[0]
  for(let row=0;row<Math.ceil(props.items.length/columns.value);row++)rows.push(rows[row]+(heights.value.get(row)??rowHeight.value)+gap.value)
  return rows
})
const ranges=computed(()=>{
  const count=props.items.length,start=Math.min(first.value,Math.max(0,Math.floor((count-1)/columns.value)*columns.value))
  const segments=[{from:start,to:Math.min(count,Math.max(start+columns.value,last.value))}]
  const index=focused.value
  if(index!==undefined&&index<count&&(index<segments[0].from||index>=segments[0].to))segments.push({from:Math.floor(index/columns.value)*columns.value,to:Math.min(count,(Math.floor(index/columns.value)+1)*columns.value)})
  segments.sort((a,b)=>a.from-b.from)
  let cursor=0
  return segments.map(segment=>{
    const before=offsets.value[Math.floor(segment.from/columns.value)]-offsets.value[cursor]
    cursor=Math.ceil(segment.to/columns.value)
    return {...segment,before,items:props.items.slice(segment.from,segment.to)}
  })
})
const after=computed(()=>offsets.value.at(-1)!-offsets.value[Math.ceil((ranges.value.at(-1)?.to??0)/columns.value)])
let host:HTMLElement|undefined,observer:ResizeObserver|undefined,frame:number|undefined,binding=0,measuredWidth=0,savedScrollTop=0,restoring=true,pinnedBottom=false,measurement=0
const observed=new Set<Element>()
function onScroll():void {
  if(restoring||!host||!root.value?.isConnected)return
  const moved=Math.abs(host.scrollTop-savedScrollTop)>.5
  savedScrollTop=host.scrollTop
  if(moved)pinnedBottom=host.scrollHeight-host.clientHeight-host.scrollTop<=1
  schedule()
}
function schedule():void { if(active.value&&frame===undefined)frame=requestAnimationFrame(measure) }
function viewportTop():number {
  if(!host||!root.value)return 0
  return host===root.value?host.scrollTop:host.getBoundingClientRect().top+host.clientTop-root.value.getBoundingClientRect().top
}
function rowAt(position:number,rows:readonly number[]):number {
  let low=0,high=rows.length-1
  while(low<high){const mid=(low+high+1)>>1;if(rows[mid]<=position)low=mid;else high=mid-1}
  return low
}
function measure():void {
  frame=undefined
  const grid=root.value
  if(!grid||!host||!active.value||!grid.clientWidth||!host.clientHeight)return
  const previousOffsets=offsets.value,previousColumns=columns.value,previousTop=viewportTop(),previousScroll=host.scrollTop
  const anchor=rowAt(previousTop,previousOffsets),anchorIndex=anchor*previousColumns
  const pass=++measurement,token=binding
  const style=getComputedStyle(grid),count=style.gridTemplateColumns.split(/\s+/).filter(Boolean).length
  if(Math.abs(grid.clientWidth-measuredWidth)>1||count!==columns.value){
    measuredWidth=grid.clientWidth
    if(style.gridTemplateColumns!=='none'&&count>0)columns.value=count
    heights.value=new Map()
  }
  gap.value=Number.parseFloat(style.rowGap)||0
  const nodes=Array.from(grid.querySelectorAll<HTMLElement>('[data-virtual-index]'))
  const measured=new Map<number,number>()
  for(const node of nodes){
    const row=Math.floor(Number(node.dataset.virtualIndex)/columns.value)
    measured.set(row,Math.max(measured.get(row)||0,node.offsetHeight))
  }
  if(measured.size)measuredRowHeight.value=Math.max(...measured.values())
  if(!heights.value.size&&nodes[0])rowHeight.value=Math.max(1,nodes[0].offsetHeight)
  const next=new Map(heights.value)
  let changed=false
  for(const [row,height] of measured)if(height>0&&Math.abs((next.get(row)||0)-height)>.5){next.set(row,height);changed=true}
  if(changed)heights.value=next
  const current=new Set<Element>([grid,host,...nodes])
  for(const node of observed)if(!current.has(node)){observer?.unobserve(node);observed.delete(node)}
  for(const node of current)if(!observed.has(node)){observer?.observe(node);observed.add(node)}
  const anchorRow=Math.floor(anchorIndex/columns.value)
  const top=previousTop>=0?(offsets.value[Math.min(anchorRow,offsets.value.length-1)]+previousTop-previousOffsets[anchor]):previousTop
  const delta=top-previousTop,keepBottom=pinnedBottom
  first.value=Math.max(0,Math.min(rowAt(top,offsets.value)-3,Math.ceil(props.items.length/columns.value)-1))*columns.value
  last.value=Math.min(props.items.length,Math.max(first.value+columns.value,(rowAt(top+host.clientHeight,offsets.value)+4)*columns.value))
  if(Math.abs(delta)>.5||(changed&&keepBottom))void nextTick(()=>{
    if(token!==binding||pass!==measurement||!host||!active.value)return
    host.scrollTop=keepBottom?host.scrollHeight:previousScroll+delta
    savedScrollTop=host.scrollTop
    schedule()
  })

}
async function scrollToIndex(index:number,focus=false):Promise<void> {
  if(!host||!root.value||index<0||index>=props.items.length||!active.value)return
  pinnedBottom=false
  const token=binding,row=Math.floor(index/columns.value),top=offsets.value[row],bottom=offsets.value[row+1]-gap.value,current=viewportTop()
  if(top<current||bottom>current+host.clientHeight)host.scrollTop+=top-current-(host.clientHeight-(bottom-top))/2
  if(focus)focused.value=index
  measure()
  await nextTick()
  if(token!==binding)return
  const node=root.value?.querySelector<HTMLElement>(`[data-virtual-index="${index}"]`)
  if(node){
    const bounds=node.getBoundingClientRect(),view=host.getBoundingClientRect()
    if(bounds.top<view.top||bounds.bottom>view.bottom)host.scrollTop+=bounds.top-view.top-(host.clientHeight-bounds.height)/2
    if(focus)node.focus({preventScroll:true})
  }
}
function focusIn(event:FocusEvent):void {
  const target=(event.target as Element)?.closest<HTMLElement>('[data-virtual-index]')
  if(target)focused.value=Number(target.dataset.virtualIndex)
}
async function keyboard(event:KeyboardEvent):Promise<void> {
  if(event.key!=='Tab'||focused.value===undefined)return
  const index=focused.value+(event.shiftKey?-1:1)
  if(index<0||index>=props.items.length||root.value?.querySelector(`[data-virtual-index="${index}"]`))return
  event.preventDefault()
  await scrollToIndex(index,true)
}
function stop():void {
  binding++
  restoring=true
  host?.removeEventListener('scroll',onScroll)
  window.removeEventListener('resize',schedule)
  observer?.disconnect();observer=undefined;observed.clear()
  if(frame!==undefined)cancelAnimationFrame(frame)
  frame=undefined;host=undefined
}
watch([root,active],async()=>{
  stop()
  const token=binding
  await nextTick()
  if(token!==binding||!root.value||!active.value)return
  let parent:HTMLElement|null=root.value
  while(parent&&!/auto|scroll/.test(getComputedStyle(parent).overflowY))parent=parent.parentElement
  host=parent||undefined
  if(!host)return
  host.addEventListener('scroll',onScroll,{passive:true})
  window.addEventListener('resize',schedule)
  observer=new ResizeObserver(schedule)
  observer.observe(root.value);observer.observe(host)
  host.scrollTop=savedScrollTop
  restoring=false
  measure()
  if(props.locateIndex!==undefined)void scrollToIndex(props.locateIndex)
},{flush:'post',immediate:true})
watch(()=>props.items,()=>{focused.value=undefined;heights.value=new Map();schedule();if(props.locateIndex!==undefined)void nextTick(()=>scrollToIndex(props.locateIndex!))})
watch(()=>props.items.length,schedule)
watch(()=>props.locateIndex,index=>{if(index!==undefined)void nextTick(()=>scrollToIndex(index))})
onBeforeUnmount(stop)
defineExpose({scrollToIndex})
</script>

<template>
  <div ref="root" class="cczj-virtual-grid" :style="{'--cczj-virtual-row-height':measuredRowHeight+'px','--cczj-virtual-row-gap':gap+'px'}" @keydown.capture="keyboard" @focusin="focusIn" @focusout="if(!root?.contains($event.relatedTarget as Node)){focused=undefined;schedule()}">
    <template v-for="(range,rangeIndex) in ranges" :key="rangeIndex">
      <div v-if="range.before" class="cczj-virtual-grid-spacer cczj-pointer-events-none" :style="{height:Math.max(0,range.before-gap)+'px'}" aria-hidden="true" />
      <template v-for="(item,index) in range.items" :key="itemKey(item,range.from+index)"><slot :item="item" :index="range.from+index" /></template>
    </template>
    <div v-if="after" class="cczj-virtual-grid-spacer cczj-pointer-events-none" :style="{height:Math.max(0,after-gap)+'px'}" aria-hidden="true" />
  </div>
</template>

<style scoped>
.cczj-virtual-grid { overflow-anchor:none; grid-auto-rows:max-content; }
.cczj-virtual-grid-spacer { grid-column:1 / -1; }
</style>
