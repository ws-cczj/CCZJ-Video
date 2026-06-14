<script setup lang="ts">
defineProps<{ size?: 'sm' | 'md' | 'lg'; label?: string }>()
</script>

<template>
  <div class="spinner-wrap" :class="size || 'md'">
    <div class="spinner-ring"></div>
    <span v-if="label" class="label">{{ label }}</span>
  </div>
</template>

<style scoped>
.spinner-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 60px 20px;
  color: var(--text-muted);
  text-align: center;
}

.spinner-ring {
  border: 3px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.spinner-wrap.sm .spinner-ring {
  width: 20px;
  height: 20px;
  border-width: 2px;
}

.spinner-wrap.md .spinner-ring {
  width: 32px;
  height: 32px;
}

.spinner-wrap.lg .spinner-ring {
  width: 48px;
  height: 48px;
  border-width: 4px;
}

.spinner-wrap .label {
  font-size: 13px;
  color: var(--text-muted);
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
