<script setup lang="ts">
import {useI18n} from 'vue-i18n'
import Icon from '../../components/Icon.vue'
defineProps<{volume:number;muted:boolean}>()
const open=defineModel<boolean>('open',{required:true})
const emit=defineEmits<{mute:[];input:[event:Event];interaction:[]}>()
const {t}=useI18n()
</script>
<template>
      <div class="volume-group" @click.stop @mouseenter="open = true" @mouseleave="open = false">
        <button class="ctrl-btn" @click.stop="emit('mute');emit('interaction')"
          :aria-label="muted ? t('player.unmute') : t('player.muteWithHotkey')" :title="muted ? t('player.unmute') : t('player.muteWithHotkey')">
          <Icon :name="muted ? 'volume-off' : 'volume'" :size="16" />
        </button>
        <div class="volume-popup" :class="{ show: open }" @click.stop>
          <div class="volume-slider-wrap">
            <input class="volume-slider-v" type="range" min="0" max="1" step="0.05" :value="muted ? 0 : volume"
              @input="emit('input',$event)" @change="emit('interaction')" />
          </div>
          <span class="volume-label">{{ Math.round((muted ? 0 : volume) * 100) }}</span>
        </div>
      </div>

</template>
<style scoped src="../../styles/components/player-volume-controls.css"></style>
