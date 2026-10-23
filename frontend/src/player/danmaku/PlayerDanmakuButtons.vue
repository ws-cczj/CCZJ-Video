<script setup lang="ts">
import { onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
const props=defineProps<{ enabled: boolean; ready: boolean; expanded: boolean; mobile?: boolean }>()
const emit = defineEmits<{ toggle: []; settings: [anchor: HTMLElement, hover: boolean]; leave: []; dismiss: [] }>()
const { t } = useI18n()
let hoverTimer:ReturnType<typeof setTimeout>|undefined,pressTimer:ReturnType<typeof setTimeout>|undefined
let pressed=false,startX=0,startY=0
function enter(event:PointerEvent){
 clearTimeout(hoverTimer)
 if(event.pointerType!=='mouse'||!props.ready)return
 const element=event.currentTarget as HTMLElement
 hoverTimer=setTimeout(()=>emit('settings',element,true),150)
}
function leave(){clearTimeout(hoverTimer);clearTimeout(pressTimer);emit('leave')}
function down(event:PointerEvent){
 pressed=false;clearTimeout(hoverTimer);clearTimeout(pressTimer)
 if(event.pointerType==='mouse'||!props.ready)return
 startX=event.clientX;startY=event.clientY
 const element=event.currentTarget as HTMLElement
 pressTimer=setTimeout(()=>{pressed=true;emit('settings',element,false)},450)
}
function move(event:PointerEvent){if(Math.abs(event.clientX-startX)+Math.abs(event.clientY-startY)>12)clearTimeout(pressTimer)}
function up(){clearTimeout(pressTimer)}
function click(){clearTimeout(hoverTimer);clearTimeout(pressTimer);if(pressed){pressed=false;return}emit('toggle')}
function keyboard(event:KeyboardEvent){
 if(event.key==='ArrowUp'||event.key==='ArrowDown'){event.preventDefault();event.stopPropagation();emit('settings',event.currentTarget as HTMLElement,false)}
 if(event.key==='Escape'){event.preventDefault();event.stopPropagation();emit('dismiss')}
}
onBeforeUnmount(()=>{clearTimeout(hoverTimer);clearTimeout(pressTimer)})
</script>
<template>
  <div class="cczj-danmaku-actions" :class="{'cczj-danmaku-touch':mobile}" @click.stop @dblclick.stop @pointerdown.stop>
    <button class="ctrl-btn cczj-danmaku-toggle" data-player-action="danmaku-toggle" :disabled="!ready" :aria-label="t('danmaku.enabled')" :aria-description="t('danmaku.controlHint')" :aria-pressed="enabled" aria-haspopup="dialog" :aria-expanded="expanded" @pointerenter="enter" @pointerleave="leave" @pointerdown="down" @pointermove="move" @pointerup="up" @pointercancel="leave" @contextmenu.prevent @keydown="keyboard" @click="click"><Icon name="danmaku" :size="19" /><span>{{t('danmaku.title')}}</span></button>
  </div>
</template>
<style scoped>
.cczj-danmaku-actions{@apply cczj-flex cczj-items-center cczj-justify-center cczj-text-white cczj-flex-shrink-0 cczj-gap-4}
.cczj-danmaku-actions button{@apply cczj-inline-flex cczj-items-center cczj-justify-center;gap:5px;min-height:32px;padding:0 5px;border:0;@apply cczj-rounded;background:transparent;color:inherit;@apply cczj-cursor-pointer;font:inherit;@apply cczj-whitespace-nowrap}
.cczj-danmaku-toggle[aria-pressed=true]{color:#00aeec}.cczj-danmaku-toggle[aria-pressed=false]{opacity:.65}.cczj-danmaku-actions button:hover{background:rgba(255,255,255,.12)}.cczj-danmaku-actions button:focus-visible{outline:2px solid #00aeec;outline-offset:2px}.cczj-danmaku-actions button:disabled{opacity:.4;@apply cczj-cursor-default}
.cczj-danmaku-touch button{min-height:44px;min-width:44px}
</style>
