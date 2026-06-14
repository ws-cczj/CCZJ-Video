<script setup lang="ts">
import { computed } from 'vue'

interface Option {
  value: string | number
  label: string
}

const props = withDefaults(defineProps<{
  modelValue: string | number
  options: Option[]
  size?: 'sm' | 'md'
}>(), {
  size: 'md',
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: string | number): void
}>()

const activeIdx = computed(() => props.options.findIndex(o => o.value === props.modelValue))
</script>

<template>
  <div class="ui-seg" :class="`ui-seg--${size}`">
    <div
      v-if="activeIdx >= 0"
      class="ui-seg__indicator"
      :style="{ transform: `translateX(${activeIdx * 100}%)`, width: `${100 / options.length}%` }"
    ></div>
    <button
      v-for="opt in options"
      :key="String(opt.value)"
      class="ui-seg__item"
      :class="{ 'ui-seg__item--active': opt.value === modelValue }"
      type="button"
      @click="emit('update:modelValue', opt.value)"
    >
      {{ opt.label }}
    </button>
  </div>
</template>

<style scoped>
.ui-seg {
  position: relative;
  display: inline-flex;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 3px;
  gap: 2px;
}

.ui-seg__indicator {
  position: absolute;
  top: 3px;
  bottom: 3px;
  left: 3px;
  background: var(--accent);
  border-radius: 7px;
  transition: transform 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  z-index: 0;
}

.ui-seg__item {
  position: relative;
  z-index: 1;
  padding: 6px 14px;
  border: none;
  border-radius: 7px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: color 0.15s ease;
  white-space: nowrap;
  font-family: inherit;
}
.ui-seg__item:hover { color: var(--text-primary); }
.ui-seg__item--active { color: var(--accent-contrast); }

.ui-seg--sm .ui-seg__item {
  padding: 5px 10px;
  font-size: 12px;
}
.ui-seg--sm { border-radius: 8px; }
.ui-seg--sm .ui-seg__indicator { border-radius: 6px; }
</style>