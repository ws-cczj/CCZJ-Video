<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import { useApplicationMode } from '../modes/applicationMode'
withDefaults(defineProps<{active?:boolean;exportActionLabel?:string}>(),{active:true,exportActionLabel:'diagnostics.openReportFolder'})
const mode=useApplicationMode()
const panel=defineAsyncComponent(()=>mode.mode==='video'?import('./VideoDiagnosticsPanel.vue'):import('./ReadingDiagnosticsPanel.vue'))
</script>
<template><component :is="panel" :active="active" :export-action-label="exportActionLabel" /></template>
