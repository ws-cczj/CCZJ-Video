<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{ modelValue: number; min?: number; max?: number; step?: number; ariaLabel: string }>(), { min: 0, max: Number.MAX_SAFE_INTEGER, step: 1 })
const emit = defineEmits<{ (e: 'update:modelValue', value: number): void; (e: 'change', value: number): void }>()
const { t } = useI18n()
const draft = ref(String(props.modelValue))
const focused = ref(false)
const message = ref('')
const stepValue = computed(() => /^\d+$/.test(draft.value) && Number.isSafeInteger(Number(draft.value)) ? Number(draft.value) : props.modelValue)
watch(() => props.modelValue, value => { draft.value = String(value) })
const rangeHint = computed(() => props.max === Number.MAX_SAFE_INTEGER
  ? t('numericInput.minimum', { min: props.min }) : t('numericInput.range', { min: props.min, max: props.max }))
function publish(value: number): void {
  draft.value = String(value)
  if (value !== props.modelValue) { emit('update:modelValue', value); emit('change', value) }
}
function commit(): void {
  const value = Number(draft.value.trim())
  if (!/^\d+$/.test(draft.value.trim()) || !Number.isSafeInteger(value)) {
    draft.value = String(props.modelValue)
    message.value = t('numericInput.invalid')
    return
  }
  const next = Math.min(props.max, Math.max(props.min, value))
  message.value = value === next ? '' : rangeHint.value
  publish(next)
}
function stepBy(direction: number): void {
  const value = stepValue.value
  message.value = ''
  publish(Math.min(props.max, Math.max(props.min, value + direction * props.step)))
}
</script>

<template>
  <div class="number-field" :class="{ invalid: message }">
    <button type="button" :aria-label="t('numericInput.decrease', { label: ariaLabel })" :disabled="stepValue <= min" @mousedown.prevent @click="stepBy(-1)">−</button>
    <input v-model="draft" type="text" inputmode="numeric" :aria-label="ariaLabel" :aria-invalid="message ? 'true' : undefined"
      @input="message = ''" @focus="focused = true" @blur="focused = false; commit()"
      @keydown.enter.prevent="commit" @keydown.up.prevent="stepBy(1)" @keydown.down.prevent="stepBy(-1)"
      @keydown.esc="draft = String(modelValue); message = ''" />
    <button type="button" :aria-label="t('numericInput.increase', { label: ariaLabel })" :disabled="stepValue >= max" @mousedown.prevent @click="stepBy(1)">+</button>
    <span v-if="message || focused" class="number-hint" role="status">{{ message || rangeHint }}</span>
  </div>
</template>

<style scoped>
.number-field { @apply cczj-relative; display: inline-grid; grid-template-columns: 24px minmax(44px, 1fr) 24px; width: 112px; @apply cczj-border; border-radius: 9px; background: var(--bg-input); @apply cczj-text-primary; transition: border-color var(--cczj-motion-fast), box-shadow var(--cczj-motion-fast); }
.number-field:focus-within { @apply cczj-border-primary; box-shadow: 0 0 0 3px var(--accent-alpha-10); }
input { @apply cczj-w-full cczj-min-w-0; box-sizing: border-box; padding: 7px 2px; border: 0; outline: 0; background: transparent; color: inherit; @apply cczj-text-center; font: inherit; font-size: .88rem; font-variant-numeric: tabular-nums; }
button { padding: 0; border: 0; @apply cczj-rounded-md; background: transparent; @apply cczj-text-muted; font: inherit; @apply cczj-cursor-pointer; transition: color var(--cczj-motion-fast), background-color var(--cczj-motion-fast); }
button:hover:not(:disabled) { background: var(--bg-hover); @apply cczj-text-accent; }
button:disabled { opacity: .35; @apply cczj-cursor-default; }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.number-hint { @apply cczj-absolute; top: calc(100% + 6px); @apply cczj-right-0 cczj-z-10; width: max-content; max-width: 200px; padding: 7px 10px; @apply cczj-border cczj-rounded-md; background: var(--bg-card); @apply cczj-text-secondary; box-shadow: 0 5px 18px rgba(0,0,0,.12); font-size: .78rem; @apply cczj-leading-normal cczj-pointer-events-none; }
</style>
