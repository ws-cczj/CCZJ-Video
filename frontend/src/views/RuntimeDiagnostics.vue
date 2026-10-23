<script setup lang="ts">
import { builtinDiagnosticsPanel } from '../plugins/builtinPanels'
</script>
<template><div class="cczj-runtime-diagnostics-page"><component :is="builtinDiagnosticsPanel" /></div></template>
<style scoped>
.cczj-runtime-diagnostics-page{@apply cczj-w-full cczj-min-w-0;max-width:1104px;margin:auto;padding:40px 32px 56px}
@media(max-width:700px){.cczj-runtime-diagnostics-page{padding:24px 16px 40px}}
</style>
