<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
defineProps<{name:string;count:number;error:string}>()
const open=defineModel<boolean>('open',{required:true})
const visible=defineModel<boolean>('visible',{required:true})
const emit=defineEmits<{pick:[];clear:[];interaction:[]}>()
const {t}=useI18n()
</script>
<template>
      <div class="subtitle-group" @click.stop>
        <button class="ctrl-btn" :class="{ active: count > 0 }"
          @click.stop="open = !open; emit('interaction')" :aria-label="t('player.subtitle')" :title="t('player.subtitle')">
          <Icon name="subtitles" :size="16" />
        </button>
        <div class="subtitle-popup" :class="{ show: open }" @click.stop>
          <div class="subtitle-title">{{ t('player.subtitleTitle') }}</div>
          <div v-if="name" class="subtitle-current">
            <span class="subtitle-name" :title="name">{{ name }}</span>
            <span class="subtitle-count">{{ t('player.subtitleCount', { count: count }) }}</span>
          </div>
          <div v-if="error" class="subtitle-error">{{ error }}</div>
          <button class="subtitle-item" @click.stop="emit('pick')">
            <Icon name="folder" :size="13" />
            <span>{{ count > 0 ? t('player.subtitleReplace') : t('player.subtitleLoad') }}</span>
          </button>
          <button v-if="count > 0" class="subtitle-item"
            @click.stop="visible = !visible; emit('interaction')">
            <Icon name="subtitles" :size="13" />
            <span>{{ visible ? t('player.subtitleHide') : t('player.subtitleShow') }}</span>
          </button>
          <button v-if="count > 0" class="subtitle-item subtitle-item--danger"
            @click.stop="emit('clear'); emit('interaction')">
            <Icon name="trash" :size="13" />
            <span>{{ t('player.subtitleRemove') }}</span>
          </button>
          <div class="subtitle-hint">{{ t('player.subtitleHint') }}</div>
        </div>
      </div>
</template>
<style scoped src="../../styles/components/player-subtitle-controls.css"></style>
