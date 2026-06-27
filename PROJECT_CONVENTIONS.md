# CCZJ Video 项目开发规范

> 本文档是项目的"开发圣经"，所有贡献者（包括 AI 助手）都应遵循以下规范。

---

## 1. 项目架构概览

### 1.1 技术栈
- **后端**: Go + Wails v3 + SQLite
- **前端**: Vue 3 + TypeScript + Vite + Pinia + Vue Router
- **国际化**: vue-i18n (Composition API 模式)
- **图标**: @iconify/vue + Carbon 图标集
- **动画**: @vueuse/motion
- **CSS**: 自定义 cczj- 工具类系统

### 1.2 目录结构
```
CCZJ Video/
├── app/                    # Go 后端代码
│   ├── applog/            # 日志系统
│   ├── collect/           # 采集引擎
│   ├── db/                # 数据库层
│   ├── douban/            # 豆瓣集成（爬虫、评论）
│   ├── handler/           # 业务逻辑处理器
│   ├── model/             # 数据模型
│   └── util/              # 工具函数
├── frontend/
│   ├── src/
│   │   ├── components/    # 可复用组件
│   │   │   ├── ui/        # 基础 UI 组件 (Button, Modal, Tag 等)
│   │   │   ├── Icon.vue   # 图标组件 (@iconify/vue 封装)
│   │   │   └── *.vue      # 业务组件
│   │   ├── views/         # 页面视图
│   │   │   ├── admin/     # 管理后台
│   │   │   └── *.vue      # 主页面
│   │   ├── stores/        # Pinia 状态管理
│   │   ├── event/         # 全局事件总线
│   │   ├── locales/       # 国际化翻译文件
│   │   ├── utils/         # 前端工具函数
│   │   ├── styles/        # 全局样式 (cczj-utilities.css)
│   │   └── router/        # 路由配置
│   └── bindings/          # Wails 自动生成的 JS 绑定（勿手动修改）
└── wails.json             # Wails 配置
```

---

## 2. CSS 规范：必须使用 cczj- 工具类

### 2.1 核心原则
**所有样式优先使用 `styles/cczj-utilities.css` 中定义的工具类，避免在组件内写重复的 CSS。**

### 2.2 命名规范
- 前缀: `cczj-` (避免与 UnoCSS 等框架冲突)
- 格式: `cczj-{属性}-{值}` 或 `cczj-{属性方向}-{值}`

### 2.3 常用工具类速查

#### 布局
```html
<!-- Flex 布局 -->
<div class="cczj-flex cczj-items-center cczj-gap-4">
<!-- 网格 -->
<div class="cczj-grid cczj-gap-4">
<!-- 隐藏 -->
<div class="cczj-hidden">
```

#### 间距 (1单位 = 0.25rem = 4px)
```html
<!-- 外边距 -->
<div class="cczj-mt-4 cczj-mb-2">  <!-- margin-top: 1rem, margin-bottom: 0.5rem -->
<div class="cczj-mx-auto">         <!-- margin: 0 auto -->
<!-- 内边距 -->
<div class="cczj-p-4 cczj-px-6">   <!-- padding: 1rem, padding-x: 1.5rem -->
```

#### 尺寸
```html
<div class="cczj-w-full cczj-h-full">      <!-- width/height: 100% -->
<div class="cczj-w-16 cczj-h-16">          <!-- width/height: 4rem -->
<div class="cczj-min-w-0 cczj-max-w-full"> <!-- min-width: 0, max-width: 100% -->
```

#### 文本
```html
<div class="cczj-text-center cczj-font-bold">
<div class="cczj-text-sm cczj-text-muted">
<div class="cczj-truncate">                <!-- 单行截断 -->
<div class="cczj-line-clamp-2">            <!-- 多行截断 -->
```

#### 交互
```html
<button class="cczj-pointer cczj-select-none">
<div class="cczj-opacity-50 cczj-pointer-events-none">
```

#### 动画过渡
```html
<div class="cczj-transition cczj-transition-fast">
<div class="cczj-transition-none">
```

