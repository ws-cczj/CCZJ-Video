<script setup lang="ts">
/**
 * 主题编辑器弹窗：主题分组的两张卡片网格只负责发起「新建 / 编辑预设 / 编辑自定义」，
 * 编辑态（editing、预览临时主题、背景图取色）全部留在这里，由父组件通过模板 ref 调用。
 * 弹窗本体挂在 Teleport 里，拿不到 Settings.vue 的 scope id，所以它自己需要的
 * `.toggle`（深色模式那枚复选框）留在这份样式里，不依赖父级。
 */
defineOptions({ name: 'ThemeEditorModal' })
import { ref, reactive, computed, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { Button, Modal, Spinner } from '../../components/ui'
import { useThemeStore, readSurfaceAlpha, isPackAssetRef, readThemeRoleColors, THEME_ROLE_KEYS, type ThemeColorRole, type CustomTheme, type ColorPalette } from '../../stores/theme'
import { useErrorStore } from '../../stores/error'
import { pickColorFromImage } from '../../utils/imagePalette'
import { isMobileRuntime } from '../../api/runtime'
import { createRequestScope } from '../../api/requestScope'
import ThemeImageCrop from './ThemeImageCrop.vue'
import { prepareThemeImage, type ThemeCrop } from '../../utils/themeImage'
import { readFileDataURL } from '../../api/fileTransfer'

const { t } = useI18n()
const mobile = isMobileRuntime()
const themeStore = useThemeStore()
const errorStore = useErrorStore()
const replaceManual=ref(false)
const manualFields=new Set<string>()
const basicColors=['text','background','sidebar','content','primaryAction','successColor','warningColor','dangerColor','infoColor','tagColor','carouselColor','sidebarAlpha','contentAlpha'] as const
const roleGroups: {label:string;roles:ThemeColorRole[]}[]=[
  {label:'paletteSurfaces',roles:['bgInput','bgElevated','border','borderStrong']},
  {label:'paletteText',roles:['textSecondary','textMuted','accentText']},
  {label:'paletteControls',roles:['btnSolidText','btnSoft','btnSoftText','selectedBg','selectedText','selectedBorder']},
  {label:'paletteEpisodes',roles:['episodeBg','episodeText','episodeWatchedBg','episodeWatchedText','episodeActiveBg','episodeActiveText','episodeProgress']},
  {label:'semanticColors',roles:['successText','warningText','dangerText','infoText']},
]

// ---------- 主题编辑器 ----------
const editorOpen = ref(false)
const editing = reactive<CustomTheme & { __mode: 'create' | 'edit' }>({
  ...themeStore.makeEmptyCustom(),
  __mode: 'create',
})
const rolePalette=computed(()=>themeStore.paletteFromCustom(editing))
function roleValue(role:ThemeColorRole):string {return colorToHex(rolePalette.value[role])}
function setRole(role:ThemeColorRole,event:Event):void {
  manualFields.add(role)
  editing.roleColors={...editing.roleColors,[role]:(event.target as HTMLInputElement).value}
  applyPreview()
}
function resetRole(role:ThemeColorRole):void {
  manualFields.delete(role)
  const colors={...editing.roleColors};delete colors[role];editing.roleColors=colors
  applyPreview()
}
function editColor(key:string):void {manualFields.add(key);applyPreview()}
function prepareManualFields(existing?:CustomTheme):void {
  replaceManual.value=false;manualFields.clear()
  if(existing){for(const key of basicColors)if(existing[key]!==undefined)manualFields.add(key);for(const key of THEME_ROLE_KEYS)if(existing.roleColors?.[key])manualFields.add(key)}
}

const backgroundImageUrl = ref<string | null>(null)
const cropSource=ref<string | null>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)
const imageBusy=ref(false), paletteBusy=ref(false), saving=ref(false), cropOpen=ref(false)
const previewLoading=ref(false),previewFailed=ref(false),previewAttempt=ref(0)
let previewTimer:ReturnType<typeof setTimeout>|undefined
watch(backgroundImageUrl,src=>{
  clearTimeout(previewTimer);previewFailed.value=false;previewLoading.value=!!src
  if(src)previewTimer=setTimeout(()=>previewError(),30000)
},{flush:'sync'})
function previewLoaded():void {clearTimeout(previewTimer);previewLoading.value=false;previewFailed.value=false}
function previewError():void {clearTimeout(previewTimer);previewLoading.value=false;previewFailed.value=true}
function retryPreview():void {previewLoading.value=true;previewFailed.value=false;previewAttempt.value++;clearTimeout(previewTimer);previewTimer=setTimeout(previewError,30000)}
const busy=computed(()=>imageBusy.value||paletteBusy.value||saving.value||previewLoading.value)
let originalImage:Blob|null=null
let imageAbort:AbortController|null=null
let previousThemeId = ''
const imageRequests = createRequestScope()
let imageGeneration = 0, disposed = false
let paletteAbort: AbortController | null = null
function releaseImages(): void { imageGeneration++; imageAbort?.abort(); imageAbort=null; imageBusy.value=false; paletteBusy.value=false; cropOpen.value=false; originalImage=null; imageRequests.cancel(); paletteAbort?.abort(); paletteAbort = null }
watch(editorOpen, open => { if (!open) {releaseImages();cropSource.value=null;backgroundImageUrl.value=null} }, { flush: 'sync' })
onBeforeUnmount(() => { clearTimeout(previewTimer);disposed = true; releaseImages(); if (editorOpen.value) closeEditor() })

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

