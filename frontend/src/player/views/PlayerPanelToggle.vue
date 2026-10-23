<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'

defineProps<{collapsed:boolean}>()
const emit=defineEmits<{toggle:[]}>()
const {t}=useI18n()
const visible=ref(false)
let timer:ReturnType<typeof setTimeout>|undefined
function reveal(){
 visible.value=true
 clearTimeout(timer)
 timer=setTimeout(()=>{visible.value=false},5000)
}
function approach(event:PointerEvent){
 if(event.pointerType==='touch')return
 const host=event.currentTarget as HTMLElement
 const bounds=host.getBoundingClientRect()
 if(bounds.right-event.clientX<=80&&Math.abs(event.clientY-(bounds.top+bounds.height/2))<=110)reveal()
}
function toggle(){reveal();emit('toggle')}
onBeforeUnmount(()=>clearTimeout(timer))
defineExpose({approach})
</script>
<template>
 <button class="panel-toggle-btn" :class="{'is-shown':visible}" :title="collapsed?t('player.expandPanel'):t('player.collapsePanel')" :aria-label="collapsed?t('player.expandPanel'):t('player.collapsePanel')" :aria-expanded="!collapsed" aria-controls="player-information-panel" @pointerenter="reveal" @focus="reveal" @click.stop="toggle" @pointerdown.stop @dblclick.stop>
  <Icon :name="collapsed?'chevron-left':'chevron-right'" :size="14" />
 </button>
</template>
