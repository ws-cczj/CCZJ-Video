<script setup lang="ts">
import { ref, watch } from 'vue'
import { getProxiedImageUrl } from '../utils'
import imageNotFound from '../assets/images/image_notfound.png'

defineOptions({ inheritAttrs: false })

interface Props {
  src?: string | null
  alt?: string
  fallback?: string
}

const props = withDefaults(defineProps<Props>(), {
  src: '',
  alt: '',
  fallback: imageNotFound,
})

const resolvedSrc = ref(props.fallback)
const requestToken = ref(0)

async function resolveImage(rawSrc: string | null | undefined): Promise<void> {
  const token = ++requestToken.value
  const source = String(rawSrc || '').trim()
  resolvedSrc.value = props.fallback
  if (!source) {
    return
  }

  let nextSrc = source
  if (/^https?:\/\//i.test(source)) {
    nextSrc = await getProxiedImageUrl(source)
  }

  if (token === requestToken.value) {
    resolvedSrc.value = nextSrc || props.fallback
  }
}

function onImageError(): void {
  if (resolvedSrc.value !== props.fallback) {
    resolvedSrc.value = props.fallback
  }
}

watch(() => props.src, (value) => {
  void resolveImage(value)
}, { immediate: true })
</script>

<template>
  <img v-bind="$attrs" :src="resolvedSrc" :alt="alt" @error="onImageError" />
</template>
