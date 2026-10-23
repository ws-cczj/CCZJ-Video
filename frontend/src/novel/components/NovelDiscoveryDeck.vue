<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { NovelBook } from '../domain'
import NovelCover from './NovelCover.vue'
import Icon from '../../components/Icon.vue'

const props = defineProps<{ books: NovelBook[] }>()
const { t } = useI18n(), current = ref(0)
const book = computed(() => props.books[current.value])
const nextBook = computed(() => props.books[(current.value + 1) % props.books.length])
const position = (index: number) => (index - current.value + props.books.length) % props.books.length
watch(() => props.books.map(row => `${row.sourceKey}:${row.remoteId}`).join('|'), () => { current.value = 0 })
function next(): void { if (props.books.length > 1) current.value = (current.value + 1) % props.books.length }
</script>

<template>
  <aside class="room-discovery" :aria-label="t('novelRoom.pick')">
    <header class="room-discovery-heading"><span class="room-pick-label"><span class="room-paper-mark" aria-hidden="true"><i></i><i></i><i></i></span>{{ t('novelRoom.pick') }}</span><span v-if="book" class="room-deck-position" aria-hidden="true">{{ String(current + 1).padStart(2, '0') }}<i>/</i>{{ String(books.length).padStart(2, '0') }}</span></header>
    <div v-if="book" class="room-discovery-content">
      <div class="room-book-stack">
        <template v-for="(candidate, index) in books" :key="`${candidate.sourceKey}:${candidate.remoteId}`"><span v-if="position(index) >= 2" class="room-stack-cover" :data-position="position(index)" aria-hidden="true"><NovelCover :book="candidate" /></span></template>
        <button v-if="nextBook && books.length > 1" class="room-stack-cover room-next-leaf" data-position="1" :aria-label="t('novelRoom.swap')" :title="t('novelRoom.swap')" @click="next"><NovelCover :book="nextBook" /></button>
        <router-link :to="`/novel/book/${book.id}`" class="room-stack-cover room-current-cover" data-position="0" :aria-label="book.title" :tabindex="0"><NovelCover :key="`${book.sourceKey}:${book.remoteId}`" class="room-sheet-cover" :book="book" /></router-link>
      </div>
      <div class="room-discovery-info" aria-live="polite" aria-atomic="true">
        <Transition name="room-pick" mode="out-in"><router-link :key="`${book.sourceKey}:${book.remoteId}`" :to="`/novel/book/${book.id}`" class="room-desk-book"><small>{{ t('novelRoom.publication') }}</small><h2>{{ book.title }}</h2><p>{{ book.author }}</p><span class="room-open">{{ t('novelRoom.openBook') }}<Icon name="chevron-right" :size="17" /></span></router-link></Transition>
      </div>
    </div>
    <div v-else class="room-discovery-loading" aria-hidden="true"><span></span><span></span></div>
  </aside>
</template>
