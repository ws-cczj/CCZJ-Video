<script setup lang="ts">
import { useRoute } from 'vue-router'
import { tr } from '../locales'
import Icon from '../components/Icon.vue'
const route = useRoute()
const props=defineProps<{path?:string}>()
const items = [
  { path: '/', icon: 'home', label: 'sidebar.home' },
  { path: '/short-drama', icon: 'clapperboard', label: 'shortDrama.navTitle' },
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
    <router-link v-for="item in items" :key="item.path" :to="item.path" :class="{ 'cczj-mobile-navigation-active': active(item.path), 'cczj-mobile-navigation-featured': item.path === '/search' }" :aria-current="active(item.path) ? 'page' : undefined" :aria-label="tr(item.label)">
      <span class="cczj-mobile-navigation-icon"><Icon :name="item.icon" :size="item.path === '/search' ? 29 : 21" /></span>
      <span v-if="item.path !== '/search'">{{ tr(item.label) }}</span>
    </router-link>
  </nav>
</template>

<style scoped>
.cczj-mobile-navigation a { font-weight: 500; }
.cczj-mobile-navigation a.cczj-mobile-navigation-active { color: var(--accent-dim) !important; }
.cczj-mobile-navigation a .cczj-mobile-navigation-icon { @apply cczj-transition-none; }
.cczj-mobile-navigation a:active .cczj-mobile-navigation-icon { transform: none; }
.cczj-mobile-navigation-featured .cczj-mobile-navigation-icon {
  width: 44px;
  height: 44px;
  color: inherit;
}
.cczj-mobile-navigation-featured .cczj-mobile-navigation-icon :deep(svg) { color: inherit; }
</style>
