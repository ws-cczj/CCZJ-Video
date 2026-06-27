<script setup lang="ts">
defineOptions({ name: 'AdminPanel' })
import { useRoute, useRouter } from 'vue-router'
import Icon from '../../components/Icon.vue'
import { Button } from '../../components/ui'

const route = useRoute()
const router = useRouter()

// ========== 导航分组 ==========
interface NavItem {
  path: string
  label: string
  icon: string
}
interface NavGroup {
  label: string
  items: NavItem[]
}

const navGroups: NavGroup[] = [
  {
    label: '概览',
    items: [
      { path: '/dev-admin/dashboard', label: '仪表盘', icon: 'monitor' },
    ],
  },
  {
    label: '内容管理',
    items: [
      { path: '/dev-admin/sources', label: '采集源', icon: 'source' },
      { path: '/dev-admin/videos', label: '视频数据', icon: 'film' },
      { path: '/dev-admin/categories', label: '分类管理', icon: 'tag' },
    ],
  },
  {
    label: '自动化',
    items: [
      { path: '/dev-admin/scheduler', label: '采集调度器', icon: 'refresh' },
      { path: '/dev-admin/douban', label: '豆瓣数据', icon: 'globe' },
    ],
  },
  {
    label: '运维工具',
    items: [
      { path: '/dev-admin/downloads', label: '下载管理', icon: 'download' },
      { path: '/dev-admin/data', label: '数据导入导出', icon: 'database' },
      { path: '/dev-admin/logs', label: '系统日志', icon: 'code' },
    ],
  },
  {
    label: '系统',
    items: [
      { path: '/dev-admin/settings', label: '系统设置', icon: 'settings' },
    ],
  },
]

function isActive(path: string): boolean {
  return route.path === path || route.path.startsWith(path + '/')
}
</script>

<template>
  <div class="admin-shell">
    <!-- 顶部标题栏 -->
    <header class="admin-hd">
      <div class="admin-hd-left">
        <Icon name="code" :size="20" />
        <h1 class="admin-title">后台管理系统</h1>
      </div>
      <Button variant="secondary" size="sm" @click="router.push('/')">
        <Icon name="back" :size="14" /> 返回前台
      </Button>
    </header>

    <div class="admin-body">
      <!-- 左侧导航 -->
      <nav class="admin-nav">
        <div v-for="group in navGroups" :key="group.label" class="nav-group">
          <div class="nav-group-label">{{ group.label }}</div>
          <button
            v-for="item in group.items"
            :key="item.path"
            :class="['nav-link', { active: isActive(item.path) }]"
            @click="router.push(item.path)"
          >
            <span class="nav-indicator"></span>
            <Icon :name="item.icon" :size="15" />
            <span>{{ item.label }}</span>
          </button>
        </div>
      </nav>

      <!-- 右侧内容 -->
      <main class="admin-content">
        <router-view />
      </main>
    </div>
  </div>
</template>

<style>
/* ====================== 后台管理系统 — 全局共享样式 ====================== */

/* 布局 */
.admin-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.admin-hd {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 20px;
  height: 48px;
  min-height: 48px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-card);
}
.admin-hd-left {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--accent);
}
.admin-title {
  font-size: 15px;
  font-weight: 700;
  margin: 0;
  letter-spacing: 0.5px;
}
.admin-body {
  display: flex;
  flex: 1;
  min-height: 0;
}
.admin-nav {
  width: 180px;
  min-width: 180px;
  border-right: 1px solid var(--border);
  overflow-y: auto;
  padding: 8px 0;
  background: transparent;
}
.admin-content {
  flex: 1;
  overflow-y: auto;
  padding: 20px 24px 40px;
}

/* 导航分组 */
.nav-group {
  padding: 0 8px;
  margin-bottom: 4px;
}
.nav-group-label {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.8px;
  color: var(--text-muted);
  padding: 12px 10px 6px;
}
.nav-link {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  font-family: inherit;
  transition: all 0.15s ease;
  position: relative;
  margin: 1px 0;
}
.nav-link:hover {
  background: var(--accent-alpha-10);
  color: var(--accent);
}
.nav-link.active {
  background: var(--accent-alpha-15);
  color: var(--accent);
  font-weight: 600;
}
.nav-indicator {
  position: absolute;
  left: 0;
  top: 20%;
  bottom: 20%;
  width: 3px;
  background: var(--accent);
  border-radius: 0 3px 3px 0;
  opacity: 0;
  transform: scaleY(0);
  transition: all 0.15s ease;
}
.nav-link.active .nav-indicator {
  opacity: 1;
  transform: scaleY(1);
}

/* ====== 通用管理卡片 ====== */
.a-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 18px 20px;
  margin-bottom: 16px;
}
.a-card-hd {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 14px;
  flex-wrap: wrap;
  gap: 8px;
}
.a-card-hd h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 700;
  color: var(--text-primary);
}
.a-card-hd-acts {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
}

