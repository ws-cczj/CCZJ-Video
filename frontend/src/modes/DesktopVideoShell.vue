<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { tr } from '../locales'
import Icon from '../components/Icon.vue'
import { useDownloadStore } from '../stores/download'
import Sidebar from '../components/Sidebar.vue'
import MobileRouteStage from '../mobile/MobileRouteStage.vue'
import { shortDramaScene } from '../player/shortDrama/scene'
import { videoNavigationPaths } from './videoNavigation'
const cached = ['Home','Search','ShortDrama','Sources','Favorites','History','Recent','Downloads','Settings']
const dl = useDownloadStore(), router = useRouter()
const route=useRoute()
const shortDramaPlayback=computed(()=>shortDramaScene(route,false)==='playback')
// 下载悬浮球显示逻辑：任务<1 时不显示；任务>=1 且鼠标靠近右下角时显示
const fabVisible = ref(false)
function handleFabMouseMove(e: MouseEvent): void {
  if (dl.activeCount < 1) {
    fabVisible.value = false
    return
  }
  const proximity = 140 // 鼠标靠近的判定半径（像素）
  const w = window.innerWidth
  const h = window.innerHeight
  const dx = w - e.clientX
  const dy = h - e.clientY
  const dist = Math.sqrt(dx * dx + dy * dy)
  fabVisible.value = dist <= proximity
}
const hoverFab = ref(false)
function onFabEnter(): void {
  hoverFab.value = true
  fabVisible.value = true
}
function onFabLeave(): void {
  hoverFab.value = false
}

watch(() => dl.activeCount > 0, (active, _previous, cleanup) => {
  if (!active) { fabVisible.value=false;hoverFab.value=false;return }
  window.addEventListener('mousemove',handleFabMouseMove)
  cleanup(() => window.removeEventListener('mousemove',handleFabMouseMove))
}, { immediate:true })
</script>
<template>
  <div class="app-body">
    <Sidebar v-if="!shortDramaPlayback" />
    <MobileRouteStage class="desktop-video-stage" :mobile="false" :cache="cached" :sections="videoNavigationPaths" wallpaper>
      <template #default="{ Component, route: shownRoute }">
        <main class="main-content" :class="{ 'short-drama-host': shownRoute.path === '/short-drama' }">
          <component :is="Component" :class="{ 'cczj-h-full': shownRoute.path === '/short-drama' }" />
        </main>
      </template>
    </MobileRouteStage>
  </div>
    <!-- 右下角下载悬浮球 -->
    <button
      v-if="dl.activeCount > 0"
      class="download-fab"
      :class="{ 'fab-visible': fabVisible || hoverFab }"
      @focus="fabVisible = true"
      @blur="fabVisible = false"
      @click="router.push('/downloads')"
      @mouseenter="onFabEnter"
      @mouseleave="onFabLeave"
      :title="tr('downloads.title')"
    >
      <Icon name="download" :size="18" />
      <span v-if="dl.activeCount > 0" class="fab-badge">{{ dl.activeCount }}</span>
    </button>

</template>

<style scoped>
.main-content.short-drama-host { padding: 0; @apply cczj-overflow-hidden; }
</style>
