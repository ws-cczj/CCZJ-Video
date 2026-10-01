/**
 * 界面动画总开关。
 *
 * 状态只有一个落点：settings KV 里的 ui_motion（缺省开）。写进去的是「用户想要什么」，
 * 落到界面上的是 <html data-cczj-motion>，让 styles/animations.css 那条退化合并策略
 * 去停掉所有过渡。中间隔了一层判断：动画库扩展包不在场时这个属性一律不写——
 * 「开启动画/关闭动画」那颗开关是 motion-effects 提供的分组，包被停用或被删掉之后，
 * 界面上就没有任何地方能把它关回来，留着属性等于把用户锁在无动画里。
 *
 * 开关的状态本身照旧留在 KV：把包启用回来，上次的偏好直接认回来。
 */
import { ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { GetSetting, SetSetting } from '../api/app'
import { usePluginStore } from './plugins'

/** 设置表里的键。'0' 表示关闭动画，其余值（含空）都按开启处理。 */
export const MOTION_SETTING_KEY = 'ui_motion'

/** 提供这套动效的内置扩展包。开关只在它处于「已启用」时生效。 */
export const MOTION_PACK_ID = 'motion-effects'

const ATTRIBUTE = 'data-cczj-motion'

/** 读一个 CSS 时长变量。变量没定义或解析不出来就是 0，调用方按「不等」处理。 */
function cssDurationMs(name: string): number {
  const raw = window.getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  const value = Number.parseFloat(raw)
  if (!Number.isFinite(value) || value <= 0) return 0
  return raw.endsWith('ms') ? value : value * 1000
}

export const useMotionStore = defineStore('motion', () => {
  const pluginStore = usePluginStore()
  const enabled = ref(true)
  const loaded = ref(false)

  /** 动画库扩展包在不在场。注册表还没读回来时算不在，等 watch 那一跳补上。 */
  const packReady = ref(false)

  function apply(): void {
    const root = document.documentElement
    if (enabled.value || !packReady.value) root.removeAttribute(ATTRIBUTE)
    else root.setAttribute(ATTRIBUTE, 'off')
  }

  async function load(): Promise<void> {
    try {
      const raw = await GetSetting(MOTION_SETTING_KEY)
      enabled.value = String(raw ?? '').trim() !== '0'
    } catch {
      enabled.value = true
    }
    loaded.value = true
    // 注册表要有人读才有 packs：这里自己催一次，不指望主题页或扩展面板先打开过。
    void pluginStore.ensureLoaded()
    // 注册表到货、重扫、启停某个包都会推这一条：属性跟着包的存在性走，
    // 不需要谁在挂载或卸载包里额外提醒一次。
    watch(
      () => pluginStore.readyPacks.some(pack => pack.id === MOTION_PACK_ID),
      (present) => { packReady.value = present; apply() },
      { immediate: true },
    )
  }

  async function setEnabled(next: boolean): Promise<void> {
    if (enabled.value === next) return
    enabled.value = next
    apply()
    // 与其余设置项一致：写库失败不打断界面，下次启动退回上次落库的值。
    try { await SetSetting(MOTION_SETTING_KEY, next ? '1' : '0') } catch { /* 忽略 */ }
  }

  /**
   * 换图前那一下淡出的时长。关掉动画就是 0，调用方直接换源不做过渡。
   * 上限是防呆：包里的变量被改成几秒时不该有图片在那儿空等。
   */
  function dipMs(): number {
    if (!enabled.value || !packReady.value) return 0
    return Math.min(cssDurationMs('--cczj-motion-fast'), 300)
  }

  /** 给「动画」分组显示参数用：当前生效的时长令牌（毫秒）。 */
  function tokenMs(name: string): number {
    return cssDurationMs(name)
  }

  return { enabled, loaded, packReady, load, setEnabled, dipMs, tokenMs }
})