function separateLegacyEpisodes(p:ColorPalette):void {
  if(editing.colorVersion===2)return
  editing.roleColors={episodeBg:colorToHex(p.episodeBg),episodeText:colorToHex(p.episodeText),...readThemeRoleColors(editing.roleColors)}
  editing.colorVersion=2
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
  releaseImages()
  prepareManualFields()
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
    backgroundOriginal:themeStore.customThemes.find(c=>c.id===cur.id)?.backgroundOriginal,
    dark: cur.mode === 'dark',
    roleColors:readThemeRoleColors(Object.fromEntries(THEME_ROLE_KEYS.map(key=>[key,colorToHex(cur.palette[key])]))),
    colorVersion:2,
    __mode: 'create' as const,
  })
  seedSurfaceAlphas(cur.palette)
  backgroundImageUrl.value = bgSrc(cur.bgImage) || null
  cropSource.value=bgSrc(editing.backgroundOriginal)||backgroundImageUrl.value
  editorOpen.value = true
  applyPreview()
}

function openEditPreset(preset: { id: string; name: string; primary: string; palette: ColorPalette; mode: 'dark' | 'light'; bgImage?: string }): void {
  releaseImages()
  previousThemeId = themeStore.currentId
  const existingOverride = themeStore.customThemes.find((c) => c.id === preset.id)
  prepareManualFields(existingOverride)
  Object.keys(editing).forEach((k) => { delete (editing as any)[k] })
  if (existingOverride) {
    // 如果已有 override，但它的 backgroundImage 是旧构建的预设资源路径，
    // 则强制替换为当前构建的正确 URL，避免显示破图。
    const fixed: CustomTheme & { __mode: 'edit' } = {
      ...existingOverride,
      roleColors:readThemeRoleColors(existingOverride.roleColors),
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
    separateLegacyEpisodes(themeStore.paletteFromCustom(existingOverride))
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
      roleColors:readThemeRoleColors(themeStore.themes.find(t=>t.id===preset.id)?.tint),
      colorVersion:2,
      __mode: 'edit' as const,
    })
    seedSurfaceAlphas(preset.palette)
  }
  backgroundImageUrl.value = bgSrc(editing.backgroundImage) || null
  cropSource.value=bgSrc(editing.backgroundOriginal)||backgroundImageUrl.value
  editorOpen.value = true
  applyPreview()
}

