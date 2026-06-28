# CCZJ Video 项目开发规范

> 本文档是项目的"开发圣经"，所有贡献者（包括 AI 助手）都应遵循以下规范。

---

## 1. 项目架构概览

### 1.1 技术栈
- **后端**: Go + Wails v3 + SQLite
- **前端**: Vue 3 + TypeScript + Vite + Pinia + Vue Router
- **图标**: @iconify/vue + Carbon 图标集
- **CSS**: 自定义 cczj- 工具类系统 + UnoCSS
- **画质增强**: Anime4K (WebGL2) + FSRCNNX (WebGL2)

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
│   ├── updater/           # 版本更新模块（GitHub Release 检测、下载、安装）
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
│   │   ├── locales/       # 国际化翻译文件（保留但暂未启用）
│   │   ├── utils/         # 前端工具函数（含画质增强引擎）
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
- 画质增强相关的 WebGL canvas 样式

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
  border: 1px solid var(--border);
  border-radius: 8px;
}
</style>
```

---

## 3. 图标规范 (@iconify/vue)

### 3.1 使用方式
```vue
<script setup>
import Icon from '@/components/Icon.vue'
</script>

<template>
  <Icon name="home" :size="20" />
  <Icon name="search" :size="16" class="text-accent" />
</template>
```

### 3.2 可用图标名
使用 Carbon 图标集，完整列表: https://icon-sets.iconify.design/carbon/

常用图标:
- `home`, `search`, `settings`, `close`
- `play`, `pause`, `stop`, `volume`
- `star`, `heart`, `bookmark`
- `arrow-left`, `arrow-right`, `chevron-up`
- `plus`, `minus`, `trash`, `edit`

### 3.3 自定义图标
如需添加不在映射表中的图标，直接传入 Carbon 图标名:
```vue
<Icon name="carbon:cloud-download" :size="20" />
```

---

## 4. 事件系统规范

### 4.1 全局事件总线
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

### 4.2 Wails 事件 (后端 → 前端)
```typescript
import { Events } from '@wailsio/runtime'

// 监听后端事件
Events.On('download:progress', (event) => {
  const { progress, speed } = event.data
})

// 更新相关事件
Events.On('update:available', (event) => {})
Events.On('update:download:progress', (event) => {})
Events.On('update:version:changed', (event) => {})
```

### 4.3 事件命名规范
- 格式: `模块:动作` (如 `player:play`, `download:complete`, `update:available`)
- 小写 + 冒号分隔
- 动词使用现在时

---

## 5. 状态管理规范 (Pinia)

### 5.1 Store 命名
- 文件名: `video.ts`, `download.ts`, `updateState.ts`
- Store 名: `useVideoStore`, `useDownloadStore`, `useUpdateStateStore`

### 5.2 使用方式
```typescript
import { useVideoStore } from '@/stores/video'

const videoStore = useVideoStore()

// 访问状态
const videos = videoStore.videos

// 调用 action
await videoStore.loadVideos()

// 监听变化
watch(() => videoStore.currentVideo, (video) => {
})
```

### 5.3 不要在组件外使用 Store
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

## 6. Go 后端规范

### 6.1 Wails 绑定
- 所有暴露给前端的方法都定义在 `app.go` 的 `App` 结构体上
- 方法签名必须使用可 JSON 序列化的类型
- 运行 `wails dev` 会自动生成 `frontend/bindings/` 下的 JS/TS 绑定

### 6.2 数据库操作
- 所有数据库操作封装在 `app/db/` 包中
- 使用 SQLite + modernc.org/sqlite
- 表名前缀: `v_` (视频表), `global_` (全局表)

### 6.3 版本更新模块 (updater)
- 更新检测逻辑封装在 `app/updater/updater.go`
- 支持多渠道版本获取（GitHub API、GitHub Raw、jsdelivr CDN、Gitee）
- 每个版本源最多重试 3 次
- 版本信息缓存 5 分钟，减少网络请求
- 下载超时时间 60 分钟
- Windows ARM 平台自动跳过更新检查

```go
// 检查更新（reCheck: 是否强制刷新缓存）
func CheckUpdate(reCheck bool) (*UpdateInfo, error)

// 下载更新
func DownloadUpdate(ctx context.Context, url string, savePath string) (string, error)

// 安装更新
func InstallUpdate(filePath string) error
```

### 6.4 错误处理
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

### 6.5 日志
```go
import "cczjVideo/app/applog"

