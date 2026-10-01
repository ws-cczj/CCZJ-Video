<script setup lang="ts">
defineOptions({ name: 'ExtensionsPanel' })
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { OpenFolder } from '../api/app'
import Icon from './Icon.vue'
import { Button, Empty, Tag } from './ui'
import { useErrorStore } from '../stores/error'
import { useConfirmStore } from '../stores/confirm'
import { usePluginStore, type PluginInfo } from '../stores/plugins'
import { humanizeBytes } from '../utils'
import { syncPluginScripts } from '../plugins/runtime'
import { loadPermissionLedger, permissionDecision, resetPermission, type PackPermission } from '../plugins/permissions'
import { dropReports, installing, type InstallReport } from '../plugins/dropInstall'

const { t } = useI18n()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const pluginStore = usePluginStore()

const busyId = ref('')
/** 正在忙的是哪个动作：只有被点的那一个按钮转圈，同一行的另一个保持原样。 */
const busyAction = ref<'' | 'toggle' | 'uninstall' | 'retry' | 'permission'>('')
const rescanning = ref(false)
/**
 * 一次开关/卸载/重扫从点击到注入落地全程占住。开关之所以要跑到注入结束，是因为
 * 「启用」这件事的可见结果就是那个包挂上来的样式、分组和侧边栏条目——按钮先松掉、
 * 界面隔两百毫秒才变，用户看到的就是两段跳。期间再点第二个也会读到旧注册表。
 */
const pending = ref(false)

onMounted(() => {
  void pluginStore.refresh(false)
  // 卡片上那三个状态（已允许 / 已拒绝 / 还没问过）读的是授权账本，而账本原本只在包
  // 真动手那一刻才去取。不在这儿读一次，一个被拒过的包会显示成「还没问过」。
  void loadPermissionLedger()
})

const KIND_KEYS: Record<string, string> = {
  source: 'settings.extensionsKindSource',
  shader: 'settings.extensionsKindShader',
  theme: 'settings.extensionsKindTheme',
  script: 'settings.extensionsKindScript',
}

function kindLabel(kind: string): string {
  return t(KIND_KEYS[kind] || 'settings.extensionsTitle')
}

function packDesc(pack: PluginInfo): string {
  return pluginStore.localized(pack.description)
}

/** 状态色跟着语义走：已启用绿、已停用中性、校验没过红，和别的面板一个读法。 */
function statusVariant(status: string): 'success' | 'danger' | 'default' {
  if (status === 'ready') return 'success'
  if (status === 'invalid') return 'danger'
  return 'default'
}

async function toggle(pack: PluginInfo, enabled: boolean): Promise<void> {
  if (pending.value) return
  pending.value = true
  busyId.value = pack.id
  busyAction.value = 'toggle'
  let msg = ''
  try {
    msg = await pluginStore.setEnabled(pack.id, enabled)
    // 停用它要收回已经注入的东西，启用它要挂上样式和分组——跑完这一轮才算真的开关完了。
    if (!msg) await syncPluginScripts()
  } finally {
    pending.value = false
    busyId.value = ''
    busyAction.value = ''
  }
  if (msg) {
    errorStore.fromError(t('settings.extensionsTitle'), new Error(msg), 'ExtensionsPanel.toggle')
    return
  }
  errorStore.info(t('settings.extensionsTitle'), `${pluginStore.localized(pack.name)} · ${pluginStore.stateLabel(enabled ? 'ready' : 'disabled')}`)
}

/**
 * 卸载：让 Go 删掉那个包目录。这一步不可逆，所以先弹确认；删完必须重跑一轮注入，
 * 否则那个包加过的侧边栏条目、样式和设置页分组会留在界面上——文件没了界面还有。
 */
