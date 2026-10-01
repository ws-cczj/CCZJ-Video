<script setup lang="ts">
/**
 * 主题编辑器弹窗：主题分组的两张卡片网格只负责发起「新建 / 编辑预设 / 编辑自定义」，
 * 编辑态（editing、预览临时主题、背景图取色）全部留在这里，由父组件通过模板 ref 调用。
 * 弹窗本体挂在 Teleport 里，拿不到 Settings.vue 的 scope id，所以它自己需要的
 * `.toggle`（深色模式那枚复选框）留在这份样式里，不依赖父级。
 */
defineOptions({ name: 'ThemeEditorModal' })
import { ref, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { Button, Modal } from '../../components/ui'
import { useThemeStore, readSurfaceAlpha, isPackAssetRef, type CustomTheme, type ColorPalette } from '../../stores/theme'
import { useErrorStore } from '../../stores/error'
import { pickColorFromImage } from '../../utils/imagePalette'

const { t } = useI18n()
const themeStore = useThemeStore()
const errorStore = useErrorStore()

// ---------- 主题编辑器 ----------
const editorOpen = ref(false)
const editing = reactive<CustomTheme & { __mode: 'create' | 'edit' }>({
  ...themeStore.makeEmptyCustom(),
  __mode: 'create',
})

const backgroundImageUrl = ref<string | null>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)
let previousThemeId = ''

function colorToHex(c: string): string {
  const m = c.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
  if (m) {
    const r = parseInt(m[1]).toString(16).padStart(2, '0')
    const g = parseInt(m[2]).toString(16).padStart(2, '0')
    const b = parseInt(m[3]).toString(16).padStart(2, '0')
    return `#${r}${g}${b}`
  }
  return c
}

/**
 * 编辑器里「组件角色色」的初值：从当前生效的调色板播种。
 * `<input type="color">` 绑定 undefined 会直接显示成黑色，所以每个字段都必须有值。
 */
function componentColorsFromPalette(p: ColorPalette) {
  return {
    primaryAction: colorToHex(p.btnSolid),
    successColor: colorToHex(p.success),
    warningColor: colorToHex(p.warning),
    dangerColor: colorToHex(p.danger),
    infoColor: colorToHex(p.info),
    tagColor: colorToHex(p.bgTag),
    carouselColor: colorToHex(p.carouselControl),
  }
}

/** 只填空位：老存档里缺哪个补哪个，已保存的用户选择不会被覆盖。 */
function fillMissingComponentColors(p: ColorPalette): void {
  const seed = componentColorsFromPalette(p)
  for (const [key, value] of Object.entries(seed)) {
    if (!(editing as Record<string, unknown>)[key]) {
      (editing as Record<string, unknown>)[key] = value
    }
  }
}

/**
 * 透明度滑块播种（只填空位，已保存的用户选择不会被覆盖）：读当前生效调色板里
 * 那层 rgba 的 A 通道。带背景图的预设手工写的是 0.92~0.95，硬编码 0.65/0.88
 * 会让滑块一打开就和主题实际观感对不上。
 */
function seedSurfaceAlphas(p: ColorPalette): void {
  if (typeof editing.sidebarAlpha !== 'number') editing.sidebarAlpha = readSurfaceAlpha(p.bgSidebar) ?? 0.65
  if (typeof editing.contentAlpha !== 'number') editing.contentAlpha = readSurfaceAlpha(p.bgCard) ?? 0.88
}

