<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '../../components/ui'
import Icon from '../../components/Icon.vue'
import type { Episode } from '../../types'

const props = defineProps<{ episodes: Episode[]; current: number; open: boolean; progress: (index: number) => number }>()
const emit = defineEmits<{ select: [index: number] }>()
const { t } = useI18n()
const descending = ref(false), group = ref(0), root = ref<HTMLElement>()
const groups = computed(() => {
  const rows = Array.from({ length: Math.ceil(props.episodes.length / 20) }, (_, index) => ({
    index, label: `${props.episodes[index * 20]!.ep_num}–${props.episodes[Math.min(props.episodes.length, (index + 1) * 20) - 1]!.ep_num}`,
  }))
  return descending.value ? rows.reverse() : rows
})
const visible = computed(() => {
  const rows = props.episodes.map((episode, index) => ({ episode, index })).slice(group.value * 20, (group.value + 1) * 20)
  return descending.value ? rows.reverse() : rows
})
function label(episode: Episode): string { return episode.ep_name || t('detail.episode', { num: episode.ep_num }) }
function caption(episode: Episode): string {
  const name = episode.ep_name?.trim() || ''
  if (/^第\s*\d+\s*集$/.test(name) || /^Episode\s+\d+$/i.test(name)) return ''
  return name.replace(/^第\s*\d+\s*集[·：:\s]+/, '')
}
async function locate(): Promise<void> {
  group.value = Math.floor(props.current / 20)
  await nextTick()
  const range = root.value?.querySelector<HTMLElement>('.drama-range[aria-pressed="true"]')
  const ranges = range?.parentElement
  if (range && ranges) ranges.scrollLeft += range.getBoundingClientRect().left - ranges.getBoundingClientRect().left - (ranges.clientWidth - range.offsetWidth) / 2
  root.value?.querySelector<HTMLElement>('[aria-current="true"]')?.scrollIntoView({block:'nearest',inline:'nearest'})
}
watch(() => props.open, open => { if (open) void locate() }, { immediate: true })
watch(() => props.current, () => { if (props.open) void locate() })
</script>

<template>
  <div ref="root" class="drama-catalog cczj-flex cczj-flex-col cczj-flex-1 cczj-min-h-0 cczj-overflow-hidden cczj-gap-6">
    <div class="cczj-flex cczj-flex-shrink-0 cczj-items-center cczj-justify-between cczj-gap-4">
      <span class="drama-catalog-count">{{ t('shortDrama.episodeCount', { count: episodes.length }) }}</span>
      <div class="cczj-flex cczj-items-center cczj-gap-2">
        <Button size="sm" variant="text" @click="locate">{{ t('shortDrama.locate') }}</Button>
        <Button size="sm" variant="text" :aria-pressed="descending" @click="descending = !descending"><Icon name="list" :size="15" />{{ t(descending ? 'shortDrama.descending' : 'shortDrama.ascending') }}</Button>
      </div>
    </div>
    <p v-if="episodes[current] && caption(episodes[current]!)" class="drama-current cczj-flex-shrink-0 cczj-line-clamp-2">{{ t('shortDrama.playing') }} · {{ label(episodes[current]!) }}</p>
    <nav class="drama-ranges cczj-flex cczj-gap-3 cczj-overflow-x-auto" :aria-label="t('shortDrama.episodeRanges')">
      <Button v-for="range in groups" :key="range.index" class="drama-range" size="sm" variant="text" :aria-pressed="range.index === group" @click="group = range.index">{{ range.label }}</Button>
    </nav>
    <div class="drama-episode-grid cczj-grid cczj-gap-4 cczj-min-h-0 cczj-overflow-y-auto">
      <Button v-for="{episode:ep,index} in visible" :key="index" class="drama-episode cczj-relative" variant="text" :aria-current="index === current ? 'true' : undefined" :aria-label="label(ep)" :title="label(ep)" @click="emit('select',index)">
        <span class="drama-episode-number">{{ ep.ep_num }}</span>
        <span v-if="caption(ep)" class="drama-episode-name cczj-line-clamp-2">{{ caption(ep) }}</span>
        <span v-if="index === current" class="drama-current-dot cczj-absolute" aria-hidden="true" />
        <span v-if="index === current" class="cczj-sr-only">{{ t('shortDrama.playing') }}</span>
        <Icon v-else-if="progress(index) > 0" class="drama-watched cczj-absolute" name="check" :size="11" />
      </Button>
    </div>
  </div>
</template>

<style scoped>
.drama-catalog-count { color: var(--text-secondary); font-size: 13px; }
.drama-current { margin: 0; color: var(--text-secondary); font-size: 13px; line-height: 1.5; }
.drama-ranges { @apply cczj-flex-shrink-0; border-bottom: 1px solid #ffffff0c; scrollbar-width: none; }
.drama-range { @apply cczj-flex-shrink-0 cczj-relative; min-height: 44px; padding: 8px 12px; font-size: 12px; font-weight: 400; border-radius: 0; }
.drama-range[aria-pressed=true] { color: var(--accent); }
.drama-range[aria-pressed=true]::after { content: ''; @apply cczj-absolute; height: 2px; left: 12px; right: 12px; bottom: 0; background: currentColor; border-radius: 2px; }
.drama-episode-grid { grid-template-columns: repeat(auto-fit,minmax(56px,1fr)); grid-auto-rows: max-content; align-content: start; scrollbar-width: none; }
.drama-episode-grid::-webkit-scrollbar { @apply cczj-hidden; }
.drama-episode { @apply cczj-min-w-0; min-height: 56px; padding: 12px 6px; border-radius: 8px; background: #ffffff06; border-color: #ffffff05; color: #d2d2d8; font-weight: 400; }
.drama-episode[aria-current=true] { color: var(--accent); background: var(--accent-alpha-10); border-color: var(--accent-alpha-30); }
.drama-episode :deep(.ui-btn__content) { @apply cczj-flex-col cczj-min-w-0 cczj-w-full; gap: 6px; }
.drama-episode-number { font-size: 16px; font-variant-numeric: tabular-nums; }
.drama-episode-name { @apply cczj-max-w-full; font-size: 12px; line-height: 1.45; }
.drama-episode-name { white-space: normal; overflow-wrap: anywhere; }
.drama-current-dot { top: 6px; right: 6px; width: 4px; height: 4px; border-radius: 50%; background: currentColor; }
.drama-watched { top: 5px; right: 5px; color: #ffffff5c; }
</style>
