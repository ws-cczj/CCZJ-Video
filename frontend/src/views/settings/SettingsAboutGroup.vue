<script setup lang="ts">
/**
 * 「关于」分组：版本卡片 + 检查更新 / 重启 / 退回上一版 + 免责声明。
 *
 * 版本号仍然由 Settings.vue 在页面挂载时读一次再传进来（和拆分前一样：
 * 不管当前停在哪一组，进页面就把它取好了），这里只负责展示与这几个动作。
 */
defineOptions({ name: 'SettingsAboutGroup' })
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { Button } from '../../components/ui'
import { updateController, updateChecking, fmtSize } from '../../stores/updateState'
import { licenseModalOpen } from '../../stores/licenseState'
import { useThemeStore } from '../../stores/theme'
import { useErrorStore } from '../../stores/error'
import { useConfirmStore } from '../../stores/confirm'
import { useDownloadStore } from '../../stores/download'
import { formatTime } from '../../utils'
import { openExternalURL, isMobileRuntime, isIOSRuntime } from '../../api/runtime'
import {
  GetSetting, RestartApp, GetSwapBackupInfo, RollbackUpdate,
} from '../../api/app'

const props = defineProps<{ appVersion: string }>()

const { t } = useI18n()
const themeStore = useThemeStore()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const downloadStore = useDownloadStore()

// 已同意的条款版本读自设置表本身：界面要说的是"这台机器上记下的是什么"，
// 拿常量冒充会掩盖未同意或写入失败的情况。
const acceptedTerms = ref('')
const projectURL = 'https://github.com/ws-cczj/CCZJ-Video'
const authorEmail = 'yikeshuoguo@qq.com'

async function openProjectURL(url: string): Promise<void> {
  try {
    await openExternalURL(url)
  } catch (error) {
    errorStore.fromError(t('settings.projectOpenFailed'), error, 'SettingsAboutGroup.openProjectURL')
  }
}

// 「退回上一版」的原料是 exe 旁边那份 .old 副本，它可能根本不存在（没装过更新、
// 或 copy 那一步失败）。没有副本就不出现这个按钮：给一个按不动的入口，
// 用户读到的是"功能坏了"，而不是"这次没有可退的东西"。
const swapBackup = ref<{ size_bytes?: number; modified_at?: number } | null>(null)

onMounted(async () => {
  try {
    acceptedTerms.value = await GetSetting('license_terms_version')
  } catch { /* 读不到就显示未同意 */ }
  if (isMobileRuntime()) return
  try {
    const info = await GetSwapBackupInfo()
    if (info?.available) swapBackup.value = info
  } catch { /* 读不到就当没有 */ }
})

// 重启
const restarting = ref(false)

async function restartApp(): Promise<void> {
  const hasActiveDl = downloadStore.hasActive
  const message = hasActiveDl
    ? t('settings.restartMsgDl')
    : t('settings.restartMsg')
  const yes = await confirmStore.confirm({
    title: t('settings.restartTitle'),
    message,
    okText: t('settings.restartBtn'),
    level: 'warn',
  })
  if (!yes) return
  restarting.value = true
  try {
    RestartApp()
  } catch (e: any) {
    errorStore.fromError(t('settings.restartFailed'), e, 'Settings.restartApp')
  } finally {
    restarting.value = false
  }
}

// 退回上一版：把 exe 换回上一次更新前的副本，然后让位给脚本。
// 副本一旦被 move 消费掉就没了，所以这一步必须让用户先看清楚退的是哪一份。
const rollingBack = ref(false)

async function rollbackUpdate(): Promise<void> {
  const backup = swapBackup.value
  if (!backup) return
  const size = fmtSize(Number(backup.size_bytes ?? 0))
  const modifiedAt = Number(backup.modified_at ?? 0)
  const when = modifiedAt > 0 ? formatTime(new Date(modifiedAt * 1000).toISOString()) : t('common.unknown')
  const yes = await confirmStore.confirm({
    title: t('settings.rollbackTitle'),
    message: t('settings.rollbackMsg', { version: props.appVersion, size, when }),
    okText: t('settings.rollbackBtn'),
    level: 'warn',
  })
  if (!yes) return
  rollingBack.value = true
  try {
    await RollbackUpdate()
  } catch (e: any) {
    errorStore.fromError(t('settings.rollbackFailed'), e, 'SettingsAboutGroup.rollbackUpdate')
    rollingBack.value = false
  }
}
</script>

