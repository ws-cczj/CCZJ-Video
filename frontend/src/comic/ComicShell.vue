<script setup lang="ts">
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import MobileRouteStage from '../mobile/MobileRouteStage.vue'
import { useComicState } from './state'
import { isMobileRuntime } from '../api/runtime'

const router = useRouter(), { t } = useI18n(), state = useComicState()
const navigation = [
  { path: '/comic', label: 'comicRoom.browse', icon: 'panels', aliases: [] },
  { path: '/comic/library', label: 'comicRoom.following', icon: 'bookmark', aliases: [] },
  { path: '/comic/settings', label: 'sidebar.settings', icon: 'settings', aliases: ['/comic/me'] },
]
const selected = (path: string, item: typeof navigation[number]) => path === item.path || item.aliases.includes(path)
const primary = (path: string) => navigation.some(item => selected(path, item))
function back(): void {
  if (String(router.options.history.state.back || '').startsWith('/comic')) router.back()
  else void router.replace('/comic')
}
</script>

<template>
  <div class="cczj-comic-shell reading-shell manga-room" :class="{ 'comic-native': isMobileRuntime() }">
    <MobileRouteStage :mobile="isMobileRuntime()" :cache="['ComicHome','ComicCatalog']">
      <template #default="{ Component, route: shownRoute }">
        <header v-if="!shownRoute.path.startsWith('/comic/read/')" class="reading-header">
          <router-link v-if="primary(shownRoute.path)" to="/comic" class="reading-brand"><span class="reading-brand-icon"><Icon name="panels" :size="22" /></span><strong>CCZJ<span>{{ t('readingUI.comics') }}</span></strong></router-link>
          <template v-else><button class="reading-icon-button" :aria-label="t('common.back')" @click="back"><Icon name="chevron-left" :size="22" /></button><span class="reading-header-title">{{ t(String(shownRoute.meta.title || 'comic.details')) }}</span></template>
          <nav class="reading-desktop-nav" :aria-label="t('comic.navigation')"><router-link v-for="item in navigation" :key="item.path" :to="item.path" :aria-current="selected(shownRoute.path, item) ? 'page' : undefined"><Icon :name="item.icon" :size="18" />{{ t(item.label) }}</router-link></nav>
          <router-link to="/comic/search" class="reading-icon-button reading-source-link" :aria-label="t('readingUI.findComics')"><Icon name="search" :size="20" /></router-link>
        </header>
        <p v-if="state.error" class="comic-error" role="alert">{{ t('comic.storageError') }} <button class="comic-text-button" @click="state.retry()">{{ t('common.retry') }}</button></p>
        <main class="comic-main main-content" :class="{ 'comic-reader-content-host': shownRoute.path.startsWith('/comic/read/') }"><component v-if="state.ready" :is="Component" /></main>
      </template>
      <template #navigation="{ route: shownRoute }"><nav v-if="primary(shownRoute.path)" class="reading-bottom-nav" :aria-label="t('comic.navigation')"><router-link v-for="item in navigation" :key="item.path" :to="item.path" :aria-current="selected(shownRoute.path, item) ? 'page' : undefined"><Icon :name="item.icon" :size="22" /><span>{{ t(item.label) }}</span></router-link></nav></template>
    </MobileRouteStage>
  </div>
</template>

<style src="./styles/comic.css"></style>
<style src="../reading/library.css"></style>
<style src="./styles/manga-room.css"></style>
