<script setup lang="ts">
/**
 * 「关于」分组：版本卡片 + 检查更新 / 重启 + 免责声明。
 *
 * 版本号仍然由 Settings.vue 在页面挂载时读一次再传进来（和拆分前一样：
 * 不管当前停在哪一组，进页面就把它取好了），这里只负责展示与两个动作。
 */
defineOptions({ name: 'SettingsAboutGroup' })
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { Button } from '../../components/ui'
import { updateController } from '../../stores/updateState'
import { licenseModalOpen } from '../../stores/licenseState'
import { useThemeStore } from '../../stores/theme'
import { useErrorStore } from '../../stores/error'
import { useConfirmStore } from '../../stores/confirm'
import { useDownloadStore } from '../../stores/download'
import { GetSetting, RestartApp } from '../../api/app'

defineProps<{ appVersion: string }>()

const { t } = useI18n()
const themeStore = useThemeStore()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const downloadStore = useDownloadStore()

// 已同意的条款版本读自设置表本身：界面要说的是"这台机器上记下的是什么"，
// 拿常量冒充会掩盖未同意或写入失败的情况。
const acceptedTerms = ref('')

onMounted(async () => {
  try {
    acceptedTerms.value = await GetSetting('license_terms_version')
  } catch { /* 读不到就显示未同意 */ }
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

      <div class="about-actions cczj-flex cczj-gap-5">
        <Button
          variant="primary"
          size="md"
          @click="updateController.checkUpdate?.()"
        >
          <Icon name="refresh" :size="14" /> {{ t('settings.checkUpdate') }}
        </Button>
        <Button
          variant="secondary"
          size="md"
          :disabled="restarting"
          :loading="restarting"
          @click="restartApp"
        >
          <Icon name="refresh" :size="14" /> {{ t('settings.restart') }}
        </Button>
      </div>
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
  font-weight: 700;
  margin: 0 0 12px;
  letter-spacing: 0.3px;
}

/* ============ 关于 ============ */
.about-card {
  gap: 16px;
  padding: 16px 18px;
  background: linear-gradient(135deg, var(--accent-alpha-10), transparent 75%);
  border: 1px solid var(--border);
  border-radius: 12px;
}
.about-icon {
  width: 52px; height: 52px;
  border-radius: 14px;
  background: var(--accent);
  color: var(--accent-contrast);
  box-shadow: 0 6px 18px var(--accent-alpha-35);
}
.about-card h3 { margin: 0 0 4px; font-size: 1.14rem; font-weight: 700; }
.about-card p { margin: 0 0 4px; font-size: 0.93rem; color: var(--text-secondary); }
.about-card small { color: var(--text-muted); font-size: 0.86rem; }

.about-actions {
  margin-top: 16px;
  gap: 10px;
  flex-wrap: wrap;
}

/* ============ 声明卡片 ============ */
.disclaimer-card {
  gap: 14px;
  padding: 16px 18px;
  background: linear-gradient(135deg, rgba(255, 193, 7, 0.08), transparent 75%);
  border: 1px solid var(--border);
  border-radius: 12px;
}
.disclaimer-icon {
  flex-shrink: 0;
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: var(--warning);
  color: #1a1a1a;
  box-shadow: 0 4px 12px var(--warning-alpha-10);
}
.disclaimer-content {
  flex: 1;
  min-width: 0;
}
.disclaimer-content p {
  margin: 0 0 6px;
  font-size: 0.89rem;
  line-height: 1.55;
  color: var(--text-secondary);
}
.disclaimer-content p:last-child { margin-bottom: 0; }

/* ============ 使用许可 ============ */
.license-row {
  gap: 14px;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  padding: 14px 18px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 12px;
}
.license-row-text {
  flex: 1;
  min-width: 0;
  margin: 0;
  font-size: 0.89rem;
  line-height: 1.55;
  color: var(--text-secondary);
}
</style>
