<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { GetDoubanComments } from '../../bindings/cczjVideo/app'

interface DoubanComment {
  id: string
  avatar: string
  username: string
  profile: string
  status: string
  rating: number
  rating_title: string
  time: string
  location: string
  votes: number
  content: string
}

interface DoubanCommentsResp {
  comments: DoubanComment[]
  total: number
  page: number
  total_pages: number
}

const props = defineProps<{
  doubanId: string
}>()

const loading = ref(false)
const error = ref<string | null>(null)
const comments = ref<DoubanComment[]>([])
const page = ref(1)
const totalPages = ref(1)
const total = ref(0)
const sort = ref<'new_score' | 'time'>('new_score')

const sortOptions = [
  { value: 'new_score' as const, label: '热门' },
  { value: 'time' as const, label: '最新' },
]

async function fetchComments() {
  if (!props.doubanId) return
  loading.value = true
  error.value = null
  try {
    const resp = await GetDoubanComments({
      douban_id: props.doubanId,
      page: page.value,
      sort: sort.value,
    }) as DoubanCommentsResp
    comments.value = resp?.comments || []
    totalPages.value = resp?.total_pages || 1
    total.value = resp?.total || 0
  } catch (e: any) {
    error.value = e?.message || String(e)
    comments.value = []
  } finally {
    loading.value = false
  }
}

function goToPage(p: number) {
  if (p < 1 || p > totalPages.value || p === page.value) return
  page.value = p
  fetchComments()
}

function changeSort(s: 'new_score' | 'time') {
  if (sort.value === s) return
  sort.value = s
  page.value = 1
  fetchComments()
}

// 页码按钮计算
const visiblePages = computed(() => {
  const pages: (number | string)[] = []
  const cur = page.value
  const tp = totalPages.value
  const delta = 2
  const start = Math.max(1, cur - delta)
  const end = Math.min(tp, cur + delta)
  if (start > 1) { pages.push(1); if (start > 2) pages.push('...') }
  for (let i = start; i <= end; i++) pages.push(i)
  if (end < tp) { if (end < tp - 1) pages.push('...'); pages.push(tp) }
  return pages
})

// 评分星标
function renderStars(rating: number): string {
  return '★'.repeat(rating) + '☆'.repeat(5 - rating)
}

// 评分颜色
function ratingColor(rating: number): string {
  if (rating >= 4) return '#f59e0b'  // 金色
  if (rating >= 3) return '#22c55e'  // 绿色
  if (rating >= 2) return '#6b7280'  // 灰色
  return '#ef4444'                    // 红色
}

watch(() => props.doubanId, (newId) => {
  if (newId) {
    page.value = 1
    fetchComments()
  }
}, { immediate: true })
</script>

<template>
  <div class="douban-comments">
    <div class="comments-header">
      <h3 class="comments-title">
        <span class="title-icon">💬</span>
        豆瓣评论
        <span v-if="total > 0" class="comments-count">（约 {{ total }} 条）</span>
      </h3>
      <div class="sort-switcher">
        <button
          v-for="opt in sortOptions"
          :key="opt.value"
          class="sort-btn"
          :class="{ active: sort === opt.value }"
          @click="changeSort(opt.value)"
        >
          {{ opt.label }}
        </button>
      </div>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="comments-loading">
      <div class="loading-spinner"></div>
      <span>正在加载评论...</span>
    </div>

    <!-- 错误 -->
    <div v-else-if="error" class="comments-error">
      <span class="error-icon">⚠️</span>
      <span>{{ error }}</span>
      <button class="retry-btn" @click="fetchComments">重试</button>
    </div>

    <!-- 无评论 -->
    <div v-else-if="comments.length === 0" class="comments-empty">
      暂无评论
    </div>

    <!-- 评论列表 -->
    <div v-else class="comments-list">
      <div v-for="c in comments" :key="c.id" class="comment-item">
        <div class="comment-avatar">
          <img :src="c.avatar" :alt="c.username" loading="lazy" referrerpolicy="no-referrer" />
        </div>
        <div class="comment-body">
          <div class="comment-meta">
            <span class="comment-username">{{ c.username }}</span>
            <span v-if="c.rating > 0" class="comment-rating" :style="{ color: ratingColor(c.rating) }">
              <span class="stars">{{ renderStars(c.rating) }}</span>
              <span class="rating-title">{{ c.rating_title }}</span>
            </span>
            <span v-if="c.status" class="comment-status">{{ c.status }}</span>
          </div>
          <div class="comment-content">{{ c.content }}</div>
          <div class="comment-footer">
            <span class="comment-time">{{ c.time }}</span>
            <span v-if="c.location" class="comment-location">📍 {{ c.location }}</span>
            <span class="comment-votes" :class="{ 'has-votes': c.votes > 0 }">
              👍 {{ c.votes }}
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- 分页 -->
    <div v-if="totalPages > 1 && !loading" class="comments-pagination">
      <button class="page-btn" :disabled="page <= 1" @click="goToPage(page - 1)">‹ 上一页</button>
      <template v-for="(p, i) in visiblePages" :key="i">
        <span v-if="p === '...'" class="page-ellipsis">…</span>
        <button v-else class="page-btn" :class="{ active: p === page }" @click="goToPage(p as number)">{{ p }}</button>
      </template>
      <button class="page-btn" :disabled="page >= totalPages" @click="goToPage(page + 1)">下一页 ›</button>
    </div>
  </div>
