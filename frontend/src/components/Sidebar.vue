<script setup lang="ts">
import { useRouter, useRoute } from 'vue-router'
import { useSourceStore } from '../stores/source'
import { useDevMode } from '../stores/devMode'
import { useI18n } from 'vue-i18n'
import { computed } from 'vue'
import Icon from './Icon.vue'

const router = useRouter()
const route = useRoute()
const sourceStore = useSourceStore()
const devMode = useDevMode()
const { t } = useI18n()

interface NavItem {
  path: string
  label: string
  icon: string
}

const navItems = computed<NavItem[]>(() => [
  { path: '/', label: t('sidebar.home'), icon: 'home' },
  { path: '/search', label: t('sidebar.search'), icon: 'search' },
  { path: '/favorites', label: t('sidebar.favorites'), icon: 'star' },
  { path: '/history', label: t('sidebar.history'), icon: 'clock' },
])

const toolItems = computed<NavItem[]>(() => [
  { path: '/sources', label: t('sidebar.sources'), icon: 'source' },
  { path: '/video-types', label: t('sidebar.videoTypes'), icon: 'tag' },
  { path: '/downloads', label: t('sidebar.downloads'), icon: 'download' },
  { path: '/settings', label: t('sidebar.settings'), icon: 'settings' },
])

const devItem = computed<NavItem>(() => ({ path: '/dev-admin', label: t('sidebar.devMode'), icon: 'code' }))

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}
</script>

<template>
  <aside class="sidebar cczj-flex cczj-flex-col cczj-select-none cczj-text-primary cczj-transition">
    <nav class="nav-section">
      <div class="section-label cczj-flex cczj-items-center cczj-justify-center cczj-gap-5">
        <span class="line left cczj-flex-1"></span>
        <span class="label-text cczj-text-sm cczj-font-bold">{{ t('sidebar.browse') }}</span>
        <span class="line right cczj-flex-1"></span>
      </div>
      <button
        v-for="item in navItems"
        :key="item.path"
        class="cczj-flex cczj-items-center cczj-gap-6 cczj-w-full cczj-rounded-lg cczj-bg-transparent cczj-text-primary cczj-cursor-pointer cczj-font-semibold cczj-text-left cczj-transition-fast cczj-relative"
        :class="['nav-item', { active: isActive(item.path) }]"
        @click="router.push(item.path)"
      >
        <span class="nav-indicator cczj-absolute cczj-left-0 cczj-opacity-0 cczj-transition-fast"></span>
        <Icon :name="item.icon" :size="18" />
        <span class="nav-label">{{ item.label }}</span>
      </button>
    </nav>

    <nav class="nav-section">
      <div class="section-label cczj-flex cczj-items-center cczj-justify-center cczj-gap-5">
        <span class="line left cczj-flex-1"></span>
        <span class="label-text cczj-text-sm cczj-font-bold">{{ t('sidebar.manage') }}</span>
        <span class="line right cczj-flex-1"></span>
      </div>
      <button
        v-for="item in toolItems"
        :key="item.path"
        class="cczj-flex cczj-items-center cczj-gap-6 cczj-w-full cczj-rounded-lg cczj-bg-transparent cczj-text-primary cczj-cursor-pointer cczj-font-semibold cczj-text-left cczj-transition-fast cczj-relative"
        :class="['nav-item', { active: isActive(item.path) }]"
        @click="router.push(item.path)"
      >
        <span class="nav-indicator cczj-absolute cczj-left-0 cczj-opacity-0 cczj-transition-fast"></span>
        <Icon :name="item.icon" :size="18" />
        <span class="nav-label">{{ item.label }}</span>
      </button>
    </nav>

    <!-- 开发者模式栏目（密码解锁且手动开启后可见） -->
    <nav v-if="devMode.enabled" class="nav-section">
      <div class="section-label cczj-flex cczj-items-center cczj-justify-center cczj-gap-5">
        <span class="line left cczj-flex-1"></span>
        <span class="label-text cczj-text-sm cczj-font-bold">{{ t('sidebar.developer') }}</span>
        <span class="line right cczj-flex-1"></span>
      </div>
      <button
        class="cczj-flex cczj-items-center cczj-gap-6 cczj-w-full cczj-rounded-lg cczj-bg-transparent cczj-text-primary cczj-cursor-pointer cczj-font-semibold cczj-text-left cczj-transition-fast cczj-relative"
        :class="['nav-item', { active: isActive(devItem.path) }]"
        @click="router.push(devItem.path)"
      >
        <span class="nav-indicator cczj-absolute cczj-left-0 cczj-opacity-0 cczj-transition-fast"></span>
        <Icon :name="devItem.icon" :size="18" />
        <span class="nav-label">{{ devItem.label }}</span>
      </button>
    </nav>

    <div class="sidebar-footer cczj-mt-auto">
      <div class="source-info cczj-flex cczj-items-center cczj-gap-5 cczj-text-13 cczj-text-primary">
        <span class="source-dot cczj-flex-shrink-0 cczj-rounded-50 cczj-transition" :class="{ online: sourceStore.currentSource }"></span>
        <span class="source-name cczj-truncate cczj-text-secondary cczj-font-medium" @click="devMode.clickSourceName">{{ sourceStore.currentSource?.name || t('sidebar.noSource') }}</span>
      </div>
    </div>
  </aside>
</template>

<style scoped>
/* =================================================================
   Linear/Notion 风格侧边栏
   - 背景透明（与主背景融合）
   - 右侧 1px 主题色淡化分割线
   - 分区标签用左右短线条夹住
   - hover/active：主题色淡背景 + 左侧竖条指示
   ================================================================= */
.sidebar {
  width: 200px;
  min-width: 200px;
  border-right: 1px solid var(--accent-alpha-20);
  backdrop-filter: saturate(1.1) blur(6px);
  -webkit-backdrop-filter: saturate(1.1) blur(6px);
}

.nav-section {
  padding: 14px 10px 8px;
}

/* —— 分区标签：用左右线条夹住文字 —— */
.section-label {
  padding: 8px 8px 14px;
}

.section-label .line {
  height: 1px;
  background: var(--accent-alpha-35);
}

.section-label .label-text {
  color: var(--accent);
  letter-spacing: 3px;
  white-space: nowrap;
}

/* —— 菜单项：字号放大、颜色用 text-primary 保证对比度 —— */
.nav-item {
  margin: 3px 0;
  padding: 11px 14px;
  border: none;
  font-size: 15px;
  font-family: inherit;
}

.nav-item:hover {
  background: var(--accent-alpha-15);
  color: var(--accent);
}

.nav-item.active {
  background: var(--accent-alpha-20);
  color: var(--accent);
  font-weight: 700;
}

.nav-item.active .nav-indicator {
  opacity: 1;
  transform: scaleY(1);
}

.nav-indicator {
  top: 20%;
  bottom: 20%;
  width: 4px;
  background: var(--accent);
  border-radius: 0 3px 3px 0;
  transform: scaleY(0);
}

/* —— 底部 —— */
.sidebar-footer {
  padding: 14px 18px;
  border-top: 1px solid var(--accent-alpha-20);
}

.source-dot {
  width: 6px;
  height: 6px;
  background: var(--text-muted);
}

.source-dot.online {
  background: var(--accent);
  box-shadow: 0 0 8px var(--accent-alpha-35);
}
</style>