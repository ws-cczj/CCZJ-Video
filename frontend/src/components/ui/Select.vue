<script setup lang="ts">
defineOptions({ name: 'SelectDropdown' })
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick, useId } from 'vue'
import MotionTransition from './MotionTransition.vue'
import { tr } from '../../locales'

interface Option {
  value: string | number
  label: string
  disabled?: boolean
}

const props = withDefaults(defineProps<{
  modelValue?: string | number
  options: Option[]
  placeholder?: string
  disabled?: boolean
  size?: 'sm' | 'md'
  /** inline 模式：面板不 Teleport 到 body，而是 position:absolute 相对本组件定位。
   *  适用于面板需要留在父级 scoped 作用域（如播放器控制条深色主题）、
   *  或父容器会被全屏/动态隐藏（Teleport 出去后 getBoundingClientRect 归零）的场景。 */
  inline?: boolean
  /** inline 模式下面板展开方向：'up' 向上（底部控制条场景），'down' 向下（默认） */
  inlineDrop?: 'up' | 'down'
}>(), {
  modelValue: '',
  placeholder: '',
  disabled: false,
  size: 'md',
  inline: false,
  inlineDrop: 'down',
})

const emit = defineEmits(['update:modelValue', 'change', 'open-change'])

const open = ref(false)
const rootRef = ref<HTMLElement | null>(null)
const panelStyle = ref<Record<string, string>>({})

// 同页可能有多个 Select，id 必须实例唯一，aria-controls / aria-activedescendant 才有有效指向
const uid = useId()
const listId = `${uid}-listbox`
const optionId = (idx: number): string => `${uid}-option-${idx}`

// 键盘导航始终让焦点留在 trigger 上，靠 aria-activedescendant 指向高亮项：
// 面板会被 Teleport 到 body，把焦点移进去既会多出浏览器默认焦点框，也会让 Escape 归还焦点的逻辑变复杂。
const activeIndex = ref(-1)
const activeDescendant = computed(() => (props.options[activeIndex.value] ? optionId(activeIndex.value) : undefined))

const selected = computed(() => {
  const found = props.options.find((o) => o.value === props.modelValue)
  return found || null
})

function scrollHighlightIntoView(): void {
  const el = activeIndex.value >= 0 ? document.getElementById(optionId(activeIndex.value)) : null
  el?.scrollIntoView({ block: 'nearest' })
}

function setHighlight(idx: number): void {
  activeIndex.value = idx
  // 高亮项可能在滚动面板的视口外，等这一帧 DOM 更新完再滚到可见
  nextTick(scrollHighlightIntoView)
}

/** 跳过 disabled 项循环移动高亮；全部禁用或空列表时保持原状 */
function moveHighlight(delta: number): void {
  const total = props.options.length
  if (total === 0) return
  let idx = activeIndex.value < 0 ? (delta > 0 ? -1 : total) : activeIndex.value
  for (let step = 0; step < total; step += 1) {
    idx = (idx + delta + total) % total
    if (!props.options[idx]?.disabled) {
      setHighlight(idx)
      return
    }
  }
}

function syncHighlightToSelection(): void {
  setHighlight(props.options.findIndex((o) => o.value === props.modelValue && !o.disabled))
}

function chooseHighlighted(): void {
  const opt = activeIndex.value >= 0 ? props.options[activeIndex.value] : undefined
  if (!opt || opt.disabled) return
  choose(opt.value)
}

function openPanel(): void {
  if (props.disabled || open.value) return
  open.value = true
  emit('open-change', true)
  // 打开即把高亮对齐到当前选中项，方向键才有一段合理的起点
  syncHighlightToSelection()
}

function toggle(): void {
  if (props.disabled) return
  if (open.value) {
    closePanel()
    return
  }
  openPanel()
}

function choose(value: string | number): void {
  if (props.disabled) return
  closePanel()
  if (value !== props.modelValue) {
    emit('update:modelValue', value)
    emit('change', value)
  }
}

function closePanel(): void {
  if (!open.value) return
  open.value = false
  emit('open-change', false)
}

