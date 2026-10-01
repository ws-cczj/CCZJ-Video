<script setup lang="ts">
import { tr } from '../../locales'
import { OpenFolder } from '../../api/app'
import Icon from '../../components/Icon.vue'
import type { ExportResult } from './useSourceDisplay'

/**
 * 导出成功后的底部提示条。导出本身由页面发起，这里只展示结果并代为打开目录。
 */
const props = defineProps<{ result: ExportResult | null }>()

const emit = defineEmits<{ dismiss: [] }>()

async function openExportFolder(): Promise<void> {
  if (!props.result) return
  try {
    await OpenFolder(props.result.path)
  } catch (e) {
    console.error('open folder failed', e)
  }
}
</script>

<template>
  <div class="export-toast">
    <Icon name="check" :size="14" />
    <span class="export-toast-text">{{ tr('sources.exported') }} <strong>{{ props.result?.sourceKey }}</strong> {{ tr('sources.exportTo') }}:</span>
    <code class="export-toast-path" :title="props.result?.path">{{ props.result?.path }}</code>
    <button class="mini-btn accent" @click="openExportFolder">
      <Icon name="folder" :size="10" /><span>{{ tr('sources.openFolder') }}</span>
    </button>
    <button class="mini-btn" @click="emit('dismiss')">
      <Icon name="x" :size="10" />
    </button>
  </div>
</template>

<style scoped>

/* === 导出成功提示条 === */
.export-toast {
  position: fixed;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 18px;
  background: var(--bg-card);
  border: 1px solid var(--accent-alpha-30);
  border-radius: 12px;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.25);
  z-index: 2000;
  color: var(--text-primary);
  font-size: 13px;
  max-width: 640px;
}
.export-toast-text {
  white-space: nowrap;
}
.export-toast-path {
  font-size: 11px;
  color: var(--accent);
  background: var(--accent-alpha-10);
  padding: 2px 8px;
  border-radius: 4px;
  max-width: 240px;
  font-family: 'SF Mono', Consolas, monospace;
}

</style>
