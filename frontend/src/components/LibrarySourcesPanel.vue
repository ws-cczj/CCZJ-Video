<script setup lang="ts">
/** Shared reading-source management; the backend scopes every operation by kind. */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  addLibrarySource,
  deleteLibrarySource,
  listLibrarySources,
  setLibrarySourceEnabled,
  testLibrarySource,
  updateLibrarySource,
  type SourceInfoDTO,
  type TestOutcomeDTO,
  type TestStageDTO,
} from '../api/library'
import { normalizeApiError } from '../api/app'
import { useAsyncResource, isEmptyList } from '../reading/asyncResource'
import { useErrorStore } from '../stores/error'
import { useConfirmStore } from '../stores/confirm'
import { Badge, Button, Empty, Modal, Spinner } from './ui'
import LibrarySourceCatalog from './LibrarySourceCatalog.vue'
import Icon from './Icon.vue'
import { sourceHealthStatus, type SourceStatus } from '../api/sourceCatalog'

defineOptions({ name: 'LibrarySourcesPanel' })

const props = defineProps<{ kind: 'novel' | 'comic' }>()
const activeTab=ref<'library'|'installed'>('installed')
const installedQuery=ref('')

const { t, te } = useI18n()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()

// 解构出来是为了让模板里能直接用 status / data 而不必写 .value：
// 嵌在普通对象里的 ref 不会被模板自动解包，只有 setup 顶层的绑定才会。
const { status, data, error, run, retry } = useAsyncResource<SourceInfoDTO[]>({
  load: () => listLibrarySources(props.kind),
  isEmpty: isEmptyList,
})

const editorOpen = ref(false)
const editing = ref<SourceInfoDTO | null>(null)
const form = ref({ name: '', apiUrl: '', document: '' })
const formError = ref('')
const saving = ref(false)

const testingKey = ref('')
const testOutcome = ref<TestOutcomeDTO | null>(null)
const testOpen = ref(false)

const title = computed(() => t(props.kind === 'novel' ? 'librarySources.novelTitle' : 'librarySources.comicTitle'))
const installedRows=computed(()=>(data.value??[]).filter(row=>`${row.name} ${row.apiUrl}`.toLowerCase().includes(installedQuery.value.toLowerCase())))
function installedStatus(source: SourceInfoDTO): SourceStatus {
  return sourceHealthStatus(!source.documentError, source.quality)
}
function checkedTime(source: SourceInfoDTO): string {
  return source.quality ? new Date(source.quality.checkedAt * 1000).toLocaleString() : ''
}

async function reload(): Promise<void> {
  await run()
}
async function imported(): Promise<void> {
  activeTab.value = 'installed'
  await reload()
}

/**
 * 一份能改的最小文档模板。
 *
 * 给模板而不是给一个空文本框，是因为这份配置的字段名拼错时后端会拒（这是对的），
 * 但用户面对一个空框不知道从哪开始。模板里的选择器是常见小说站的形状，
 * 改几个字符串就能用；漫画源要把 content 换成 pages 与 image。
 */
function templateDocument(): Record<string, unknown> {
  return {
    version: 3,
    kind: props.kind,
    check_keyword: '',
    operations: {
      search: {
        format: 'html',
        list: { query: 'ul.results > li' },
        fields: {
          title: { query: 'a', attr: 'text' },
          url: { query: 'a', attr: 'href' },
          author: { query: 'span.author' },
        },
      },
      detail: {
        format: 'html',
        fields: {
          title: { query: 'h1' },
          author: { query: 'p.author' },
          summary: { query: 'p.intro' },
          toc_url: { query: 'a.toc', attr: 'href' },
        },
      },
      toc: {
        format: 'html',
        list: { query: 'ul.chapters > li' },
        fields: {
          title: { query: 'a', attr: 'ownText' },
          chapter_url: { query: 'a', attr: 'href' },
        },
      },
      ...(props.kind === 'novel'
        ? { content: { format: 'html', text: { query: 'div#content', attr: 'text' } } }
        : {
            pages: {
              format: 'html',
              list: { query: 'div.pages img' },
              fields: { image_url: { attr: 'src' } },
            },
          }),
    },
  }
}

function openAdd(): void {
  editing.value = null
  form.value = { name: '', apiUrl: '', document: JSON.stringify(templateDocument(), null, 2) }
  formError.value = ''
  editorOpen.value = true
}

function openEdit(source: SourceInfoDTO): void {
  editing.value = source
  form.value = {
    name: source.name,
    apiUrl: source.apiUrl,
    // 回填的是原文，不是格式化后的：后端把文档原文的 sha256 当正文缓存的版本键，
    // 一次"只是排版变好看了"的保存会作废这本书全部的正文缓存。
    document: source.document,
  }
  formError.value = ''
  editorOpen.value = true
}

