<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Segment } from '../../components/ui'
import { HOLD_SPEEDS, PICTURE_FITS, type PictureFit } from '../usePlaybackPreferences'
import type { VideoPictureSize } from '../../utils/videoLayout'
const props=defineProps<{holdSpeed:number;pictureFit:PictureFit;pictureSize?:VideoPictureSize}>()
const emit=defineEmits<{holdSpeed:[value:number];pictureFit:[value:string]}>()
const {t}=useI18n()
const holdOptions=HOLD_SPEEDS.map(value=>({value,label:`${value}×`}))
const fitOptions=computed(()=>PICTURE_FITS.map(value=>({value,label:t(`player.pictureFits.${value}`)})))
const ratios=computed(()=>{
  const size=props.pictureSize
  if(!size)return
  const source=size.sourceWidth/size.sourceHeight,viewport=size.viewportWidth/size.viewportHeight
  return {source:`${source.toFixed(2)}:1`,viewport:`${viewport.toFixed(2)}:1`,same:Math.abs(source/viewport-1)<.005}
})
</script>

<template>
  <div class="playback-settings-option">
    <span>{{t('player.holdSpeed')}}</span>
    <Segment :model-value="holdSpeed" :options="holdOptions" size="sm" wrap role="group" :aria-label="t('player.holdSpeed')" @update:model-value="emit('holdSpeed',Number($event))" />
    <p>{{t('player.holdSpeedHint')}}</p>
  </div>
  <div class="playback-settings-option">
    <span>{{t('player.pictureFit')}}</span>
    <Segment :model-value="pictureFit" :options="fitOptions" size="sm" wrap role="group" :aria-label="t('player.pictureFit')" @update:model-value="emit('pictureFit',String($event))" />
    <p>{{t(`player.pictureFitHints.${pictureFit}`)}}</p>
    <p v-if="ratios">{{t('player.pictureFitRatios',{source:ratios.source,viewport:ratios.viewport})}}</p>
    <p v-if="ratios?.same">{{t('player.pictureFitSameRatioHint')}}</p>
  </div>
</template>

<style scoped>
.playback-settings-option { @apply cczj-flex cczj-flex-col cczj-gap-4 cczj-min-w-0; padding: 12px 10px; color: inherit; }
.playback-settings-option > span { @apply cczj-text-sm cczj-font-medium; }
.playback-settings-option > p { margin: 0; @apply cczj-text-xs; line-height: 1.6; opacity: .7; overflow-wrap: anywhere; }
</style>
