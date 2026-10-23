<script setup lang="ts">
import { tr } from '../../locales'
import Icon from '../../components/Icon.vue'
import { Spinner as LoadingSpinner } from '../../components/ui'
import type { ParamsDoc } from './useSourceDisplay'

/**
 * 卡片展开区的参数指南。
 * 文档缓存在页面那一份 paramsDoc 上（沿用拆分前的语义：一次只有一份展开），
 * 这里只负责展开它、显示它和复制示例。
 */
defineProps<{ sourceKey: string; apiUrl: string; doc: ParamsDoc | null; docLoading: boolean }>()

const emit = defineEmits<{ toggle: [] }>()

function copyText(text: string, label = '已复制'): void {
  if (!text) return
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(() => { console.log(label + ': ' + text) }).catch(() => { fallbackCopy(text) })
  } else {
    fallbackCopy(text)
  }
}
function fallbackCopy(text: string): void {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  try { document.execCommand('copy') } catch (e) { console.warn('copy failed', e) }
  document.body.removeChild(ta)
}
</script>

<template>
  <!-- 参数指南 -->
  <div class="params-section">
    <button class="params-toggle" @click="emit('toggle')">
      <Icon name="info" :size="12" />
      <span>{{ doc ? tr('sources.collapseParamsGuide') : tr('sources.viewParamsGuide') }}</span>
    </button>
    <div v-if="doc" class="params-content">
      <div v-if="docLoading" class="params-loading"><LoadingSpinner :label="tr('sources.loadingParams')" /></div>
      <template v-else>
        <div class="param-block">
          <div class="param-block-header">
            <span class="param-block-title">{{ tr('sources.url') }}</span>
            <button class="copy-btn" @click="copyText(doc.base_url)">
              <Icon name="copy" :size="11" /><span>{{ tr('common.copy') }}</span>
            </button>
          </div>
          <code class="params-code">{{ doc.base_url }}</code>
        </div>
        <div v-if="doc.query_ac && doc.query_ac.length > 0" class="param-block">
          <div class="param-block-header"><span class="param-block-title">{{ tr('sources.acTypes') }}</span></div>
          <div v-for="(item, idx) in doc.query_ac" :key="'ac'+idx" class="param-row">
            <code class="param-name">{{ item.name }}</code>
            <span class="param-desc">{{ item.desc }}</span>
            <button class="copy-btn-sm" @click="copyText(item.example)">{{ tr('common.copy') }}</button>
          </div>
        </div>
        <div v-if="doc.query_common && doc.query_common.length > 0" class="param-block">
          <div class="param-block-header"><span class="param-block-title">{{ tr('sources.commonParams') }}</span></div>
          <div v-for="(item, idx) in doc.query_common" :key="'qc'+idx" class="param-row">
            <code class="param-name">{{ item.name }}</code>
            <span class="param-desc">{{ item.desc }}</span>
            <button class="copy-btn-sm" @click="copyText(item.example)">{{ tr('common.copy') }}</button>
          </div>
        </div>
        <div v-if="doc.query_advanced && doc.query_advanced.length > 0" class="param-block">
          <div class="param-block-header"><span class="param-block-title">{{ tr('sources.advancedParams') }}</span></div>
          <div v-for="(item, idx) in doc.query_advanced" :key="'qa'+idx" class="param-row">
            <code class="param-name">{{ item.name }}</code>
            <span class="param-desc">{{ item.desc }}</span>
            <button class="copy-btn-sm" @click="copyText(item.example)">{{ tr('common.copy') }}</button>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>

/* === 参数指南 === */
.params-section {
  border-top: 1px solid rgba(255,255,255,0.06);
  padding-top: 10px;
}
.params-toggle {
  @apply cczj-inline-flex cczj-items-center cczj-gap-3;
  background: transparent; border: none;
  @apply cczj-text-muted cczj-cursor-pointer cczj-text-sm;
  padding: 4px 0;
  transition: color 0.15s ease;
}
.params-toggle:hover { @apply cczj-text-accent; }
.params-content {
  @apply cczj-mt-5 cczj-flex cczj-flex-col cczj-gap-5;
}
.params-loading { @apply cczj-p-8 cczj-text-center; }
.param-block {
  background: rgba(255,255,255,0.03);
  border: 1px solid rgba(255,255,255,0.06);
  @apply cczj-rounded-md; padding: 10px 12px;
}
.param-block-header {
  @apply cczj-flex cczj-items-center cczj-justify-between cczj-mb-3;
}
.param-block-title {
  @apply cczj-text-11 cczj-font-semibold cczj-text-accent;
  text-transform: uppercase; letter-spacing: 0.3px;
}
.params-code {
  @apply cczj-block; padding: 6px 10px;
  background: rgba(255,255,255,0.05); @apply cczj-rounded;
  font-family: 'SF Mono', Consolas, monospace; @apply cczj-text-11 cczj-text-primary cczj-break-all; line-height: 1.6;
  @apply cczj-border;
}
.param-row {
  @apply cczj-flex cczj-items-center cczj-gap-4;
  padding: 5px 0;
  border-bottom: 1px dashed rgba(255,255,255,0.06);
}
.param-row:last-child { border-bottom: none; }
.param-name {
  @apply cczj-flex-shrink-0; padding: 2px 8px;
  background: var(--accent-alpha-10); @apply cczj-text-accent cczj-rounded cczj-text-11 cczj-font-semibold;
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 70px; @apply cczj-text-center;
}
.param-desc { @apply cczj-flex-1 cczj-text-11 cczj-text-secondary; }
.copy-btn {
  @apply cczj-inline-flex cczj-items-center cczj-gap-2;
  padding: 4px 10px; @apply cczj-rounded;
  border: 1px solid var(--accent-alpha-35);
  background: var(--accent-alpha-10); @apply cczj-text-accent cczj-text-11 cczj-font-medium cczj-cursor-pointer cczj-transition-fast cczj-flex-shrink-0;
}
.copy-btn:hover { background: var(--accent); color: var(--accent-contrast); }
.copy-btn-sm {
  @apply cczj-px-5-py-2 cczj-rounded cczj-border;
  background: transparent; @apply cczj-text-muted cczj-text-11 cczj-cursor-pointer cczj-transition-fast cczj-flex-shrink-0;
}
.copy-btn-sm:hover { @apply cczj-border-primary cczj-text-accent; background: var(--accent-alpha-10); }

</style>
