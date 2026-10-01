<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { tr } from '../../locales'
import { AddSource, UpdateSource, SetSourceStrategy } from '../../api/app'
import { usePluginStore } from '../../stores/plugins'
import { useErrorStore } from '../../stores/error'
import { useSourceStore } from '../../stores/source'
import { extractDomainKey } from '../../utils'
import { Button, Modal } from '../../components/ui'
import { useSourceDisplay, type EditSource } from './useSourceDisplay'

/**
 * 新建/编辑采集源弹窗。
 *
 * 表单状态和保存链路一起搬过来，顺序保持拆分前一致：写库 → 关窗 → 只在策略
 * 真的变了时才调 SetSourceStrategy。定时配置（schedule_config）与 adv_config
 * 不在这份 payload 里，改源名不会抹掉它们。
 *
 * 弹窗由页面决定何时打开（传 modelValue + 待编辑的那一行），下面的 watch 只做
 * 表单初化，不取任何数据。
 */
const props = defineProps<{ modelValue: boolean; source: EditSource | null }>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const sourceStore = useSourceStore()
const errorStore = useErrorStore()
const pluginStore = usePluginStore()
const { sk } = useSourceDisplay()

// === 采集适配策略选择 ===
// 内置 MAC CMS 永远可选；扩展包适配由 app/plugin 注册表提供；库里那份认不出来的
// 策略文档（手写的、或包已卸载）用 keep 表示"界面不动它"，否则保存源名就会抹掉配置。
const ADAPTER_BUILTIN = 'builtin'
const ADAPTER_KEEP = 'keep'

const form = ref({ name: '', api_url: '', collect_limit: 0, collect_hours: 0, enabled: true, adapter: ADAPTER_BUILTIN })
const showAdvanced = ref(false)
/** 打开编辑弹窗时库里的 strategy_config，用于判断这次保存是否真的要动策略列。 */
const storedStrategy = ref('')

const adapterChoices = computed(() => {
  const list: { value: string; label: string }[] = [{ value: ADAPTER_BUILTIN, label: tr('sources.adapterBuiltin') }]
  if (form.value.adapter === ADAPTER_KEEP) list.push({ value: ADAPTER_KEEP, label: tr('sources.adapterCustom') })
  for (const { packId, packName, adapter } of pluginStore.sourceAdapters) {
    const name = pluginStore.localized(adapter.name) || adapter.id
    list.push({ value: `${packId}/${adapter.id}`, label: packName ? `${packName} · ${name}` : name })
  }
  return list
})

const autoKey = computed(() => extractDomainKey(form.value.api_url))

function strategyOfChoice(value: string): string | null {
  if (value === ADAPTER_BUILTIN) return ''
  const hit = pluginStore.sourceAdapters.find(o => `${o.packId}/${o.adapter.id}` === value)
  return hit ? pluginStore.strategyText(hit.adapter) : null
}

function adapterOfStored(stored?: string): string {
  const text = (stored || '').trim()
  if (!text) return ADAPTER_BUILTIN
  const hit = pluginStore.sourceAdapters.find(o => pluginStore.strategyText(o.adapter) === text)
  return hit ? `${hit.packId}/${hit.adapter.id}` : ADAPTER_KEEP
}

watch([() => props.modelValue, () => props.source], ([open, source]) => {
  if (!open) return
  if (source) {
    storedStrategy.value = (source.strategy_config || '').trim()
    form.value = {
      name: source.name,
      api_url: source.api_url,
      collect_limit: source.collect_limit ?? 0,
      collect_hours: source.collect_hours ?? 0,
      enabled: (source.enabled ?? 1) === 1,
      adapter: adapterOfStored(source.strategy_config),
    }
    showAdvanced.value = !!(source.collect_limit || source.collect_hours || source.strategy_config)
  } else {
    form.value = { name: '', api_url: '', collect_limit: 50, collect_hours: 0, enabled: true, adapter: ADAPTER_BUILTIN }
    storedStrategy.value = ''
    showAdvanced.value = false
  }
}, { immediate: true })

async function save(): Promise<void> {
  const editing = props.source?.source_key || ''
  const payload = {
    source_key: editing,
    name: form.value.name,
    api_url: form.value.api_url,
    collect_limit: Number(form.value.collect_limit) || 0,
    collect_hours: Number(form.value.collect_hours) || 0,
    enabled: form.value.enabled ? 1 : 0,
  }
  // 策略文档不在 UpdateSource 的字段里（那一列有自己的入口），所以先算好这次要不要改它。
  // keep / 认不出的选择都不写，避免一次改名字把库里的采集配置抹掉。
  const choice = form.value.adapter
  const target = choice === ADAPTER_BUILTIN ? '' : (choice === ADAPTER_KEEP ? null : strategyOfChoice(choice))
  if (target === null && choice !== ADAPTER_KEEP) {
    errorStore.warn(tr('sources.adapterGoneTitle'), tr('sources.adapterGoneMsg'))
    return
  }
  try {
    let key = editing
    if (editing) {
      await UpdateSource(payload as any)
    } else {
      const before = new Set(sourceStore.sources.map(sk))
      await AddSource(payload as any)
      await sourceStore.loadSources(true)
      // 新源的 source_key 由后端从 api_url 推导，前端只认这次新增出来的那一行。
      const created = sourceStore.sources.find(s => !before.has(sk(s)))
      key = created ? sk(created) : ''
    }
    emit('update:modelValue', false)
    if (target !== null && target !== storedStrategy.value) {
      if (!key) throw new Error(tr('sources.addKeyNotFound'))
      await SetSourceStrategy(key, target)
      await sourceStore.loadSources(true)
    }
  } catch (e) {
    errorStore.fromError(tr('sources.saveFailed'), e, 'Sources.save')
  }
}
</script>

