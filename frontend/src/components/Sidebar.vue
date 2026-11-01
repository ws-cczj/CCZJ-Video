<script setup lang="ts">
import { useRouter, useRoute } from 'vue-router'
import { useSourceStore } from '../stores/source'
import { useI18n } from 'vue-i18n'
import { computed } from 'vue'
import Icon from './Icon.vue'
import { MotionList } from './ui'
import { videoNavigation } from '../modes/videoNavigation'

const router = useRouter()
const route = useRoute()
const sourceStore = useSourceStore()
const { t } = useI18n()

interface NavItem {
  path: string
  label: string
  icon: string
}

function localize(items: typeof videoNavigation.value.browse): NavItem[] {
  return items.map(item=>({path:item.path,icon:item.icon,label:item.labelKey?t(item.labelKey):typeof item.label==='function'?item.label():item.label||''}))
}
const navItems = computed(()=>localize(videoNavigation.value.browse))
const toolItems = computed(()=>localize(videoNavigation.value.management))

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}
</script>

<template>
  <aside class="sidebar cczj-flex cczj-flex-col cczj-select-none cczj-text-primary cczj-transition">
    <div class="sidebar-navigation cczj-flex-1 cczj-min-h-0 cczj-overflow-y-auto cczj-overflow-x-hidden">
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
      <!-- 这一区会随扩展包的启停增删条目。用 list 过渡包起来，新条目是淡入落位、
           后面的条目平滑让位，而不是整列瞬间弹一格。 -->
      <MotionList preset="list">
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
      </MotionList>
    </nav>

    </div>
    <div class="sidebar-footer cczj-mt-auto">
      <div class="source-info cczj-flex cczj-items-center cczj-gap-5 cczj-text-13 cczj-text-primary">
        <span class="source-dot cczj-flex-shrink-0 cczj-rounded-50 cczj-transition" :class="{ online: sourceStore.currentSource }"></span>
        <span class="source-name cczj-truncate cczj-text-secondary cczj-font-medium">{{ sourceStore.currentSource?.name || t('sidebar.noSource') }}</span>
      </div>
    </div>
  </aside>
</template>

<style scoped>
/* =================================================================
   桌面导航与固定来源状态
   - 背景透明（与主背景融合）
   - 右侧 1px 主题色淡化分割线
   - 分区标签用左右短线条夹住
   - hover/active：主题色淡背景 + 左侧竖条指示
   ================================================================= */
.sidebar {
  @apply cczj-min-h-0 cczj-overflow-hidden;
  @apply cczj-overflow-x-hidden cczj-flex-shrink-0;
  width: 200px;
  min-width: 200px;
  border-right: 1px solid var(--accent-alpha-20);
  backdrop-filter: saturate(1.1) blur(6px);
  -webkit-backdrop-filter: saturate(1.1) blur(6px);
}

.nav-section {
  @apply cczj-flex-shrink-0;
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
  @apply cczj-text-accent;
  letter-spacing: 3px;
  @apply cczj-whitespace-nowrap;
}

/* —— 菜单项：字号放大、颜色用 text-primary 保证对比度 —— */
.nav-item {
  transition: background-color var(--cczj-motion-fast), color var(--cczj-motion-fast);
  margin: 3px 0;
  padding: 11px 14px;
  border: none;
  font-size: calc(15rem / 14);
  font-family: inherit;
}

.nav-item .icon-svg { transition: transform var(--cczj-motion-normal) var(--cczj-motion-ease-standard); }
.nav-item:hover .icon-svg { transform: translateX(2px); }
.nav-item:active .icon-svg { transform: scale(.9); }
.nav-item:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.nav-item:hover {
  background: var(--accent-alpha-15);
  @apply cczj-text-accent;
}

.nav-item.active {
  background: var(--accent-alpha-20);
  @apply cczj-text-accent cczj-font-bold;
}

.nav-item.active .nav-indicator {
  @apply cczj-opacity-100;
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
  @apply cczj-flex-shrink-0;
  padding: 14px 18px;
  border-top: 1px solid var(--accent-alpha-20);
}

.source-dot {
  width: 6px;
  @apply cczj-h-1.5;
  background: var(--text-muted);
}

.source-dot.online {
  background: var(--accent);
  box-shadow: 0 0 8px var(--accent-alpha-35);
}
.sidebar-navigation { scrollbar-width: none; }
.sidebar-navigation::-webkit-scrollbar { @apply cczj-hidden; }
@media (max-height: 700px) {
  .nav-section { padding: 6px 10px 3px; }
  .section-label { padding: 4px 8px 7px; }
  .nav-item { margin: 1px 0; padding: 7px 12px; line-height: 1.4; }
  .sidebar-footer { padding: 8px 18px; }
}
</style>
