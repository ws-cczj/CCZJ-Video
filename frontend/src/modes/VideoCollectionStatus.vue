<script setup lang="ts">
import { onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { tr } from '../locales'
import { useCollectStore } from '../stores/collect'
import { useSourceStore } from '../stores/source'
import { useErrorStore } from '../stores/error'

const collect = useCollectStore()
const sources = useSourceStore()
const notices = useErrorStore()
const router = useRouter()
const shown = new Map<string, { phase: string; id: string }>()

watch(() => Array.from(collect.sourceStates.values(), state => ({
  key: state.sourceKey, started: state.startTime, finished: state.lastRun.finishedAt,
  running: state.running, done: state.done, failed: !!state.error,
})), states => {
  for (const state of states) {
    if (!state.running && !state.done) continue
    const phase = `${state.started}:${state.finished}:${state.running}:${state.done}:${state.failed}`
    if (shown.get(state.key)?.phase === phase) continue
    const old = shown.get(state.key)
    if (old) notices.dismiss(old.id)
    const name = sources.sources.find(source => source.source_key === state.key)?.name || state.key
    const status = tr(state.running ? 'common.collecting' : state.failed ? 'common.failed' : 'common.done')
    const notice = notices.push({
      level: state.failed ? 'warn' : 'info', title: `${status} · ${name}`,
      message: tr('sources.collectionTip'), source: `collection.status.${state.key}`, autoDismiss: 5000,
      actionLabel: tr('sources.openCollection'),
      action: () => { void router.push({ path: '/sources', query: { source: state.key } }) },
    })
    shown.set(state.key, { phase, id: notice.id })
  }
}, { immediate: true })

onUnmounted(() => { for (const notice of shown.values()) notices.dismiss(notice.id); shown.clear() })
</script>
<template><span hidden aria-hidden="true"></span></template>
