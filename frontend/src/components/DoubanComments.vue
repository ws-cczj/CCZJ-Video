<script setup lang="ts">
import { ref, watch, computed, onBeforeUnmount } from 'vue'
import { normalizeApiError } from '../api/app'
import { GetDoubanComments } from '../api/comments'
import { tr } from '../locales'
import RemoteImage from './RemoteImage.vue'

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

// computed 而不是常量：setup 里直接求值会把译文冻在挂载时的语言上。
const sortOptions = computed(() => [
  { value: 'new_score' as const, label: tr('detail.sortHot') },
  { value: 'time' as const, label: tr('detail.sortLatest') },
])

let requestRevision = 0
let loadingTimer: ReturnType<typeof setTimeout> | undefined
let pending: ReturnType<typeof GetDoubanComments> | undefined
function cancelPending(): void {
  const request = pending
  pending = undefined
  try { void Promise.resolve(request?.cancel?.()).catch(() => {}) } catch { /* Already settled. */ }
}
onBeforeUnmount(() => { requestRevision++; clearTimeout(loadingTimer); cancelPending() })
async function fetchComments() {
  const revision = ++requestRevision
  cancelPending()
  clearTimeout(loadingTimer)
  if (!props.doubanId) return
  loading.value = true
  error.value = null
  // The IP gate and challenge may outlast one network timeout. Stop the
  // blocking spinner while the same request continues warming the cache.
  loadingTimer = setTimeout(() => {
    if (revision !== requestRevision) return
    loading.value = false
    error.value = tr('detail.commentsSlow')
  }, 12000)
  try {
    pending = GetDoubanComments({
      douban_id: props.doubanId,
      page: page.value,
      sort: sort.value,
    })
    const resp = await pending as DoubanCommentsResp
    if (revision !== requestRevision) return
    comments.value = Array.isArray(resp?.comments) ? resp.comments : []
    error.value = null
    totalPages.value = resp?.total_pages || 1
    total.value = resp?.total || 0
  } catch (e: any) {
    if (revision !== requestRevision) return
    // 走 normalizeApiError：Go 侧的错误带 CODE: 前缀，直接展示会把内部编码甩到脸上。
    const failure = normalizeApiError(e)
    if(failure.code==='CANCELLED')return
    error.value = failure.code === 'UNAVAILABLE' ? tr('detail.commentsUnavailable') : failure.message
    comments.value = []
  } finally {
    if (revision === requestRevision) { clearTimeout(loadingTimer); pending = undefined; loading.value = false }
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
  const stars = Number.isFinite(rating) ? Math.max(0, Math.min(5, Math.floor(rating))) : 0
  return '★'.repeat(stars) + '☆'.repeat(5 - stars)
}

// 评分颜色
function ratingColor(rating: number): string {
  if (rating >= 4) return '#f59e0b'  // 金色
  if (rating >= 3) return '#22c55e'  // 绿色
  if (rating >= 2) return '#6b7280'  // 灰色
  return '#ef4444'                    // 红色
}

watch(() => props.doubanId, (newId) => {
  requestRevision++
  cancelPending()
  clearTimeout(loadingTimer)
  comments.value = []
  total.value = 0
  totalPages.value = 1
  error.value = null
  loading.value = false
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
        {{ tr('detail.doubanComments') }}
        <span v-if="total > 0" class="comments-count">{{ tr('detail.commentCount', { count: total }) }}</span>
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
    <div v-if="loading" class="comments-loading" role="status">
      <div class="loading-spinner"></div>
      <span>{{ tr('detail.loadingComments') }}</span>
    </div>

    <!-- 错误 -->
    <div v-else-if="error" class="comments-error" role="alert">
      <span class="error-icon">⚠️</span>
      <span>{{ error }}</span>
      <button class="retry-btn" @click="fetchComments">{{ tr('detail.retry') }}</button>
    </div>

    <!-- 无评论 -->
    <div v-else-if="comments.length === 0" class="comments-empty">
      {{ tr('detail.noComments') }}
    </div>

    <!-- 评论列表 -->
    <div v-else class="comments-list">
      <div v-for="c in comments" :key="c.id" class="comment-item">
        <div class="comment-avatar">
          <RemoteImage :src="c.avatar" :alt="c.username" loading="lazy" />
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
      <button class="page-btn" :disabled="page <= 1" @click="goToPage(page - 1)">‹ {{ tr('detail.prevPage') }}</button>
      <template v-for="(p, i) in visiblePages" :key="i">
        <span v-if="p === '...'" class="page-ellipsis">…</span>
        <button v-else class="page-btn" :class="{ active: p === page }" @click="goToPage(p as number)">{{ p }}</button>
      </template>
      <button class="page-btn" :disabled="page >= totalPages" @click="goToPage(page + 1)">{{ tr('detail.nextPage') }} ›</button>
    </div>
  </div>
</template>

<style scoped src="../styles/components/douban-comments.css"></style>
