<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  GetSetting, SetSetting, GetCloseBehavior, SetCloseBehavior,
  WindowGetResizable, WindowSetResizable, WindowGetSize, WindowSetSize,
  RestartApp,
} from '../../../bindings/cczjVideo/app'
import { useErrorStore } from '../../stores/error'
import { useConfirmStore } from '../../stores/confirm'
import { useDevMode } from '../../stores/devMode'
import Icon from '../../components/Icon.vue'
import { Button } from '../../components/ui'

const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const devMode = useDevMode()

// 调试模式
const debugMode = ref(false)
// 关闭行为
const closeToTray = ref(true)
// 窗口设置
const windowResizable = ref(false)
const windowWidth = ref(1280)
const windowHeight = ref(800)

async function loadSettings(): Promise<void> {
  try { const v = await GetSetting('debug_mode'); debugMode.value = v === '1' } catch { /* */ }
  try { closeToTray.value = await GetCloseBehavior() } catch { /* */ }
  try { windowResizable.value = await WindowGetResizable() } catch { /* */ }
  try {
    const size = await WindowGetSize()
    if (size) {
      windowWidth.value = size.width || 1280
      windowHeight.value = size.height || 800
    }
  } catch { /* */ }
}

async function saveDebugMode(): Promise<void> {
  try {
    await SetSetting('debug_mode', debugMode.value ? '1' : '0')
    errorStore.info('已保存', 'debug_mode = ' + (debugMode.value ? '1' : '0'), '', 'AdminSettings')
  } catch (e: any) {
    errorStore.fromError('保存失败', e, 'AdminSettings')
  }
}

async function saveCloseBehavior(): Promise<void> {
  try {
    await SetCloseBehavior(closeToTray.value)
    errorStore.info('已保存', closeToTray.value ? '关闭时最小化到托盘' : '关闭时退出', '', 'AdminSettings')
  } catch (e: any) {
    errorStore.fromError('保存失败', e, 'AdminSettings')
  }
}

async function saveResizable(): Promise<void> {
  try {
    await WindowSetResizable(windowResizable.value)
    errorStore.info('已应用', `窗口可调整大小: ${windowResizable.value}`, '', 'AdminSettings')
  } catch (e: any) {
    errorStore.fromError('设置失败', e, 'AdminSettings')
  }
}

async function applyWindowSize(): Promise<void> {
  try {
    await WindowSetSize(windowWidth.value, windowHeight.value)
    errorStore.info('已应用', `窗口尺寸: ${windowWidth.value}x${windowHeight.value}`, '', 'AdminSettings')
  } catch (e: any) {
    errorStore.fromError('设置失败', e, 'AdminSettings')
  }
}

async function restartApp(): Promise<void> {
  const ok = await confirmStore.confirm({
    title: '重启应用', message: '确认重启？未保存的操作将丢失。', okText: '重启', level: 'warn',
  })
  if (!ok) return
  try { await RestartApp() } catch (e: any) {
    errorStore.fromError('重启失败', e, 'AdminSettings')
  }
}

onMounted(async () => {
  await loadSettings()
})
</script>

<template>
  <div>
    <!-- 调试选项 -->
    <div class="a-card">
      <div class="a-card-hd"><h3>调试选项</h3></div>
      <label class="a-toggle">
        <input type="checkbox" v-model="debugMode" @change="saveDebugMode" />
        <span>调试模式</span>
      </label>
      <p class="a-desc" style="margin-top:6px">输出更详细的运行日志</p>
    </div>

    <!-- 关闭行为 -->
    <div class="a-card">
      <div class="a-card-hd"><h3>关闭行为</h3></div>
      <label class="a-toggle">
        <input type="checkbox" v-model="closeToTray" @change="saveCloseBehavior" />
        <span>关闭时最小化到托盘</span>
      </label>
    </div>

    <!-- 窗口设置 -->
    <div class="a-card">
      <div class="a-card-hd"><h3>窗口设置</h3></div>
      <div class="settings-grid">
        <label class="a-toggle">
          <input type="checkbox" v-model="windowResizable" @change="saveResizable" />
          <span>允许拖动调整大小</span>
        </label>
        <div class="a-row">
          <span class="a-desc">窗口尺寸:</span>
          <input v-model.number="windowWidth" type="number" class="a-inp" style="width:80px" min="800" />
          <span class="a-desc">x</span>
          <input v-model.number="windowHeight" type="number" class="a-inp" style="width:80px" min="600" />
          <Button variant="secondary" size="sm" @click="applyWindowSize">应用</Button>
        </div>
      </div>
    </div>

    <!-- 开发者模式 -->
    <div class="a-card">
      <div class="a-card-hd"><h3>开发者模式</h3></div>
      <label class="a-toggle">
        <input type="checkbox" :checked="devMode.enabled" @change="(e: any) => devMode.setEnabled(e.target.checked)" />
        <span>打开开发者模式</span>
      </label>
      <p class="a-desc" style="margin-top:6px">关闭后侧边栏"开发者模式"栏目将隐藏</p>
    </div>

    <!-- 重启 -->
    <div class="a-card">
      <div class="a-card-hd"><h3>应用操作</h3></div>
      <Button variant="danger" size="sm" @click="restartApp">
        <Icon name="reset" :size="14" /> 重启应用
      </Button>
    </div>
  </div>
</template>

<style scoped>
.settings-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
