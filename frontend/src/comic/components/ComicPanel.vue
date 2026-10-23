<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import RemoteImage from '../../components/RemoteImage.vue'
import Icon from '../../components/Icon.vue'
import type { ComicPageStatus } from '../domain'

const props = withDefaults(defineProps<{
  url: string
  alt: string
  status: ComicPageStatus
  /** 在预取窗口内才挂 <img>；窗口外的位图要能释放掉。 */
  active: boolean
  number: number
  /** 量到一次的真实宽高比，用来在位图释放后撑住格子高度。 */
  ratio?: number
  nonce?: number
  generation?: number
  mode?: 'strip' | 'page'
}>(), { ratio: 0, nonce: 0, generation: 0, mode: 'strip' })

const emit = defineEmits<{
  (event: 'ready', generation: number, ratio: number): void
  (event: 'failed', generation: number): void
  (event: 'retry'): void
}>()

const { t } = useI18n()
// 这里必须 v-if 而不是 v-show：卸载 <img> 才是"取消在途请求 + 释放位图"这件事本身。
const mounted = computed(() => props.active && (props.status === 'loading' || props.status === 'ready'))
const artStyle = computed(() => props.mode === 'strip' && props.ratio > 0 ? { aspectRatio: String(props.ratio) } : undefined)
const expected = computed(() => props.url ? new URL(props.url, document.baseURI).href : '')

function onLoad(event: Event): void {
  const image = event.target as HTMLImageElement
  // RemoteImage 加载失败时会换上兜底图，那一次的 load 不能当成这一页就绪。
  if (!expected.value || image.currentSrc !== expected.value) return
  emit('ready', props.generation, image.naturalWidth > 0 && image.naturalHeight > 0 ? image.naturalWidth / image.naturalHeight : 0)
}

function onError(event: Event): void {
  const image = event.target as HTMLImageElement
  if (expected.value && image.currentSrc && image.currentSrc !== expected.value) return
  emit('failed', props.generation)
}
</script>
<template>
  <article class="comic-panel">
    <div class="comic-panel-art" :style="artStyle">
      <RemoteImage v-if="mounted" :animate="false" :key="`${generation}-${nonce}`" :src="url" :alt="alt" class="cczj-w-full cczj-h-full cczj-block" @load="onLoad" @error="onError" />
      <span class="comic-panel-number">{{ number.toString().padStart(2, '0') }}</span>
      <div v-show="status === 'loading'" class="comic-panel-status"><Icon name="image" :size="26" /><span>{{ t('common.loading') }}</span></div>
      <div v-show="status === 'error'" class="comic-panel-status">
        <Icon name="alert-triangle" :size="26" />
        <span>{{ t('comic.reader.pageFailed') }}</span>
        <button type="button" @click.stop="emit('retry')"><Icon name="refresh" :size="16" />{{ t('common.retry') }}</button>
      </div>
    </div>
  </article>
</template>