function openEdit(id: string): void {
  const existing = themeStore.customThemes.find((c) => c.id === id)
  if (!existing) return
  prepareManualFields(existing)
  releaseImages()
  previousThemeId = themeStore.currentId
  const copy: any = {...existing,roleColors:readThemeRoleColors(existing.roleColors)}
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
  separateLegacyEpisodes(themeStore.paletteFromCustom(existing))
  backgroundImageUrl.value = bgSrc(editing.backgroundImage) || null
  cropSource.value=bgSrc(editing.backgroundOriginal)||backgroundImageUrl.value
  editorOpen.value = true
  applyPreview()
}

/** 背景字段的实际取址：pack:// 引用换成扩展包里读出的图，读不到就当作没有背景。 */
function bgSrc(bg?: string | null): string | undefined {
  return themeStore.resolveThemeBg(bg || undefined)
}

function closeEditor(): void {
  if(saving.value&&!disposed)return
  // 取消编辑：清理 __preview__ 临时主题，并还原之前的主题
  themeStore.customThemes = themeStore.customThemes.filter(c => c.id !== '__preview__')
  if (previousThemeId && themeStore.currentId === '__preview__') {
    themeStore.currentId=previousThemeId
    themeStore.apply()
  }
  editorOpen.value = false
}

// 预览：把 editing 作为临时主题推入并切换
function applyPreview(): void {
  if (disposed || !editorOpen.value) return
  const preview: CustomTheme = {
    id: '__preview__',
    previewBaseId:editing.id,
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
    roleColors:readThemeRoleColors(editing.roleColors),
    colorVersion:editing.colorVersion,
  }
  const others = themeStore.customThemes.filter(c => c.id !== '__preview__')
  themeStore.customThemes = [...others, preview]
  themeStore.currentId = '__preview__'
  themeStore.apply()
}

async function saveEditing(): Promise<void> {
  if(busy.value||cropOpen.value)return
  saving.value=true
  try {
    await new Promise(resolve=>setTimeout(resolve,0))
    const name = (editing.name || t('settings.myTheme')).trim()
    const targetId = editing.id

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
      backgroundOriginal:editing.backgroundOriginal,
      dark: editing.dark,
      sidebarAlpha: editing.sidebarAlpha ?? 0.65,
      contentAlpha: editing.contentAlpha ?? 0.88,
      roleColors:readThemeRoleColors(editing.roleColors),
      colorVersion:editing.colorVersion,
    }
    if(await themeStore.saveCustom(themeToSave))editorOpen.value = false
    else applyPreview()
  } catch(error){errorStore.fromError(t('settings.saveTheme'),error,'ThemeEditorModal.save');applyPreview()}
  finally {saving.value=false}
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
  for(const key of basicColors){
    if(replaceManual.value || !manualFields.has(key))Object.assign(editing,{[key]:derived[key]})
  }
  editing.roleColors={...derived.roleColors,...Object.fromEntries(Object.entries(editing.roleColors??{}).filter(([key])=>!replaceManual.value&&manualFields.has(key)))}
  editing.colorVersion=2
  if(replaceManual.value)manualFields.clear()
  applyPreview()
}

// 图片选择
async function applyImagePalette(): Promise<void> {
  const src = bgSrc(editing.backgroundImage)
  if (!src || disposed || !editorOpen.value) return
  const generation = imageGeneration
  paletteAbort?.abort()
  const abort = new AbortController()
  paletteAbort = abort
  paletteBusy.value=true
  const picked = await pickColorFromImage(src, abort.signal)
  if (paletteAbort === abort) {paletteAbort = null;paletteBusy.value=false}
  if (disposed || !editorOpen.value || abort.signal.aborted || generation !== imageGeneration || src !== bgSrc(editing.backgroundImage)) return
  if (!picked) {
    errorStore.warn(t('settings.pickFromImage'), t('settings.pickFromImageFailed'), '', 'Settings.applyImagePalette')
    return
  }
  editing.primary = picked
  deriveFromPrimary()
  errorStore.info(t('settings.pickFromImage'), picked, '', 'Settings.applyImagePalette')
}