async function uninstall(pack: PluginInfo): Promise<void> {
  if (pending.value) return
  const name = pluginStore.localized(pack.name) || pack.id
  const ok = await confirmStore.confirm({
    title: t('settings.extensionsUninstallTitle'),
    message: t('settings.extensionsUninstallMsg', { name }),
    okText: t('settings.extensionsUninstall'),
    level: 'danger',
  })
  if (!ok) return
  pending.value = true
  busyId.value = pack.id
  busyAction.value = 'uninstall'
  let msg = ''
  try {
    msg = await pluginStore.uninstall(pack.id)
    if (!msg) {
      // 顺带清掉这个 id 的本机隔离记录：同一目录名可能重新拖回来，旧记录不该继续拦注入。
      pluginStore.retryScript(pack.id)
      await syncPluginScripts()
    }
  } finally {
    pending.value = false
    busyId.value = ''
    busyAction.value = ''
  }
  if (msg) {
    errorStore.fromError(t('settings.extensionsUninstallFailed'), new Error(msg), 'ExtensionsPanel.uninstall')
    return
  }
  errorStore.info(t('settings.extensionsTitle'), t('settings.extensionsUninstalled', { name }))
}

/**
 * 扩展包目录的展示文字。Go 侧的绝对路径存在 pluginStore.directory，但那条绑定还没生成时
 * 这一行不能整块消失 —— 用户点开这个面板最想要的就是「文件夹该放哪儿」，所以退回
 * %APPDATA% 写法：粘到资源管理器地址栏会自动展开，等于一条能用的路径。
 */
const dirText = computed(() => pluginStore.directory || t('settings.extensionsDirFallback'))
/** 只有真读到绝对路径才给「打开目录」：OpenFolder 只认数据目录下的路径，未展开的 %APPDATA% 会被拒。 */
const hasRealDir = computed(() => !!pluginStore.directory)

async function rescan(): Promise<void> {
  if (pending.value) return
  pending.value = true
  rescanning.value = true
  try {
    await pluginStore.refresh(true)
    // 重扫是「包文件可能变了」的唯一信号，注入型包必须跟着重建，否则改完脚本要重启应用才生效。
    await syncPluginScripts()
  } catch (e: any) {
    errorStore.fromError(t('settings.extensionsRescan'), e, 'ExtensionsPanel.rescan')
  } finally {
    pending.value = false
    rescanning.value = false
  }
}

/** 重试一个本机跑挂的脚本包：清掉隔离记录，再走一轮注入。 */
async function retryScriptPack(pack: PluginInfo): Promise<void> {
  if (pending.value) return
  pending.value = true
  busyId.value = pack.id
  busyAction.value = 'retry'
  let left = ''
  try {
    pluginStore.retryScript(pack.id)
    await syncPluginScripts()
    left = pluginStore.scriptError(pack.id)
  } finally {
    pending.value = false
    busyId.value = ''
    busyAction.value = ''
  }
  if (left) {
    errorStore.warn(t('settings.extensionsTitle'), `${pluginStore.localized(pack.name)} · ${left}`, '', 'ExtensionsPanel.retry')
    return
  }
  errorStore.info(t('settings.extensionsTitle'), `${pluginStore.localized(pack.name)} · ${t('settings.extensionsStateReady')}`)
}

// ---------- 拖放安装 ----------
// 投放本身由 Go 的原生拖放驱动（整窗都是投放区，见 plugins/dropInstall.ts）：
// 这里的 DOM drag/drop 事件在原生模式下只会和运行时抢同一次拖放，所以不听不拦，
// 只把结果摊开给人看。

function reportText(report: InstallReport): string {
  // 失败的人话由 Go 给（它才知道是哪个键写错了），这里只说「这个包没装上」。
  if (report.ok) return t('settings.extensionsInstallOk', { name: report.packId })
  return t('settings.extensionsInstallRejected', { name: report.dropped })
}

function clearReports(): void {
  dropReports.value = []
}

async function copyDir(): Promise<void> {
  const dir = dirText.value
  try {
    await navigator.clipboard.writeText(dir)
    errorStore.info(t('common.copy'), t('settings.extensionsDirCopied'), '', 'ExtensionsPanel.copyDir')
  } catch {
    errorStore.warn(t('common.copy'), dir, '', 'ExtensionsPanel.copyDir')
  }
}