async function save(): Promise<void> {
  saving.value = true
  formError.value = ''
  try {
    if (editing.value) {
      await updateLibrarySource(editing.value.sourceKey, form.value.name, form.value.apiUrl, form.value.document)
    } else {
      await addLibrarySource(props.kind, form.value.name, form.value.apiUrl, form.value.document)
    }
    editorOpen.value = false
    await reload()
  } catch (raw) {
    // 配置不合法要显示在表单里而不是弹一条 toast：用户正要改的就是这份配置，
    // toast 三秒就消失了，而错误可能有好几行。
    formError.value = normalizeApiError(raw).message
  } finally {
    saving.value = false
  }
}

async function remove(source: SourceInfoDTO): Promise<void> {
  const ok = await confirmStore.confirm({
    message: t('librarySources.deleteConfirm', { name: source.name }),
    level: 'danger',
  })
  if (!ok) {
    return
  }
  try {
    await deleteLibrarySource(source.sourceKey)
    await reload()
  } catch (raw) {
    errorStore.error(normalizeApiError(raw).message)
  }
}

async function toggle(source: SourceInfoDTO): Promise<void> {
  try {
    await setLibrarySourceEnabled(source.sourceKey, !source.enabled)
    await reload()
  } catch (raw) {
    errorStore.error(normalizeApiError(raw).message)
  }
}

async function runTest(source: SourceInfoDTO): Promise<void> {
  testingKey.value = source.sourceKey
  testOutcome.value = null
  try {
    testOutcome.value = await testLibrarySource(source.sourceKey)
    testOpen.value = true
    await reload()
  } catch (raw) {
    errorStore.error(normalizeApiError(raw).message)
  } finally {
    testingKey.value = ''
  }
}

// 绑定把 Go 的 []*TestStage 生成成 (TestStage | null)[]，
// 而 Go 侧不会往里塞 nil。在模板外面滤一次，模板里就不必处处判空。
const stages = computed(() =>
  (testOutcome.value?.stages ?? []).filter((stage): stage is TestStageDTO => stage !== null),
)

function stageLabel(name: string): string {
  const key = `librarySources.stage.${name}`
  // 后端可能加新阶段；查不到就原样显示标识符，总比显示一个空白格强。
  return te(key) ? t(key) : name
}

onMounted(reload)
</script>