async function loadBackgroundImage(file: File): Promise<void> {
  if(saving.value)return
  releaseImages()
  originalImage=file
  await processImage(file)
}

async function processImage(blob:Blob,crop?:ThemeCrop):Promise<void> {
  if(crop?.x===0&&crop.y===0&&crop.width===100&&crop.height===100)crop=undefined
  const generation=++imageGeneration
  imageAbort?.abort();paletteAbort?.abort();paletteBusy.value=false
  const abort=new AbortController();imageAbort=abort;imageBusy.value=true
  try {
    const prepared=await prepareThemeImage(blob,abort.signal,crop)
    if(abort.signal.aborted)return
    const url=await imageRequests.run(()=>readFileDataURL(new File([prepared],'background',{type:prepared.type})))
    const source=editing.backgroundOriginal??cropSource.value
    const original=crop?(source?.startsWith('data:')?source:await imageRequests.run(()=>readFileDataURL(new File([blob],'original',{type:blob.type})))):undefined
    if(disposed||!editorOpen.value||abort.signal.aborted||generation!==imageGeneration)return
    editing.backgroundOriginal=original
    backgroundImageUrl.value=url;editing.backgroundImage=url;cropOpen.value=false
    if(!crop)cropSource.value=url
    applyPreview()
    await applyImagePalette()
  } catch(error) {
    if(!crop&&generation===imageGeneration)originalImage=null
    if(!abort.signal.aborted&&!disposed&&editorOpen.value&&generation===imageGeneration)
      errorStore.fromError(t('settings.backgroundPreview'),error,'ThemeEditorModal.backgroundImage')
  } finally {if(generation===imageGeneration){imageBusy.value=false;imageAbort=null}}
}

async function applyCrop(crop:ThemeCrop):Promise<void> {
  if(busy.value)return
  if(originalImage){await processImage(originalImage,crop);return}
  const src=cropSource.value
  if(!src)return
  const generation=imageGeneration
  const abort=new AbortController();imageAbort=abort;imageBusy.value=true
  try {
    const response=await fetch(src,{signal:abort.signal})
    if(!response.ok)throw new Error(t('settings.imageProcessFailed'))
    const blob=await response.blob()
    if(generation!==imageGeneration||abort.signal.aborted)return
    originalImage=blob
    await processImage(blob,crop)
  } catch(error){if(!abort.signal.aborted)errorStore.fromError(t('settings.cropImage'),error,'ThemeEditorModal.crop')}
  finally{if(generation===imageGeneration)imageBusy.value=false}
}

async function restoreOriginal():Promise<void> {
  if(busy.value||!editing.backgroundOriginal)return
  const original=editing.backgroundOriginal
  releaseImages()
  editing.backgroundImage=original;editing.backgroundOriginal=undefined
  backgroundImageUrl.value=bgSrc(original)??null;cropSource.value=backgroundImageUrl.value
  applyPreview()
  await applyImagePalette()
}

function onImageSelected(evt: Event): void {
  const input = evt.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  void loadBackgroundImage(file)
  // 同一张图换一次也要能重新取色：清掉 value，否则再选同一文件不触发 change
  input.value = ''
}

function clearBackgroundImage(): void {
  releaseImages()
  backgroundImageUrl.value = null
  cropSource.value=null
  editing.backgroundImage = undefined
  editing.backgroundOriginal=undefined
  applyPreview()
}

