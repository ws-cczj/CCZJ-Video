<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '../components/ui'
import { isMobileRuntime } from '../api/runtime'
import { normalizeReaderKey,type ReadingInputPreferences } from './inputPreferences'
const props=defineProps<{value:ReadingInputPreferences}>()
const emit=defineEmits<{(e:'change',value:Partial<ReadingInputPreferences>):void}>()
const {t}=useI18n(),capture=ref<''|'previousKey'|'nextKey'>('')
function bindKey(event:KeyboardEvent){
 if(!capture.value)return
 event.preventDefault();event.stopPropagation()
 if(event.code==='Escape'){capture.value='';return}
 const code=normalizeReaderKey(event.code,'')
 if(!code||code===props.value[capture.value==='nextKey'?'previousKey':'nextKey'])return
 emit('change',{[capture.value]:code});capture.value=''
}
function keyLabel(code:string):string{return ({ArrowLeft:'←',ArrowRight:'→',ArrowUp:'↑',ArrowDown:'↓'} as Record<string,string>)[code]??code.replace(/^(Key|Digit)/,'')}
</script>
<template><template v-if="!isMobileRuntime()">
 <div class="reading-input-row cczj-flex cczj-items-center cczj-justify-between cczj-flex-wrap cczj-gap-5"><span>{{t('novel.wheelPaging')}}</span><Button :aria-pressed="value.wheelPaging" @click="emit('change',{wheelPaging:!value.wheelPaging})">{{t(value.wheelPaging?'novel.enabled':'novel.disabled')}}</Button></div>
 <div class="reading-input-row cczj-flex cczj-items-center cczj-justify-between cczj-flex-wrap cczj-gap-5"><span>{{t('novel.keyboardPaging')}}</span><Button :aria-pressed="value.keyboardPaging" @click="emit('change',{keyboardPaging:!value.keyboardPaging})">{{t(value.keyboardPaging?'novel.enabled':'novel.disabled')}}</Button></div>
 <div v-for="field in (['previousKey','nextKey'] as const)" :key="field" class="reading-input-row cczj-flex cczj-items-center cczj-justify-between cczj-flex-wrap cczj-gap-5"><span>{{t(field==='previousKey'?'novel.previousPageKey':'novel.nextPageKey')}}</span><Button :aria-label="t(field==='previousKey'?'novel.previousPageKey':'novel.nextPageKey')" @click="capture=field" @blur="capture=''" @keydown="bindKey">{{capture===field?t('novel.pressKey'):keyLabel(value[field])}}</Button></div>
 <p class="reading-input-help">{{t('novel.desktopHelp')}}</p>
</template></template>
<style scoped>
.reading-input-row{padding:14px 0;border-bottom:1px solid var(--reader-line,var(--novel-line));font-size:calc(13rem / 14);}
.reading-input-help{font-size:calc(12rem / 14);line-height:1.8;color:var(--reader-muted,var(--novel-muted));}
</style>
