<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import { useOverlayFocus } from '../composables/useOverlayFocus'
defineProps<{ title: string }>()
const emit=defineEmits<{(e:'close'):void}>(),{t}=useI18n(),dialog=ref<HTMLElement|null>(null)
function close():void { emit('close') }
useOverlayFocus(dialog, () => true, close)
</script>
<template><div class="reader-sheet-mask" @click.stop.self="close"><section ref="dialog" data-motion-panel class="reader-sheet" role="dialog" aria-modal="true" tabindex="-1" :aria-label="title"><header><h2>{{ title }}</h2><button class="reader-sheet-close" :aria-label="t('common.close')" @click="close"><Icon name="close" :size="20" /></button></header><div class="reader-sheet-content"><slot /></div></section></div></template>
<style scoped>
.reader-sheet-mask { @apply cczj-absolute;inset:0;z-index:40;background:#0005;@apply cczj-flex cczj-items-end cczj-justify-center; }
.reader-sheet { background:var(--reader-surface,#fff);color:var(--reader-ink,#282620);border-radius:20px 20px 0 0;max-height:85%;@apply cczj-w-full;max-width:760px;@apply cczj-flex cczj-flex-col;padding:8px 24px calc(20px + env(safe-area-inset-bottom,0px));box-shadow:0 -8px 40px #0001; }
header { @apply cczj-flex cczj-items-center cczj-justify-between cczj-gap-6 cczj-flex-shrink-0; } h2 { margin:0;@apply cczj-text-lg cczj-font-semibold; }
.reader-sheet-close { border:0;@apply cczj-grid;place-items:center;width:44px;height:44px;color:inherit;background:transparent;@apply cczj-cursor-pointer; }
.reader-sheet-content { @apply cczj-overflow-auto;overscroll-behavior:contain;scrollbar-width:none;@apply cczj-min-h-0;padding-inline:4px; }.reader-sheet-content::-webkit-scrollbar { @apply cczj-hidden; }
@media(min-width:900px){.reader-sheet { max-height:78%; }}
</style>
