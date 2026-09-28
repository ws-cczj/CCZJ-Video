<script setup lang="ts">
defineOptions({ name: 'PlayLinePicker' })
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import * as AppMod from '../api/app'
import { useErrorStore } from '../stores/error'
import { useGeneration } from '../composables/useGeneration'
import Icon from './Icon.vue'
import { humanizeBytes } from '../utils'
import { playLineLabel } from '../utils/playLines'
import type { PlayLine, PlayLineSpeedItem, PlayLineSpeedResponse } from '../types'

const props = defineProps<{
  lines: PlayLine[]
  modelValue: number
  sourceKey: string
  vodId: string
  globalId?: number
  epNum?: number
}>()

const emit = defineEmits<{ 'update:modelValue': [index: number] }>()

const { t } = useI18n()
const errorStore = useErrorStore()
const speedGen = useGeneration()

const results = ref<Record<number, PlayLineSpeedItem>>({})
const bestIndex = ref(-2)
const testing = ref(false)

// 换影片后线路下标指向的是另一批地址，旧的测速结果挂在新芯片上会是假数据。
watch(() => props.lines, () => {
  speedGen.invalidate()
  testing.value = false
  results.value = {}
  bestIndex.value = -2
})

const showPicker = computed(() => props.lines.length > 1)

function itemFor(line: PlayLine): PlayLineSpeedItem | undefined {
  return results.value[line.index]
}

function chipName(line: PlayLine, i: number): string {
  return playLineLabel(line, t('playLine.ordinal', { n: i + 1 }))
}

function speedText(line: PlayLine): string {
  const item = itemFor(line)
  if (!item) return ''
  if (!item.ok) return t('playLine.unavailable')
  if (item.bytes_per_sec > 0) return `${humanizeBytes(item.bytes_per_sec)}/s`
  // bytes_per_sec 为 0 表示这次下载快过本机时钟精度、量不出吞吐，不是慢线路。
  return item.latency_ms > 0 ? `${item.latency_ms} ms` : t('playLine.available')
}

function isDead(line: PlayLine): boolean {
  const item = itemFor(line)
  return !!item && !item.ok
}

function choose(i: number): void {
  if (i !== props.modelValue) emit('update:modelValue', i)
}

async function runSpeedTest(): Promise<void> {
  if (testing.value || !props.sourceKey) return
  const my = speedGen.begin()
  testing.value = true
  try {
    const resp = (await (AppMod as any).SpeedTestPlayLines({
      source_key: props.sourceKey,
      vod_id: props.vodId,
      global_id: props.globalId ?? 0,
      ep_num: props.epNum ?? 0,
    })) as PlayLineSpeedResponse | null
    if (!speedGen.isCurrent(my)) return
    const items: Record<number, PlayLineSpeedItem> = {}
    for (const item of resp?.items || []) items[Number(item.index)] = item
    results.value = items
    bestIndex.value = Number(resp?.best_index ?? -1)
    if (bestIndex.value < 0) errorStore.warn(t('playLine.allFailed'))
  } catch (err) {
    if (!speedGen.isCurrent(my)) return
    errorStore.fromError(t('playLine.testFailed'), err, 'PlayLinePicker')
  } finally {
    if (speedGen.isCurrent(my)) testing.value = false
  }
}
</script>

<template>
  <div v-if="showPicker" class="line-picker cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-2">
    <span class="line-picker-title cczj-text-sm cczj-font-medium cczj-text-muted">{{ t('playLine.title') }}</span>
    <div class="line-chips cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-2 cczj-flex-1">
      <button v-for="(line, i) in lines" :key="`line-${line.index}`" type="button" class="line-chip"
        :class="{ active: i === modelValue, dead: isDead(line) }" :disabled="testing" @click="choose(i)"
        :title="chipName(line, i) + (speedText(line) ? ` · ${speedText(line)}` : '')">
        <span class="line-chip-name cczj-truncate">{{ chipName(line, i) }}</span>
        <span v-if="speedText(line)" class="line-chip-speed">{{ speedText(line) }}</span>
        <span v-if="line.index === bestIndex" class="line-chip-best">
          <Icon name="check" :size="10" />
          <span>{{ t('playLine.fastest') }}</span>
        </span>
      </button>
    </div>
    <button type="button" class="line-test-btn" :disabled="testing || !sourceKey" @click="runSpeedTest"
      :title="t('playLine.speedTestTip')">
      <Icon name="refresh" :size="12" :class="{ 'line-test-spin': testing }" />
      <span>{{ testing ? t('playLine.testing') : t('playLine.speedTest') }}</span>
    </button>
  </div>
</template>

<style scoped>
.line-picker {
  gap: 8px;
}

.line-chips {
  gap: 8px;
}

.line-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 220px;
  padding: 4px 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.4;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
}

.line-chip:hover:not(:disabled) {
  border-color: var(--border-strong);
  color: var(--text-primary);
}

.line-chip.active {
  border-color: var(--accent);
  background: var(--bg-hover);
  color: var(--text-primary);
}

.line-chip.dead {
  opacity: 0.6;
}

.line-chip.dead .line-chip-speed {
  color: var(--danger);
}

.line-chip-name {
  min-width: 0;
}

.line-chip-speed {
  flex-shrink: 0;
  color: var(--text-muted);
  font-size: 11px;
}

.line-chip-best {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
  color: var(--accent);
  font-size: 11px;
}

.line-test-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease;
}

.line-test-btn:hover:not(:disabled) {
  border-color: var(--accent);
  color: var(--accent);
}

.line-test-btn:disabled {
  opacity: 0.6;
  cursor: default;
}

.line-test-spin {
  animation: line-test-rotate 1s linear infinite;
}

@keyframes line-test-rotate {
  to {
    transform: rotate(360deg);
  }
}
</style>