async function openDir(): Promise<void> {
  const dir = pluginStore.directory
  if (!dir) return
  try {
    await OpenFolder(dir)
  } catch (e: any) {
    errorStore.fromError(t('settings.extensionsOpenDir'), e, 'ExtensionsPanel.openDir')
  }
}

/** 某个包里在本机被隔离的着色器档位（编译/链接失败过）。 */
function brokenOf(pack: PluginInfo) {
  if (pack.kind !== 'shader') return []
  return pluginStore.allShaderOptions
    .filter(o => o.packId === pack.id && pluginStore.brokenMessage(o.key))
    .map(o => ({ key: o.key, label: pluginStore.shaderLabel(o), message: pluginStore.brokenMessage(o.key) }))
}

/** 脚本包注入的文件清单：入口在前，样式跟在后面，用户照着这个名字去改文件。 */
function scriptFilesOf(pack: PluginInfo): string {
  if (!pack.script) return ''
  return [pack.script.entry, ...(pack.script.styles ?? [])].join(' · ')
}

function adapterNamesOf(pack: PluginInfo): string {
  return (pack.source?.adapters ?? []).map(a => pluginStore.localized(a.name) || a.id).join(' · ')
}

// ---------- 授权闸门 ----------
// 这一行说的是「这个包声明自己要用哪几条能力，以及你上次给了什么答复」。
// 声明来自 manifest（Go 校验过），答复记在 settings 的 plugin_permissions，
// 由 frontend/src/plugins/permissions.ts 在包真的动手那一刻才去要。

function permissionsOf(pack: PluginInfo): PackPermission[] {
  return (pack.permissions ?? []) as PackPermission[]
}

function permLabel(name: PackPermission): string {
  return t(name === 'network' ? 'permissions.networkLabel' : 'permissions.writeLabel')
}

/** 还没问过的能力不显示成「已拒绝」：那个答复根本还没发生。 */
function permStateOf(pack: PluginInfo, name: PackPermission): 'granted' | 'blocked' | 'unasked' {
  const decision = permissionDecision(pack.id, name)
  if (decision === 'granted') return 'granted'
  if (decision === 'denied') return 'blocked'
  return 'unasked'
}

function permStateKey(state: string): string {
  if (state === 'granted') return 'permissions.granted'
  if (state === 'blocked') return 'permissions.blocked'
  return 'permissions.unasked'
}

function permVariant(state: string): 'success' | 'danger' | 'default' {
  if (state === 'granted') return 'success'
  if (state === 'blocked') return 'danger'
  return 'default'
}

async function resetPackPermission(pack: PluginInfo, name: PackPermission): Promise<void> {
  busyId.value = pack.id
  busyAction.value = 'permission'
  try {
    await resetPermission(pack.id, name)
    errorStore.info(t('settings.extensionsTitle'), `${pluginStore.localized(pack.name) || pack.id} · ${permLabel(name)} · ${t('permissions.unasked')}`)
  } finally {
    busyId.value = ''
    busyAction.value = ''
  }
}

/**
 * 次行元信息。校验没过的包连 version 都读不出来，直接拼 `v{{version}}` 会留下半截
 * 「· v」，看着像是我们的解析器坏了而不是他的 manifest 坏了，所以空段一律不进列表。
 */
function metaOf(pack: PluginInfo): string {
  return [
    pack.id,
    pack.version ? `v${pack.version}` : '',
    pack.author,
  ].filter(Boolean).join(' · ')
}

/**
 * 目录统计那一行。数字全部来自 Go 扫描时的 WalkDir（不是 manifest 自称的），
 * 所以它回答的是「这个文件夹里到底有什么」——改完包没生效时，这是唯一能自查的一行。
 */
function statLine(pack: PluginInfo): string {
  return [
    pack.files ? t('settings.extensionsStatFiles', { n: pack.files }) : '',
    pack.bytes ? humanizeBytes(pack.bytes) : '',
    pack.updated ? t('settings.extensionsStatUpdated', { time: pack.updated }) : '',
  ].filter(Boolean).join(' · ')
}

