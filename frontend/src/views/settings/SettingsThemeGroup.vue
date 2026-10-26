<script setup lang="ts">
/**
 * 「主题外观」分组：预设 + 自定义主题两张卡片网格（浅色 / 深色各一张）。
 *
 * 桌面卡片负责选择与发起编辑；手机通过悬浮入口编辑当前主题。编辑态在 ThemeEditorModal 里，
 * 由这里通过函数式模板 ref 拿实例调用它的 open* 方法——和拆分前在同一份
 * setup 作用域里直接调函数是同一个调用序列。
 */
defineOptions({ name: 'SettingsThemeGroup' })
import { computed, ref, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { useThemeStore, type ColorPalette } from '../../stores/theme'
import { useConfirmStore } from '../../stores/confirm'
import ThemeEditorModal from './ThemeEditorModal.vue'
import { Button } from '../../components/ui'
import { presentationProfile } from '../../platform/presentation'

const { t } = useI18n()
const themeStore = useThemeStore()
const confirmStore = useConfirmStore()
const mobile = presentationProfile().ui === 'mobile'
function editCurrent(): void {
  const preset = themeStore.themes.find(item => item.id === themeStore.currentId)
  if (preset) editor.value?.openEditPreset(preset)
  else editor.value?.openEdit(themeStore.currentId)
}

/** ThemeEditorModal 用 defineExpose 交出来的三个打开入口 + 预览重绘。 */
type ThemeEditorHandle = InstanceType<typeof ThemeEditorModal>
const editor = ref<ThemeEditorHandle | null>(null)
function bindEditor(el: Element | ComponentPublicInstance | null): void {
  editor.value = el as ThemeEditorHandle | null
}

async function onDeleteTheme(id: string, name: string): Promise<void> {
  const yes = await confirmStore.confirm({
    title: t('settings.deleteTheme'),
    message: t('settings.deleteThemeMsg', { name }),
    okText: t('common.delete'),
    level: 'danger',
  })
  if (!yes) return
  themeStore.deleteCustom(id)
}

/** 背景字段的实际取址：pack:// 引用换成扩展包里读出的图，读不到就当作没有背景。 */
function bgSrc(bg?: string | null): string | undefined {
  return themeStore.resolveThemeBg(bg || undefined)
}

function resolvePreset(t: { id: string; name: string; primary: string; palette: ColorPalette; mode: 'dark' | 'light'; bgImage?: string }) {
  const override = themeStore.customThemes.find((c) => c.id === t.id)
  if (!override) return { data: t, isOverride: false, bg: bgSrc(t.bgImage), textPrimary: t.palette.textPrimary, sidebarBg: t.palette.bgSidebar }
  // 判断 override 的背景图是否仍是"预设资源"：若是，用当前构建的 URL；
  // 如果是用户上传的 data URL / http URL，则用用户自己的值。
  let bg: string | undefined = override.backgroundImage
  if (bg && !bg.startsWith('data:') && !/^https?:\/\//i.test(bg)) {
    const resolved = themeStore.resolvePresetAsset(bg)
    if (resolved && resolved !== bg) bg = t.bgImage || resolved
  }
  // pack:// 引用在这一层换成实际图：包被删或还没读出来时按「无背景图」渲染，跟应用主题时的判断一致。
  bg = bgSrc(bg)
  // 有背景图时，改用半透明的 tint 让图片展示出来（纯颜色主题仍然使用纯色）。
  // 透明度取主题自己存的那份，写死 0.55 会让卡片预览和实际应用不是一个浓度。
  const effectiveSidebarBg = bg
    ? hexToRgbaCss(override.sidebar || t.palette.bgSidebar, override.sidebarAlpha ?? 0.65)
    : (override.sidebar || t.palette.bgSidebar)
  return {
    data: override,
    isOverride: true,
    bg,
    textPrimary: override.text || t.palette.textPrimary,
    sidebarBg: effectiveSidebarBg,
  }
}

function hexToRgbaCss(color: string, alpha: number): string {
  if (!color) return `rgba(0,0,0,${alpha})`
  // 已是 rgba / rgb
  if (color.startsWith('rgb')) return color
  const hex = color.replace('#', '').trim()
  const full = hex.length === 3
    ? hex.split('').map((c) => c + c).join('')
    : hex
  const r = parseInt(full.substring(0, 2), 16)
  const g = parseInt(full.substring(2, 4), 16)
  const b = parseInt(full.substring(4, 6), 16)
  if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) return color
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

// ---------- 列表：把预设 + 自定义分别按 light/dark 分组 ----------
const lightPresets = computed(() => themeStore.themes.filter(t => t.mode === 'light'))
const darkPresets  = computed(() => themeStore.themes.filter(t => t.mode === 'dark'))

// 自定义主题中，id 与预设重合的视为“对该预设的覆盖/修改”，不再作为独立卡片显示
const presetIdsSet = computed(() => new Set(themeStore.themes.map((t) => t.id)))
const pureCustomThemes = computed(() =>
  themeStore.customThemes.filter((c) => !presetIdsSet.value.has(c.id))
)

// 卡片只负责显示，所以这里把 pack:// 引用换成实际图；持久化仍走 customThemes 里的原始引用。
const customCards = computed(() =>
  pureCustomThemes.value.map((c) => ({ ...c, backgroundImage: bgSrc(c.backgroundImage) }))
)

function isActive(id: string): boolean {
  return themeStore.currentId === id
}

function pickTheme(id: string): void {
  themeStore.setTheme(id)
}
</script>

<template>
 <div v-if="themeStore.saveError" role="alert" class="cczj-theme-save-error">{{themeStore.saveError}} <button type="button" @click="themeStore.retrySettings()">{{t('common.retry')}}</button></div>

  <div class="panel group-card theme-group cczj-flex cczj-flex-col cczj-gap-2">
    <section class="block">
      <h3>{{ t('settings.themeColor') }}</h3>

      <h4 class="sub-title">{{ t('settings.lightThemes') }}</h4>
      <div class="theme-grid cczj-grid">
        <!-- 预设 -->
        <button
          v-for="preset in lightPresets"
          :key="preset.id"
          class="theme-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
          :class="{ active: isActive(preset.id), hasBg: !!resolvePreset(preset).bg, 'is-override': resolvePreset(preset).isOverride }"
          :style="[
            resolvePreset(preset).bg
              ? {
                  backgroundColor: resolvePreset(preset).isOverride ? resolvePreset(preset).sidebarBg : 'transparent',
                  backgroundImage: `url(${resolvePreset(preset).bg})`,
                  backgroundSize: 'cover',
                  backgroundPosition: 'center',
                  color: resolvePreset(preset).textPrimary
                }
              : { background: resolvePreset(preset).isOverride ? resolvePreset(preset).sidebarBg : preset.palette.bgSidebar, color: resolvePreset(preset).textPrimary }
          ]"
          @click="pickTheme(preset.id)"
        >
          <span v-if="!mobile" class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
            <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="editor?.openEditPreset(preset)" :title="t('settings.editTheme')">
              <Icon name="pencil" :size="12" />
            </button>
          </span>
          <span v-if="!resolvePreset(preset).bg" class="swatch" :style="{ background: resolvePreset(preset).data.primary }"></span>
          <span v-if="resolvePreset(preset).bg" class="swatch small" :style="{ background: resolvePreset(preset).data.primary }"></span>
          <span class="label cczj-truncate" :style="{ color: resolvePreset(preset).textPrimary }">{{ resolvePreset(preset).data.name }}</span>
          <span v-if="isActive(preset.id)" class="check cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="check" :size="12" /></span>
        </button>

        <!-- 自定义（排除与预设同名的覆盖项，那些已通过上方预设卡显示） -->
        <button
          v-for="c in customCards.filter((c) => !c.dark)"
          :key="c.id"
          class="theme-card custom cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
          :class="{ active: isActive(c.id), hasBg: !!c.backgroundImage }"
          :style="[
            c.backgroundImage
              ? {
                  backgroundColor: hexToRgbaCss(c.sidebar || c.background, 0.55),
                  backgroundImage: `url(${c.backgroundImage})`,
                  backgroundSize: 'cover',
                  backgroundPosition: 'center',
                  color: c.text
                }
              : { background: c.sidebar, color: c.text }
          ]"
          @click="pickTheme(c.id)"
        >
          <span v-if="!mobile" class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
            <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="editor?.openEdit(c.id)" :title="t('settings.editTheme')">
              <Icon name="pencil" :size="12" />
            </button>
          </span>
          <span v-if="!c.backgroundImage" class="swatch" :style="{ background: c.primary }"></span>
          <span v-if="c.backgroundImage" class="swatch small" :style="{ background: c.primary }"></span>
          <span class="label cczj-truncate" :style="{ color: c.text }">{{ c.name }}</span>
          <span v-if="!mobile" class="card-actions cczj-absolute cczj-flex cczj-gap-2" @click.stop>
            <button class="mini-btn danger" @click="onDeleteTheme(c.id, c.name)" :title="t('common.delete')">
              <Icon name="x" :size="12" />
            </button>
          </span>
        </button>

        <!-- 添加（浅色区） -->
        <button class="theme-card add cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer" @click="editor?.openCreate(); editor?.applyPreview()">
          <span class="swatch plus"><Icon name="plus" :size="22" /></span>
          <span class="label cczj-truncate">{{ t('settings.addTheme') }}</span>
        </button>
      </div>

      <h4 class="sub-title">{{ t('settings.darkThemes') }}</h4>
      <div class="theme-grid cczj-grid">
        <button
          v-for="preset in darkPresets"
          :key="preset.id"
          class="theme-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
          :class="{ active: isActive(preset.id), hasBg: !!resolvePreset(preset).bg, 'is-override': resolvePreset(preset).isOverride }"
          :style="[
            resolvePreset(preset).bg
              ? {
                  backgroundColor: resolvePreset(preset).isOverride ? resolvePreset(preset).sidebarBg : 'transparent',
                  backgroundImage: `url(${resolvePreset(preset).bg})`,
                  backgroundSize: 'cover',
                  backgroundPosition: 'center',
                  color: resolvePreset(preset).textPrimary
                }
              : { background: resolvePreset(preset).isOverride ? resolvePreset(preset).sidebarBg : preset.palette.bgSidebar, color: resolvePreset(preset).textPrimary }
          ]"
          @click="pickTheme(preset.id)"
        >
          <span v-if="!mobile" class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
            <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="editor?.openEditPreset(preset)" :title="t('settings.editTheme')">
              <Icon name="pencil" :size="12" />
            </button>
          </span>
          <span v-if="!resolvePreset(preset).bg" class="swatch" :style="{ background: resolvePreset(preset).data.primary }"></span>
          <span v-if="resolvePreset(preset).bg" class="swatch small" :style="{ background: resolvePreset(preset).data.primary }"></span>
          <span class="label cczj-truncate" :style="{ color: resolvePreset(preset).textPrimary }">{{ resolvePreset(preset).data.name }}</span>
          <span v-if="isActive(preset.id)" class="check cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="check" :size="12" /></span>
        </button>

        <button
          v-for="c in customCards.filter((c) => c.dark)"
          :key="c.id"
          class="theme-card custom cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
          :class="{ active: isActive(c.id), hasBg: !!c.backgroundImage }"
          :style="[
            c.backgroundImage
              ? {
                  backgroundColor: hexToRgbaCss(c.sidebar || c.background, 0.55),
                  backgroundImage: `url(${c.backgroundImage})`,
                  backgroundSize: 'cover',
                  backgroundPosition: 'center',
                  color: c.text
                }
              : { background: c.sidebar, color: c.text }
          ]"
          @click="pickTheme(c.id)"
        >
          <span v-if="!mobile" class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
            <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="editor?.openEdit(c.id)" :title="t('settings.editTheme')">
              <Icon name="pencil" :size="12" />
            </button>
          </span>
          <span v-if="!c.backgroundImage" class="swatch" :style="{ background: c.primary }"></span>
          <span v-if="c.backgroundImage" class="swatch small" :style="{ background: c.primary }"></span>
          <span class="label cczj-truncate" :style="{ color: c.text }">{{ c.name }}</span>
          <span v-if="!mobile" class="card-actions cczj-absolute cczj-flex cczj-gap-2" @click.stop>
            <button class="mini-btn danger" @click="onDeleteTheme(c.id, c.name)" :title="t('common.delete')">
              <Icon name="x" :size="12" />
            </button>
          </span>
        </button>
      </div>
    </section>

    <!-- ========== 主题编辑器 弹窗 ========== -->
    <ThemeEditorModal :ref="bindEditor" />
  </div>
  <Button v-if="mobile" class="theme-edit-fab" variant="primary" :aria-label="t('settings.editTheme')" :title="t('settings.editTheme')" @click="editCurrent"><Icon name="pencil" :size="22" /></Button>