function onDropZoneClick(): void {
  if (backgroundImageUrl.value) return
  fileInputRef.value?.click()
}
function onDragOver(event:DragEvent): void { if(event.dataTransfer)event.dataTransfer.dropEffect='copy' }
function onDragLeave(): void { /* hover state handled by CSS */ }
function onDrop(e: DragEvent): void {
  (e.currentTarget as HTMLElement).classList.remove('file-drop-target-active')
  if(busy.value)return
  const file = e.dataTransfer?.files?.[0]
  if (!file) return
  void loadBackgroundImage(file)
}

defineExpose({ openCreate, openEdit, openEditPreset, applyPreview })
</script>

<template>
  <Modal
    :model-value="editorOpen"
    :title="editing.__mode === 'edit' ? t('settings.editTheme') : t('settings.createTheme')"
    width="min(980px, 94vw)"
    :show-footer="true"
    :closable="!saving"
    @update:model-value="(v: boolean) => !v && closeEditor()"
  >
    <div class="modal-body cczj-mobile-theme-editor"
      :aria-busy="busy">
      <fieldset :disabled="busy" class="editor-fields cczj-min-w-0">
      <div class="edit-row cczj-flex cczj-flex-wrap">
        <div class="field cczj-flex cczj-flex-col cczj-gap-2">
          <label>{{ t('settings.themeName') }}</label>
          <input type="text" :aria-label="t('settings.themeName')" v-model="editing.name" :placeholder="t('settings.myThemePlaceholder')" @input="applyPreview" />
        </div>
        <div class="flags cczj-flex cczj-items-center cczj-flex-wrap">
          <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
            <input type="checkbox" v-model="editing.dark" @change="deriveFromPrimary" />
            <span>{{ t('settings.darkMode') }}</span>
          </label>
          <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
            <input type="checkbox" v-model="replaceManual" />
            <span>{{ t('settings.replaceManualColors') }}</span>
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
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.accentColor')" v-model="editing.primary" @input="deriveFromPrimary" /><span>{{ editing.primary }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.textColor') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.textColor')" v-model="editing.text" @input="editColor('text')" /><span>{{ editing.text }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.bgApp') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.bgApp')" v-model="editing.background" @input="editColor('background')" /><span>{{ editing.background }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t(mobile ? 'mobile.secondaryBackground' : 'settings.bgSidebar') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t(mobile ? 'mobile.secondaryBackground' : 'settings.bgSidebar')" v-model="editing.sidebar" @input="editColor('sidebar')" /><span>{{ editing.sidebar }}</span></div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.bgContent') }}</label>
          <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.bgContent')" v-model="editing.content" @input="editColor('content')" /><span>{{ editing.content }}</span></div>
        </div>
      </div>

      <h4 class="group-title">{{ t('settings.bgAlpha') }}</h4>
      <div class="picker-grid">
        <div class="picker-item">
          <label>{{ t(mobile ? 'mobile.secondaryAlpha' : 'settings.sidebarAlpha') }}</label>
          <div class="picker-cell range-cell cczj-flex cczj-items-center cczj-gap-4">
            <input type="range" :aria-label="t(mobile ? 'mobile.secondaryAlpha' : 'settings.sidebarAlpha')" v-model.number="editing.sidebarAlpha" min="0" max="1" step="0.05" @input="editColor('sidebarAlpha')" />
            <span>{{ Math.round((editing.sidebarAlpha ?? 0.65) * 100) }}%</span>
          </div>
        </div>
        <div class="picker-item">
          <label>{{ t('settings.cardAlpha') }}</label>
          <div class="picker-cell range-cell cczj-flex cczj-items-center cczj-gap-4">
            <input type="range" :aria-label="t('settings.cardAlpha')" v-model.number="editing.contentAlpha" min="0" max="1" step="0.05" @input="editColor('contentAlpha')" />
            <span>{{ Math.round((editing.contentAlpha ?? 0.88) * 100) }}%</span>
          </div>
        </div>
      </div>

      <h4 class="group-title">{{ t('settings.globalColors') }}</h4>
      <div class="picker-grid small">
        <div class="picker-item"><label>{{ t('settings.btnAction') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.btnAction')" v-model="editing.primaryAction" @input="editColor('primaryAction')" /><span>{{ editing.primaryAction }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.tagColor') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.tagColor')" v-model="editing.tagColor" @input="editColor('tagColor')" /><span>{{ editing.tagColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.carouselColor') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.carouselColor')" v-model="editing.carouselColor" @input="editColor('carouselColor')" /><span>{{ editing.carouselColor }}</span></div></div>
      </div>

      <h4 class="group-title">{{ t('settings.semanticColors') }}</h4>
      <div class="picker-grid small">
        <div class="picker-item"><label>{{ t('settings.paletteRoleSuccess') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.paletteRoleSuccess')" v-model="editing.successColor" @input="editColor('successColor')" /><span>{{ editing.successColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.paletteRoleWarning') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.paletteRoleWarning')" v-model="editing.warningColor" @input="editColor('warningColor')" /><span>{{ editing.warningColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.paletteRoleDanger') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.paletteRoleDanger')" v-model="editing.dangerColor" @input="editColor('dangerColor')" /><span>{{ editing.dangerColor }}</span></div></div>
        <div class="picker-item"><label>{{ t('settings.paletteRoleInfo') }}</label><div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" :aria-label="t('settings.paletteRoleInfo')" v-model="editing.infoColor" @input="editColor('infoColor')" /><span>{{ editing.infoColor }}</span></div></div>
      </div>

      <details class="palette-advanced">
        <summary>{{ t('settings.advancedColors') }}</summary>
        <section v-for="group in roleGroups" :key="group.label">
          <h4 class="group-title">{{ t('settings.'+group.label) }}</h4>
          <div class="picker-grid small">
            <div v-for="role in group.roles" :key="role" class="picker-item">
              <label>{{ t('settings.colorRoles.'+role) }}</label>
              <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4">
                <input type="color" :aria-label="t('settings.colorRoles.'+role)" :value="roleValue(role)" @input="setRole(role,$event)" />
                <span>{{ roleValue(role) }}</span>
                <Button v-if="editing.roleColors?.[role]" variant="text" size="sm" icon :aria-label="t('settings.resetColorRole',{role:t('settings.colorRoles.'+role)})" @click="resetRole(role)"><Icon name="refresh" :size="14" /></Button>
              </div>
            </div>
          </div>
        </section>
      </details>

      <div
        class="bg-drop-zone cczj-relative cczj-flex cczj-items-center cczj-justify-center cczj-overflow-hidden cczj-cursor-pointer"
        :class="{ 'has-image': !!backgroundImageUrl }"
        :data-file-drop-target="mobile ? undefined : ''"
        :role="backgroundImageUrl ? undefined : 'button'"
        :tabindex="backgroundImageUrl ? -1 : 0"
        :aria-label="t(mobile ? 'mobile.chooseBackground' : 'settings.dropImage')"
        @click="onDropZoneClick"
        @keydown.enter.self.prevent="onDropZoneClick"
        @keydown.space.self.prevent="onDropZoneClick"
        @dragover.prevent="onDragOver($event)"
        @dragleave="onDragLeave"
        @drop.stop.prevent="onDrop"
      >
        <div v-if="backgroundImageUrl" class="image-actions cczj-absolute cczj-top-2 cczj-right-2 cczj-flex cczj-gap-2" @click.stop>
          <Button variant="overlay" size="sm" icon :disabled="busy" :title="t('settings.editImage')" :aria-label="t('settings.editImage')" @click="fileInputRef?.click()"><Icon name="edit" :size="16" /></Button>
          <Button variant="overlay" size="sm" icon :disabled="busy || previewFailed" :title="t('settings.cropImage')" :aria-label="t('settings.cropImage')" @click="cropOpen=true"><Icon name="crop" :size="16" /></Button>
          <Button variant="overlay" size="sm" icon :disabled="busy" :title="t('settings.removeImage')" :aria-label="t('settings.removeImage')" @click="clearBackgroundImage"><Icon name="x" :size="16" /></Button>
        </div>
        <img v-if="backgroundImageUrl" :key="previewAttempt" :src="backgroundImageUrl" :alt="t('settings.backgroundPreview')" draggable="false" @load="previewLoaded" @error="previewError" />
        <div v-else class="bg-drop-hint cczj-flex cczj-flex-col cczj-items-center cczj-gap-5">
          <Icon name="plus" :size="42" />
          <span>{{ t(mobile ? 'mobile.chooseBackground' : 'settings.dropImage') }}</span>
        </div>
        <div v-if="busy || themeStore.backgroundLoading" class="image-loading cczj-absolute cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center" role="status">
          <Spinner :label="t(saving?'settings.imageSaving':'settings.imageProcessing')" />
        </div>
        <div v-else-if="previewFailed" class="image-loading cczj-absolute cczj-inset-0 cczj-flex cczj-flex-col cczj-gap-4 cczj-items-center cczj-justify-center" role="alert">
          <span>{{t('settings.imageLoadFailed')}}</span><Button @click.stop="retryPreview">{{t('common.retry')}}</Button>
        </div>
        <input ref="fileInputRef" type="file" accept="image/*" @change="onImageSelected" hidden />
      </div>
      <div v-if="backgroundImageUrl" class="cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4 cczj-mt-2">
        <Button variant="secondary" size="sm" :disabled="previewFailed" @click="applyImagePalette"><Icon name="image" :size="13" /> {{ t('settings.pickFromImage') }}</Button>
      </div>
      </fieldset>
      <ThemeImageCrop v-if="editorOpen && cropSource" :open="cropOpen" :src="cropSource" :busy="imageBusy || paletteBusy || previewLoading" :has-original="!!editing.backgroundOriginal" @apply="applyCrop" @cancel="cropOpen=false" @restore="restoreOriginal" />
    </div>

    <template #footer>
      <Button v-if="editing.__mode === 'edit'" variant="danger" size="md" :disabled="busy" @click="deleteEditing">
        <Icon name="trash" :size="14" /> {{ t('common.delete') }}
      </Button>
      <span style="flex: 1"></span>
      <Button variant="secondary" size="md" :disabled="saving" @click="closeEditor">{{ t('common.cancel') }}</Button>
      <Button variant="primary" size="md" :disabled="busy || cropOpen" :loading="saving" @click="saveEditing">
        <Icon name="save" :size="14" /> {{ t('settings.saveTheme') }}
      </Button>
    </template>
  </Modal>
