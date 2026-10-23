<script setup lang="ts">
import { computed } from 'vue'
import RemoteImage from '../../components/RemoteImage.vue'
const props = defineProps<{ src?: string; title: string }>()
const tone = computed(() => [...props.title].reduce((sum, char) => sum + char.charCodeAt(0), 0) % 4)
</script>

<template>
  <div class="comic-cover-art" :data-tone="tone">
    <div class="comic-cover-placeholder" aria-hidden="true"><span>CCZJ</span><strong>{{ title }}</strong><i /></div>
    <RemoteImage v-if="src" :src="src" :alt="title" loading="lazy" fallback="" />
  </div>
</template>

<style scoped>
.comic-cover-art { position: relative; width: 100%; height: 100%; aspect-ratio: 3 / 4; @apply cczj-overflow-hidden; background: #dce5e7; color: #294951; }
.comic-cover-art[data-tone="1"] { background: #e8dacf; color: #704d3c; }
.comic-cover-art[data-tone="2"] { background: #dfe3d7; color: #4d5e46; }
.comic-cover-art[data-tone="3"] { background: #e1ddea; color: #5b4b70; }
.comic-cover-art > img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.comic-cover-placeholder { position: absolute; inset: 0; @apply cczj-flex cczj-flex-col; padding: 13%; gap: 14px; }
.comic-cover-placeholder span { font-size: 10px; letter-spacing: .16em; opacity: .65; }
.comic-cover-placeholder strong { font-size: clamp(16px, 2vw, 24px); line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; @apply cczj-overflow-hidden; overflow-wrap: anywhere; }
.comic-cover-placeholder i { margin-top: auto; width: 52%; aspect-ratio: 1; border: 1px solid currentColor; border-radius: 50%; opacity: .18; align-self: flex-end; }
</style>
