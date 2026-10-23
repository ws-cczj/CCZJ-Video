<script setup lang="ts">
import {useI18n} from 'vue-i18n'
import Icon from '../../components/Icon.vue'
defineProps<{speed:number}>()
const open=defineModel<boolean>('open',{required:true})
const emit=defineEmits<{change:[speed:number];interaction:[]}>()
const speedOptions=[.5,.75,1,1.25,1.5,2,3]
const {t}=useI18n()
</script>
<template>
      <div class="speed-group" @click.stop>
        <button class="ctrl-btn speed-btn" :aria-label="t('player.playbackSpeed')"
          @click="open = !open; emit('interaction')" :title="t('player.playbackSpeed')">
          <span class="speed-text">{{ speed }}x</span>
          <Icon name="chevron-down" :size="10" />
        </button>
        <div class="speed-popup" :class="{ show: open }">
          <button v-for="s in speedOptions" :key="s" class="speed-item" :class="{ active: speed === s }"
            @click="emit('change',s);emit('interaction')">
            {{ s }}x
            <Icon v-if="speed === s" name="check" :size="12" />
          </button>
        </div>
      </div>

</template>
<style scoped src="../../styles/components/player-speed-controls.css"></style>