/**
 * 包图标的 data URL，按包 id 存。异步逐个读：一个包里的图标文件读挂了只少一张图，
 * 不该让整张列表停在加载态。
 */
const iconURLs = ref<Record<string, string>>({})

async function loadIcons(): Promise<void> {
  const next: Record<string, string> = {}
  await Promise.all(
    pluginStore.packs
      .filter(pack => pack.icon)
      .map(async pack => {
        const url = await pluginStore.readAsset(pack.id, pack.icon ?? '')
        if (url) next[pack.id] = url
      }),
  )
  iconURLs.value = next
}

// 重扫会清空 store 里的资源缓存，所以 packs 一换就要重读图标，否则换掉的图标不刷新。
watch(() => pluginStore.packs, () => { void loadIcons() }, { immediate: true })

/**
 * 包没带图标时的图形：按 kind 给一个形状，而不是包名首字。首字既不代表能力也
 * 没法一眼区分两个同类的包，而「这个包没画自己的图标」本来就该由形状之外的留白来说明。
 */
const KIND_GLYPHS: Record<string, string> = {
  source: 'source',
  theme: 'palette',
  shader: 'sliders',
  script: 'code',
}

function kindGlyph(pack: PluginInfo): string {
  return KIND_GLYPHS[pack.kind] || 'layers'
}
</script>

