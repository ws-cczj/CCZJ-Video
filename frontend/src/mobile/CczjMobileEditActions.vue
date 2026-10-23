<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
defineProps<{ open: boolean }>()
const emit=defineEmits<{done:[];cancel:[]}>()
const {t}=useI18n()
</script>
<template><Transition name="cczj-mobile-edit-float"><div v-if="open" class="cczj-mobile-edit-actions" data-cczj-no-tab-swipe>
  <button class="cczj-mobile-edit-cancel" :aria-label="t('common.cancel')" @click="emit('cancel')"><Icon name="close" :size="19" /></button>
  <button class="cczj-mobile-edit-confirm" :aria-label="t('common.done')" @click="emit('done')"><Icon name="check" :size="21" /></button>
</div></Transition></template>