</template>

<style scoped>
.cczj-theme-save-error{@apply cczj-p-6;border:1px solid var(--danger);@apply cczj-rounded-md cczj-text-primary cczj-mb-6}.cczj-theme-save-error button{min-height:44px;padding:8px 14px;@apply cczj-ml-5 cczj-border cczj-rounded-md;background:var(--bg-card);@apply cczj-text-primary}

/* 卡片外框与 .block 的压平由 Settings.vue 的 .panel.group-card（:deep）统一画，
   这里只留本组内部的排版——h3 不是根节点，拿不到父级 scope id。 */
.block h3 {
  font-size: 0.97rem;
  @apply cczj-font-bold;
  margin: 0 0 12px;
  letter-spacing: 0.3px;
}
.sub-title {
  font-size: 0.93rem;
  @apply cczj-text-secondary cczj-font-bold;
  text-transform: uppercase;
  letter-spacing: 1px;
  margin: 0 0 10px;
  padding-top: 6px;
  border-top: 1px dashed var(--border);
}
.sub-title:first-of-type {
  border-top: none;
  padding-top: 0;
}

/* ============ 主题网格 ============ */
.theme-grid {
  grid-template-columns: repeat(auto-fill, minmax(128px, 1fr));
  @apply cczj-gap-7;
  margin-bottom: 18px;
}
.theme-card {
  @apply cczj-relative cczj-gap-5;
  padding: 14px 10px 14px;
  min-height: 160px;
  background: var(--bg-secondary);
  border: 2px solid transparent;
  @apply cczj-rounded-lg cczj-transition-fast cczj-overflow-hidden;
}
.theme-card::before {
  /* 背景层：用 inset 提供一个浅阴影，让卡片更有质感 */
  content: '';
  @apply cczj-absolute;
  inset: 0;
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.04);
  border-radius: 10px;
  @apply cczj-pointer-events-none;
}
.theme-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.10);
}
.theme-card.active {
  @apply cczj-border-primary;
  box-shadow: 0 6px 24px var(--accent-alpha-35);
}
.theme-card .card-overlay {
  @apply cczj-absolute;
  inset: 0;
  border-radius: 10px;
  @apply cczj-z-0 cczj-pointer-events-none cczj-opacity-100;
}
.theme-card.hasBg .swatch,
.theme-card.hasBg .label {
  @apply cczj-relative;
  z-index: 1;
}
.theme-card .swatch {
  width: 60px;
  height: 60px;
  @apply cczj-rounded-50;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.15), inset 0 -4px 10px rgba(0, 0, 0, 0.10);
  @apply cczj-flex-shrink-0;
}
/* 「新建主题」这几张占位卡不是预览，它们跟着界面当前主题走。
 * 以前写死 #f5f5f5/#8a8a8a 这套浅灰：浅色主题下正好，切到深色主题就成了一块
 * 刺眼的白卡。改成从卡片表面和 muted 文字色混出来的中性灰，两档都成立，
 * 浅色调出来的结果和原来肉眼看不出差别。 */