function openCreate(): void {
  previousThemeId = themeStore.currentId
  const cur = themeStore.current
  // 与 openEdit/openEditPreset 一致：先清空 editing，否则上次编辑的语义色会残留到新建主题
  Object.keys(editing).forEach((k) => { delete (editing as any)[k] })
  Object.assign(editing, {
    id: `custom_${Date.now()}`,
    name: cur.name + t('settings.copySuffix'),
    primary: cur.accent,
    text: colorToHex(cur.palette.textPrimary),
    background: colorToHex(cur.palette.bgApp),
    sidebar: colorToHex(cur.palette.bgSidebar),
    content: colorToHex(cur.palette.bgCard),
    ...componentColorsFromPalette(cur.palette),
    backgroundImage: cur.bgImage,
    dark: cur.mode === 'dark',
    __mode: 'create' as const,
  })
  seedSurfaceAlphas(cur.palette)
  backgroundImageUrl.value = bgSrc(cur.bgImage) || null
  editorOpen.value = true
  applyPreview()
}

function openEditPreset(preset: { id: string; name: string; primary: string; palette: ColorPalette; mode: 'dark' | 'light'; bgImage?: string }): void {
  previousThemeId = themeStore.currentId
  const existingOverride = themeStore.customThemes.find((c) => c.id === preset.id)
  Object.keys(editing).forEach((k) => { delete (editing as any)[k] })
  if (existingOverride) {
    // 如果已有 override，但它的 backgroundImage 是旧构建的预设资源路径，
    // 则强制替换为当前构建的正确 URL，避免显示破图。
    const fixed: CustomTheme & { __mode: 'edit' } = {
      ...JSON.parse(JSON.stringify(existingOverride)),
      __mode: 'edit',
    }
    if (preset.bgImage) {
      const candidate = themeStore.resolvePresetAsset(fixed.backgroundImage)
      if (candidate && candidate !== fixed.backgroundImage) {
        fixed.backgroundImage = preset.bgImage
      }
    }
    Object.assign(editing, fixed)
    fillMissingComponentColors(preset.palette)
    seedSurfaceAlphas(preset.palette)
  } else {
    Object.assign(editing, {
      id: preset.id,
      name: preset.name,
      primary: preset.primary,
      text: colorToHex(preset.palette.textPrimary),
      background: colorToHex(preset.palette.bgApp),
      sidebar: colorToHex(preset.palette.bgSidebar),
      content: colorToHex(preset.palette.bgCard),
      ...componentColorsFromPalette(preset.palette),
      backgroundImage: preset.bgImage || null,
      dark: preset.mode === 'dark',
      __mode: 'edit' as const,
    })
    seedSurfaceAlphas(preset.palette)
  }
  backgroundImageUrl.value = bgSrc(editing.backgroundImage) || null
  editorOpen.value = true
  applyPreview()
}

function openEdit(id: string): void {
  const existing = themeStore.customThemes.find((c) => c.id === id)
  if (!existing) return
  previousThemeId = themeStore.currentId
  const copy: any = JSON.parse(JSON.stringify(existing))
  // 先清理 editing 所有字段，再合并现有主题字段 + __mode: 'edit'，确保编辑模式正确
  Object.keys(editing).forEach((k) => { delete (editing as any)[k] })
  copy.__mode = 'edit'
  // 对独立自定义主题（非预设覆盖）也修一次资源 URL —— 但扩展包引用要跳过：
  // 它不是构建产物路径，一旦被换成 data URL，保存时就会连着几 MB 的 base64 写回
  // theme_customs 那一行。引用串本身保持稳定，展示时交给 bgSrc。
  if (copy.backgroundImage && !isPackAssetRef(copy.backgroundImage)) {
    const resolved = themeStore.resolvePresetAsset(copy.backgroundImage)
    if (resolved && resolved !== copy.backgroundImage) copy.backgroundImage = resolved
  }
  Object.assign(editing, copy)
  // 老存档可能没有这些角色色：按当前主题算一遍补上，别让取色框变成纯黑
  fillMissingComponentColors(themeStore.paletteFromCustom(existing))
  backgroundImageUrl.value = bgSrc(editing.backgroundImage) || null
  editorOpen.value = true
  applyPreview()
}

/** 背景字段的实际取址：pack:// 引用换成扩展包里读出的图，读不到就当作没有背景。 */
function bgSrc(bg?: string | null): string | undefined {
  return themeStore.resolveThemeBg(bg || undefined)
}