<template>
  <!-- 新建/编辑弹窗 -->
  <Modal
    :model-value="props.modelValue"
    :title="props.source ? tr('sources.edit') : tr('sources.add')"
    width="520px"
    :show-footer="true"
    @update:model-value="(v: boolean) => !v && emit('update:modelValue', false)"
  >
    <p class="modal-desc">{{ tr('sources.formDesc') }}</p>
    <div class="form-group">
      <label>{{ tr('sources.url') }} <span class="required">*</span></label>
      <input v-model="form.api_url" :placeholder="tr('sources.apiUrlPlaceholder')" />
    </div>
    <div v-if="form.api_url" class="auto-info">
      <span class="auto-label">{{ tr('sources.autoDetect') }}:</span>
      <code>{{ autoKey || tr('sources.enterValidUrl') }}</code>
    </div>
    <div class="form-group" style="display:flex;align-items:center;gap:8px">
      <label style="display:flex;align-items:center;gap:6px;cursor:pointer">
        <input type="checkbox" v-model="form.enabled" style="width:auto" />
        <span>{{ tr('sources.enableThisSource') }}</span>
      </label>
    </div>
    <div class="form-group">
      <label>{{ tr('sources.displayName') }} <span class="optional">{{ tr('sources.optional') }}</span></label>
      <input v-model="form.name" :placeholder="autoKey || tr('sources.autoUseSourceKey')" />
    </div>
    <button class="toggle-advanced" @click="showAdvanced = !showAdvanced">
      <span>{{ showAdvanced ? '▾' : '▸' }}</span>
      <span>{{ tr('sources.advancedOptions') }}</span>
    </button>
    <div v-if="showAdvanced" class="form-group">
      <label>{{ tr('sources.perPageLabel') }} <span class="optional">{{ tr('sources.perPageHint') }}</span></label>
      <input type="number" min="0" max="500" v-model.number="form.collect_limit" placeholder="50" />
    </div>
    <div v-if="showAdvanced" class="form-group">
      <label>{{ tr('sources.defaultHoursLabel') }}<span class="optional">{{ tr('sources.defaultHoursHint') }}</span></label>
      <input type="number" min="0" max="8760" v-model.number="form.collect_hours" placeholder="24" />
    </div>
    <div v-if="showAdvanced && adapterChoices.length > 1" class="form-group">
      <label>{{ tr('sources.adapterLabel') }} <span class="optional">{{ tr('sources.adapterHint') }}</span></label>
      <label v-for="opt in adapterChoices" :key="opt.value" class="adapter-option">
        <input type="radio" name="source-adapter" :value="opt.value" v-model="form.adapter" />
        <span>{{ opt.label }}</span>
      </label>
    </div>
    <template #footer>
      <Button variant="secondary" size="md" @click="emit('update:modelValue', false)">{{ tr('common.cancel') }}</Button>
      <Button variant="primary" size="md" :disabled="!form.api_url" @click="save">{{ tr('common.save') }}</Button>
    </template>
  </Modal>
</template>

<style scoped>

/* Modal.vue 把内容 Teleport 到 body 下，页面的 scoped 选择器选不到这里，
   所以弹窗自己的文案/表单样式都留在本组件。 */
.modal-desc { font-size: 13px; color: var(--text-muted); margin: 0 0 18px; }
.form-group { margin-bottom: 14px; }
.form-group label { display: block; font-size: 12px; color: var(--text-secondary); margin-bottom: 6px; font-weight: 500; }
.form-group input {
  width: 100%; padding: 10px 14px; border-radius: 10px;
  border: 1px solid var(--border); background: var(--bg-input);
  color: var(--text-primary); font-size: 13px; outline: none;
  transition: all 0.15s ease; box-sizing: border-box;
}
.form-group input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.form-group input::placeholder { color: var(--text-muted); }
.required { color: var(--danger); }
.optional { color: var(--text-muted); font-weight: normal; }
.adapter-option { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; font-size: 13px; color: var(--text-primary); cursor: pointer; }
.adapter-option input { width: auto; padding: 0; }
.adapter-option span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.auto-info {
  display: flex; align-items: center; gap: 8px;
  font-size: 12px; color: var(--accent);
  padding: 10px 12px; background: var(--accent-alpha-10);
  border-radius: 8px; margin-bottom: 14px;
}
.auto-info code { font-weight: 600; font-family: 'SF Mono', Consolas, monospace; }
.toggle-advanced {
  display: inline-flex; align-items: center; gap: 6px;
  background: transparent; border: none;
  color: var(--text-muted); cursor: pointer;
  font-size: 12px; padding: 6px 0; margin-bottom: 8px;
  transition: color 0.15s ease;
}
.toggle-advanced:hover { color: var(--accent); }

</style>
