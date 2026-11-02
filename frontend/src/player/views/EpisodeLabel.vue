<script setup lang="ts">
import { computed } from 'vue'
import { splitEpisodeLabel } from '../videoInfoFormat'

const props = defineProps<{ label: string }>()
const display = computed(() => splitEpisodeLabel(props.label))
</script>

<template>
  <span class="cczj-episode-label">
    <span class="cczj-episode-label-title">{{ display.title }}</span>
    <time v-if="display.date" class="cczj-episode-label-date" :datetime="display.date">{{ display.date }}</time>
  </span>
</template>

<style>
.cczj-episode-label { @apply cczj-flex cczj-flex-col cczj-min-w-0 cczj-w-full; gap: 2px; white-space: normal; }
.cczj-episode-label-title { line-height: 1.4; overflow-wrap: anywhere; }
.cczj-episode-label-date { font-size: .8em; line-height: 1.3; opacity: .74; font-variant-numeric: tabular-nums; }
.cczj-mobile-shell .cczj-mobile-detail .episodes-grid.cczj-episode-grid-detailed,
.cczj-mobile-shell .cczj-mobile-player .ep-grid.cczj-episode-grid-detailed,
.cczj-mobile-playback-drawer .cczj-mobile-drawer-episode-grid.cczj-episode-grid-detailed { grid-template-columns: repeat(3,minmax(0,1fr)); }
.episodes-grid.cczj-episode-grid-detailed > .episode-btn,
.ep-grid.cczj-episode-grid-detailed > .ep-item,
.cczj-mobile-shell .cczj-episode-grid-detailed > button:is(.episode-btn,.ep-item),
.cczj-mobile-drawer-episode-grid.cczj-episode-grid-detailed > button { height: auto; min-height: 44px; padding: 4px 6px; }
.cczj-mobile-shell .cczj-episode-grid-detailed > button:is(.episode-btn,.ep-item),
.cczj-mobile-drawer-episode-grid.cczj-episode-grid-detailed > button { padding: 2px 4px; }
</style>
