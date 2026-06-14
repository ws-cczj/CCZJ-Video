/**
 * 开发者模式解锁流程（完全隐藏，用户不可见）：
 *   1. 设置 → 关于应用 → 点击"CCZJ Video"名称 3 次
 *   2. 侧边栏左下角 → 点击"XX资源"名称 3 次
 *   3. 弹窗输入 6 位密码 541688 解锁
 *   4. 解锁后基本设置中出现「打开开发者模式」开关
 *   5. 开启后侧边栏出现「开发者模式」栏目
 */
import { ref, reactive } from 'vue'

const CORRECT_PASSWORD = '541688'

// 模块级状态（单例）
const appNameClicks = ref(0)
const sourceNameClicks = ref(0)
const unlocked = ref(false)
const enabled = ref(false)
const showPasswordModal = ref(false)
const passwordInput = ref('')
const passwordError = ref('')

let nameClickTimer: ReturnType<typeof setTimeout> | null = null
let sourceClickTimer: ReturnType<typeof setTimeout> | null = null

function resetAppNameClicks(): void { appNameClicks.value = 0 }
function resetSourceNameClicks(): void { sourceNameClicks.value = 0 }

function clickAppName(): void {
  if (unlocked.value) return
  appNameClicks.value++
  if (nameClickTimer) clearTimeout(nameClickTimer)
  if (appNameClicks.value >= 3) {
    appNameClicks.value = 3
    nameClickTimer = setTimeout(resetAppNameClicks, 5000)
    tryOpenPassword()
  } else {
    nameClickTimer = setTimeout(resetAppNameClicks, 3000)
  }
}

function clickSourceName(): void {
  if (unlocked.value) return
  sourceNameClicks.value++
  if (sourceClickTimer) clearTimeout(sourceClickTimer)
  if (sourceNameClicks.value >= 3) {
    sourceNameClicks.value = 3
    sourceClickTimer = setTimeout(resetSourceNameClicks, 5000)
    tryOpenPassword()
  } else {
    sourceClickTimer = setTimeout(resetSourceNameClicks, 3000)
  }
}

function tryOpenPassword(): void {
  if (appNameClicks.value >= 3 && sourceNameClicks.value >= 3) {
    showPasswordModal.value = true
    passwordInput.value = ''
    passwordError.value = ''
    // 重置计数器，防止重复触发
    appNameClicks.value = 0
    sourceNameClicks.value = 0
    if (nameClickTimer) { clearTimeout(nameClickTimer); nameClickTimer = null }
    if (sourceClickTimer) { clearTimeout(sourceClickTimer); sourceClickTimer = null }
  }
}

function verifyPassword(): void {
  if (passwordInput.value === CORRECT_PASSWORD) {
    unlocked.value = true
    enabled.value = true
    persist()
    closePasswordModal()
  } else {
    passwordError.value = '密码错误，请重试'
    passwordInput.value = ''
  }
}

function closePasswordModal(): void {
  showPasswordModal.value = false
  passwordInput.value = ''
  passwordError.value = ''
}

function setEnabled(val: boolean): void {
  enabled.value = val
  persist()
}

function persist(): void {
  try {
    localStorage.setItem('cczj_dev_unlocked', unlocked.value ? '1' : '0')
    localStorage.setItem('cczj_dev_enabled', enabled.value ? '1' : '0')
  } catch { /* ignore */ }
}

function restore(): void {
  try {
    unlocked.value = localStorage.getItem('cczj_dev_unlocked') === '1'
    enabled.value = localStorage.getItem('cczj_dev_enabled') === '1'
  } catch { /* ignore */ }
}

// 模块加载时从 localStorage 恢复
restore()

// 使用 reactive 包裹，确保模板中 ref 自动解包
export function useDevMode() {
  return reactive({
    unlocked,
    enabled,
    showPasswordModal,
    passwordInput,
    passwordError,
    clickAppName,
    clickSourceName,
    verifyPassword,
    closePasswordModal,
    setEnabled,
  })
}