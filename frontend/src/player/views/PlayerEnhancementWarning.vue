<script setup lang="ts">
import {useI18n} from 'vue-i18n'
defineProps<{open:boolean}>()
const emit=defineEmits<{confirm:[];cancel:[]}>()
const {t}=useI18n()
</script>
<template>
    <div v-if="open" class="ai-warning-overlay" @click.stop @wheel.stop>
      <div class="ai-warning-dialog">
        <div class="ai-warning-header">
          <span class="ai-warning-icon">⚡</span>
          <span>{{ t('player.aiEnhanceTitle') }}</span>
        </div>
        <div class="ai-warning-body">
          <p>{{ t('player.aiEnhanceIntro') }}</p>
          <ul>
            <li><b>{{ t('player.animeEnhance') }}</b> — {{ t('player.animeEnhanceDesc') }}</li>
            <li><b>{{ t('player.filmEnhance') }}</b> — {{ t('player.filmEnhanceDesc') }}</li>
          </ul>
          <p>{{ t('player.aiEnhanceNotice') }}</p>
          <ul>
            <li><b>{{ t('player.aiWarnGpuLoad') }}</b> — {{ t('player.aiWarnGpuLoadDesc') }}</li>
            <li><b>{{ t('player.aiWarnBattery') }}</b> — {{ t('player.aiWarnBatteryDesc') }}</li>
            <li><b>{{ t('player.aiWarnPerf') }}</b> — {{ t('player.aiWarnPerfDesc') }}</li>
          </ul>
          <p class="ai-warning-note">{{ t('player.aiEnhanceFallbackNote') }}</p>
        </div>
        <div class="ai-warning-footer">
          <button class="ai-warning-btn ai-warning-btn--cancel" @click="emit('cancel')">{{ t('common.cancel') }}</button>
          <button class="ai-warning-btn ai-warning-btn--confirm" @click="emit('confirm')">{{ t('player.confirmEnable') }}</button>
        </div>
      </div>
    </div>

</template>
<style scoped src="../../styles/components/player-enhancement-warning.css"></style>
