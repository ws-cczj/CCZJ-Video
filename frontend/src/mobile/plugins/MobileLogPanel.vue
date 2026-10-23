<script setup lang="ts">
import LogPanel from '../../components/LogPanel.vue'
withDefaults(defineProps<{active?:boolean}>(),{active:true})
</script>
<template><LogPanel class="cczj-mobile-plugin-logs" :active="active" :row-height="76" :local-file-actions="false" detail-mode="sheet" export-action-label="diagnostics.shareExport" /></template>
<style src="./logs.css"></style>
