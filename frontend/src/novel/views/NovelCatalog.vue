<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useCatalog } from '../../reading/useCatalog'
import NovelBookCard from '../components/NovelBookCard.vue'
const { t } = useI18n(), route = useRoute(), router = useRouter()
const genre = computed(() => typeof route.query.genre === 'string' ? route.query.genre : 'all')
const complete = computed(() => route.query.status === 'complete')
const library = useCatalog('novel', () => ({ genre: genre.value === 'all' ? '' : genre.value, complete: complete.value }))
const genres = library.genres
const books = library.items
function filter(key: string, value: string): void { void router.replace({ path: '/novel/categories', query: { ...route.query, [key]: value || undefined } }) }
// 重置要一次导航清掉全部筛选：连着调两次 filter 会 replace 两次，
// 第二次读到的是第一次之前的 route.query，等于只清掉了一个条件。
function resetFilters(): void { void router.replace({ path: '/novel/categories' }) }
</script>
<template>
  <div class="novel-page"><div class="novel-section-heading"><h1>{{ t('novel.chooseStory') }}</h1><span>{{ t('common.loadedCount',{count:books.length}) }}</span></div>
    <div v-if="library.status.value === 'loading' || library.status.value === 'idle'" class="novel-empty"><p>{{ t('common.loading') }}</p></div>
    <div v-else-if="library.status.value === 'error'" class="novel-empty"><h2>{{ library.error.value?.message || t('common.unknownError') }}</h2><button class="novel-button" @click="library.retry()">{{ t('common.retry') }}</button></div>
    <div v-else-if="library.status.value === 'empty'" class="novel-empty"><h2>{{ t('novel.noBooks') }}</h2><p>{{ t('novel.catalogEmptyHint') }}</p><div class="novel-empty-actions"><button v-if="genre !== 'all' || complete" class="novel-button" @click="resetFilters">{{ t('novel.resetFilters') }}</button><router-link class="novel-button novel-button-primary" to="/novel/search">{{ t('novel.search') }}</router-link><router-link class="novel-button" to="/novel/sources">{{ t('novel.manageSources') }}</router-link></div></div>
    <div v-else class="novel-filter-layout"><nav class="novel-genres" :aria-label="t('novel.categories')"><button v-for="id in (genres.length ? ['all',...genres] : ['all'])" :key="id" :aria-pressed="genre === id" @click="filter('genre',id)">{{ id === 'all' ? t('novel.genres.all') : id }}</button></nav>
    <div class="novel-catalog-results"><div class="novel-filter-options"><button :aria-pressed="!complete" @click="filter('status','')">{{ t('common.all') }}</button><button :aria-pressed="complete" @click="filter('status','complete')">{{ t('novel.complete') }}</button></div>
      <section v-if="books.length" class="novel-row-grid"><NovelBookCard v-for="book in books" :key="book.id" :book="book" compact /></section>
      <div v-else class="novel-empty"><h2>{{ t('novel.noBooks') }}</h2><button class="novel-button" @click="resetFilters">{{ t('novel.resetFilters') }}</button></div>
      <p v-if="library.nextError.value" role="alert">{{ library.nextError.value.message }}</p>
      <button v-if="library.hasNext.value" class="novel-button" :disabled="library.loading.value" @click="library.more()">{{ t(library.loading.value ? 'common.loading' : 'common.loadMore') }}</button>
    </div></div>
  </div>
</template>