#### 主题色 (使用 CSS 变量)
```html
<div class="cczj-text-primary cczj-bg-secondary">
<div class="cczj-text-accent cczj-border-accent">
<div class="cczj-text-success cczj-bg-danger">
```

### 2.4 何时写自定义 CSS？
✅ **可以写**:
- 复杂的动画效果 (@keyframes)
- 伪元素样式 (::before, ::after)
- 媒体查询 (@media)
- 特殊的视觉设计（渐变、阴影组合）

❌ **不要写**:
- 简单的 `display: flex`
- 基础的 `margin/padding`
- 常规的 `text-align`, `font-size`
- 简单的 `opacity`, `cursor`

### 2.5 示例对比

**❌ 错误做法**:
```vue
<template>
  <div class="card">
    <h3 class="title">标题</h3>
  </div>
</template>
<style scoped>
.card {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 20px;
}
.title {
  font-size: 16px;
  font-weight: 600;
  text-align: center;
}
</style>
```

**✅ 正确做法**:
```vue
<template>
  <div class="card cczj-flex cczj-flex-col cczj-gap-4 cczj-p-5">
    <h3 class="title cczj-text-lg cczj-font-semibold cczj-text-center">标题</h3>
  </div>
</template>
<style scoped>
.card {
  /* 只写特殊样式，如边框、阴影 */
  border: 1px solid var(--border);
  border-radius: 8px;
}
</style>
```

---

## 3. 国际化规范 (vue-i18n)

### 3.1 核心原则
**所有用户可见的文本都必须使用 `t()` 函数，禁止硬编码中文。**

### 3.2 使用方式
```typescript
// Composition API
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

// 模板中使用
<h1>{{ t('page.title') }}</h1>
<p>{{ t('page.description', { count: 10 }) }}</p>

// 脚本中使用
const message = t('common.confirm')
```

### 3.3 翻译键命名规范
- 使用点分隔: `namespace.key.subkey`
- 小驼峰命名: `continueWatching` (非 `continue_watching`)
- 通用文本放 `common.*`
- 页面特定文本放 `pageName.*`

### 3.4 翻译文件结构
```typescript
// locales/zh-CN.ts
export default {
  common: {
    ok: '确定',
    cancel: '取消',
    loading: '加载中...',
  },
  home: {
    title: '首页',
    continueWatching: '继续观看',
    noData: '暂无数据',
  },
  search: {
    placeholder: '搜索视频...',
    results: '找到 {count} 个结果',
  },
  // ...
}
```

### 3.5 带参数的翻译
```typescript
// 翻译文件
results: '找到 {count} 个结果'

// 使用
t('search.results', { count: 10 })  // → "找到 10 个结果"
```

### 3.6 语言切换
```typescript
import { setLocale } from '@/locales'

// 切换到英文
await setLocale('en')

// 切换到中文
await setLocale('zh-CN')
```

---

## 4. 图标规范 (@iconify/vue)

### 4.1 使用方式
```vue
<script setup>
import Icon from '@/components/Icon.vue'
</script>

<template>
  <Icon name="home" :size="20" />
  <Icon name="search" :size="16" class="text-accent" />
</template>
```

### 4.2 可用图标名
使用 Carbon 图标集，完整列表: https://icon-sets.iconify.design/carbon/

常用图标:
- `home`, `search`, `settings`, `close`
- `play`, `pause`, `stop`, `volume`
- `star`, `heart`, `bookmark`
- `arrow-left`, `arrow-right`, `chevron-up`
- `plus`, `minus`, `trash`, `edit`

### 4.3 自定义图标
如需添加不在映射表中的图标，直接传入 Carbon 图标名:
```vue
<Icon name="carbon:cloud-download" :size="20" />
```

---

## 5. 动画规范 (@vueuse/motion)

### 5.1 路由切换动画
已在 `App.vue` 中配置全局路由过渡:
```vue
<router-view v-slot="{ Component }">
  <component :is="Component" v-motion :initial="{ opacity: 0 }" :enter="{ opacity: 1 }" />
</router-view>
```

