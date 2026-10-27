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
/* Content title and secondary date: https://www.mgtv.com/b/859271/24322442.html */
.cczj-episode-label { @apply cczj-flex cczj-flex-col cczj-min-w-0 cczj-w-full cczj-gap-2; white-space: normal; }
.cczj-episode-label-title { line-height: 1.4; overflow-wrap: anywhere; }
.cczj-episode-label-date { font-size: .8em; line-height: 1.3; opacity: .74; font-variant-numeric: tabular-nums; }
.cczj-mobile-shell .cczj-mobile-detail .episodes-grid.cczj-episode-grid-detailed,
.cczj-mobile-shell .cczj-mobile-player .ep-grid.cczj-episode-grid-detailed,
.cczj-mobile-playback-drawer .cczj-mobile-drawer-episode-grid.cczj-episode-grid-detailed { @apply cczj-grid-cols-2; }
.episodes-grid.cczj-episode-grid-detailed > .episode-btn,
.ep-grid.cczj-episode-grid-detailed > .ep-item,
.cczj-mobile-shell .cczj-episode-grid-detailed > button:is(.episode-btn,.ep-item),
.cczj-mobile-drawer-episode-grid.cczj-episode-grid-detailed > button { height: auto; min-height: 76px; padding: 16px 8px 12px; }
</style>
