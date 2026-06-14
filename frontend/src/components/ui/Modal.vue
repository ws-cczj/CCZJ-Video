<script setup lang="ts">
import { onMounted, onBeforeUnmount, watch } from 'vue'

const props = withDefaults(defineProps<{
  /** 是否显示 */
  modelValue: boolean
  /** 标题 */
  title?: string
  /** 宽度 */
  width?: string
  /** 是否显示关闭按钮 */
  closable?: boolean
  /** 点击遮罩层是否关闭 */
  maskClosable?: boolean
  /** 底部是否显示取消/确定按钮 */
  showFooter?: boolean
  /** 确定按钮文字 */
  okText?: string
  /** 取消按钮文字 */
  cancelText?: string
  /** 确定按钮 loading */
  okLoading?: boolean
  /** 禁用确定按钮 */
  okDisabled?: boolean
}>(), {
  closable: true,
  maskClosable: true,
  showFooter: false,
  okText: '确定',
  cancelText: '取消',
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'ok'): void
  (e: 'cancel'): void
}>()

function close(): void {
  emit('update:modelValue', false)
  emit('cancel')
}

function onKeydown(e: KeyboardEvent): void {
  if (!props.modelValue) return
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="ui-modal-fade">
      <div v-if="modelValue" class="ui-modal-overlay" @click.self="maskClosable && close()">
        <Transition name="ui-modal-scale" appear>
          <div
            v-if="modelValue"
            class="ui-modal"
            :style="width ? { width } : undefined"
            role="dialog"
            aria-modal="true"
            :aria-label="title"
          >
            <!-- Header -->
            <div v-if="title || closable" class="ui-modal__header">
              <slot name="header">
                <h3 class="ui-modal__title">{{ title }}</h3>
              </slot>
              <button
                v-if="closable"
                class="ui-modal__close"
                type="button"
                @click="close"
                aria-label="关闭"
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            </div>

            <!-- Body -->
            <div class="ui-modal__body">
              <slot />
            </div>

            <!-- Footer -->
            <div v-if="showFooter" class="ui-modal__footer">
              <slot name="footer">
                <button class="ui-btn ui-btn--secondary ui-btn--md" @click="close">{{ cancelText }}</button>
                <button
                  class="ui-btn ui-btn--primary ui-btn--md"
                  :disabled="okDisabled"
                  @click="emit('ok')"
                >
                  <span v-if="okLoading" class="ui-btn__spinner"></span>
                  <span v-else>{{ okText }}</span>
                </button>
              </slot>
            </div>
          </div>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* Overlay */
.ui-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 18, 28, 0.55);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: var(--z-modal-overlay);
  padding: 24px;
}

/* Modal Box */
.ui-modal {
  width: auto;
  min-width: min(420px, 92vw);
  max-width: 92vw;
  min-height: 0;
  max-height: 90vh;
  border-radius: 8px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.35);
  color: var(--text-primary);
}

/* Header */
.ui-modal__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
}
.ui-modal__title {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.ui-modal__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.ui-modal__close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* Body */
.ui-modal__body {
  padding: 20px;
  overflow-y: auto;
  flex: 1;
}

/* Footer */
.ui-modal__footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  background: var(--bg-secondary);
  border-bottom-left-radius: 8px;
  border-bottom-right-radius: 8px;
}

/* Reuse button styles (minimal dup to keep modal self-contained) */
.ui-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid transparent;
  border-radius: 4px;
  cursor: pointer;
  font-family: inherit;
  font-weight: 600;
  font-size: 13px;
  line-height: 1;
  white-space: nowrap;
  transition: all 0.15s ease;
}
.ui-btn--md { padding: 8px 16px; }
.ui-btn--primary {
  background: var(--accent);
  color: var(--accent-contrast);
  border-color: var(--accent);
}
.ui-btn--primary:hover:not(:disabled) {
  background: var(--accent-dim);
  transform: translateY(-1px);
}
.ui-btn--primary:disabled { opacity: 0.5; cursor: not-allowed; }
.ui-btn--secondary {
  background: var(--bg-secondary);
  color: var(--text-secondary);
  border-color: var(--border);
}
.ui-btn--secondary:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ui-btn__spinner {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid currentColor;
  border-top-color: transparent;
  border-radius: 50%;
  animation: ui-spin 0.6s linear infinite;
}
@keyframes ui-spin {
  to { transform: rotate(360deg); }
}

/* Transitions */
.ui-modal-fade-enter-active,
.ui-modal-fade-leave-active {
  transition: opacity 0.2s ease;
}
.ui-modal-fade-enter-from,
.ui-modal-fade-leave-to {
  opacity: 0;
}

.ui-modal-scale-enter-active {
  transition: opacity 0.2s ease, transform 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}
.ui-modal-scale-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.ui-modal-scale-enter-from {
  opacity: 0;
  transform: scale(0.96) translateY(8px);
}
.ui-modal-scale-leave-to {
  opacity: 0;
  transform: scale(0.96);
}
</style>