function closeEditor(): void {
  // 取消编辑：清理 __preview__ 临时主题，并还原之前的主题
  themeStore.customThemes = themeStore.customThemes.filter(c => c.id !== '__preview__')
  if (previousThemeId && themeStore.currentId === '__preview__') {
    themeStore.setTheme(previousThemeId)
  }
  editorOpen.value = false
}

// 预览：把 editing 作为临时主题推入并切换
function applyPreview(): void {
  const preview: CustomTheme = {
    id: '__preview__',
    name: editing.name || t('settings.previewTheme'),
    primary: editing.primary,
    text: editing.text,
    background: editing.background,
    sidebar: editing.sidebar,
    content: editing.content,
    primaryAction: editing.primaryAction,
    successColor: editing.successColor,
    warningColor: editing.warningColor,
    dangerColor: editing.dangerColor,
    infoColor: editing.infoColor,
    tagColor: editing.tagColor,
    carouselColor: editing.carouselColor,
    backgroundImage: editing.backgroundImage,
    dark: editing.dark,
    sidebarAlpha: editing.sidebarAlpha ?? 0.65,
    contentAlpha: editing.contentAlpha ?? 0.88,
  }
  const others = themeStore.customThemes.filter(c => c.id !== '__preview__')
  themeStore.customThemes = [...others, preview]
  themeStore.currentId = '__preview__'
  themeStore.apply()
}

async function saveEditing(): Promise<void> {
  const name = (editing.name || t('settings.myTheme')).trim()
  const isEdit = editing.__mode === 'edit'
  const targetId = isEdit ? editing.id : `custom_${Date.now()}`

  // 先清理预览临时主题
  themeStore.customThemes = themeStore.customThemes.filter((c) => c.id !== '__preview__')

  const themeToSave: CustomTheme = {
    id: targetId,
    name,
    primary: editing.primary,
    text: editing.text,
    background: editing.background,
    sidebar: editing.sidebar,
    content: editing.content,
    primaryAction: editing.primaryAction,
    successColor: editing.successColor,
    warningColor: editing.warningColor,
    dangerColor: editing.dangerColor,
    infoColor: editing.infoColor,
    tagColor: editing.tagColor,
    carouselColor: editing.carouselColor,
    backgroundImage: editing.backgroundImage,
    dark: editing.dark,
    sidebarAlpha: editing.sidebarAlpha ?? 0.65,
    contentAlpha: editing.contentAlpha ?? 0.88,
  }
  await themeStore.saveCustom(themeToSave)
  editorOpen.value = false
}

async function deleteEditing(): Promise<void> {
  if (editing.__mode !== 'edit') return
  const id = editing.id
  editorOpen.value = false
  // 先清理 __preview__ 临时主题
  themeStore.customThemes = themeStore.customThemes.filter((c) => c.id !== '__preview__')
  // 用 deleteCustom 来删除并持久化
  await themeStore.deleteCustom(id)
  // 如果当前主题就是被删除的，deleteCustom 会切到默认主题
}

// 派生：按主色 + 深浅模式一键生成全套配色
function deriveFromPrimary(): void {
  // 带背景图的预设要把作者那层 tint 一起交给派生函数，否则"派生"会把暖灰底换成通用灰。
  const tint = themeStore.themes.find((preset) => preset.id === editing.id)?.tint
  const derived = themeStore.deriveFromPrimary(editing.primary, editing.dark, tint)
  editing.text = derived.text
  editing.background = derived.background
  editing.sidebar = derived.sidebar
  editing.content = derived.content
  editing.sidebarAlpha = derived.sidebarAlpha
  editing.contentAlpha = derived.contentAlpha
  // 按钮色只认 store 返回的 primaryAction：那个废弃别名字段读出来永远是
  // undefined，绑到取色框会把色块变成纯黑并静默丢掉用户选的按钮色。
  editing.primaryAction = derived.primaryAction
  editing.successColor = derived.successColor
  editing.warningColor = derived.warningColor
  editing.dangerColor = derived.dangerColor
  editing.infoColor = derived.infoColor
  editing.tagColor = derived.tagColor
  editing.carouselColor = derived.carouselColor
  applyPreview()
}

