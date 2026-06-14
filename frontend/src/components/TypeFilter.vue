<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'

const props = withDefaults(defineProps<{
  types: Array<{ type_id: string | number; name: string }>
  modelValue: string | number
}>(), {
  types: () => [],
})
const emit = defineEmits(['update:modelValue'])

const safeTypes = computed(() =>
  Array.isArray(props.types) ? props.types.filter(t => t && (t.type_id !== undefined)) : []
)

// 在"全部"之后加上真实类型
const allChips = computed(() => [
  { type_id: 'all' as const, name: '全部' },
  ...safeTypes.value,
])

// 是否处于"展开"状态
const expanded = ref(false)
// 第一排显示的数量（初始 6，根据容器宽度动态调整）
const firstRowCount = ref(6)
const containerRef = ref<HTMLElement | null>(null)
const chipWidthEstimate = 72 // 估算每个标签约 72px（含间距）

function updateFirstRowCount(): void {
  if (!containerRef.value) return
  const w = containerRef.value.clientWidth || 600
  // 可容纳数量 = 容器宽度 / 每个标签估算宽度；至少保留 4 个
  const count = Math.max(4, Math.floor(w / chipWidthEstimate))
  firstRowCount.value = count
}

onMounted(() => {
  updateFirstRowCount()
  window.addEventListener('resize', updateFirstRowCount)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateFirstRowCount)
})

// 可视区（第一排 + 展开时的全部）
const visibleChips = computed(() => allChips.value)

// 判断当前是否有"溢出"（是否需要显示 展开/收起 按钮）
const hasOverflow = computed(() => allChips.value.length > firstRowCount.value)

function isActive(t: { type_id: string | number }): boolean {
  const a = String(props.modelValue ?? '')
  const b = String(t.type_id)
  if (a === 'all' || a === '0' || a === '') return b === 'all' || b === '0'
  return a === b
}

function toggleExpanded(): void {
  expanded.value = !expanded.value
}
</script>

<template>
  <div class="type-filter" ref="containerRef">
    <!-- 第一行：始终显示，最多显示 firstRowCount 个 chip -->
    <div class="tf-row">
      <button
        v-for="(t, idx) in visibleChips"
        :key="String(t.type_id)"
        :class="['type-btn', { active: isActive(t), 'tf-hidden': !expanded && idx >= firstRowCount }]"
        @click="emit('update:modelValue', t.type_id)"
      >
        {{ t.name }}
      </button>
      <!-- 展开/收起 按钮（有溢出时显示） -->
      <button
        v-if="hasOverflow"
        :class="['type-btn', 'type-btn--toggle', { 'is-expand': !expanded }]"
        @click="toggleExpanded"
      >
        <span>{{ expanded ? '收起' : '更多' }}</span>
        <svg :class="{ 'is-rotated': expanded }" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style scoped>
.type-filter {
  width: 100%;
  padding: 8px 0;
}
.tf-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.type-btn {
  padding: 6px 12px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  white-space: nowrap;
  transition: all 0.15s ease;
  font-family: inherit;
  line-height: 1.2;
}
.type-btn:hover {
  border-color: var(--accent);
  color: var(--text-primary);
  background: var(--bg-hover);
}
.type-btn.active {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
}
/* 溢出时隐藏非第一排的标签（用 display:none 不占空间） */
.type-btn.tf-hidden {
  display: none;
}
.type-btn--toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 6px 10px;
  background: transparent;
  border: 1px dashed var(--border);
  color: var(--text-muted);
}
.type-btn--toggle:hover {
  color: var(--accent);
  border-color: var(--accent);
  background: transparent;
}
.type-btn--toggle svg {
  transition: transform 0.2s ease;
}
.type-btn--toggle svg.is-rotated {
  transform: rotate(180deg);
}
</style>