applog.Info("用户登录: %s", username)
applog.Error("数据库错误: %v", err)
```

---

## 7. 组件开发规范

### 7.1 Vue 组件结构
```vue
<script setup lang="ts">
// 1. 导入
import { ref, computed } from 'vue'
import Icon from '@/components/Icon.vue'

// 2. Props & Emits
const props = defineProps<{
  videoId: string
}>()

const emit = defineEmits<{
  (e: 'update', value: string): void
}>()

// 3. 响应式状态
const loading = ref(false)
const data = ref<Video | null>(null)

// 4. 计算属性
const title = computed(() => data.value?.name || '未命名')

// 5. 方法
async function loadData() {
  loading.value = true
  try {
    data.value = await fetchVideo(props.videoId)
  } finally {
    loading.value = false
  }
}

// 6. 生命周期
onMounted(() => {
  loadData()
})
</script>

<template>
  <div class="component cczj-flex cczj-flex-col cczj-gap-4">
    <h2 class="cczj-text-lg cczj-font-bold">{{ title }}</h2>
    <Icon name="play" :size="20" />
  </div>
</template>

<style scoped>
.component {
}
</style>
```

### 7.2 UI 组件导出
所有基础 UI 组件从 `components/ui/index.ts` 统一导出:
```typescript
import { Button, Modal, Input } from '@/components/ui'
```

---

## 8. 性能优化规范

### 8.1 图片懒加载
```vue
<img :src="video.poster" loading="lazy" />
```

### 8.2 防抖和节流
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

### 8.3 路由懒加载
已在 `router/index.ts` 中配置，所有页面组件使用 `() => import()`:
```typescript
{
  path: '/home',
  component: () => import('@/views/Home.vue')
}
```

### 8.4 画质增强性能优化
- Anime4K 使用 WebGL2 渲染，需检查 GPU 兼容性
- FilmUpscaler 支持自动质量降级，根据帧率动态调整
- 视频 seek 时重置缓存，避免画面撕裂

---

## 9. Git 提交规范

### 9.1 提交信息格式
```
<type>(<scope>): <subject>

<body>

<footer>
```

### 9.2 Type 类型
- `feat`: 新功能
- `fix`: 修复 bug
- `refactor`: 重构
- `style`: 样式调整
- `docs`: 文档更新
- `chore`: 构建/工具变更

### 9.3 示例
```
feat(player): 添加 Anime4K 画质增强功能

- 新增 anime4kUpscaler.ts 超分辨率引擎
- 支持 S/M/L 三档模型切换
- 集成到 VideoPlayer.vue 播放器

Closes #123
```

---

## 10. 常见陷阱

### 10.1 不要在模板中调用复杂函数
```vue
<!-- ❌ 每次渲染都会执行 -->
<div>{{ formatData(complexCalculation()) }}</div>

<!-- ✅ 使用计算属性 -->
<div>{{ formattedData }}</div>
```

### 10.2 不要在 v-for 中使用 index 作为 key
```vue
<!-- ❌ 列表顺序变化会导致错误 -->
<div v-for="(item, index) in items" :key="index">

<!-- ✅ 使用唯一 ID -->
<div v-for="item in items" :key="item.id">
```

### 10.3 不要忘记清理监听器
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

### 10.4 Wails 绑定更新
修改 `app.go` 后必须重新运行:
```bash
wails dev
# 或
wails build
```
自动生成的文件在 `frontend/bindings/` 中，**不要手动修改**。

### 10.5 WebGL 资源清理
画质增强引擎使用 WebGL 资源，必须在组件销毁时调用 `destroy()` 方法:
```typescript
onUnmounted(() => {
  upscaler?.destroy()
})
```

---

## 11. 开发检查清单

### 新功能开发
- [ ] 使用 cczj- 工具类编写样式
- [ ] 处理加载状态和错误状态
- [ ] 考虑空数据情况
- [ ] 响应式布局测试
- [ ] 如果使用 WebGL，确保资源正确清理

### 提交前检查
- [ ] `npm run build` 无错误
- [ ] `go build` 无错误
- [ ] 功能测试通过
- [ ] 代码符合规范
- [ ] 提交信息规范

---

## 12. 参考资源

- **Vue 3 文档**: https://vuejs.org/
- **Pinia 文档**: https://pinia.vuejs.org/
- **Carbon 图标集**: https://icon-sets.iconify.design/carbon/
- **Wails v3 文档**: https://wails.io/docs/
- **Anime4K**: https://github.com/bloc97/Anime4K
- **FSRCNNX**: https://github.com/igv/FSRCNN-TensorFlow

---

**最后更新**: 2026-06-28  
**维护者**: CCZJ Video 开发团队