// 图片选择
async function applyImagePalette(): Promise<void> {
  const src = bgSrc(editing.backgroundImage)
  if (!src) return
  const picked = await pickColorFromImage(src)
  if (!picked) {
    errorStore.warn(t('settings.pickFromImage'), t('settings.pickFromImageFailed'), '', 'Settings.applyImagePalette')
    return
  }
  editing.primary = picked
  deriveFromPrimary()
  errorStore.info(t('settings.pickFromImage'), picked, '', 'Settings.applyImagePalette')
}

function onImageSelected(evt: Event): void {
  const input = evt.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const url = reader.result as string
    backgroundImageUrl.value = url
    editing.backgroundImage = url
    applyPreview()
    void applyImagePalette()
  }
  reader.readAsDataURL(file)
  // 同一张图换一次也要能重新取色：清掉 value，否则再选同一文件不触发 change
  input.value = ''
}

function clearBackgroundImage(): void {
  backgroundImageUrl.value = null
  editing.backgroundImage = undefined
  applyPreview()
}

function onDropZoneClick(): void {
  if (backgroundImageUrl.value) return
  fileInputRef.value?.click()
}
function onDragOver(): void { /* hover state handled by CSS via :hover */ }
function onDragLeave(): void { /* hover state handled by CSS */ }
function onDrop(e: DragEvent): void {
  const file = e.dataTransfer?.files?.[0]
  if (!file || !file.type.startsWith('image/')) return
  const reader = new FileReader()
  reader.onload = () => {
    const url = reader.result as string
    backgroundImageUrl.value = url
    editing.backgroundImage = url
    applyPreview()
    void applyImagePalette()
  }
  reader.readAsDataURL(file)
}

defineExpose({ openCreate, openEdit, openEditPreset, applyPreview })
</script>

