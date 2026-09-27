import { defineStore } from 'pinia'
import { ref } from 'vue'
import { tr } from '../locales'

export interface ConfirmPayload {
  title?: string
  message: string
  okText?: string
  cancelText?: string
  level?: 'info' | 'warn' | 'danger'
}

interface ActiveConfirm extends ConfirmPayload {
  resolve: (ok: boolean) => void
}

export const useConfirmStore = defineStore('confirm', () => {
  const active = ref<ActiveConfirm | null>(null)

  function confirm(p: ConfirmPayload): Promise<boolean> {
    return new Promise<boolean>((resolve) => {
      // 如果已有弹窗，先自动取消旧的
      if (active.value) {
        active.value.resolve(false)
      }
      active.value = {
        title: p.title || tr('confirm.title'),
        message: p.message || '',
        okText: p.okText || tr('common.confirm'),
        cancelText: p.cancelText || tr('common.cancel'),
        level: p.level || 'warn',
        resolve,
      }
    })
  }

  function handle(value: boolean): void {
    if (active.value) {
      const r = active.value.resolve
      active.value = null
      r(value)
    }
  }

  return { active, confirm, handle }
})
