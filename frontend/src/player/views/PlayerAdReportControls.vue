<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
defineProps<{open:boolean;domains:string[]}>()
const emit=defineEmits<{toggle:[];report:[domain:string];interaction:[]}>()
const {t}=useI18n()
</script>
<template>
      <div class="report-ad-group" @click.stop>
        <button class="ctrl-btn" @click.stop="emit('toggle');emit('interaction')" :aria-label="t('player.reportAdTip')" :title="t('player.reportAdTip')">
          <Icon name="flag" :size="16" />
        </button>
        <div class="report-ad-popup" :class="{ show: open }" @click.stop>
          <div class="report-ad-title">{{ t('player.reportAdHint') }}</div>
          <div v-if="domains.length === 0" class="report-ad-empty">{{ t('player.reportAdEmpty') }}</div>
          <button v-for="d in domains" :key="d" class="report-ad-item" @click.stop="emit('report',d)">
            <Icon name="shield" :size="13" />
            <span class="report-ad-domain">{{ d }}</span>
          </button>
        </div>
      </div>
</template>
<style scoped src="../../styles/components/player-ad-controls.css"></style>
