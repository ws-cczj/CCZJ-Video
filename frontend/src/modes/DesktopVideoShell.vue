<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { tr } from '../locales'
import Icon from '../components/Icon.vue'
import { useDownloadStore } from '../stores/download'
import Sidebar from '../components/Sidebar.vue'
import { MotionTransition } from '../components/ui'
import { createDesktopRouteScenes } from './desktopRouteScene'
const scene = createDesktopRouteScenes()
const cached = ['Home','Search','Sources','Favorites','History','Recent','Downloads','Settings']
const dl = useDownloadStore(), router = useRouter()
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

onMounted(() => { window.addEventListener('mousemove',handleFabMouseMove) })
onBeforeUnmount(() => window.removeEventListener('mousemove',handleFabMouseMove))
</script>
<template>
  <div class="app-body">
    <Sidebar />
    <main class="main-content">
      <router-view v-slot="{ Component }">
        <MotionTransition preset="page" mode="out-in">
          <KeepAlive :include="cached"><component v-if="Component" :is="scene(Component)" :view="Component" :key="$route.name === 'player' ? `player:${String($route.params.sourceKey || '')}:${String($route.params.globalId || '')}` : $route.path" /></KeepAlive>
        </MotionTransition>
      </router-view>
    </main>
  </div>
    <!-- 右下角下载悬浮球 -->
    <button
      class="download-fab"
      :class="{ 'fab-visible': fabVisible || hoverFab }"
      @click="router.push('/downloads')"
      @mouseenter="onFabEnter"
      @mouseleave="onFabLeave"
      :title="tr('downloads.title')"
    >
      <Icon name="download" :size="18" />
      <span v-if="dl.activeCount > 0" class="fab-badge">{{ dl.activeCount }}</span>
    </button>

</template>
