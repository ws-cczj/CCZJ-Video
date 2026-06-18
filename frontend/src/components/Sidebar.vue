<script setup lang="ts">
import { useRouter, useRoute } from 'vue-router'
import { useSourceStore } from '../stores/source'
import { useDevMode } from '../stores/devMode'
import Icon from './Icon.vue'

const router = useRouter()
const route = useRoute()
const sourceStore = useSourceStore()
const devMode = useDevMode()

interface NavItem {
  path: string
  label: string
  icon: string
}

const navItems: NavItem[] = [
  { path: '/', label: '首页', icon: 'home' },
  { path: '/search', label: '搜索', icon: 'search' },
  { path: '/favorites', label: '收藏', icon: 'star' },
  { path: '/history', label: '历史', icon: 'clock' },
]

const toolItems: NavItem[] = [
  { path: '/sources', label: '采集源', icon: 'source' },
  { path: '/video-types', label: '视频类型', icon: 'tag' },
  { path: '/downloads', label: '下载', icon: 'download' },
  { path: '/settings', label: '设置', icon: 'settings' },
]

const devItem: NavItem = { path: '/dev-admin', label: '开发者模式', icon: 'code' }

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path.startsWith(path)
}
</script>

<template>
  <aside class="sidebar">
    <nav class="nav-section">
      <div class="section-label">
        <span class="line left"></span>
        <span class="label-text">浏览</span>
        <span class="line right"></span>
      </div>
      <button
        v-for="item in navItems"
        :key="item.path"
        :class="['nav-item', { active: isActive(item.path) }]"
        @click="router.push(item.path)"
      >
        <span class="nav-indicator"></span>
        <Icon :name="item.icon" :size="18" />
        <span class="nav-label">{{ item.label }}</span>
      </button>
    </nav>

    <nav class="nav-section">
      <div class="section-label">
        <span class="line left"></span>
        <span class="label-text">管理</span>
        <span class="line right"></span>
      </div>
      <button
        v-for="item in toolItems"
        :key="item.path"
        :class="['nav-item', { active: isActive(item.path) }]"
        @click="router.push(item.path)"
      >
        <span class="nav-indicator"></span>
        <Icon :name="item.icon" :size="18" />
        <span class="nav-label">{{ item.label }}</span>
      </button>
    </nav>

    <!-- 开发者模式栏目（密码解锁且手动开启后可见） -->
    <nav v-if="devMode.enabled" class="nav-section">
      <div class="section-label">
        <span class="line left"></span>
        <span class="label-text">开发者</span>
        <span class="line right"></span>
      </div>
      <button
        :class="['nav-item', { active: isActive(devItem.path) }]"
        @click="router.push(devItem.path)"
      >
        <span class="nav-indicator"></span>
        <Icon :name="devItem.icon" :size="18" />
        <span class="nav-label">{{ devItem.label }}</span>
      </button>
    </nav>

    <div class="sidebar-footer">
      <div class="source-info">
        <span class="source-dot" :class="{ online: sourceStore.currentSource }"></span>
        <span class="source-name" @click="devMode.clickSourceName">{{ sourceStore.currentSource?.name || '未选择来源' }}</span>
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
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--accent-alpha-20);
  transition: border-color 0.3s ease;
  -webkit-user-select: none;
  user-select: none;
  color: var(--text-primary);
  backdrop-filter: saturate(1.1) blur(6px);
  -webkit-backdrop-filter: saturate(1.1) blur(6px);
}

.nav-section {
  padding: 14px 10px 8px;
}

/* —— 分区标签：用左右线条夹住文字 —— */
.section-label {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 8px 8px 14px;
  gap: 10px;
}

.section-label .line {
  flex: 1;
  height: 1px;
  background: var(--accent-alpha-35);
}

.section-label .label-text {
  font-size: 14px;
  font-weight: 700;
  color: var(--accent);
  letter-spacing: 3px;
  white-space: nowrap;
}

/* —— 菜单项：字号放大、颜色用 text-primary 保证对比度 —— */
.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  margin: 3px 0;
  padding: 11px 14px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--text-primary);
  cursor: pointer;
  font-size: 15px;
  font-weight: 600;
  text-align: left;
  font-family: inherit;
  transition: background 0.15s ease, color 0.15s ease;
  position: relative;
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
  position: absolute;
  left: 0;
  top: 20%;
  bottom: 20%;
  width: 4px;
  background: var(--accent);
  border-radius: 0 3px 3px 0;
  opacity: 0;
  transform: scaleY(0);
  transition: all 0.18s ease;
}

/* —— 底部 —— */
.sidebar-footer {
  margin-top: auto;
  padding: 14px 18px;
  border-top: 1px solid var(--accent-alpha-20);
}

.source-info {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  color: var(--text-primary);
}

.source-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--text-muted);
  flex-shrink: 0;
  transition: all 0.2s ease;
}

.source-dot.online {
  background: var(--accent);
  box-shadow: 0 0 8px var(--accent-alpha-35);
}

.source-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
  font-weight: 500;
}
</style>
