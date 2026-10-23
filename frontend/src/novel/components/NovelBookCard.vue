<script setup lang="ts">
import { computed } from 'vue'
import { splitGenres } from '../data/novelLibrary'
import type { NovelBook } from '../domain'
import NovelCover from './NovelCover.vue'
const props = defineProps<{ book: NovelBook; rank?: number; compact?: boolean }>()
const meta = computed(() => splitGenres(props.book.genres).slice(0, 2).join(' · ') || props.book.sourceName)
</script>
<template>
  <router-link :to="`/novel/book/${book.id}`" class="novel-book-card" :class="{ 'novel-book-row': compact }">
    <div class="novel-book-art"><NovelCover :book="book" /><span v-if="rank" class="novel-rank">{{ String(rank).padStart(2,'0') }}</span></div>
    <div class="novel-book-copy"><h3>{{ book.title }}</h3><p>{{ book.author || book.sourceName }}</p><span v-if="compact && book.summary" class="novel-book-summary">{{ book.summary }}</span><small>{{ meta }}<span v-if="compact && book.status"> · {{ book.status }}</span></small></div>
  </router-link>
</template>