.theme-card.add {
  background: color-mix(in srgb, var(--text-muted) 12%, var(--bg-card));
  border: 2px dashed transparent;
  @apply cczj-text-muted;
}
.theme-card.add:hover {
  background: color-mix(in srgb, var(--text-muted) 20%, var(--bg-card));
  border-color: color-mix(in srgb, var(--text-muted) 35%, var(--bg-card));
}
.theme-card.add .label { @apply cczj-text-muted; }
.theme-card .swatch.plus {
  background: color-mix(in srgb, var(--text-muted) 16%, var(--bg-card));
  @apply cczj-text-muted cczj-inline-flex cczj-items-center cczj-justify-center;
  border: 2px dashed color-mix(in srgb, var(--text-muted) 35%, var(--bg-card));
  @apply cczj-shadow-none;
}
.theme-card .swatch.plus svg { @apply cczj-text-muted; }
.theme-card .swatch.small {
  width: 18px;
  height: 18px;
  @apply cczj-mt-2;
  /* 描边取当前主题的表面色（浅色主题下就是白，跟原来一致），
   * 深色主题的亮主色上才不会被同色吞掉。 */
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.2), inset 0 0 0 2px color-mix(in srgb, var(--bg-card) 30%, transparent);
}
.theme-card .label {
  font-size: 0.86rem;
  @apply cczj-text-secondary cczj-font-bold cczj-max-w-full cczj-text-center;
}
.check {
  @apply cczj-absolute cczj-top-2 cczj-right-2;
  width: 22px;
  height: 22px;
  @apply cczj-rounded-50;
  background: var(--accent);
  color: var(--accent-contrast);
  font-size: 0.86rem;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.25);
  z-index: 2;
}

/* 主题卡片上的编辑按钮（右上角，hover 显示） */
.card-actions-top {
  top: 6px;
  right: 6px;
  @apply cczj-gap-2 cczj-opacity-0;
  transition: opacity 0.15s ease;
  z-index: 3;
}
.theme-card:hover .card-actions-top {
  @apply cczj-opacity-100;
}

/* 自定义主题卡片上的删除按钮（右下角，hover 显示） */
.card-actions {
  bottom: 6px;
  right: 6px;
  @apply cczj-gap-2 cczj-opacity-0;
  transition: opacity 0.15s ease;
  z-index: 3;
}
.theme-card:hover .card-actions {
  @apply cczj-opacity-100;
}
.mini-btn {
  @apply cczj-w-24 cczj-h-24 cczj-rounded cczj-border;
  background: var(--bg-card);
  @apply cczj-text-secondary cczj-cursor-pointer cczj-transition-fast;
}
.mini-btn:hover {
  background: var(--accent);
  color: var(--accent-contrast);
  @apply cczj-border-primary;
}
.mini-btn.danger:hover {
  background: var(--danger);
  border-color: var(--danger);
  @apply cczj-text-white;
}

.small { font-size: 0.79rem; }
</style>
