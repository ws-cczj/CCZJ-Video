<script setup lang="ts">
import { defineAsyncComponent, onMounted } from 'vue'
import BackToTop from '../components/BackToTop.vue'
import { useDownloadStore } from '../stores/download'
import { presentationProfile } from '../platform/presentation'
import VideoCollectionStatus from './VideoCollectionStatus.vue'

const profile = presentationProfile()
const shell = defineAsyncComponent(profile.ui === 'mobile'
  ? () => import('../mobile/CczjMobileShell.vue')
  : () => import('./DesktopVideoShell.vue'))
const downloads = useDownloadStore()
onMounted(() => { void downloads.init() })
</script>
<template>
  <component :is="shell" />
  <VideoCollectionStatus />
  <BackToTop />
</template>