</template>

<style scoped>
.editor-fields { border:0; padding:0; margin:0; }
.image-actions {z-index:3;}
.image-loading {background:color-mix(in srgb,var(--bg-card) 85%,transparent);z-index:2;}
.bg-drop-zone.file-drop-target-active { border-color:var(--accent); background:var(--bg-hover); }
.palette-advanced { margin:16px 0; padding:12px; @apply cczj-border cczj-rounded-md; }
.palette-advanced > summary { @apply cczj-cursor-pointer cczj-font-semibold; min-height:44px; line-height:44px; }
.palette-advanced .picker-cell > span { @apply cczj-flex-1; }

.modal-body {
  padding: 18px 20px;
  @apply cczj-overflow-y-auto;
}
.edit-row {
  @apply cczj-items-end cczj-gap-10;
  padding-bottom: 14px;
  @apply cczj-mb-5;
  border-bottom: 1px dashed var(--border);
}
.field { @apply cczj-gap-2 cczj-flex-1; min-width: 180px; }
.field label { font-size: 0.79rem; @apply cczj-text-muted cczj-font-semibold; }
.field input[type='text'] {
  @apply cczj-px-6-py-4 cczj-border cczj-rounded-md;
  background: var(--bg-secondary);
  @apply cczj-text-primary;
  font-size: 0.93rem;
  @apply cczj-outline-none;
}
.field input[type='text']:focus { @apply cczj-border-primary; }
.flags { gap: 18px; }

