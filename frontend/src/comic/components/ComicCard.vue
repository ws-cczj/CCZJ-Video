<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ComicCover from './ComicCover.vue'
import type { ComicSeries } from '../domain'

const props = defineProps<{ series: ComicSeries }>()
const { t } = useI18n()
const meta = computed(() => [props.series.author, props.series.latest].filter(Boolean).join(' · ') || props.series.sourceName)
const badge = computed(() => props.series.available ? props.series.status : t('comic.source.unavailable'))
</script>
<template>
  <router-link :to="`/comic/series/${series.id}`" class="comic-card">
    <div class="comic-cover">
      <ComicCover :src="series.cover" :title="series.title" />
      <span v-if="badge">{{ badge }}</span>
    </div>
    <h3>{{ series.title }}</h3>
    <p>{{ meta }}</p>
  </router-link>
</template>