### 5.2 列表项动画
```vue
<div v-for="item in items" :key="item.id"
     v-motion
     :initial="{ y: 20, opacity: 0 }"
     :enter="{ y: 0, opacity: 1 }"
     :delay="index * 50">
  {{ item.name }}
</div>
```

### 5.3 模态框动画
```vue
<Modal v-model="show" v-motion
       :initial="{ scale: 0.9, opacity: 0 }"
       :enter="{ scale: 1, opacity: 1 }"
       :leave="{ scale: 0.9, opacity: 0 }">
  内容
</Modal>
```

### 5.4 悬停动画
```vue
<div v-motion
     :whileHover="{ scale: 1.05 }"
     :whileTap="{ scale: 0.95 }">
  点击我
</div>
```

---

## 6. 事件系统规范

### 6.1 全局事件总线
使用 `window.app_event` 进行跨组件通信:

```typescript
// 监听事件
appEvent.on('player:timeupdate', (time) => {
  console.log('当前时间:', time)
})

// 发送事件
appEvent.emit('player:play')

// 移除监听
appEvent.off('player:timeupdate', handler)
```

### 6.2 Wails 事件 (后端 → 前端)
```typescript
import { Events } from '@wailsio/runtime'

// 监听后端事件
Events.On('download:progress', (event) => {
  const { progress, speed } = event.data
  // 更新 UI
})
```

### 6.3 事件命名规范
- 格式: `模块:动作` (如 `player:play`, `download:complete`)
- 小写 + 冒号分隔
- 动词使用现在时

---

## 7. 状态管理规范 (Pinia)

### 7.1 Store 命名
- 文件名: `video.ts`, `user.ts`
- Store 名: `useVideoStore`, `useUserStore`

### 7.2 使用方式
```typescript
import { useVideoStore } from '@/stores/video'

const videoStore = useVideoStore()

// 访问状态
const videos = videoStore.videos

// 调用 action
await videoStore.loadVideos()

// 监听变化
watch(() => videoStore.currentVideo, (video) => {
  // 处理变化
})
```

### 7.3 不要在组件外使用 Store
```typescript
// ❌ 错误
const store = useVideoStore()
export function myFunction() {
  store.doSomething()
}

// ✅ 正确
export function myFunction() {
  const store = useVideoStore()
  store.doSomething()
}
```

---

## 8. Go 后端规范

### 8.1 Wails 绑定
- 所有暴露给前端的方法都定义在 `app.go` 的 `App` 结构体上
- 方法签名必须使用可 JSON 序列化的类型
- 运行 `wails dev` 会自动生成 `frontend/bindings/` 下的 JS/TS 绑定

### 8.2 数据库操作
- 所有数据库操作封装在 `app/db/` 包中
- 使用 SQLite + go-sqlite3
- 表名前缀: `v_` (视频表), `global_` (全局表)

### 8.3 错误处理
```go
// 返回错误给前端
func (a *App) GetVideo(id string) (*model.Video, error) {
    video, err := db.GetVideoByID(id)
    if err != nil {
        return nil, fmt.Errorf("获取视频失败: %w", err)
    }
    return video, nil
}
```

### 8.4 日志
```go
import "cczjVideo/app/applog"

applog.Info("用户登录: %s", username)
applog.Error("数据库错误: %v", err)
```

---

## 9. 组件开发规范

### 9.1 Vue 组件结构
```vue
<script setup lang="ts">
// 1. 导入
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/Icon.vue'

// 2. 组合式函数
const { t } = useI18n()

// 3. Props & Emits
const props = defineProps<{
  videoId: string
}>()

const emit = defineEmits<{
  (e: 'update', value: string): void
}>()

// 4. 响应式状态
const loading = ref(false)
const data = ref<Video | null>(null)

// 5. 计算属性
const title = computed(() => data.value?.name || t('common.untitled'))

// 6. 方法
async function loadData() {
  loading.value = true
  try {
    data.value = await fetchVideo(props.videoId)
  } finally {
    loading.value = false
  }
}

// 7. 生命周期
onMounted(() => {
  loadData()
})
</script>

<template>
  <div class="component cczj-flex cczj-flex-col cczj-gap-4">
    <!-- 使用 cczj- 工具类 -->
    <h2 class="cczj-text-lg cczj-font-bold">{{ title }}</h2>
    <Icon name="play" :size="20" />
  </div>
</template>

<style scoped>
.component {
  /* 只写特殊样式 */
}
</style>
```

