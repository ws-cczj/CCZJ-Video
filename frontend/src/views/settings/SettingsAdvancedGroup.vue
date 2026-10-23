<script setup lang="ts">
defineOptions({ name: 'SettingsAdvancedGroup' })
import DataBackupPanel from '../../components/DataBackupPanel.vue'
import DoubanQueuePanel from '../../components/DoubanQueuePanel.vue'
import NetworkPolicyPanel from '../../components/NetworkPolicyPanel.vue'
</script>
<template>
  <div class="panel advanced-settings cczj-grid cczj-gap-8">
    <NetworkPolicyPanel />
    <DataBackupPanel />
    <DoubanQueuePanel />
  </div>
</template>
<style scoped>
.advanced-settings { grid-template-columns:minmax(0,1fr); }
.advanced-settings :deep(.block),.advanced-settings :deep(.adv-card) { padding:22px 24px; border:1px solid var(--border);border-radius:14px;background:var(--bg-card);@apply cczj-min-w-0; }
.advanced-settings :deep(.block-hd) { @apply cczj-gap-6 cczj-flex-wrap; margin-bottom:12px; }
.advanced-settings :deep(.block-hd h3) { margin:0; }
.advanced-settings :deep(.desc) { margin-bottom:14px;max-width:78ch;line-height:1.8; }
@media(max-width:640px){.advanced-settings :deep(.block),.advanced-settings :deep(.adv-card){padding:18px 16px;}.advanced-settings :deep(.archive-row){@apply cczj-flex-wrap;}}
</style>
