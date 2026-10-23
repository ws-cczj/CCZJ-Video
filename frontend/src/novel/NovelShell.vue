<script setup lang="ts">
import { useRouter } from 'vue-router'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { isMobileRuntime } from '../api/runtime'
import Icon from '../components/Icon.vue'
import BackToTop from '../components/BackToTop.vue'
import MobileRouteStage from '../mobile/MobileRouteStage.vue'
import { useNovelReading } from './readingState'
import { lastBookcityChannel } from './browseLocation'

const router = useRouter(), { t } = useI18n(), reading = useNovelReading()
const navigation: { path: string; label: string; icon: string; aliases: string[] }[] = [
  { path: '/novel', label: 'novelRoom.bookcity', icon: 'book-open', aliases: [] },
  { path: '/novel/shelf', label: 'novel.shelf', icon: 'bookmark', aliases: [] },
  { path: '/novel/settings', label: 'sidebar.settings', icon: 'settings', aliases: [] },
]
const selected = (path: string, item: typeof navigation[number]) => path === item.path || item.aliases.includes(path)
const primary = (path: string) => navigation.some(item => selected(path, item))
const bookcityLocation = computed(() => ({ path: '/novel', query: lastBookcityChannel.value ? { shelf: lastBookcityChannel.value } : {} }))
const destination = (path: string) => path === '/novel' ? bookcityLocation.value : path
function back(): void {
  if (String(router.options.history.state.back || '').startsWith('/novel')) router.back()
  else void router.replace('/novel')
}
</script>

<template>
  <div class="cczj-novel-shell reading-shell library-room" :class="{ 'novel-native': isMobileRuntime() }">
    <MobileRouteStage :mobile="isMobileRuntime()" :cache="['NovelHome', 'NovelSearch']">
      <template #default="{ Component, route: shownRoute }">
        <header v-if="!shownRoute.path.startsWith('/novel/read/')" class="reading-header">
          <router-link v-if="primary(shownRoute.path)" class="reading-brand" :to="bookcityLocation"><span class="reading-brand-icon"><Icon name="book-open" :size="22" /></span><strong>CCZJ<span>{{ t('readingUI.novels') }}</span></strong></router-link>
          <template v-else><button class="reading-icon-button" :aria-label="t('common.back')" @click="back"><Icon name="chevron-left" :size="22" /></button><span class="reading-header-title">{{ t(String(shownRoute.meta.title || 'novel.details')) }}</span></template>
          <nav class="reading-desktop-nav" :aria-label="t('novel.navigation')"><router-link v-for="item in navigation" :key="item.path" :to="destination(item.path)" :aria-current="selected(shownRoute.path, item) ? 'page' : undefined"><Icon :name="item.icon" :size="18" />{{ t(item.label) }}</router-link></nav>
          <router-link class="reading-icon-button reading-source-link" to="/novel/search" :aria-label="t('readingUI.findBooks')"><Icon name="search" :size="20" /></router-link>
        </header>
        <p v-if="reading.storageError" class="novel-storage-error" role="alert">{{ t('novel.storageError') }} <button class="novel-text-button" @click="reading.retry()">{{ t('common.retry') }}</button></p>
        <main class="novel-main main-content" :class="{ 'novel-reader-content-host': shownRoute.path.startsWith('/novel/read/') }"><component v-if="reading.ready" :is="Component" /></main>
      </template>
      <template #navigation="{ route: shownRoute }"><nav v-if="primary(shownRoute.path)" class="reading-bottom-nav" :aria-label="t('novel.navigation')"><router-link v-for="item in navigation" :key="item.path" :to="destination(item.path)" :aria-current="selected(shownRoute.path, item) ? 'page' : undefined"><Icon :name="item.icon" :size="22" /><span>{{ t(item.label) }}</span></router-link></nav></template>
    </MobileRouteStage>
    <BackToTop compact v-if="!router.currentRoute.value.path.startsWith('/novel/read/')" />
  </div>
</template>

<style src="./styles/novel.css"></style>
<style src="../reading/library.css"></style>
<style src="./styles/library-room.css"></style>
