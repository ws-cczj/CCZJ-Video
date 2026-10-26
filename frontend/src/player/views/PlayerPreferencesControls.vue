<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
defineProps<{mobile:boolean;autoplay:boolean;autoNext:boolean;autoResume:boolean;hasDanmaku?:boolean}>()
const open=defineModel<boolean>('open',{required:true})
const emit=defineEmits<{autoplay:[];autoNext:[];autoResume:[];interaction:[];shortcuts:[];danmakuSettings:[]}>()
const {t}=useI18n()
</script>
<template>
      <div class="playback-settings-group" @click.stop>
        <button v-if="!mobile" class="ctrl-btn" @click="open = !open; emit('interaction')" :aria-label="t('settings.playback')" :title="t('settings.playback')">
          <Icon name="settings" :size="16" />
        </button>
        <div class="playback-settings-popup" :class="{ show: mobile || open }" @click.stop>
          <div class="playback-settings-item">
            <span>{{ t('player.autoPlay') }}</span>
            <label class="ps-toggle">
              <input type="checkbox" :checked="autoplay" @change="emit('autoplay')" />
              <span class="ps-switch"></span>
            </label>
          </div>
          <div class="playback-settings-item">
            <span>{{ t('player.autoNext') }}</span>
            <label class="ps-toggle">
              <input type="checkbox" :checked="autoNext" @change="emit('autoNext')" />
              <span class="ps-switch"></span>
            </label>
          </div>
          <div v-if="mobile" class="playback-settings-item">
            <span>{{ t('mobile.resumeAlways') }}</span>
            <label class="ps-toggle"><input type="checkbox" :checked="autoResume" @change="emit('autoResume')" /><span class="ps-switch"></span></label>
          </div>
          <div class="playback-settings-divider"></div>
          <button v-if="hasDanmaku" class="playback-settings-link" data-player-action="danmaku-settings" @click.stop="open=false;emit('danmakuSettings')">
            <Icon name="danmaku-settings" :size="16" /><span>{{t('danmaku.settings')}}</span><Icon name="chevron-right" :size="12" />
          </button>
          <button v-if="!mobile" class="playback-settings-link" @click.stop="open = false; emit('shortcuts')">
            <Icon name="keyboard" :size="14" />
            <span>{{ t('player.shortcutSettings') }}</span>
            <Icon name="chevron-right" :size="12" />
          </button>
        </div>
      </div>
</template>
<style scoped src="../../styles/components/player-preferences.css"></style>
