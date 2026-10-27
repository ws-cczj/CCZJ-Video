<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '../utils/markdown'

const props = defineProps<{ content: string }>()
const rendered = computed(() => renderMarkdown(props.content))
</script>

<template><div class="markdown-content" v-html="rendered"></div></template>

<style scoped>
.markdown-content { overflow-wrap: anywhere; line-height: 1.65; }
.markdown-content :deep(> :first-child) { margin-top: 0; }
.markdown-content :deep(> :last-child) { margin-bottom: 0; }
.markdown-content :deep(h1),.markdown-content :deep(h2),.markdown-content :deep(h3),.markdown-content :deep(h4),.markdown-content :deep(h5),.markdown-content :deep(h6) { margin: 16px 0 8px; font-size: 1em; line-height: 1.4; color: var(--text-primary); }
.markdown-content :deep(h1) { font-size: 1.2em; }
.markdown-content :deep(h2) { font-size: 1.1em; }
.markdown-content :deep(p),.markdown-content :deep(ul),.markdown-content :deep(ol) { margin: 8px 0; }
.markdown-content :deep(ul),.markdown-content :deep(ol) { padding-left: 24px; }
.markdown-content :deep(li + li) { margin-top: 4px; }
.markdown-content :deep(a) { color: var(--accent); text-decoration: underline; }
.markdown-content :deep(blockquote) { margin: 12px 0; padding-left: 12px; border-left: 3px solid var(--border-strong); color: var(--text-muted); }
.markdown-content :deep(code) { font-family: ui-monospace, Menlo, Consolas, monospace; font-size: .9em; background: var(--bg-hover); border-radius: var(--radius-sm); padding: 1px 4px; }
.markdown-content :deep(pre) { @apply cczj-overflow-x-auto; margin: 12px 0; padding: 10px; background: var(--bg-card); border-radius: var(--radius-sm); }
.markdown-content :deep(pre code) { padding: 0; background: transparent; white-space: pre; }
.markdown-content :deep(table) { @apply cczj-block cczj-overflow-x-auto; max-width: 100%; border-collapse: collapse; margin: 12px 0; }
.markdown-content :deep(th),.markdown-content :deep(td) { border: 1px solid var(--border); padding: 6px 10px; }
.markdown-content :deep(img) { max-width: 100%; height: auto; }
.markdown-content :deep(hr) { border: 0; border-top: 1px solid var(--border); margin: 16px 0; }
</style>
