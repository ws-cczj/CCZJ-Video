import { computed } from 'vue'
import { pluginNavItems } from '../plugins/registry'

interface VideoNavigationItem { path: string; icon: string; labelKey?: string; label?: string | (() => string) }
const browse: readonly VideoNavigationItem[] = [
  { path:'/',labelKey:'sidebar.home',icon:'home' },
  { path:'/short-drama',labelKey:'shortDrama.title',icon:'clapperboard' },
  { path:'/search',labelKey:'sidebar.search',icon:'search' },
  { path:'/recent',labelKey:'sidebar.recent',icon:'clock' },
  { path:'/favorites',labelKey:'sidebar.favorites',icon:'star' },
  { path:'/history',labelKey:'sidebar.history',icon:'history' },
]
const management: readonly VideoNavigationItem[] = [
  { path:'/sources',labelKey:'sidebar.sources',icon:'source' },
  { path:'/danmaku-sources',labelKey:'danmakuSources.title',icon:'danmaku' },
  { path:'/video-types',labelKey:'sidebar.videoTypes',icon:'tag' },
  { path:'/downloads',labelKey:'sidebar.downloads',icon:'download' },
  { path:'/recycle',labelKey:'sidebar.recycle',icon:'trash' },
]

/** Sidebar order also defines spatial route direction, including injected tools. */
export const videoNavigation = computed(() => ({ browse, management: [
  ...management,
  ...pluginNavItems.value.map(item => ({path:item.path,icon:item.icon,label:item.label})),
  {path:'/settings',labelKey:'sidebar.settings',icon:'settings'},
] as readonly VideoNavigationItem[] }))
export const videoNavigationPaths = computed(() => [...videoNavigation.value.browse,...videoNavigation.value.management].map(item=>item.path))
