<script setup lang="ts">
import {computed} from 'vue'
import {useI18n} from 'vue-i18n'
import {Select as SelectDropdown} from '../../components/ui'
const props=defineProps<{mobile:boolean;value:string;options:Array<{value:string;label:string}>}>()
const emit=defineEmits<{change:[value:string|number];open:[open:boolean]}>()
const {t}=useI18n()
const groups=computed(()=>[
 {title:t('mobile.imageProcessing'),options:props.options.filter(option=>option.value==='original'||option.value==='ai_film')},
 {title:t('player.animeEnhance'),options:props.options.filter(option=>option.value.startsWith('ai_anime_'))},
 {title:t('mobile.extensionEffects'),options:props.options.filter(option=>option.value.startsWith('ai_custom_'))},
].filter(group=>group.options.length>0))
</script>
<template>
 <div v-if="mobile" class="cczj-mobile-more-quality">
  <p class="cczj-mobile-drawer-hint">{{t('mobile.imageProcessingHint')}}</p>
  <section v-for="group in groups" :key="group.title" class="cczj-mobile-drawer-group"><h3>{{group.title}}</h3><div class="cczj-mobile-quality-options" role="group" :aria-label="group.title"><button v-for="option in group.options" :key="option.value" :aria-pressed="value===option.value" @click="emit('change',option.value)">{{option.label}}</button></div></section>
 </div>
 <div v-else class="quality-group" role="group" :aria-label="t('mobile.quality')" @click.stop style="margin-left:auto">
  <SelectDropdown :model-value="value" :options="options" size="sm" inline inline-drop="up" @change="emit('change',$event)" @open-change="emit('open',$event)" />
 </div>
</template>
<style scoped src="../../styles/components/player-quality-controls.css"></style>
