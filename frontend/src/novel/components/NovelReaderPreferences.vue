<script setup lang="ts">
import { Button } from '../../components/ui'
import Icon from '../../components/Icon.vue'
import ReaderInputPreferences from '../../reading/ReaderInputPreferences.vue'
import { useI18n } from 'vue-i18n'
import { useNovelReading } from '../readingState'
import type { ReadingTheme } from '../domain'
const reading=useNovelReading(),{t}=useI18n()
defineProps<{ preview?: boolean }>()
const themes:ReadingTheme[]=['paper','white','night']
</script>
<template>
  <div class="novel-reader-preferences">
    <section v-if="preview" class="novel-reader-preview" :aria-label="t('readingUI.readerPreview')" :data-paper="reading.preferences.theme" :style="{ '--preview-size': reading.preferences.size + 'px', '--preview-leading': reading.preferences.leading, '--preview-font': reading.preferences.serif ? 'SimSun, serif' : 'system-ui, sans-serif' }"><h2>{{ t('readingUI.previewTitle') }}</h2><p>{{ t('readingUI.previewText') }}</p></section>
    <div class="novel-preference-row novel-brightness-row"><span>{{ t('novel.brightness') }}</span><input type="range" min="20" max="100" :value="reading.preferences.brightness*100" :disabled="reading.preferences.systemBrightness" :aria-label="t('novel.brightness')" @input="reading.setPreferences({brightness:Number(($event.target as HTMLInputElement).value)/100})" /><Button :aria-pressed="reading.preferences.systemBrightness" @click="reading.setPreferences({systemBrightness:!reading.preferences.systemBrightness})">{{ t('novel.followSystem') }}</Button></div>
    <div class="novel-preference-row"><span>{{ t('novel.textSize') }}</span><div class="novel-font-stepper"><Button :disabled="reading.preferences.size<=16" :aria-label="t('novel.smallerText')" @click="reading.setPreferences({size:reading.preferences.size-1})">A−</Button><output>{{ reading.preferences.size }}</output><Button :disabled="reading.preferences.size>=28" :aria-label="t('novel.largerText')" @click="reading.setPreferences({size:reading.preferences.size+1})">A+</Button></div></div>
    <div class="novel-preference-row"><span>{{ t('novel.readingTheme') }}</span><div class="novel-reader-colors"><Button v-for="theme in themes" :key="theme" :class="`novel-paper-${theme}`" :aria-pressed="reading.preferences.theme===theme" @click="reading.setPreferences({theme})">{{ t(`novel.themes.${theme}`) }}</Button></div></div>
    <div class="novel-preference-row"><span>{{ t('novel.turnMode') }}</span><div class="novel-font-options"><Button :aria-pressed="reading.preferences.flow==='paged'" @click="reading.setPreferences({flow:'paged'})">{{ t('novel.horizontalPages') }}</Button><Button :aria-pressed="reading.preferences.flow==='scroll'" @click="reading.setPreferences({flow:'scroll'})">{{ t('novel.verticalScroll') }}</Button></div></div>
    <details class="novel-preference-group" :open="preview || undefined"><summary>{{ t('novelUpgrade.typography') }}<Icon name="chevron-down" :size="18" /></summary>
    <div class="novel-preference-row"><span>{{ t('novel.typeface') }}</span><div class="novel-font-options"><Button :aria-pressed="reading.preferences.serif" @click="reading.setPreferences({serif:true})">{{ t('novel.serif') }}</Button><Button :aria-pressed="!reading.preferences.serif" @click="reading.setPreferences({serif:false})">{{ t('novel.sans') }}</Button></div></div>
    <div class="novel-preference-row"><span>{{ t('novel.lineSpacing') }}</span><div class="novel-font-options"><Button v-for="(n,i) in [1.6,1.85,2.1]" :key="n" :aria-pressed="reading.preferences.leading===n" @click="reading.setPreferences({leading:n})">{{ t(['novel.compact','novel.standard','novel.relaxed'][i]) }}</Button></div></div>
    <div class="novel-preference-row"><span>{{ t('novel.pageMargin') }}</span><div class="novel-font-options"><Button v-for="(n,i) in [16,24,32]" :key="n" :aria-pressed="reading.preferences.margin===n" @click="reading.setPreferences({margin:n})">{{ t(['novel.compact','novel.standard','novel.relaxed'][i]) }}</Button></div></div>
    </details>
    <details class="novel-preference-group" :open="preview || undefined"><summary>{{ t('novelUpgrade.input') }}<Icon name="chevron-down" :size="18" /></summary>
    <ReaderInputPreferences :value="reading.preferences" @change="reading.setPreferences"/>
    <p class="novel-reader-help">{{ t('novel.gestureHelp') }}</p>
    </details>
  </div>
</template>