<template>
  <section class="library-sources" :class="`library-sources-${kind}`">
    <nav class="library-sources-tabs" :aria-label="title"><Button :aria-pressed="activeTab==='installed'" @click="activeTab='installed'">{{t('readingUI.mySources')}} <span v-if="data?.length">{{data.length}}</span></Button><Button :aria-pressed="activeTab==='library'" @click="activeTab='library'">{{t('readingUI.addSource')}}</Button><router-link v-if="data?.length" class="library-sources-search-link" :to="kind==='novel'?'/novel/search':'/comic/search'">{{t('librarySources.startSearch')}}</router-link></nav>
    <LibrarySourceCatalog v-if="activeTab==='library'" :kind="kind" @imported="imported" />
    <section v-if="activeTab==='installed'" class="library-sources-installed">
    <header class="library-sources-head">
      <div>
        <h2>{{ title }}</h2>
        <p class="library-sources-hint">{{ t('librarySources.installedCount', { count: data?.length ?? 0 }) }}</p>
      </div>
      <Button v-if="data?.length" size="sm" @click="activeTab='library'">{{ t('readingUI.addSource') }}</Button>
    </header>
    <label v-if="data?.length" class="library-sources-filter"><span>{{t('sourceCatalog.find')}}</span><input v-model="installedQuery" type="search" /></label>

    <Spinner v-if="status === 'loading'" size="md" />

    <div v-else-if="status === 'error'" class="library-sources-error">
      <p>{{ error?.message }}</p>
      <Button size="sm" @click="retry()">{{ t('common.retry') }}</Button>
    </div>

    <Empty
      v-else-if="status === 'empty'"
      :title="t('librarySources.emptyTitle')"
      :description="t('readingUI.sourceEmptyHint')"
    ><template #icon><Icon name="database" :size="36" /></template><Button variant="primary" @click="activeTab='library'">{{ t('readingUI.addSource') }}</Button></Empty>

    <ul v-else class="library-sources-list">
      <li v-for="source in installedRows" :key="source.sourceKey" class="library-source">
        <div class="library-source-main">
          <div class="library-source-title">
            <span class="library-source-name">{{ source.name }}</span>
            <Badge v-if="!source.enabled" variant="default">{{ t('librarySources.disabled') }}</Badge>
            <!-- 自动停用与用户手动关掉是两件事：前者会在冷却到期后自动恢复，
                 后者不会。不区分的话用户会以为"我明明开着它自己又关了"。 -->
            <Badge v-else-if="source.autoDisabled" variant="warning">{{ t('librarySources.autoDisabled') }}</Badge>
            <Badge v-else variant="success">{{ t('librarySources.enabled') }}</Badge>
            <Badge v-if="source.itemCount > 0" variant="primary">
              {{ t('librarySources.itemCount', { count: source.itemCount }) }}
            </Badge>
          </div>
          <p class="library-source-url">{{ source.apiUrl }}</p>
          <p class="library-source-form-hint">{{t(`sourceCatalog.status.${installedStatus(source)}`)}}<template v-if="source.quality"> · {{source.quality.score}}/100 · {{t('librarySources.qualityCheckedAt',{time:checkedTime(source)})}}</template></p>
          <p v-if="source.quality" class="library-source-form-hint">{{t('librarySources.qualityScope',{stages:source.quality.stages.length,success:source.quality.successes,samples:source.quality.samples})}}</p>
          <!-- 配置写坏了也要把这一行显示出来：让源凭空消失的话，
               用户连去修它的入口都没有。 -->
          <p v-if="source.documentError" class="library-source-invalid">{{ source.documentError }}</p>
        </div>
        <div class="library-source-actions">
          <Button size="sm" variant="ghost" @click="toggle(source)">{{ source.enabled ? t('librarySources.disable') : t('librarySources.enable') }}</Button>
          <details><summary>{{t('sourceCatalog.details')}}</summary>
          <Button size="sm" variant="ghost" :loading="testingKey === source.sourceKey" @click="runTest(source)">
            {{ t('librarySources.test') }}
          </Button>
          <Button size="sm" variant="ghost" @click="openEdit(source)">{{ t('librarySources.edit') }}</Button>
          <Button size="sm" variant="danger" @click="remove(source)">{{ t('librarySources.delete') }}</Button>
          </details>
        </div>
      </li>
    </ul>
    <p v-if="data?.length&&!installedRows.length">{{t('sourceCatalog.noMatches')}}</p>
    <details class="library-source-advanced"><summary>{{ t('librarySources.fieldDocument') }}</summary><Button size="sm" @click="openAdd">{{ t('librarySources.add') }}</Button></details>
    </section>

    <Modal v-model="editorOpen" :title="editing ? t('librarySources.edit') : t('librarySources.add')" width="640px">
      <div class="library-source-form">
        <label>
          <span>{{ t('librarySources.fieldName') }}</span>
          <input v-model="form.name" type="text" :placeholder="t('librarySources.fieldNamePlaceholder')" />
        </label>
        <label>
          <span>{{ t('librarySources.fieldApiUrl') }}</span>
          <input v-model="form.apiUrl" type="text" placeholder="https://" :disabled="editing !== null" />
        </label>
        <p v-if="editing" class="library-source-form-hint">{{ t('librarySources.apiUrlLocked') }}</p>
        <label>
          <span>{{ t('librarySources.fieldDocument') }}</span>
          <textarea v-model="form.document" rows="18" spellcheck="false"></textarea>
        </label>
        <p class="library-source-form-hint">{{ t('librarySources.documentHint') }}</p>
        <p v-if="formError" class="library-source-invalid">{{ formError }}</p>
      </div>
      <template #footer>
        <Button size="sm" @click="editorOpen = false">{{ t('common.cancel') }}</Button>
        <Button size="sm" variant="primary" :loading="saving" @click="save">{{ t('common.confirm') }}</Button>
      </template>
    </Modal>

    <Modal v-model="testOpen" :title="t('librarySources.testResult')" width="520px">
      <ol v-if="testOutcome" class="library-test-stages">
        <li v-for="stage in stages" :key="stage.name" class="library-test-stage" :data-status="stage.status">
          <span class="library-test-name">{{ stageLabel(stage.name) }}</span>
          <Badge :variant="stage.status === 'ok' ? 'success' : stage.status === 'failed' ? 'danger' : 'default'">
            {{ t(`librarySources.stageStatus.${stage.status}`) }}
          </Badge>
          <span v-if="(stage.count ?? 0) > 0" class="library-test-count">{{ stage.count }}</span>
          <span class="library-test-millis">{{ stage.millis }}ms</span>
          <p v-if="stage.message" class="library-test-message">{{ stage.message }}</p>
        </li>
      </ol>
      <p class="library-source-form-hint">{{ t('librarySources.testHint') }}</p>
    </Modal>
  </section>
</template>

