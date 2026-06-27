<script setup lang="ts">
import { onMounted, onBeforeUnmount, watch, ref } from 'vue'

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

const animationTypes = [
  { enter: 'modal-jack-in', leave: 'modal-slide-out-right' },
  { enter: 'modal-flip-in-x', leave: 'modal-flip-out-x' },
  { enter: 'modal-zoom-in-down', leave: 'modal-zoom-out-up' },
  { enter: 'modal-slide-in-down', leave: 'modal-slide-out-down' },
  { enter: 'modal-light-speed-in', leave: 'modal-light-speed-out' },
]

const enterClass = ref(animationTypes[0].enter)
const leaveClass = ref(animationTypes[0].leave)

function getRandomAnimation(): { enter: string; leave: string } {
  const idx = Math.floor(Math.random() * animationTypes.length)
  return animationTypes[idx]
}

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

watch(() => props.modelValue, (val) => {
  if (val) {
    const anim = getRandomAnimation()
    enterClass.value = anim.enter
    leaveClass.value = anim.leave
  }
})

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="ui-modal-fade">
      <div v-if="modelValue" class="ui-modal-overlay" @click.self="maskClosable && close()">
        <Transition :enter-active-class="enterClass" :leave-active-class="leaveClass" appear>
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
.ui-modal-fade-enter-active {
  transition: opacity 0.25s ease;
}
.ui-modal-fade-leave-active {
  transition: opacity 0.2s ease;
}
.ui-modal-fade-enter-from,
.ui-modal-fade-leave-to {
  opacity: 0;
}

/* Jack In The Box */
.modal-jack-in {
  animation: modal-jack-in 0.5s cubic-bezier(0.36, 0.07, 0.19, 0.97) both;
}
@keyframes modal-jack-in {
  0% { opacity: 0; transform: scale(0.1) rotate(30deg); }
  50% { transform: scale(1.1) rotate(-10deg); }
  70% { transform: scale(0.9) rotate(3deg); }
  100% { opacity: 1; transform: scale(1) rotate(0); }
}

/* Flip In X */
.modal-flip-in-x {
  animation: modal-flip-in-x 0.6s cubic-bezier(0.23, 1, 0.32, 1) both;
}
@keyframes modal-flip-in-x {
  0% { opacity: 0; transform: perspective(400px) rotateX(90deg); }
  40% { transform: perspective(400px) rotateX(-10deg); }
  70% { transform: perspective(400px) rotateX(10deg); }
  100% { opacity: 1; transform: perspective(400px) rotateX(0); }
}

/* Flip Out X */
.modal-flip-out-x {
  animation: modal-flip-out-x 0.4s cubic-bezier(0.23, 1, 0.32, 1) both;
}
@keyframes modal-flip-out-x {
  0% { opacity: 1; transform: perspective(400px) rotateX(0); }
  100% { opacity: 0; transform: perspective(400px) rotateX(90deg); }
}

/* Zoom In Down */
.modal-zoom-in-down {
  animation: modal-zoom-in-down 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) both;
}
@keyframes modal-zoom-in-down {
  0% { opacity: 0; transform: scale(0.8) translateY(-40px); }
  100% { opacity: 1; transform: scale(1) translateY(0); }
}

/* Zoom Out Up */
.modal-zoom-out-up {
  animation: modal-zoom-out-up 0.3s ease both;
}
@keyframes modal-zoom-out-up {
  0% { opacity: 1; transform: scale(1) translateY(0); }
  100% { opacity: 0; transform: scale(0.9) translateY(-20px); }
}

/* Slide In Down */
.modal-slide-in-down {
  animation: modal-slide-in-down 0.4s cubic-bezier(0.16, 1, 0.3, 1) both;
}
@keyframes modal-slide-in-down {
  0% { opacity: 0; transform: translateY(-40px); }
  100% { opacity: 1; transform: translateY(0); }
}

/* Slide Out Down */
.modal-slide-out-down {
  animation: modal-slide-out-down 0.3s ease both;
}
@keyframes modal-slide-out-down {
  0% { opacity: 1; transform: translateY(0); }
  100% { opacity: 0; transform: translateY(20px); }
}

/* Slide Out Right */
.modal-slide-out-right {
  animation: modal-slide-out-right 0.3s ease both;
}
@keyframes modal-slide-out-right {
  0% { opacity: 1; transform: translateX(0); }
  100% { opacity: 0; transform: translateX(30px); }
}

/* Light Speed In */
.modal-light-speed-in {
  animation: modal-light-speed-in 0.5s cubic-bezier(0.23, 1, 0.32, 1) both;
}
@keyframes modal-light-speed-in {
  0% { opacity: 0; transform: translateX(-100%) skewX(-10deg); }
  60% { opacity: 1; transform: translateX(10%) skewX(5deg); }
  80% { transform: translateX(-5%) skewX(-2deg); }
  100% { transform: translateX(0) skewX(0); }
}

/* Light Speed Out */
.modal-light-speed-out {
  animation: modal-light-speed-out 0.3s ease both;
}
@keyframes modal-light-speed-out {
  0% { opacity: 1; transform: translateX(0) skewX(0); }
  100% { opacity: 0; transform: translateX(100%) skewX(10deg); }
}
</style>