<template>
  <Modal
    :model-value="editorOpen"
    :title="editing.__mode === 'edit' ? t('settings.editTheme') : t('settings.createTheme')"
    width="min(980px, 94vw)"
    :show-footer="true"
    @update:model-value="(v: boolean) => !v && closeEditor()"
  >
    <div class="modal-body">
      <div class="edit-row cczj-flex cczj-flex-wrap">
        <div class="field cczj-flex cczj-flex-col cczj-gap-2">
          <label>{{ t('settings.themeName') }}</label>
          <input type="text" v-model="editing.name" :placeholder="t('settings.myThemePlaceholder')" @input="applyPreview" />
        </div>
        <div class="flags cczj-flex cczj-items-center cczj-flex-wrap">
          <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
            <input type="checkbox" v-model="editing.dark" @change="applyPreview" />
            <span>{{ t('settings.darkMode') }}</span>
          </label>
          <Button variant="primary" size="sm" @click="deriveFromPrimary">
            <Icon name="sparkle" :size="13" /> {{ t('settings.deriveColors') }}
          </Button>
        </div>
      </div>

      <h4 class="group-title">{{ t('settings.mainColors') }}</h4>
      <div class="picker-grid">
        <div class="picker-item">
          <label>{{ t('settings.accentColor') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.primary" @input="applyPreview" /><span>{{ editing.primary }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.textColor') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.text" @input="applyPreview" /><span>{{ editing.text }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.bgApp') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.background" @input="applyPreview" /><span>{{ editing.background }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.bgSidebar') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.sidebar" @input="applyPreview" /><span>{{ editing.sidebar }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.bgContent') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.content" @input="applyPreview" /><span>{{ editing.content }}</span></div>
        </div>
      </div>

      <h4 class="group-title">{{ t('settings.bgAlpha') }}</h4>
      <div class="picker-grid">
        <div class="picker-item">
          <label>{{ t('settings.sidebarAlpha') }}</label>
          <div class="picker-cell range-cell cczj-flex cczj-items-center cczj-gap-4">
            <input type="range" v-model.number="editing.sidebarAlpha" min="0" max="1" step="0.05" @input="applyPreview" />
            <span>{{ Math.round((editing.sidebarAlpha ?? 0.65) * 100) }}%</span>
          </div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.cardAlpha') }}</label>
          <div class="picker-cell range-cell cczj-flex cczj-items-center cczj-gap-4">
            <input type="range" v-model.number="editing.contentAlpha" min="0" max="1" step="0.05" @input="applyPreview" />
            <span>{{ Math.round((editing.contentAlpha ?? 0.88) * 100) }}%</span>
          </div>
        </div>
      </div>

      <h4 class="group-title">{{ t('settings.globalColors') }}</h4>
      <div class="picker-grid small">
        <div class="picker-item"><label>{{ t('settings.btnAction') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.primaryAction" @input="applyPreview" /><span>{{ editing.primaryAction }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.tagColor') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.tagColor" @input="applyPreview" /><span>{{ editing.tagColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.carouselColor') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.carouselColor" @input="applyPreview" /><span>{{ editing.carouselColor }}</span></div></div>
      </div>

      <h4 class="group-title">{{ t('settings.semanticColors') }}</h4>
      <div class="picker-grid small">
        <div class="picker-item"><label>{{ t('settings.paletteRoleSuccess') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.successColor" @input="applyPreview" /><span>{{ editing.successColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.paletteRoleWarning') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.warningColor" @input="applyPreview" /><span>{{ editing.warningColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.paletteRoleDanger') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.dangerColor" @input="applyPreview" /><span>{{ editing.dangerColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.paletteRoleInfo') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.infoColor" @input="applyPreview" /><span>{{ editing.infoColor }}</span></div></div>
      </div>

      <div
        class="bg-drop-zone cczj-relative cczj-flex cczj-items-center cczj-justify-center cczj-overflow-hidden cczj-cursor-pointer"
        :class="{ 'has-image': !!backgroundImageUrl }"
        @click="onDropZoneClick"
        @dragover.prevent="onDragOver"
        @dragleave="onDragLeave"
        @drop.prevent="onDrop"
      >
        <button
          v-if="backgroundImageUrl"
          class="bg-remove cczj-absolute cczj-flex cczj-items-center cczj-justify-center"
          :title="t('settings.removeImage')"
          @click.stop="clearBackgroundImage"
        >
          <Icon name="x" :size="14" />
        </button>
        <img v-if="backgroundImageUrl" :src="backgroundImageUrl" :alt="t('settings.backgroundPreview')" />
        <div v-else class="bg-drop-hint cczj-flex cczj-flex-col cczj-items-center cczj-gap-5">
          <Icon name="plus" :size="42" />
          <span>{{ t('settings.dropImage') }}</span>
        </div>
        <input ref="fileInputRef" type="file" accept="image/*" @change="onImageSelected" hidden />
      </div>
      <div v-if="backgroundImageUrl" class="cczj-flex cczj-items-center cczj-gap-4 cczj-mt-2">
        <Button variant="secondary" size="sm" @click="applyImagePalette">
          <Icon name="image" :size="13" /> {{ t('settings.pickFromImage') }}
        </Button>
        <span class="cczj-text-xs cczj-text-muted">{{ t('settings.pickFromImageHint') }}</span>
      </div>
    </div>

    <template #footer>
      <Button v-if="editing.__mode === 'edit'" variant="danger" size="md" @click="deleteEditing">
        <Icon name="trash" :size="14" /> {{ t('common.delete') }}
      </Button>
      <span style="flex: 1"></span>
      <Button variant="secondary" size="md" @click="closeEditor">{{ t('common.cancel') }}</Button>
      <Button variant="primary" size="md" @click="saveEditing">
        <Icon name="save" :size="14" /> {{ t('settings.saveTheme') }}
      </Button>
    </template>
  </Modal>
</template>

<style scoped>
.modal-body {
  padding: 18px 20px;
  overflow-y: auto;
}
.edit-row {
  align-items: flex-end;
  gap: 20px;
  padding-bottom: 14px;
  margin-bottom: 10px;
  border-bottom: 1px dashed var(--border);
}
.field { gap: 4px; flex: 1; min-width: 180px; }
.field label { font-size: 0.79rem; color: var(--text-muted); font-weight: 600; }
.field input[type='text'] {
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 0.93rem;
  outline: none;
}
.field input[type='text']:focus { border-color: var(--accent); }
.flags { gap: 18px; }

.group-title {
  font-size: 0.79rem;
  color: var(--text-muted);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 1px;
  margin: 12px 0 8px;
}
.picker-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 10px;
  margin-bottom: 6px;
}
.picker-grid.small {
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
}
.picker-item label {
  display: block;
  font-size: 0.79rem;
  color: var(--text-muted);
  margin-bottom: 4px;
  font-weight: 600;
}
.picker-cell {
  gap: 8px;
  padding: 8px 10px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
}
.picker-cell input[type='color'] {
  width: 32px; height: 28px;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0;
  background: transparent;
  cursor: pointer;
}
.picker-cell span {
  font-size: 0.79rem;
  color: var(--text-secondary);
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
}
.picker-cell.range-cell {
  gap: 10px;
}
.picker-cell.range-cell input[type='range'] {
  flex: 1;
  height: 4px;
  -webkit-appearance: none;
  appearance: none;
  background: var(--border);
  border-radius: 2px;
  outline: none;
}
.picker-cell.range-cell input[type='range']::-webkit-slider-thumb {
  -webkit-appearance: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--accent);
  cursor: pointer;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.2);
}
.bg-drop-zone {
  margin-top: 14px;
  border: 2px dashed var(--border);
  border-radius: 10px;
  min-height: 180px;
  background: var(--bg-card);
  transition: all 0.15s ease;
}
.bg-drop-zone:hover { border-color: var(--accent); background: var(--bg-hover); }
.bg-drop-zone.has-image { border-style: solid; cursor: default; padding: 0; }
.bg-drop-zone.has-image:hover { background: var(--bg-card); }
.bg-drop-zone img {
  display: block;
  width: 100%;
  height: auto;
  max-height: 240px;
  object-fit: cover;
}
.bg-drop-hint {
  gap: 10px;
  color: var(--text-secondary);
}
.bg-drop-hint span { font-size: 0.93rem; }
.bg-remove {
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: rgba(0,0,0,0.55);
  color: #fff;
  border: none;
  cursor: pointer;
  z-index: 2;
  transition: background 0.15s ease;
}
.bg-remove:hover { background: var(--danger); }

.toggle {
  gap: 8px;
  font-size: 0.93rem;
  color: var(--text-primary);
}
.toggle input[type='checkbox'] {
  -webkit-appearance: none;
  appearance: none;
  width: 18px;
  height: 18px;
  border: 1.5px solid var(--border-strong);
  border-radius: 5px;
  background: var(--bg-card);
  cursor: pointer;
  position: relative;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.toggle input[type='checkbox']:hover {
  border-color: var(--accent);
}
.toggle input[type='checkbox']:checked {
  background: var(--accent);
  border-color: var(--accent);
}
.toggle input[type='checkbox']:checked::after {
  content: '';
  position: absolute;
  top: 3px;
  left: 5px;
  width: 4px;
  height: 8px;
  border: 2px solid var(--accent-contrast);
  border-top: 0;
  border-left: 0;
  transform: rotate(45deg);
}

.small { font-size: 0.79rem; }
</style>