function onTriggerKeydown(e: KeyboardEvent): void {
  if (props.disabled) return
  const key = e.key
  if (key === 'ArrowDown' || key === 'ArrowUp') {
    // 折叠态按方向键等价于「打开并定位到当前选中项」，展开态则移动高亮
    e.preventDefault()
    const delta = key === 'ArrowDown' ? 1 : -1
    if (!open.value) {
      openPanel()
      if (activeIndex.value < 0) moveHighlight(delta)
    } else {
      moveHighlight(delta)
    }
    return
  }
  if (key === 'Enter' || key === ' ') {
    // 展开时 Enter/Space 落在高亮项上，必须拦掉原生 button 的激活（否则会被 toggle 直接关掉）
    if (open.value) {
      e.preventDefault()
      if (activeIndex.value < 0) moveHighlight(1)
      chooseHighlighted()
    }
    return
  }
  if (key === 'Escape' && open.value) {
    e.preventDefault()
    closePanel()
  }
}

function onDocClick(e: MouseEvent): void {
  if (!rootRef.value) return
  if (!open.value) return
  if (!rootRef.value.contains(e.target as Node)) {
    closePanel()
  }
}

function updatePanelPosition(): void {
  if (!rootRef.value) return
  // inline 模式：面板用 CSS 绝对定位（配合 .select-panel.is-inline 样式），
  // 不需要 JS 计算坐标，避免 Teleport 出去后 rect 归零的问题。
  if (props.inline) {
    panelStyle.value = {}
    return
  }
  const rect = rootRef.value.getBoundingClientRect()
  const panelHeight = 260 // max-height
  const spaceBelow = window.innerHeight - rect.bottom
  const spaceAbove = rect.top
  const showBelow = spaceBelow >= panelHeight || spaceBelow >= spaceAbove

  if (showBelow) {
    panelStyle.value = {
      position: 'fixed',
      left: rect.left + 'px',
      top: (rect.bottom + 4) + 'px',
      minWidth: rect.width + 'px',
      zIndex: '10000',
      maxHeight: Math.min(panelHeight, spaceBelow - 8) + 'px',
    }
  } else {
    panelStyle.value = {
      position: 'fixed',
      left: rect.left + 'px',
      bottom: (window.innerHeight - rect.top + 4) + 'px',
      minWidth: rect.width + 'px',
      zIndex: '10000',
      maxHeight: Math.min(panelHeight, spaceAbove - 8) + 'px',
    }
  }
}

onMounted(() => {
  document.addEventListener('click', onDocClick, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick, true)
})

watch(open, async (v) => {
  if (v) {
    await nextTick()
    updatePanelPosition()
    return
  }
  activeIndex.value = -1
})
</script>

<template>
  <div
    class="select-dropdown"
    :class="{ 'is-disabled': disabled, 'is-open': open, [`size-${size}`]: true }"
    ref="rootRef"
  >
    <button
      class="select-trigger"
      type="button"
      :disabled="disabled"
      role="combobox"
      aria-haspopup="listbox"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="open ? listId : undefined"
      :aria-owns="open ? listId : undefined"
      :aria-activedescendant="activeDescendant"
      @keydown="onTriggerKeydown"
      @click.stop="toggle"
    >
      <span class="label" :class="{ placeholder: !selected }">
        {{ selected ? selected.label : (placeholder || tr('common.selectPlaceholder')) }}
      </span>
      <svg class="caret" viewBox="0 0 20 20" aria-hidden="true">
        <path d="M5 8 l5 5 5-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"></path>
      </svg>
    </button>

    <!-- 面板：inline 模式不 Teleport（留在 scoped 作用域，避免 :deep 样式失效），
         非 inline 模式 Teleport 到 body 并用 fixed 定位。 -->
    <Teleport to="body" :disabled="inline">
      <MotionTransition preset="dropdown">
        <div
          v-if="open"
          class="select-panel"
          :class="{ 'is-inline': inline, 'drop-up': inline && inlineDrop === 'up' }"
          :style="panelStyle"
          @click.stop
        >
          <ul :id="listId" class="option-list" role="listbox">
            <li
              v-for="(opt, i) in options"
              :key="String(opt.value)"
              :id="optionId(i)"
              class="option"
              role="option"
              :aria-selected="selected && selected.value === opt.value ? 'true' : 'false'"
              :aria-disabled="opt.disabled ? 'true' : undefined"
              :class="{ 'is-selected': selected && selected.value === opt.value, 'is-disabled': opt.disabled, 'is-active': activeIndex === i }"
              @click="!opt.disabled && choose(opt.value)"
            >
              <span class="option-label">{{ opt.label }}</span>
              <span v-if="selected && selected.value === opt.value" class="check-icon" aria-hidden="true">✓</span>
            </li>
            <li v-if="options.length === 0" class="empty" role="presentation">{{ tr('common.noData') }}</li>
          </ul>
        </div>
      </MotionTransition>
    </Teleport>
  </div>
