<script setup lang="ts">
import { computed, ref } from 'vue'
import { useOverlayFocus } from '../../composables/useOverlayFocus'
import { useViewActivity } from '../../composables/useViewActivity'
import MotionTransition from './MotionTransition.vue'
import Button from './Button.vue'
import { tr } from '../../locales'

const props = withDefaults(defineProps<{
  /** Keep overlays inside the active fullscreen player when requested. */
  teleportTo?: string | HTMLElement
  /** 是否显示 */
  modelValue: boolean
  /** 标题 */
  title?: string
  /** 宽度 */
  width?: string
  /** Presentation class applied to the teleported dialog surface. */
  panelClass?: string
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
  okText: '',
  cancelText: '',
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'ok'): void
  (e: 'cancel'): void
}>()
const dialog = ref<HTMLElement | null>(null)
const active = useViewActivity(() => true, 'committed')
const visible = computed(() => props.modelValue && active.value)
const focus = useOverlayFocus(dialog, () => visible.value, close, () => props.closable)

function close(): void {
  emit('update:modelValue', false)
  emit('cancel')
}

</script>

<template>
  <Teleport :to="teleportTo || 'body'">
    <MotionTransition preset="modal" @after-leave="!visible && focus.release()">
      <div v-if="visible" class="ui-modal-overlay cczj-flex cczj-items-center cczj-justify-center" @click.self="maskClosable && closable && close()">
          <div
            ref="dialog"
            data-motion-panel
            class="ui-modal cczj-min-h-0 cczj-flex cczj-flex-col"
            :class="panelClass"
            :style="width ? { width } : undefined"
            role="dialog"
            aria-modal="true"
            :aria-label="title"
            tabindex="-1"
          >
            <!-- Header -->
            <div v-if="title || closable" class="ui-modal__header cczj-flex cczj-items-center cczj-justify-between">
              <slot name="header">
                <h3 class="ui-modal__title cczj-inline-flex cczj-items-center cczj-gap-4">{{ title }}</h3>
              </slot>
              <button
                v-if="closable"
                class="ui-modal__close cczj-inline-flex cczj-items-center cczj-justify-center cczj-flex-shrink-0"
                type="button"
                @click="close"
                :aria-label="tr('common.close')"
              >
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                  <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            </div>

            <!-- Body -->
            <div class="ui-modal__body cczj-min-h-0">
              <slot />
            </div>

            <!-- Footer -->
            <div v-if="showFooter || $slots.footer" class="ui-modal__footer cczj-flex cczj-items-center cczj-justify-end cczj-gap-5">
              <slot name="footer">
                <Button @click="close">{{ cancelText || tr('common.cancel') }}</Button>
                <Button variant="primary" :loading="okLoading" :disabled="okDisabled" @click="emit('ok')">
                  {{ okText || tr('common.ok') }}
                </Button>
              </slot>
            </div>
          </div>
      </div>
    </MotionTransition>
  </Teleport>
</template>

<style scoped>
/* Overlay */
.ui-modal-overlay {
  @apply cczj-fixed;
  inset: 0;
  background: rgba(15, 18, 28, 0.55);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  z-index: var(--z-modal-overlay);
  padding: 24px;
}

/* Modal Box */
.ui-modal {
  width: auto;
  min-width: min(420px, 92vw);
  max-width: 92vw;
  max-height: 90vh;
  @apply cczj-rounded-md;
  background: var(--bg-elevated,var(--bg-card));
  @apply cczj-border cczj-overflow-hidden;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.35);
  @apply cczj-text-primary;
}

/* Header */
.ui-modal__header {
  padding: 16px 20px;
  @apply cczj-border-bottom;
}
.ui-modal__title {
  margin: 0;
  @apply cczj-text-lg cczj-font-bold;
}
.ui-modal__close {
  width: 28px;
  height: 28px;
  border: none;
  @apply cczj-rounded-sm;
  background: transparent;
  @apply cczj-text-muted cczj-cursor-pointer;
  transition: background-color var(--transition-fast), color var(--transition-fast);
}
.ui-modal__close:hover {
  background: var(--bg-hover);
  @apply cczj-text-primary;
}

/* Body */
.ui-modal__body {
  padding: 20px;
  @apply cczj-overflow-y-auto cczj-flex-1;
  /* flex 子项默认 min-height:auto，长表单下会顶破 .ui-modal 的 max-height，
     结果是页脚（保存/取消）被裁掉且滚不动。 */
}

/* Footer */
.ui-modal__footer {
  padding: 14px 20px;
  @apply cczj-border-top;
  background: var(--bg-secondary);
  border-bottom-left-radius: 8px;
  border-bottom-right-radius: 8px;
}

</style>
