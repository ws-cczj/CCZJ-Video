<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter, type RouteLocationNormalizedLoaded } from 'vue-router'
import { tr } from '../locales'
import Icon from '../components/Icon.vue'
import MobileRouteStage from './MobileRouteStage.vue'
import CczjMobileNavigation from './CczjMobileNavigation.vue'
import { pluginSettingsTabs } from '../plugins/registry'
import { useThemeStore } from '../stores/theme'
import '../player/shortDrama/presentation.css'
import { shortDramaScene } from '../player/shortDrama/scene'

const route = useRoute()
const router = useRouter()
const theme = useThemeStore()
const shortDramaPlayback = computed(() => shortDramaScene(route, true) === 'playback')
const themeStyle = computed(() => {
  if (shortDramaPlayback.value) return {
    '--cczj-mobile-canvas':'var(--cczj-drama-chrome)',
    '--cczj-mobile-surface':'var(--cczj-drama-chrome)',
    '--cczj-mobile-chrome':'var(--cczj-drama-chrome)',
    '--cczj-mobile-control':'var(--cczj-drama-control)',
    '--cczj-mobile-secondary':'var(--cczj-drama-chrome)',
    '--cczj-mobile-ink':'var(--cczj-drama-ink)',
    '--cczj-mobile-secondary-ink':'var(--cczj-drama-secondary)',
    '--cczj-mobile-muted':'var(--cczj-drama-muted)',
    '--cczj-mobile-line':'var(--cczj-drama-border)',
    '--cczj-mobile-wallpaper-veil':'transparent',
  }
  const current = theme.current
  const p = current.palette
  const dark = current.mode === 'dark'
  const picture = !!current.bgImage
  // Keep the desktop theme's input, action and text semantics. A wallpaper
  // needs a neutral reading surface, not the same tinted card at every layer.
  return { '--cczj-mobile-canvas': p.bgApp,
    '--cczj-mobile-surface': picture ? (dark ? 'rgba(24,27,32,.92)' : 'rgba(255,255,255,.91)') : p.bgCard,
    '--cczj-mobile-chrome': dark ? 'rgba(24,27,32,.8)' : 'rgba(255,255,255,.8)',
    '--cczj-mobile-control': p.bgInput,
    '--cczj-mobile-secondary': picture ? (dark ? '#242830' : '#f3f5f7') : p.bgSidebar,
    '--cczj-mobile-ink': p.textPrimary, '--cczj-mobile-secondary-ink': p.textSecondary,
    '--cczj-mobile-muted': p.textMuted,
    '--cczj-mobile-line': picture ? (dark ? 'rgba(255,255,255,.1)' : 'rgba(31,36,48,.1)') : p.border,
    '--cczj-mobile-wallpaper-veil': picture ? (dark ? 'rgba(10,13,18,.24)' : 'rgba(248,250,252,.3)') : 'transparent' }
})
const cached = ['Home', 'Search', 'Sources', 'Favorites', 'History', 'Recent', 'Downloads', 'Settings']
const primary = ['/', '/short-drama', '/search', '/favorites', '/settings']
function secondary(location:RouteLocationNormalizedLoaded):boolean { return !primary.includes(location.path) || (location.path==='/settings' && !!location.query.group) }
const titles: Record<string, string> = {
  '/': 'sidebar.home', '/search': 'sidebar.search', '/favorites': 'sidebar.favorites',
  '/history': 'history.title', '/manage': 'sidebar.manage', '/sources': 'sources.title',
  '/danmaku-sources': 'danmakuSources.title',
  '/settings': 'sidebar.settings', '/downloads': 'sidebar.downloads', '/recent': 'sidebar.recent',
  '/video-types': 'sidebar.videoTypes', '/recycle': 'sidebar.recycle',
}
function title(location:RouteLocationNormalizedLoaded):string {
  const group = typeof location.query.group === 'string' ? location.query.group : ''
  if (location.path === '/settings' && group) {
    const keys: Record<string,string> = { basic: 'settings.basic', theme: 'settings.theme', extensions: 'settings.extensions', advanced: 'advanced.title', about: 'settings.about' }
    if (keys[group]) return tr(keys[group])
    const tab = pluginSettingsTabs.value.find(item => item.id === group)
    if (tab) return typeof tab.label === 'function' ? tab.label() : tab.label
  }
  return tr(titles[location.path] || (location.path.startsWith('/detail/') ? 'common.details' : 'mobile.application'))
}
function back(): void {
  if (route.path === '/settings' && route.query.group) { if(router.options.history.state.back==='/settings')router.back();else void router.replace('/settings');return }
  if (typeof router.options.history.state.back === 'string') router.back()
  else void router.replace('/settings')
}
</script>

<template>
  <div class="cczj-mobile-shell" :class="{'cczj-short-drama-surface':shortDramaPlayback}" :style="themeStyle">
    <MobileRouteStage :mobile="true" :cache="cached">
    <template #default="{Component,route:shownRoute}">
      <header v-if="shownRoute.name!=='player' && secondary(shownRoute)" class="cczj-mobile-header">
        <button class="cczj-mobile-icon-button" :aria-label="tr('common.back')" @click="back"><Icon name="chevron-left" :size="22" /></button>
        <div class="cczj-mobile-heading"><h1>{{ title(shownRoute) }}</h1></div>
        <router-link v-if="shownRoute.path!=='/search' && shownRoute.path!=='/settings'" class="cczj-mobile-icon-button" to="/search" :aria-label="tr('sidebar.search')"><Icon name="search" :size="21" /></router-link>
        <router-link v-if="shownRoute.path!=='/settings'" class="cczj-mobile-icon-button" to="/settings" :aria-label="tr('sidebar.settings')"><Icon name="settings" :size="21" /></router-link>
      </header>
      <main class="cczj-mobile-content main-content" :class="{ 'cczj-mobile-page':shownRoute.name!=='player' && shownRoute.path!=='/short-drama','cczj-mobile-media-content':shownRoute.name==='player' || shownRoute.path==='/short-drama' }"><component :is="Component" /></main>
      </template>
    <template #navigation="{route:shownRoute}"><CczjMobileNavigation v-if="shownRoute.name!=='player' && !secondary(shownRoute)" :path="shownRoute.path" /></template>
    </MobileRouteStage>
  </div>
</template>