</template>

<style scoped>
.select-dropdown {
  position: relative;
  display: inline-block;
  min-width: 140px;
  color: var(--text-primary);
  font-size: 14px;
  user-select: none;
}

.select-dropdown.size-sm {
  font-size: 12px;
}

.select-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 8px 12px;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: inherit;
  font-family: inherit;
  cursor: pointer;
  transition: border-color var(--transition-fast), box-shadow var(--transition-fast);
  min-height: 34px;
}

.size-sm .select-trigger {
  padding: 4px 10px;
  min-height: 28px;
  border-radius: 6px;
}

.select-trigger:hover:not(:disabled) {
  border-color: var(--accent);
}

.select-trigger:focus,
.select-trigger:focus-visible {
  outline: none;
}

.is-open .select-trigger,
.select-trigger:focus-visible {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}

.select-trigger:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.label {
  flex: 1;
  text-align: left;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.label.placeholder {
  color: var(--text-muted);
}

.caret {
  width: 14px;
  height: 14px;
  margin-left: 6px;
  color: var(--text-muted);
  transition: transform var(--transition-fast);
}

.is-open .caret {
  transform: rotate(180deg);
}

.select-panel {
  /* position/left/top 由 JS panelStyle 动态设置（fixed 定位，通过 Teleport 到 body） */
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.18);
  padding: 4px;
  max-height: 260px;
  overflow: auto;
  box-sizing: border-box;
}

/* inline 模式：绝对定位相对 .select-dropdown（本身 position:relative），
   留在父级 DOM 树内，跟随父级显隐，且父级的 scoped 样式（含 :deep）能命中。 */
.select-panel.is-inline {
  position: absolute;
  left: 0;
  min-width: 100%;
  z-index: 100;
}
/* 向下展开：面板顶部贴在 trigger 下方 */
.select-panel.is-inline:not(.drop-up) {
  top: calc(100% + 4px);
  bottom: auto;
}
/* 向上展开：面板底部贴在 trigger 上方（底部控制条场景） */
.select-panel.is-inline.drop-up {
  bottom: calc(100% + 4px);
  top: auto;
}

.option-list {
  list-style: none;
  padding: 0;
  margin: 0;
}

.option {
  padding: 8px 12px;
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: var(--text-secondary);
  transition: background-color var(--transition-fast), color var(--transition-fast);
  margin: 1px 0;
}

.option:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* 键盘高亮用和 hover 同一层底色：方向键选中的那行要看得见，
   但不引入第三种颜色——焦点在触发按钮上，选项本身拿不到 :focus。 */
.option.is-active {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.option.is-selected {
  background: var(--accent-alpha-15);
  color: var(--accent);
  font-weight: 500;
}

.option.is-disabled {
  opacity: 0.4;
  cursor: not-allowed;
  color: var(--text-muted);
  background: transparent;
}

.check-icon {
  font-size: 12px;
  color: var(--accent);
  margin-left: 8px;
}

.empty {
  padding: 10px 12px;
  color: var(--text-muted);
  font-size: 12px;
  text-align: center;
}

</style>
