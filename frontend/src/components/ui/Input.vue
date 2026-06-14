<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  modelValue?: string | number
  placeholder?: string
  disabled?: boolean
  type?: 'text' | 'number' | 'password'
  size?: 'sm' | 'md' | 'lg'
  /** 是否显示搜索图标 */
  search?: boolean
  /** 是否可清除 */
  clearable?: boolean
}>(), {
  type: 'text',
  size: 'md',
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'enter'): void
  (e: 'focus'): void
  (e: 'blur'): void
}>()

const value = computed({
  get: () => (props.modelValue != null ? String(props.modelValue) : ''),
  set: (v: string) => emit('update:modelValue', v),
})
</script>

<template>
  <div
    class="ui-input"
    :class="[
      `ui-input--${size}`,
      { 'ui-input--search': search, 'ui-input--disabled': disabled },
    ]"
  >
    <!-- Search icon -->
    <svg
      v-if="search"
      class="ui-input__icon ui-input__icon--left"
      width="16" height="16"
      viewBox="0 0 24 24" fill="none"
      stroke="currentColor" stroke-width="2"
      stroke-linecap="round"
    >
      <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
    </svg>

    <input
      :type="type"
      class="ui-input__field"
      :class="{ 'ui-input__field--has-left-icon': search }"
      :value="value"
      :placeholder="placeholder"
      :disabled="disabled"
      @input="(e: Event) => value = (e.target as HTMLInputElement).value"
      @keyup.enter="emit('enter')"
      @focus="emit('focus')"
      @blur="emit('blur')"
    />

    <!-- Clear button -->
    <button
      v-if="clearable && modelValue"
      class="ui-input__clear"
      type="button"
      @click="value = ''"
      aria-label="清除"
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.ui-input {
  position: relative;
  display: inline-flex;
  align-items: center;
  width: 100%;
}

.ui-input__icon--left {
  position: absolute;
  left: 12px;
  color: var(--text-muted);
  pointer-events: none;
  z-index: 1;
}

.ui-input__field {
  width: 100%;
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: 14px;
  font-family: inherit;
  outline: none;
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}
.ui-input__field::placeholder {
  color: var(--text-muted);
}
.ui-input__field:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-15);
}
.ui-input__field--has-left-icon {
  padding-left: 38px;
}

.ui-input--sm .ui-input__field {
  padding: 5px 10px;
  font-size: 12px;
  border-radius: 6px;
}
.ui-input--sm .ui-input__icon--left {
  left: 10px;
  width: 14px;
  height: 14px;
}
.ui-input--sm .ui-input__field--has-left-icon {
  padding-left: 32px;
}

.ui-input--lg .ui-input__field {
  padding: 12px 16px;
  font-size: 15px;
  border-radius: 10px;
}
.ui-input--lg .ui-input__field--has-left-icon {
  padding-left: 44px;
}

.ui-input--disabled .ui-input__field {
  opacity: 0.6;
  cursor: not-allowed;
}

.ui-input__clear {
  position: absolute;
  right: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  background: transparent;
  color: var(--text-muted);
  border-radius: 50%;
  cursor: pointer;
  transition: all 0.15s ease;
}
.ui-input__clear:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
</style>