/* ====== 统计卡片网格 ====== */
.a-stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 10px;
  margin-bottom: 16px;
}
.a-stat {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 14px 16px;
  text-align: center;
  transition: border-color 0.15s;
}
.a-stat:hover {
  border-color: var(--accent-alpha-35);
}
.a-stat-n {
  font-size: 24px;
  font-weight: 700;
  color: var(--accent);
  line-height: 1.2;
}
.a-stat-l {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 3px;
}

/* ====== 通用表格 ====== */
.a-tb {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.a-tb th {
  text-align: left;
  padding: 9px 10px;
  border-bottom: 2px solid var(--border);
  font-weight: 600;
  color: var(--text-muted);
  white-space: nowrap;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.a-tb td {
  padding: 7px 10px;
  border-bottom: 1px solid var(--border);
}
.a-tb tbody tr:hover {
  background: var(--bg-hover);
}
.a-tb-name {
  font-weight: 600;
  color: var(--text-primary);
  max-width: 220px;
}
.a-tb-mono {
  font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace;
  font-size: 11px;
  color: var(--text-muted);
  max-width: 160px;
}
.a-tb-num {
  text-align: center;
  font-weight: 500;
  color: var(--accent);
}
.a-tb-acts {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.a-tb-empty {
  text-align: center;
  padding: 30px;
  color: var(--text-muted);
}

/* ====== Badge ====== */
.a-badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 500;
}
.a-badge-ok { background: var(--success-alpha-10); color: var(--success); }
.a-badge-off { background: var(--bg-hover); color: var(--text-muted); }
.a-badge-run { background: var(--accent-alpha-15); color: var(--accent); }
.a-badge-err { background: var(--danger-alpha-10); color: var(--danger); }
.a-badge-warn { background: var(--warning-alpha-10); color: var(--warning-text); }
.a-badge-info { background: var(--accent-alpha-10); color: var(--accent); }

/* ====== 输入框 ====== */
.a-inp {
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  outline: none;
  flex: 1;
  min-width: 100px;
}
.a-inp:focus {
  border-color: var(--accent);
}
.a-sel {
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
}

/* ====== 操作行 ====== */
.a-row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

/* ====== 表单 ====== */
.a-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.a-form-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}
.a-form-ta {
  resize: vertical;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace;
  font-size: 12px;
}

/* ====== 空态/加载 ====== */
.a-empty {
  text-align: center;
  padding: 40px 20px;
  color: var(--text-muted);
  font-size: 13px;
}
.a-loading {
  text-align: center;
  padding: 30px;
  color: var(--text-muted);
  font-size: 13px;
}

/* ====== 分页 ====== */
.a-pgn {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
  font-size: 13px;
  color: var(--text-muted);
}
.a-pgn-btns {
  display: flex;
  align-items: center;
  gap: 6px;
}
.a-pgn-num {
  font-weight: 600;
  color: var(--text-primary);
}

/* ====== 日志 ====== */
.a-log-view {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  max-height: 500px;
  overflow: auto;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace;
  font-size: 12px;
  line-height: 1.8;
  white-space: pre-wrap;
  word-break: break-all;
}
.a-log-line { padding: 1px 0; }
.a-log-line:hover { background: var(--bg-hover); }
.a-log-err { color: var(--danger); font-weight: 500; }
.a-log-warn { color: var(--warning); }
.a-log-dbg { color: var(--text-muted); }
.a-log-info { color: var(--info); }
.a-log-line mark { background: #fde047; color: #000; padding: 0 2px; border-radius: 2px; }

/* ====== 切换开关 ====== */
.a-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text-primary);
  user-select: none;
}
.a-toggle input[type="checkbox"] {
  width: auto;
  margin: 0;
}

/* ====== 描述文字 ====== */
.a-desc {
  color: var(--text-muted);
  font-size: 12px;
  margin-top: 4px;
}

/* ====== 视频详情弹窗 ====== */
.a-detail-row {
  font-size: 13px;
  line-height: 1.6;
  display: flex;
  gap: 6px;
  align-items: flex-start;
}
.a-detail-row strong {
  color: var(--text-muted);
  white-space: nowrap;
  min-width: 70px;
}
.a-detail-pic {
  max-width: 180px;
  max-height: 120px;
  border-radius: 6px;
  border: 1px solid var(--border);
}
.a-detail-json {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px;
  font-size: 11px;
  font-family: 'Cascadia Code', 'Fira Code', Consolas, monospace;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 400px;
  overflow: auto;
}

/* ====== 页面进入动画 ====== */
.admin-content > * {
  animation: adminFadeIn 0.15s ease;
}
@keyframes adminFadeIn {
  from { opacity: 0.6; }
  to { opacity: 1; }
}
</style>
