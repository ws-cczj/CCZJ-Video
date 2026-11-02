<script setup lang="ts">
import VideoCard from './VideoCard.vue'
import VirtualGrid from './VirtualGrid.vue'
import type { Video } from '../types'

const props=defineProps<{items:Video[];catalogLabel?:string;inCatalog?:boolean}>()
const emit=defineEmits<{select:[video:Video]}>()
const itemKey=(item:Video)=>`${item.source_key||''}:${item.global_id||item.vod_id}`
</script>

<template>
  <VirtualGrid :items="props.items" :item-key="itemKey" :estimated-height="300" :initial-count="24" class="virtual-video-grid">
    <template #default="{item,index}">
      <VideoCard :video="item" :data-virtual-index="index" :in-catalog="inCatalog ?? item.in_catalog" :catalog-label="catalogLabel" @click="emit('select',item)">
        <template v-if="$slots.meta" #meta><slot name="meta" :item="item" /></template>
      </VideoCard>
    </template>
  </VirtualGrid>
</template>
