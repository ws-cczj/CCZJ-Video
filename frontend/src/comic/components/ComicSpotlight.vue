<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ComicSeries } from '../domain'
import ComicCover from './ComicCover.vue'
import Icon from '../../components/Icon.vue'

const props = defineProps<{ series: ComicSeries[] }>()
const { t } = useI18n(), selected = ref(0)
const current = computed(() => props.series[selected.value])
watch(() => props.series.map(item => `${item.sourceKey}:${item.remoteId}`).join('|'), () => { selected.value = 0 })
function shuffle(): void {
  if (props.series.length > 1) selected.value = (selected.value + 1 + Math.floor(Math.random() * (props.series.length - 1))) % props.series.length
}
</script>

<template>
  <section v-if="current" class="manga-spotlight" :aria-label="t('comicRoom.spotlight')">
    <div class="manga-spotlight-copy">
      <span class="manga-kicker"><i aria-hidden="true"></i>{{ t('comicRoom.spotlight') }}<span aria-hidden="true"> / {{ String(selected + 1).padStart(2, '0') }}</span></span>
      <div class="manga-spotlight-description" aria-live="polite" aria-atomic="true">
        <small>{{ current.sourceName }}</small><h2>{{ current.title }}</h2>
        <p v-if="current.summary">{{ current.summary }}</p>
        <router-link class="manga-open" :to="`/comic/series/${current.id}`">{{ t('comicRoom.open') }}<Icon name="chevron-right" :size="19" /></router-link>
      </div>
      <button v-if="series.length > 1" class="manga-shuffle" @click="shuffle"><Icon name="refresh" :size="15" />{{ t('comicRoom.shuffle') }}</button>
    </div>
    <div class="manga-spotlight-stage">
      <span class="manga-stage-word" aria-hidden="true">COMIC!</span><span class="manga-stage-star" aria-hidden="true">✦</span>
      <router-link :key="`${current.sourceKey}:${current.remoteId}`" :to="`/comic/series/${current.id}`" class="manga-spotlight-cover" :aria-label="current.title"><ComicCover :src="current.cover" :title="current.title" /></router-link>
      <div class="manga-contact-sheet" :aria-label="t('comicRoom.chooseCover')"><button v-for="(item, index) in series" :key="`${item.sourceKey}:${item.remoteId}`" :aria-label="t('comicRoom.featureNamed', { name: item.title })" :aria-pressed="selected === index" @click="selected = index"><ComicCover :src="item.cover" :title="item.title" /></button></div>
    </div>
  </section>
  <div v-else class="manga-spotlight manga-spotlight-skeleton" aria-hidden="true"><span></span><span></span></div>
</template>
