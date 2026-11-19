<script setup lang="ts">
defineProps<{ modelValue: boolean; ariaLabel: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>

<template>
  <label class="ui-switch">
    <input
      type="checkbox"
      role="switch"
      :checked="modelValue"
      :disabled="disabled"
      :aria-label="ariaLabel"
      @change="emit('update:modelValue', ($event.target as HTMLInputElement).checked)"
    />
    <span class="ui-switch__track" aria-hidden="true" />
  </label>
</template>

<style scoped>
.ui-switch {
  @apply cczj-relative cczj-grid cczj-flex-shrink-0;
  place-items: center;
  width: 44px;
  height: 44px;
}

.ui-switch input {
  @apply cczj-absolute cczj-inset-0 cczj-opacity-0 cczj-w-full cczj-h-full cczj-cursor-pointer;
  margin: 0;
}

.ui-switch__track {
  @apply cczj-relative cczj-rounded-full cczj-border-strong cczj-bg-secondary cczj-pointer-events-none;
  width: 38px;
  height: 22px;
  transition: background-color var(--cczj-motion-fast), border-color var(--cczj-motion-fast);
}

.ui-switch__track::before {
  content: '';
  @apply cczj-absolute cczj-rounded-full;
  inset: 3px auto auto 3px;
  width: 14px;
  height: 14px;
  background: var(--text-secondary);
  transition: transform var(--cczj-motion-fast);
}

.ui-switch input:checked + .ui-switch__track {
  border-color: var(--accent);
  background: var(--accent);
}

.ui-switch input:checked + .ui-switch__track::before {
  transform: translateX(16px);
  background: var(--accent-contrast);
}

.ui-switch input:focus-visible + .ui-switch__track {
  outline: 2px solid var(--accent);
  outline-offset: 3px;
}

.ui-switch input:disabled { @apply cczj-cursor-default; }
.ui-switch input:disabled + .ui-switch__track { opacity: .55; }

@media (forced-colors: active) {
  .ui-switch__track { forced-color-adjust: none; border-color: ButtonText; background: ButtonFace; }
  .ui-switch__track::before { background: ButtonText; }
  .ui-switch input:checked + .ui-switch__track { border-color: Highlight; }
  .ui-switch input:checked + .ui-switch__track::before { background: Highlight; }
  .ui-switch input:focus-visible + .ui-switch__track { outline-color: Highlight; }
}
</style>
