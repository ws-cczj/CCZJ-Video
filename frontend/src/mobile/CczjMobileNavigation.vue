<script setup lang="ts">
import { useRoute } from 'vue-router'
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { tr } from '../locales'
import Icon from '../components/Icon.vue'
const route = useRoute()
const props=defineProps<{path?:string}>()
const root=ref<HTMLElement>()
let observer:ResizeObserver|undefined
onMounted(()=>{
  observer=new ResizeObserver(entries=>{
    const height=entries[0]?.borderBoxSize?.[0]?.blockSize??root.value?.offsetHeight
    if(height&&root.value?.parentElement)root.value.parentElement.style.setProperty('--cczj-route-navigation-height',height+'px')
  })
  if(root.value)observer.observe(root.value)
})
onBeforeUnmount(()=>observer?.disconnect())
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
  <nav ref="root" class="cczj-mobile-navigation" :aria-label="tr('sidebar.browse')">
    <router-link v-for="item in items" :key="item.path" :to="item.path" :class="{ 'cczj-mobile-navigation-active': active(item.path), 'cczj-mobile-navigation-featured': item.path === '/search' }" :aria-current="active(item.path) ? 'page' : undefined" :aria-label="tr(item.label)">
      <span class="cczj-mobile-navigation-icon"><Icon :name="item.icon" :size="item.path === '/search' ? 18 : 16" /></span>
      <span v-if="item.path !== '/search'" class="cczj-mobile-navigation-label">{{ tr(item.label) }}</span>
    </router-link>
  </nav>
</template>

<style scoped>
/* A separate translucent surface preserves icon contrast and safe-area spacing. */
.cczj-mobile-navigation a { min-height:44px; gap:2px; font-weight:500; transition:color var(--cczj-motion-fast); }
.cczj-mobile-navigation :deep(svg) { transition:color var(--cczj-motion-fast); }
.cczj-mobile-navigation { @apply cczj-absolute cczj-left-0 cczj-right-0 cczj-bottom-0; z-index:2; isolation:isolate; padding:3px 8px calc(20px + env(safe-area-inset-bottom,0px)); background:transparent; backdrop-filter:none; -webkit-backdrop-filter:none; }
.cczj-mobile-navigation::before {
  content:''; @apply cczj-absolute; inset:0; z-index:-1;
  background:var(--cczj-mobile-surface);
  opacity:var(--cczj-navigation-veil-opacity,.7);
}
.cczj-mobile-navigation a.cczj-mobile-navigation-active { color: var(--accent-text,var(--accent-dim)) !important; }
.cczj-mobile-navigation a .cczj-mobile-navigation-icon { width:26px; height:24px; @apply cczj-transition-none; }
.cczj-mobile-navigation-label { @apply cczj-text-xs; line-height:12px; }
.cczj-mobile-navigation a:active .cczj-mobile-navigation-icon { transform: none; }
.cczj-mobile-navigation a.cczj-mobile-navigation-featured { min-height:44px; margin-top:-12px; align-self:start; }
.cczj-mobile-navigation a.cczj-mobile-navigation-featured .cczj-mobile-navigation-icon {
  width:40px; height:40px;
  border-radius:8px; background:var(--btn-solid); color:var(--btn-solid-text);
  transition:background-color var(--cczj-motion-fast),color var(--cczj-motion-fast);
  /* The link owns the touch target; its decorative icon does not intercept it. */
  @apply cczj-pointer-events-none;
}
.cczj-mobile-navigation-featured .cczj-mobile-navigation-icon :deep(svg) { color:inherit; }
</style>