.group-title {
  font-size: 0.79rem;
  @apply cczj-text-muted cczj-font-semibold;
  text-transform: uppercase;
  letter-spacing: 1px;
  margin: 12px 0 8px;
}
.picker-grid {
  @apply cczj-grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  @apply cczj-gap-5 cczj-mb-3;
}
.picker-grid.small {
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
}
.picker-item label {
  @apply cczj-block;
  font-size: 0.79rem;
  @apply cczj-text-muted cczj-mb-2 cczj-font-semibold;
}
.picker-cell {
  @apply cczj-gap-4;
  padding: 8px 10px;
  background: var(--bg-secondary);
  @apply cczj-border;
  border-radius: 10px;
}
.picker-cell input[type='color'] {
  @apply cczj-w-32; height: 28px;
  @apply cczj-border cczj-rounded;
  padding: 0;
  background: transparent;
  @apply cczj-cursor-pointer;
}
.picker-cell span {
  font-size: 0.79rem;
  @apply cczj-text-secondary;
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
}
.picker-cell.range-cell {
  @apply cczj-gap-5;
}
.picker-cell.range-cell input[type='range'] {
  @apply cczj-flex-1;
  height: 4px;
  -webkit-appearance: none;
  appearance: none;
  @apply cczj-bg-border cczj-rounded-xs cczj-outline-none;
}
.picker-cell.range-cell input[type='range']::-webkit-slider-thumb {
  -webkit-appearance: none;
  @apply cczj-w-16 cczj-h-16 cczj-rounded-50;
  background: var(--accent);
  @apply cczj-cursor-pointer;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.2);
}
.bg-drop-zone {
  margin-top: 14px;
  border: 2px dashed var(--border);
  border-radius: 10px;
  min-height: 180px;
  background: var(--bg-card);
  @apply cczj-transition-fast;
}
.bg-drop-zone:hover { @apply cczj-border-primary; background: var(--bg-hover); }
.bg-drop-zone:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.bg-drop-zone.has-image { border-style: solid; @apply cczj-cursor-default; padding: 0; }
.bg-drop-zone.has-image:hover { background: var(--bg-card); }
.bg-drop-zone img {
  @apply cczj-block cczj-w-full;
  height: auto;
  max-height: 240px;
  @apply cczj-object-cover;
}
.bg-drop-hint {
  @apply cczj-gap-5 cczj-text-secondary;
}
.bg-drop-hint span { font-size: 0.93rem; }

.toggle {
  @apply cczj-gap-4;
  font-size: 0.93rem;
  @apply cczj-text-primary;
}
.toggle input[type='checkbox'] {
  -webkit-appearance: none;
  appearance: none;
  width: 18px;
  height: 18px;
  border: 1.5px solid var(--border-strong);
  border-radius: 5px;
  background: var(--bg-card);
  @apply cczj-cursor-pointer cczj-relative cczj-transition-fast cczj-flex-shrink-0;
}
.toggle input[type='checkbox']:hover {
  @apply cczj-border-primary;
}
.toggle input[type='checkbox']:checked {
  background: var(--accent);
  @apply cczj-border-primary;
}
.toggle input[type='checkbox']:checked::after {
  content: '';
  @apply cczj-absolute;
  top: 3px;
  left: 5px;
  width: 4px;
  @apply cczj-h-2;
  border: 2px solid var(--accent-contrast);
  border-top: 0;
  border-left: 0;
  transform: rotate(45deg);
}

.small { font-size: 0.79rem; }
</style>
