<script setup lang="ts">
/**
 * 「动画」分组的面板本体。
 *
 * 和「日志」「诊断」一样：界面编译在应用里，把它挂上分组条的是内置扩展包
 * motion-effects（app/plugin/builtin/motion-effects/main.js）。那个包同时带着这套动效的
 * 时长与缓动，所以这一页读出来的数字就是它此刻生效的那一份，改文件重扫就会跟着变。
 */
defineOptions({ name: 'MotionPanel' })
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, MotionTransition, Segment } from './ui'
import { MOTION_PACK_ID, useMotionStore } from '../stores/motion'
import { usePluginStore } from '../stores/plugins'

const { t } = useI18n()
const motion = useMotionStore()
const plugins = usePluginStore()

/** 面板要念的五个时长令牌，顺序按从小到大的反馈排。 */
const TOKENS = [
  { name: '--cczj-motion-fast', labelKey: 'motion.tokenFast' },
  { name: '--cczj-motion-normal', labelKey: 'motion.tokenNormal' },
  { name: '--cczj-motion-slow', labelKey: 'motion.tokenSlow' },
  { name: '--cczj-motion-emphasis', labelKey: 'motion.tokenEmphasis' },
  { name: '--cczj-motion-carousel', labelKey: 'motion.tokenCarousel' },
] as const

/** MotionTransition / MotionList 认的预设名，直接念代码里的名字，不另起一套说法。 */
const PRESETS = ['fade', 'page', 'dialog', 'dropdown', 'slide-up', 'toast', 'toast-bottom', 'list']

const switchOptions = computed(() => [
  { value: 'on', label: t('motion.enabled') },
  { value: 'off', label: t('motion.disabled') },
])

// 令牌是 CSS 变量，注入型样式表什么时候到货、用户什么时候改文件都不在这个组件知道。
// 所以自己按秒重读一遍：面板开着才读，离开这一组就停。
const tick = ref(0)
let poll: number | null = null

const tokenRows = computed(() => {
  void tick.value
  return TOKENS.map(token => ({ ...token, ms: motion.tokenMs(token.name) }))
})

const pack = computed(() => plugins.packs.find(p => p.id === MOTION_PACK_ID) || null)
const packPath = computed(() => pack.value?.dir || '')
/** 包被停用或校验失败时给一句人话：这一页的开关此刻说了不算。 */
const packInactive = computed(() => pack.value === null || pack.value.status !== 'ready')

const previewing = ref(false)
let previewTimer: number | null = null

function startPreview(): void {
  if (previewTimer !== null) clearTimeout(previewTimer)
  previewing.value = false
  // 先让它退场再入场，才看得到「进」的那一段；连着点也不会叠成两层。
  previewTimer = window.setTimeout(() => {
    previewTimer = null
    previewing.value = true
    previewTimer = window.setTimeout(() => {
      previewTimer = null
      previewing.value = false
    }, 1400)
  }, 60)
}

function onSwitch(value: string | number): void {
  void motion.setEnabled(value === 'on')
}

function refresh(): void { tick.value += 1 }

onMounted(() => {
  poll = window.setInterval(refresh, 1000)
})

onBeforeUnmount(() => {
  if (poll !== null) clearInterval(poll)
  if (previewTimer !== null) clearTimeout(previewTimer)
  poll = null
  previewTimer = null
})

// 开关一按，属性就变了；面板上的数字也要跟着说当前值。
watch(() => motion.enabled, refresh)
</script>

<template>
  <div class="motion-panel">
    <section class="block">
      <h3>{{ t('motion.switchTitle') }}</h3>
      <p class="desc">{{ t('motion.switchDesc') }}</p>
      <div class="row cczj-flex cczj-items-center cczj-gap-7">
        <Segment
          :model-value="motion.enabled ? 'on' : 'off'"
          :options="switchOptions"
          @update:model-value="onSwitch"
        />
        <Button size="sm" variant="ghost" @click="startPreview">{{ t('motion.preview') }}</Button>
        <MotionTransition preset="dialog">
          <div v-if="previewing" class="demo">{{ t('motion.demo') }}</div>
        </MotionTransition>
      </div>
      <p v-if="packInactive" class="warn">{{ t('motion.packInactive') }}</p>
    </section>

    <section class="block">
      <h3>{{ t('motion.tokens') }}</h3>
      <div class="token-grid">
        <div v-for="row in tokenRows" :key="row.name" class="token">
          <div class="token-k cczj-flex cczj-items-baseline cczj-gap-4">
            <span class="token-name">{{ row.name.replace('--cczj-motion-', '') }}</span>
            <span class="token-ms">{{ row.ms.toFixed(0) }} ms</span>
          </div>
          <div class="token-label">{{ t(row.labelKey) }}</div>
        </div>
      </div>
      <p class="hint">{{ t('motion.packHint', { path: packPath }) }}</p>
    </section>

    <section class="block">
      <h3>{{ t('motion.presets') }}</h3>
      <div class="chips cczj-flex cczj-flex-wrap">
        <span v-for="preset in PRESETS" :key="preset" class="chip">{{ preset }}</span>
      </div>
      <p class="hint">{{ t('motion.presetsHint') }}</p>
    </section>
  </div>
</template>

<style scoped>
.block {
  padding: 18px 20px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  margin-bottom: 12px;
}
.block h3 {
  font-size: 1rem;
  font-weight: 700;
  margin: 0 0 14px;
  letter-spacing: 0.3px;
}
.desc {
  color: var(--text-muted);
  font-size: 0.9rem;
  margin: 0 0 14px;
  line-height: 1.5;
}
.hint {
  color: var(--text-muted);
  font-size: 0.84rem;
  margin: 12px 0 0;
  line-height: 1.5;
  word-break: break-all;
}
.warn {
  margin: 12px 0 0;
  font-size: 0.84rem;
  color: var(--warning, var(--text-muted));
}
.demo {
  padding: 6px 12px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 0.84rem;
}
.token-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 10px;
}
.token {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-secondary);
  min-width: 0;
}
.token-name {
  font-size: 0.82rem;
  font-weight: 700;
  color: var(--text-primary);
}
.token-ms {
  font-size: 0.82rem;
  color: var(--accent);
  font-variant-numeric: tabular-nums;
}
.token-label {
  margin-top: 4px;
  font-size: 0.79rem;
  color: var(--text-muted);
  line-height: 1.4;
}
.chips {
  gap: 6px;
}
.chip {
  padding: 3px 9px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 0.79rem;
}
</style>