<template>
  <!--
    data-file-drop-target 是 Wails 运行时认的那个标记：没有它，指针掠过这里时
    dropEffect 会被强制成 none（鼠标变成禁止符号），原生拖放根本不会落到这张卡片上。
    高亮也不用自己数 dragenter——运行时进出时会给带这个标记的元素加 file-drop-target-active。
  -->
  <div class="ext-card" data-file-drop-target>
    <div class="ext-hd">
      <span class="ext-title">
        <Icon name="layers" :size="13" />
        {{ t('settings.extensionsTitle') }}
      </span>
      <span class="ext-count">{{ pluginStore.packs.length }}</span>
      <div class="ext-acts">
        <Button variant="secondary" size="sm" @click="copyDir">
          <Icon name="copy" :size="12" />
          <span>{{ t('common.copy') }}</span>
        </Button>
        <Button v-if="hasRealDir" variant="secondary" size="sm" @click="openDir">
          <Icon name="folder" :size="12" />
          <span>{{ t('settings.extensionsOpenDir') }}</span>
        </Button>
        <Button
          variant="secondary"
          size="sm"
          :disabled="pending"
          :loading="rescanning"
          @click="rescan"
        >
          <span>{{ t('settings.extensionsRescan') }}</span>
        </Button>
      </div>
    </div>

    <p class="ext-desc">{{ t('settings.extensionsDesc') }}</p>
    <div class="ext-dir" :title="dirText" @click="copyDir">{{ dirText }}</div>
    <div v-if="pluginStore.registryError" class="ext-error">{{ pluginStore.registryError }}</div>

    <!-- 拖放区是整个窗口（投放标记在 App.vue），这张卡片自己也带一份，指针掠过时
         运行时给它加 file-drop-target-active，于是这一条只需要说「能拖」。 -->
    <div class="ext-drop" :class="{ 'is-busy': installing }">
      <Icon :name="installing ? 'refresh' : 'download'" :size="13" />
      <span>{{ installing ? t('settings.extensionsInstalling') : t('settings.extensionsDropHint') }}</span>
    </div>

    <div v-if="dropReports.length" class="ext-results">
      <div
        v-for="(report, index) in dropReports"
        :key="`${report.dropped}-${index}`"
        class="ext-result"
        :class="report.ok ? 'is-ok' : 'is-fail'"
      >
        <span class="ext-result-text">{{ reportText(report) }}</span>
        <span v-if="report.detail" class="ext-result-detail">{{ report.detail }}</span>
        <span v-if="report.reasonCode" class="ext-result-code">{{ report.reasonCode }}</span>
      </div>
      <div class="ext-results-foot">
        <span class="ext-results-note">{{ t('settings.extensionsDropResultsHint') }}</span>
        <Button variant="secondary" size="sm" @click="clearReports">
          <Icon name="close" :size="12" />
          <span>{{ t('common.close') }}</span>
        </Button>
      </div>
    </div>

    <div v-if="!pluginStore.loaded" class="ext-note">{{ t('common.loading') }}</div>
    <Empty
      v-else-if="pluginStore.packs.length === 0"
      icon="📦"
      :title="t('settings.extensionsEmpty')"
      :description="t('settings.extensionsEmptyDesc')"
    />

    <div
      v-for="pack in pluginStore.packs"
      :key="pack.id"
      class="ext-item"
      :class="`is-${pack.status}`"
    >
      <div class="ext-item-hd">
        <span class="ext-item-icon" :class="{ 'is-plain': !iconURLs[pack.id] }" :title="pack.icon || undefined">
          <img v-if="iconURLs[pack.id]" :src="iconURLs[pack.id]" alt="" />
          <Icon v-else :name="kindGlyph(pack)" :size="18" />
        </span>
        <span class="ext-item-name">{{ pluginStore.localized(pack.name) || pack.id }}</span>
        <Tag size="sm">{{ kindLabel(pack.kind) }}</Tag>
        <Tag v-if="pack.builtin" size="sm">{{ t('settings.extensionsBuiltin') }}</Tag>
        <Tag size="sm" :variant="statusVariant(pack.status)">{{ pluginStore.stateLabel(pack.status) }}</Tag>
        <div class="ext-item-side">
          <!-- 内置包不给卸载：那是应用自己的东西，删了它连日志面板都没了。用户想清掉它可以直接删目录，Go 侧也会拒绝。 -->
          <!-- 忙的时候只让被点的那一个按钮转圈，别的行一律保持原样：把整排按钮置成
               disabled 会给它们加 opacity .5，而按钮的 opacity 是带过渡的——于是每开关
               一个包，整个面板先灰一下再亮回来，看起来就是「闪了一屏」。 -->
          <Button
            v-if="!pack.builtin"
            variant="danger"
            size="sm"
            :loading="busyId === pack.id && busyAction === 'uninstall'"
            @click="uninstall(pack)"
          >
            <span>{{ t('settings.extensionsUninstall') }}</span>
          </Button>
          <!-- 启用是一个按钮，不是一张开关：按下去之后这一行的写法就反过来（禁用 + 禁止符号），
               当前状态和即将发生的动作都写在同一处，不必先猜开关的哪一头是「开」。 -->
          <Button
            :variant="pack.status === 'ready' ? 'warning' : 'primary'"
            size="sm"
            :loading="busyId === pack.id && busyAction === 'toggle'"
            :disabled="pack.status === 'invalid'"
            @click="toggle(pack, pack.status !== 'ready')"
          >
            <Icon :name="pack.status === 'ready' ? 'ban' : 'check'" :size="12" />
            <span>{{ pack.status === 'ready' ? t('settings.extensionsDisable') : t('settings.extensionsEnable') }}</span>
          </Button>
        </div>
      </div>

      <div v-if="metaOf(pack)" class="ext-meta">{{ metaOf(pack) }}</div>
      <div v-if="statLine(pack)" class="ext-meta">{{ statLine(pack) }}</div>
      <p v-if="packDesc(pack)" class="ext-item-desc">{{ packDesc(pack) }}</p>
      <!-- 校验没过时机器码和那句人话都得摊开写：藏进 tooltip 或 hover 里就没人能自己查出为什么。 -->
      <div v-if="pack.status === 'invalid'" class="ext-reason">
        <div class="ext-reason-code">{{ pack.reason_code || pack.status }}</div>
        <div v-if="pack.reason" class="ext-reason-code ext-reason-text">{{ pack.reason }}</div>
        <div class="ext-hint">{{ t('settings.extensionsInvalidHint') }}</div>
      </div>

      <div v-if="pack.kind === 'source' && adapterNamesOf(pack)" class="ext-extra">
        {{ t('settings.extensionsAdapters') }}{{ adapterNamesOf(pack) }}
        <div class="ext-hint">{{ t('settings.extensionsSourceHint') }}</div>
      </div>
      <div v-else-if="pack.kind === 'theme' && pack.theme?.presets?.length" class="ext-extra">
        {{ t('settings.extensionsThemePresets') }}{{ pack.theme.presets.map(p => pluginStore.localized(p.name) || p.id).join(' · ') }}
        <div class="ext-hint">{{ t('settings.extensionsThemeHint') }}</div>
      </div>
      <div v-else-if="pack.kind === 'shader' && pack.shader?.shaders?.length" class="ext-extra">
        {{ t('settings.extensionsShaderEntries') }}{{ pack.shader.shaders.map(s => pluginStore.localized(s.name) || s.id).join(' · ') }}
        <div class="ext-hint">{{ t('settings.extensionsShaderHint') }}</div>
      </div>

      <!-- script 是横切段：主题包也能带一段自己的脚本，所以这块独立于上面那条 kind 分支。 -->
      <div v-if="pack.script" class="ext-extra">
        {{ t('settings.extensionsScriptItems') }}{{ scriptFilesOf(pack) }}
        <div class="ext-hint">{{ t('settings.extensionsScriptHint') }}</div>
      </div>

      <!-- 声明了才拦得住：这一行摊开「包要什么」和「你上次给了什么」，撤销了就等于下次重新问。 -->
      <div v-if="permissionsOf(pack).length" class="ext-perms">
        <div class="ext-perms-title">{{ t('permissions.declared') }}</div>
        <div v-for="name in permissionsOf(pack)" :key="name" class="ext-perm">
          <span class="ext-perm-name">{{ permLabel(name) }}</span>
          <Tag size="sm" :variant="permVariant(permStateOf(pack, name))">{{ t(permStateKey(permStateOf(pack, name))) }}</Tag>
          <Button
            v-if="permStateOf(pack, name) !== 'unasked'"
            variant="secondary"
            size="sm"
            :loading="busyId === pack.id && busyAction === 'permission'"
            @click="resetPackPermission(pack, name)"
          >
            <span>{{ t('permissions.reset') }}</span>
          </Button>
        </div>
        <div class="ext-hint">{{ t('permissions.hint') }}</div>
      </div>
      <template v-if="pack.script && pluginStore.scriptError(pack.id)">
        <div class="ext-broken">
          <span class="ext-broken-name">{{ t('settings.extensionsKindScript') }}</span>
          <span class="ext-broken-msg">{{ pluginStore.scriptError(pack.id) }}</span>
          <Button
            variant="secondary"
            size="sm"
            :loading="busyId === pack.id && busyAction === 'retry'"
            @click="retryScriptPack(pack)"
          >
            <span>{{ t('common.retry') }}</span>
          </Button>
        </div>
        <div class="ext-hint">{{ t('settings.extensionsScriptBroken') }}</div>
      </template>

      <div v-for="broken in brokenOf(pack)" :key="broken.key" class="ext-broken">
        <span class="ext-broken-name">{{ broken.label }}</span>
        <span class="ext-broken-msg">{{ broken.message }}</span>
        <Button variant="secondary" size="sm" @click="pluginStore.retryShader(broken.key)">
          <span>{{ t('common.retry') }}</span>
        </Button>
      </div>
      <div v-if="pack.kind === 'shader' && brokenOf(pack).length" class="ext-hint">
        {{ t('settings.extensionsBrokenShader') }}
      </div>
    </div>

    <!-- 卸载是真删目录，这句话得写在面板上；内置包没有那个按钮，也得在这一行说清楚。 -->
    <div class="ext-foot">
      <Icon name="info" :size="12" />
      <span>{{ t('settings.extensionsUninstallHint') }}</span>
    </div>
  </div>
</template>

<style scoped src="../styles/components/extensions-panel.css"></style>