</template>

<style scoped>
.douban-comments {
  background: var(--bg-card, #1a1a2e);
  border-radius: 12px;
  padding: 20px;
  color: var(--text-primary, #e0e0e0);
}

.comments-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border, #333);
}

.comments-title {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  display: flex;
  align-items: center;
  gap: 6px;
}

.title-icon {
  font-size: 18px;
}

.comments-count {
  font-size: 12px;
  font-weight: 400;
  color: var(--text-muted, #888);
}

.sort-switcher {
  display: flex;
  gap: 4px;
  background: var(--bg-secondary, #16213e);
  border-radius: 8px;
  padding: 2px;
}

.sort-btn {
  padding: 4px 14px;
  border: none;
  border-radius: 6px;
  font-size: 12px;
  cursor: pointer;
  background: transparent;
  color: var(--text-secondary, #aaa);
  transition: all 0.2s ease;
}

.sort-btn.active {
  background: var(--accent, #4f46e5);
  color: #fff;
}

.sort-btn:hover:not(.active) {
  background: var(--bg-hover, #1e293b);
}

/* 加载中 */
.comments-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 40px 0;
  color: var(--text-muted, #888);
  font-size: 14px;
}

.loading-spinner {
  width: 20px;
  height: 20px;
  border: 2px solid var(--border, #333);
  border-top-color: var(--accent, #4f46e5);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

/* 错误 */
.comments-error {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 20px;
  background: rgba(239, 68, 68, 0.1);
  border-radius: 8px;
  font-size: 13px;
  color: var(--text-secondary, #aaa);
}

.error-icon {
  font-size: 16px;
}

.retry-btn {
  padding: 4px 12px;
  border: 1px solid var(--border, #333);
  border-radius: 6px;
  background: transparent;
  color: var(--accent, #4f46e5);
  font-size: 12px;
  cursor: pointer;
  margin-left: auto;
}

.retry-btn:hover {
  background: var(--accent-alpha-10, rgba(79, 70, 229, 0.1));
}

/* 空状态 */
.comments-empty {
  text-align: center;
  padding: 40px 0;
  color: var(--text-muted, #888);
  font-size: 14px;
}

/* 评论列表 */
.comments-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.comment-item {
  display: flex;
  gap: 12px;
  padding: 12px;
  border-radius: 8px;
  transition: background 0.2s ease;
}

.comment-item:hover {
  background: var(--bg-hover, #1e293b);
}

.comment-avatar {
  flex-shrink: 0;
}

.comment-avatar img {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  object-fit: cover;
  border: 1px solid var(--border, #333);
}

.comment-body {
  flex: 1;
  min-width: 0;
}

.comment-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  flex-wrap: wrap;
}

.comment-username {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #e0e0e0);
}

.comment-rating {
  display: flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
}

.stars {
  letter-spacing: -1px;
  font-size: 12px;
}

.rating-title {
  font-size: 11px;
  opacity: 0.8;
}

.comment-status {
  font-size: 11px;
  color: var(--text-muted, #888);
  background: var(--bg-secondary, #16213e);
  padding: 1px 6px;
  border-radius: 4px;
}

.comment-content {
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-secondary, #ccc);
  margin-bottom: 8px;
  word-break: break-word;
}

.comment-footer {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 11px;
  color: var(--text-muted, #888);
}

.comment-location {
  display: flex;
  align-items: center;
  gap: 2px;
}

.comment-votes {
  display: flex;
  align-items: center;
  gap: 2px;
}

.comment-votes.has-votes {
  color: var(--accent, #4f46e5);
}

/* 分页 */
.comments-pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid var(--border, #333);
}

.page-btn {
  padding: 6px 12px;
  border: 1px solid var(--border, #333);
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary, #aaa);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.page-btn:hover:not(:disabled):not(.active) {
  background: var(--bg-hover, #1e293b);
  border-color: var(--accent, #4f46e5);
}

.page-btn.active {
  background: var(--accent, #4f46e5);
  color: #fff;
  border-color: var(--accent, #4f46e5);
}

.page-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

.page-ellipsis {
  padding: 6px 8px;
  color: var(--text-muted, #888);
  font-size: 12px;
}
</style>
