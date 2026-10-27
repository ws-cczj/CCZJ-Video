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
  wrap?: boolean
}>(), {
  size: 'md',
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: string | number): void
}>()

const activeIdx = computed(() => props.options.findIndex(o => o.value === props.modelValue))
</script>

<template>
  <div class="ui-seg cczj-gap-1" :class="[`ui-seg--${size}`, { 'ui-seg--wrap': wrap }]">
    <div
      v-if="activeIdx >= 0 && !wrap"
      class="ui-seg__indicator"
      :style="{ transform: `translateX(calc(${activeIdx * 100}% + ${activeIdx * 2}px))`, width: `calc(${100 / options.length}% - ${2 + 4 / options.length}px)` }"
    ></div>
    <button
      v-for="opt in options"
      :key="String(opt.value)"
      class="ui-seg__item"
      :class="{ 'ui-seg__item--active': opt.value === modelValue }"
      type="button"
      :aria-pressed="opt.value === modelValue"
      @click="emit('update:modelValue', opt.value)"
    >
      {{ opt.label }}
    </button>
  </div>
</template>

<style scoped>
.ui-seg {
  @apply cczj-relative;
  display: inline-grid;
  grid-auto-flow: column;
  grid-auto-columns: 1fr;
  background: var(--bg-secondary);
  @apply cczj-border;
  border-radius: 10px;
  padding: 3px;
}

.ui-seg__indicator {
  @apply cczj-absolute;
  top: 3px;
  bottom: 3px;
  left: 3px;
  background: var(--accent);
  border-radius: 7px;
  transition: transform var(--cczj-motion-normal) var(--cczj-motion-ease-emphasis);
  @apply cczj-z-0;
}

.ui-seg__item {
  @apply cczj-relative;
  z-index: 1;
  padding: 6px 14px;
  border: none;
  border-radius: 7px;
  background: transparent;
  @apply cczj-text-secondary cczj-text-13 cczj-font-semibold cczj-cursor-pointer;
  transition: color var(--cczj-motion-fast), transform var(--cczj-motion-fast);
  @apply cczj-whitespace-nowrap;
  font-family: inherit;
}
.ui-seg__item:active { transform: scale(.96); }
.ui-seg__item:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.ui-seg__item:not(.ui-seg__item--active):hover { @apply cczj-text-primary; }
.ui-seg__item--active { color: var(--accent-contrast); }

.ui-seg--sm .ui-seg__item {
  padding: 5px 10px;
  @apply cczj-text-sm;
}
.ui-seg--sm { @apply cczj-rounded-md; }
.ui-seg--sm .ui-seg__indicator { @apply cczj-rounded; }
.ui-seg--wrap { @apply cczj-flex cczj-flex-wrap cczj-max-w-full; }
.ui-seg--wrap .ui-seg__item { @apply cczj-flex-1; flex-basis: 7.5rem; white-space: normal; line-height: 1.4; }
.ui-seg--wrap .ui-seg__item--active { background: var(--accent); }
</style>