<template>
  <div class="panel group-card cczj-flex cczj-flex-col cczj-gap-2">
    <section class="block">
      <div class="about-card cczj-flex cczj-items-center cczj-gap-8">
        <div class="about-icon cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="film" :size="22" /></div>
        <div>
          <h3>CCZJ Video</h3>
          <p>{{ t('settings.version') }} <strong>{{ appVersion }}</strong> · Wails + Vue 3</p>
          <small>{{ t('settings.currentTheme') }} · <em>{{ themeStore.current.name }}</em> · {{ themeStore.current.mode === 'dark' ? t('settings.dark') : t('settings.light') }}</small>
        </div>
      </div>

      <p v-if="isMobileRuntime()" class="desc">{{ t(isIOSRuntime() ? 'mobile.iosUpdate' : 'mobile.previewUpdate') }}</p>
      <div class="about-actions cczj-flex cczj-gap-5">
        <Button
          v-if="!isIOSRuntime()"
          variant="primary"
          size="md"
          :loading="updateChecking"
          @click="updateController.checkUpdate?.()"
        >
          <Icon name="refresh" :size="14" /> {{ t('settings.checkUpdate') }}
        </Button>
        <Button
          v-if="!isMobileRuntime()"
          variant="secondary"
          size="md"
          :disabled="restarting"
          :loading="restarting"
          @click="restartApp"
        >
          <Icon name="refresh" :size="14" /> {{ t('settings.restart') }}
        </Button>
        <Button
          v-if="!isMobileRuntime() && swapBackup"
          variant="secondary"
          size="md"
          :disabled="rollingBack"
          :loading="rollingBack"
          :title="t('settings.rollbackHint', { size: fmtSize(Number(swapBackup.size_bytes ?? 0)) })"
          @click="rollbackUpdate"
        >
          <Icon name="back" :size="14" /> {{ t('settings.rollbackBtn') }}
        </Button>
      </div>
    </section>

    <section class="block">
      <h3>{{ t('settings.projectContact') }}</h3>
      <dl class="about-contact-list">
        <div class="about-contact-row">
          <dt>GitHub</dt>
          <dd>{{ projectURL }}</dd>
          <Button class="about-contact-open" variant="text" size="sm" :aria-label="t('settings.projectPage')" @click="openProjectURL(projectURL)"><Icon name="chevron-right" :size="18" /></Button>
        </div>
        <div class="about-contact-row">
          <dt>{{ t('settings.contactEmail') }}</dt>
          <dd>{{ authorEmail }}</dd>
        </div>
      </dl>
    </section>

    <section class="block">
      <h3>{{ t('settings.disclaimer') }}</h3>
      <div class="disclaimer-card cczj-flex cczj-gap-7">
        <div class="disclaimer-icon cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="shield" :size="20" /></div>
        <div class="disclaimer-content">
          <p>{{ t('settings.disclaimerText') }}</p>
        </div>
      </div>
    </section>

    <section class="block">
      <h3>{{ t('license.entryTitle') }}</h3>
      <div class="license-row cczj-flex cczj-gap-7">
        <p class="license-row-text">
          {{ acceptedTerms ? t('license.termsVersion', { v: acceptedTerms }) : t('license.termsPending') }}
        </p>
        <Button variant="secondary" size="sm" @click="licenseModalOpen = true">
          {{ t('license.viewTerms') }}
        </Button>
      </div>
    </section>
  </div>
</template>

<style scoped>
/* 卡片外框与 .block 的压平仍由 Settings.vue 的 .panel.group-card 负责，
   这里留的是本组内部的排版；`.block h3` 写在 `.about-card h3` 之前，
   两条同特异度、后写的赢，和拆分前在同一份样式里的顺序一致。 */
.block h3 {
  font-size: 0.97rem;
  @apply cczj-font-bold;
  margin: 0 0 12px;
  letter-spacing: 0.3px;
}

/* ============ 关于 ============ */
.about-card {
  @apply cczj-gap-8;
  padding: 16px 18px;
  background: linear-gradient(135deg, var(--accent-alpha-10), transparent 75%);
  @apply cczj-border cczj-rounded-lg;
}
.about-icon {
  width: 52px; height: 52px;
  border-radius: 14px;
  background: var(--accent);
  color: var(--accent-contrast);
  box-shadow: 0 6px 18px var(--accent-alpha-35);
}
.about-card h3 { margin: 0 0 4px; font-size: 1.14rem; @apply cczj-font-bold; }
.about-card p { margin: 0 0 4px; font-size: 0.93rem; @apply cczj-text-secondary; }
.about-card small { @apply cczj-text-muted; font-size: 0.86rem; }

.about-actions {
  @apply cczj-mt-8 cczj-gap-5 cczj-flex-wrap;
}
/* Label/supporting-text rows: https://github.com/material-components/material-web/blob/main/docs/components/list.md */
.about-contact-list { margin: 0; @apply cczj-border cczj-rounded-lg cczj-overflow-hidden; }
.about-contact-row { @apply cczj-grid cczj-gap-2; grid-template-columns: minmax(0,1fr) auto; padding: 12px 14px; }
.about-contact-row + .about-contact-row { @apply cczj-border-top; }
.about-contact-row dt { grid-column: 1; @apply cczj-text-xs cczj-text-muted; }
.about-contact-row dd { grid-column: 1; margin: 0; @apply cczj-min-w-0 cczj-text-sm cczj-text-primary cczj-select-text; line-height: 1.6; overflow-wrap: anywhere; }
.about-contact-open { grid-column: 2; grid-row: 1 / 3; align-self: center; }

/* ============ 声明卡片 ============ */
.disclaimer-card {
  @apply cczj-gap-7;
  padding: 16px 18px;
  background: linear-gradient(135deg, rgba(255, 193, 7, 0.08), transparent 75%);
  @apply cczj-border cczj-rounded-lg;
}
.disclaimer-icon {
  @apply cczj-flex-shrink-0;
  width: 44px;
  height: 44px;
  @apply cczj-rounded-lg;
  background: var(--warning);
  color: #1a1a1a;
  box-shadow: 0 4px 12px var(--warning-alpha-10);
}
.disclaimer-content {
  @apply cczj-flex-1 cczj-min-w-0;
}
.disclaimer-content p {
  margin: 0 0 6px;
  font-size: 0.89rem;
  line-height: 1.55;
  @apply cczj-text-secondary;
}
.disclaimer-content p:last-child { @apply cczj-mb-0; }

/* ============ 使用许可 ============ */
.license-row {
  @apply cczj-gap-7 cczj-items-center cczj-justify-between cczj-flex-wrap;
  padding: 14px 18px;
  background: var(--bg-secondary);
  @apply cczj-border cczj-rounded-lg;
}
.license-row-text {
  @apply cczj-flex-1 cczj-min-w-0;
  margin: 0;
  font-size: 0.89rem;
  line-height: 1.55;
  @apply cczj-text-secondary;
}
</style>
