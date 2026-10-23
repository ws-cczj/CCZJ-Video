<script setup lang="ts">
import { computed } from 'vue'
import RemoteImage from '../../components/RemoteImage.vue'
import type { NovelBook } from '../domain'
const props = defineProps<{ book: NovelBook }>()
const tone = computed(() => [...props.book.title].reduce((sum, char) => sum + char.charCodeAt(0), 0) % 4)
</script>
<template>
  <div class="novel-cover" :data-tone="tone" aria-hidden="true">
    <div class="cover-fallback">
      <strong class="cover-title">{{ book.title }}</strong>
      <span v-if="book.author" class="cover-author">{{ book.author }}</span>
    </div>
    <RemoteImage v-if="book.cover" class="cover-image" :src="book.cover" alt="" loading="lazy" fallback="" />
  </div>
</template>
<style scoped>
.novel-cover { @apply cczj-relative; container-type: inline-size; isolation: isolate; @apply cczj-overflow-hidden; aspect-ratio: 3 / 4; @apply cczj-w-full cczj-flex-shrink-0; border-radius: 3px 5px 5px 3px; background: #e7e9df; color: #3b4c40; box-shadow: inset 3px 0 3px #00000012, 1px 3px 6px #1a100812; }
.novel-cover[data-tone="1"] { background: #eee1d4; color: #6b4b35; }
.novel-cover[data-tone="2"] { background: #e0e7e9; color: #3f5864; }
.novel-cover[data-tone="3"] { background: #e8e0e8; color: #69516d; }
.cover-image { @apply cczj-absolute; inset: 0; @apply cczj-w-full cczj-h-full cczj-object-cover; }
/* 无封面与封面加载失败共用同一个兜底：纸白底、墨色字，沿用原有封面排印。 */
.cover-fallback { @apply cczj-absolute; inset: 0; @apply cczj-flex cczj-flex-col cczj-justify-center; gap: .6em; padding: 12% 13%; border: 1px solid #27272214; }
.cover-title { display: -webkit-box; margin: 0; font-family: 'Noto Serif CJK SC','Songti SC',SimSun,serif; font-size: clamp(9px,13cqw,26px); line-height: 1.4; @apply cczj-font-semibold; letter-spacing: .06em; @apply cczj-wrap-anywhere; @apply cczj-overflow-hidden; -webkit-line-clamp: 4; -webkit-box-orient: vertical; }
.cover-author { font-size: clamp(5px,6cqw,12px); opacity: .75; @apply cczj-overflow-hidden; text-overflow: ellipsis; @apply cczj-whitespace-nowrap; }
</style>
