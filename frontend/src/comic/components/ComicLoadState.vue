<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiError } from '../../api/app'
import type { ResourceStatus } from '../../reading/asyncResource'
import Icon from '../../components/Icon.vue'

const props = withDefaults(defineProps<{
  status: ResourceStatus
  error?: ApiError | null
  icon?: string
  title?: string
  hint?: string
  /** NOT_FOUND 是"这一话/这部作品取不到了"，后端那句散文不适合直接给用户看。 */
  notFoundText?: string
}>(), { error: null, icon: 'search', title: '', hint: '', notFoundText: '' })

const emit = defineEmits<{ (event: 'retry'): void }>()
const { t } = useI18n()
const visible = computed(() => props.status === 'loading' || props.status === 'empty' || props.status === 'error')
const message = computed(() => {
  if (props.status !== 'error') return ''
  if (props.error?.code === 'NOT_FOUND' && props.notFoundText) return props.notFoundText
  return props.error?.message || t('common.unknownError')
})
</script>
<template>
  <div v-if="visible" class="comic-empty">
    <template v-if="status === 'loading'"><Icon name="refresh" :size="34" /><h2>{{ t('common.loading') }}</h2></template>
    <template v-else-if="status === 'error'">
      <Icon name="alert-triangle" :size="34" />
      <h2>{{ title || t('common.failed') }}</h2>
      <p>{{ message }}</p>
      <button type="button" class="comic-button" @click="emit('retry')"><Icon name="refresh" :size="17" />{{ t('common.retry') }}</button>
      <slot name="actions" />
    </template>
    <template v-else>
      <Icon :name="icon" :size="34" />
      <h2>{{ title || t('common.noData') }}</h2>
      <p v-if="hint">{{ hint }}</p>
      <slot name="actions" />
    </template>
  </div>
</template>
