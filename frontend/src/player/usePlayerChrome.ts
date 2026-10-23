import { computed, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'

/** Control visibility has one timer owner, shared by touch and pointer layouts. */
export function usePlayerChrome(deps:{
  mobile:boolean;playing:Ref<boolean>;pinned:()=>boolean;leaveBlocked:()=>boolean;root:()=>HTMLElement|undefined
}) {
const showControls=ref(!deps.mobile)
const engaged=ref(false)
const mouseInside=ref(false)
const controlsVisible=computed(()=>showControls.value||(!deps.mobile&&!deps.playing.value)||deps.pinned())
let hideTimer:ReturnType<typeof setTimeout>|undefined
let leaveTimer:ReturnType<typeof setTimeout>|undefined
function clearHide():void {if(hideTimer!==undefined)clearTimeout(hideTimer);hideTimer=undefined}
function scheduleHide():void {
  clearHide()
  if(deps.pinned()||(deps.mobile&&!deps.playing.value))return
  hideTimer=setTimeout(()=>{hideTimer=undefined;if(!deps.pinned())showControls.value=false},3000)
}
function keepVisible():void {
  if(deps.mobile&&!engaged.value)return
  showControls.value=true
  scheduleHide()
}
function toggleShow(visible:boolean):void {
  if(deps.pinned()){keepVisible();return}
  showControls.value=visible
  clearHide()
  if(visible)scheduleHide()
}
function onMouseLeave():void {
  if(deps.mobile)return
  mouseInside.value=false
  if(leaveTimer!==undefined)clearTimeout(leaveTimer)
  leaveTimer=setTimeout(()=>{
    leaveTimer=undefined
    const active=document.activeElement
    const overDropdown=Array.from(deps.root()?.querySelectorAll('.select-panel')||[]).some(el=>el.contains(active)||el.matches(':hover'))
    if(!overDropdown&&!deps.leaveBlocked())toggleShow(false)
  },500)
}
function reset():void {clearHide();if(leaveTimer!==undefined)clearTimeout(leaveTimer);leaveTimer=undefined;if(deps.mobile){engaged.value=false;showControls.value=false}}
function reveal():void {engaged.value=true;keepVisible()}
onMounted(()=>{
  watch(deps.playing,()=>{if(deps.mobile&&engaged.value)keepVisible()})
  watch(deps.pinned,keepVisible)
})
onBeforeUnmount(()=>{clearHide();if(leaveTimer!==undefined)clearTimeout(leaveTimer)})
return {showControls,engaged,mouseInside,controlsVisible,keepVisible,toggleShow,onMouseLeave,reset,reveal}
}