<style scoped>
.library-sources-tabs{@apply cczj-flex cczj-gap-4 cczj-items-center cczj-flex-wrap}.library-sources-tabs :deep(button){min-height:44px}.library-sources-tabs :deep(button[aria-pressed=true]){border-color:var(--accent,currentColor);color:var(--accent,currentColor)}.library-sources-search-link{@apply cczj-ml-auto cczj-p-5 cczj-text-13;min-height:44px;@apply cczj-flex cczj-items-center}.library-sources-installed{@apply cczj-flex cczj-flex-col cczj-gap-7}.library-sources-filter{@apply cczj-flex cczj-flex-col cczj-gap-3 cczj-text-13}.library-sources-filter input{min-height:44px;@apply cczj-border cczj-rounded-md;background:var(--bg-input);color:inherit;@apply cczj-px-6-py-4}.library-source-actions summary,.library-sources-advanced summary{min-height:44px;@apply cczj-flex cczj-items-center cczj-cursor-pointer cczj-text-13}.library-source-actions :deep(button){min-height:44px}@media(max-width:640px){.library-source{@apply cczj-flex-col}.library-source-actions{@apply cczj-justify-start}.library-sources-head{@apply cczj-flex-wrap}}
.library-sources {
  --btn-solid: var(--accent);
  --btn-solid-text: #fff;
  --btn-solid-hover: var(--accent);
  @apply cczj-flex cczj-flex-col cczj-gap-8;
}
.library-sources-head {
  @apply cczj-flex cczj-items-start cczj-justify-between cczj-gap-8;
}
.library-sources-head h2 {
  margin: 0;
  @apply cczj-text-xl;
}
.library-sources-hint,
.library-source-form-hint {
  margin: 4px 0 0;
  @apply cczj-text-sm;
  opacity: 0.72;
  line-height: 1.6;
}
.library-sources-error {
  @apply cczj-flex cczj-flex-col cczj-gap-4 cczj-items-start;
}
.library-sources-list {
  list-style: none;
  margin: 0;
  padding: 0;
  @apply cczj-flex cczj-flex-col cczj-gap-5;
}
.library-source {
  @apply cczj-flex cczj-items-start cczj-justify-between cczj-gap-6;
  padding: 12px 14px;
  @apply cczj-rounded-lg;
  border: 1px solid color-mix(in srgb, currentColor 12%, transparent);
}
.library-source-main {
  @apply cczj-min-w-0 cczj-flex cczj-flex-col cczj-gap-2;
}
.library-source-title {
  @apply cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-4;
}
.library-source-name {
  @apply cczj-font-semibold;
}
.library-source-url {
  margin: 0;
  @apply cczj-text-sm;
  opacity: 0.66;
  @apply cczj-break-all;
}
.library-source-invalid {
  margin: 0;
  @apply cczj-text-sm;
  color: #d9534f;
  @apply cczj-whitespace-pre;
  word-break: break-word;
}
.library-source-actions {
  @apply cczj-flex cczj-flex-wrap cczj-gap-3 cczj-justify-end;
}
.library-source-form {
  @apply cczj-flex cczj-flex-col cczj-gap-5;
}
.library-source-form label {
  @apply cczj-flex cczj-flex-col cczj-gap-2 cczj-text-13;
}
.library-source-form input,
.library-source-form textarea {
  @apply cczj-w-full;
  padding: 8px 10px;
  @apply cczj-rounded-md;
  border: 1px solid color-mix(in srgb, currentColor 18%, transparent);
  background: transparent;
  color: inherit;
  font: inherit;
}
.library-source-form textarea {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  @apply cczj-text-sm;
  line-height: 1.55;
  resize: vertical;
}
.library-source-form input:disabled {
  opacity: 0.55;
}
.library-test-stages {
  margin: 0;
  padding: 0;
  list-style: none;
  @apply cczj-flex cczj-flex-col cczj-gap-4;
}
.library-test-stage {
  @apply cczj-flex cczj-items-center cczj-flex-wrap cczj-gap-4;
  padding: 8px 10px;
  border-radius: 10px;
  border: 1px solid color-mix(in srgb, currentColor 10%, transparent);
}
.library-test-name {
  @apply cczj-font-semibold;
}
.library-test-count,
.library-test-millis {
  @apply cczj-text-sm cczj-opacity-70;
}
.library-test-message {
  flex-basis: 100%;
  margin: 0;
  @apply cczj-text-sm cczj-whitespace-pre;
  word-break: break-word;
  opacity: 0.85;
}

.library-sources-tabs {
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-secondary);
}
.library-sources-tabs :deep(.ui-btn) { min-height: 44px; }
.library-sources-tabs :deep(.ui-btn[aria-pressed=true]) {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--bg-card);
}
.library-sources-search-link { border-radius: 6px; }
.library-sources-installed-actions { @apply cczj-items-center; }
@media (max-width: 640px) {
  .library-sources-page { padding: 12px 16px 28px; }
  .library-sources-tabs { @apply cczj-grid; grid-template-columns: 1fr 1fr; }
  .library-sources-search-link { grid-column: 1 / -1; @apply cczj-justify-center; margin-left: 0; }
  .library-sources-head { @apply cczj-items-start; }
  .library-sources-installed-actions { width: 100%; @apply cczj-justify-between; }
}
</style>