### 9.2 UI 组件导出
所有基础 UI 组件从 `components/ui/index.ts` 统一导出:
```typescript
import { Button, Modal, Input } from '@/components/ui'
```

---

## 10. 性能优化规范

### 10.1 图片懒加载
```vue
<img :src="video.poster" loading="lazy" />
```

### 10.2 虚拟滚动
对于长列表（>100项），使用虚拟滚动:
```typescript
import { useVirtual } from '@vueuse/core'

const { list, containerProps, wrapperProps } = useVirtual(items, {
  itemHeight: 80,
  overscan: 5,
})
```

### 10.3 防抖和节流
```typescript
import { useDebounceFn, useThrottleFn } from '@vueuse/core'

// 搜索输入防抖
const handleSearch = useDebounceFn((keyword: string) => {
  search(keyword)
}, 300)

// 滚动事件节流
const handleScroll = useThrottleFn(() => {
  checkScrollPosition()
}, 100)
```

### 10.4 路由懒加载
已在 `router/index.ts` 中配置，所有页面组件使用 `() => import()`:
```typescript
{
  path: '/home',
  component: () => import('@/views/Home.vue')
}
```

---

## 11. Git 提交规范

### 11.1 提交信息格式
```
<type>(<scope>): <subject>

<body>

<footer>
```

### 11.2 Type 类型
- `feat`: 新功能
- `fix`: 修复 bug
- `refactor`: 重构
- `style`: 样式调整
- `docs`: 文档更新
- `chore`: 构建/工具变更

### 11.3 示例
```
feat(player): 添加豆瓣评论展示功能

- 新增 DoubanComments.vue 组件
- 集成到 Player.vue 页面
- 支持分页和排序切换

Closes #123
```

---

## 12. 常见陷阱

### 12.1 不要在模板中调用复杂函数
```vue
<!-- ❌ 每次渲染都会执行 -->
<div>{{ formatData(complexCalculation()) }}</div>

<!-- ✅ 使用计算属性 -->
<div>{{ formattedData }}</div>
```

### 12.2 不要在 v-for 中使用 index 作为 key
```vue
<!-- ❌ 列表顺序变化会导致错误 -->
<div v-for="(item, index) in items" :key="index">

<!-- ✅ 使用唯一 ID -->
<div v-for="item in items" :key="item.id">
```

### 12.3 不要忘记清理监听器
```typescript
// ❌ 内存泄漏
onMounted(() => {
  window.addEventListener('resize', handleResize)
})

// ✅ 正确清理
onMounted(() => {
  window.addEventListener('resize', handleResize)
})
onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
})
```

### 12.4 Wails 绑定更新
修改 `app.go` 后必须重新运行:
```bash
wails dev
# 或
wails build
```
自动生成的文件在 `frontend/bindings/` 中，**不要手动修改**。

---

## 13. 开发检查清单

### 新功能开发
- [ ] 使用 cczj- 工具类编写样式
- [ ] 所有文本使用 i18n `t()` 函数
- [ ] 添加必要的动画效果
- [ ] 处理加载状态和错误状态
- [ ] 考虑空数据情况
- [ ] 响应式布局测试

### 提交前检查
- [ ] `npm run build` 无错误
- [ ] `go build` 无错误
- [ ] 功能测试通过
- [ ] 代码符合规范
- [ ] 提交信息规范

---

## 14. 参考资源

- **Vue 3 文档**: https://vuejs.org/
- **vue-i18n 文档**: https://vue-i18n.intlify.dev/
- **@vueuse/motion 文档**: https://motion.vueuse.org/
- **Carbon 图标集**: https://icon-sets.iconify.design/carbon/
- **Wails v3 文档**: https://wails.io/docs/
- **Pinia 文档**: https://pinia.vuejs.org/

---

**最后更新**: 2026-01-25  
**维护者**: CCZJ Video 开发团队
