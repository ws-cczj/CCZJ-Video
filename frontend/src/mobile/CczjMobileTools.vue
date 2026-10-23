<script setup lang="ts">
import { tr } from '../locales'
import Icon from '../components/Icon.vue'
import { pluginNavItems } from '../plugins/registry'
const tools = [
  { path: '/sources', icon: 'source', label: 'sidebar.sources', description: 'mobile.sourcesDescription' },
  { path: '/danmaku-sources', icon: 'source', label: 'danmakuSources.title', description: 'danmakuSources.mobileDescription' },
  { path: '/downloads', icon: 'download', label: 'sidebar.downloads', description: 'mobile.downloadsDescription' },
  { path: '/video-types', icon: 'tag', label: 'sidebar.videoTypes', description: 'mobile.typesDescription' },
  { path: '/recycle', icon: 'trash', label: 'sidebar.recycle', description: 'mobile.recycleDescription' },
  { path: '/recent', icon: 'clock', label: 'sidebar.recent', description: 'mobile.recentDescription' },
]
</script>

<template>
  <div class="cczj-mobile-tools-section">
    <h2 class="cczj-mobile-section-title">{{ tr('mobile.commonTools') }}</h2>
    <div class="cczj-mobile-tools">
      <router-link v-for="tool in tools" :key="tool.path" :to="tool.path" class="cczj-mobile-tool">
        <span class="cczj-mobile-tool-icon"><Icon :name="tool.icon" :size="20" /></span>
        <div class="cczj-mobile-tool-copy"><strong>{{ tr(tool.label) }}</strong><span>{{ tr(tool.description) }}</span></div>
        <Icon name="chevron-right" :size="16" />
      </router-link>
    </div>
    <template v-if="pluginNavItems.length">
      <h2 class="cczj-mobile-section-title">{{ tr('settings.extensions') }}</h2>
      <div class="cczj-mobile-menu">
        <router-link v-for="item in pluginNavItems" :key="item.path" :to="item.path" class="cczj-mobile-menu-row"><Icon :name="item.icon" :size="21" /><span>{{ typeof item.label === 'function' ? item.label() : item.label }}</span><Icon name="chevron-right" :size="17" /></router-link>
      </div>
    </template>
  </div>
</template>
