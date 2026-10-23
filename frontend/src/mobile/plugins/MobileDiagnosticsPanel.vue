<script setup lang="ts">
import DiagnosticsPanel from '../../components/DiagnosticsPanel.vue'
withDefaults(defineProps<{active?:boolean}>(),{active:true})
</script>
<template><DiagnosticsPanel class="cczj-mobile-plugin-diagnostics" :active="active" export-action-label="diagnostics.shareExport" /></template>
<style src="./diagnostics.css"></style>
