<script setup lang="ts">
import { useRoute } from 'vue-router'
import { tr } from '../locales'
import Icon from '../components/Icon.vue'
const route = useRoute()
const props=defineProps<{path?:string}>()
const items = [
  { path: '/', icon: 'home', label: 'sidebar.home' },
  { path: '/short-drama', icon: 'film', label: 'shortDrama.navTitle' },
  { path: '/search', icon: 'search', label: 'sidebar.search' },
  { path: '/favorites', icon: 'star', label: 'sidebar.favorites' },
  { path: '/settings', icon: 'settings', label: 'sidebar.settings' },
]
function active(path: string): boolean {
  const current=props.path||route.path
  if (path === '/') return current === '/' || current.startsWith('/detail/')
  return path === '/settings'
    ? ['/sources', '/danmaku-sources', '/settings', '/downloads', '/video-types', '/recycle', '/recent'].includes(current)
    : current === path
}
</script>

<template>
  <nav class="cczj-mobile-navigation" :aria-label="tr('sidebar.browse')">
    <router-link v-for="item in items" :key="item.path" :to="item.path" :class="{ 'cczj-mobile-navigation-active': active(item.path) }" :aria-current="active(item.path) ? 'page' : undefined">
      <span class="cczj-mobile-navigation-icon"><Icon :name="item.icon" :size="21" /></span><span>{{ tr(item.label) }}</span>
    </router-link>
  </nav>
</template>
