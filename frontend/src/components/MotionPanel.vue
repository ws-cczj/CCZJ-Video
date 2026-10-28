<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMotionStore } from '../stores/motion'
import { useViewActivity } from '../composables/useViewActivity'
import { type MotionStyle } from '../motion/preferences'
import { Segment } from './ui'
import Icon from './Icon.vue'
const props = withDefaults(defineProps<{ active?: boolean }>(), { active: true })
const { t } = useI18n()
const motion = useMotionStore()
const active = useViewActivity(() => props.active)
const version = ref(0)
const kind = ref<'page' | 'dialog' | 'list'>('page')
const styles = computed(() => [
  { id: 'calm' as MotionStyle, title: t('motion.styleCalm'), description: t('motion.calmDesc') },
  { id: 'balanced' as MotionStyle, title: t('motion.styleBalanced'), description: t('motion.balancedDesc') },
  { id: 'lively' as MotionStyle, title: t('motion.styleLively'), description: t('motion.livelyDesc') },
])
const previewOptions = computed(() => [{value:'page',label:t('motion.pagePreview')},{value:'dialog',label:t('motion.dialogPreview')},{value:'list',label:t('motion.listPreview')}])
const toggles = computed(() => [
  { key: 'pages' as const, title: t('motion.pages'), detail: t('motion.pagesDesc') },
  { key: 'overlays' as const, title: t('motion.overlays'), detail: t('motion.overlaysDesc') },
  { key: 'feedback' as const, title: t('motion.feedback'), detail: t('motion.feedbackDesc') },
  { key: 'stagger' as const, title: t('motion.stagger'), detail: t('motion.staggerDesc') },
])
const durations = computed(() => [
  {label:t('motion.tokenFast'),value:motion.tokenMs('--cczj-motion-fast')},
  {label:t('motion.tokenNormal'),value:motion.tokenMs('--cczj-motion-normal')},
  {label:t('motion.tokenCarousel'),value:motion.tokenMs('--cczj-motion-carousel')},
])
function selectStyle(style: MotionStyle): void { void motion.setOptions({style,intensity:style==='calm'?35:style==='lively'?90:60}); version.value++ }
function range(key:'speed'|'intensity',event:Event):void { void motion.setOptions({[key]:Number((event.target as HTMLInputElement).value)}) }
function choosePreview(value:string|number):void { kind.value=value as typeof kind.value;version.value++ }
</script>
<template>
  <div class="motion-page">
    <header class="motion-header">
      <div><h2>{{ t('motion.switchTitle') }}</h2><p>{{ t('motion.subtitle') }}</p></div>
      <label class="motion-toggle"><input type="checkbox" :checked="motion.enabled" :aria-label="t('motion.switchTitle')" @change="motion.setEnabled(($event.target as HTMLInputElement).checked)" /><span /></label>
    </header>
    <p v-if="motion.reduced" class="motion-notice">{{ t('motion.systemReduced') }}</p>
    <p v-else-if="!motion.enabled" class="motion-notice">{{ t('motion.switchDesc') }}</p>
    <section class="motion-section">
      <div class="section-title"><h3>{{ t('motion.styleTitle') }}</h3><button class="small-button" @click="motion.reset()">{{ t('motion.restoreDefaults') }}</button></div>
      <div class="style-options" role="group" :aria-label="t('motion.styleTitle')">
        <button v-for="style in styles" :key="style.id" class="style-option" :class="[style.id,{selected:motion.options.style===style.id}]" :aria-pressed="motion.options.style===style.id" @click="selectStyle(style.id)">
          <div class="style-illustration" aria-hidden="true"><span /><span /><span /></div>
          <strong>{{ style.title }}<Icon v-if="motion.options.style===style.id" name="check" :size="14" /></strong><p>{{ style.description }}</p>
        </button>
      </div>
      <div class="motion-control">
        <div><h4>{{ t('motion.speed') }}</h4><p>{{ t('motion.speedDesc') }}</p></div>
        <label class="range-control"><input type="range" min="60" max="160" step="10" :value="motion.options.speed" :aria-label="t('motion.speed')" @change="range('speed',$event)" /><output>{{ motion.options.speed }}%</output></label>
      </div>
      <div class="motion-control">
        <div><h4>{{ t('motion.intensity') }}</h4><p>{{ t('motion.intensityDesc') }}</p></div>
        <label class="range-control"><input type="range" min="0" max="100" step="5" :value="motion.options.intensity" :aria-label="t('motion.intensity')" @change="range('intensity',$event)" /><output>{{ motion.options.intensity }}%</output></label>
      </div>
    </section>
    <section class="motion-section">
      <div class="section-title"><h3>{{ t('motion.previewTitle') }}</h3><button class="small-button play-preview" @click="version++"><Icon name="play" :size="13" />{{ t('motion.preview') }}</button></div>
      <Segment :model-value="kind" :options="previewOptions" @update:model-value="choosePreview" />
      <div class="preview-stage">
          <div v-if="active" :key="`${kind}-${version}`" :data-preview-version="version" class="preview-sample" :class="`preview-${kind}`">
            <div class="sample-heading"><span class="sample-symbol"><Icon :name="kind==='dialog'?'info':'play'" :size="18" /></span><strong>{{ kind==='dialog'?t('motion.dialogPreview'):t('motion.previewContent') }}</strong></div>
            <div class="sample-lines" aria-hidden="true"><i /><i /></div>
            <div v-if="kind==='list'" class="sample-list"><span v-for="i in 3" :key="i" class="sample-tile" :style="{'--preview-order':i-1}"><Icon name="play" :size="16" /></span></div>
            <div v-else class="sample-action" aria-hidden="true">CCZJ VIDEO <span>→</span></div>
          </div>
      </div>
      <p>{{ t('motion.previewHint') }}</p>
    </section>
    <section class="motion-section">
      <h3>{{ t('motion.detailTitle') }}</h3>
      <div v-for="item in toggles" :key="item.key" class="motion-control">
        <div><h4>{{ item.title }}</h4><p>{{ item.detail }}</p></div>
        <label class="motion-toggle"><input type="checkbox" :checked="motion.options[item.key]" :aria-label="item.title" @change="motion.setOptions({[item.key]:($event.target as HTMLInputElement).checked})" /><span /></label>
      </div>
      <details class="timing-details"><summary>{{ t('motion.tokens') }}</summary><div v-for="row in durations" :key="row.label"><span>{{ row.label }}</span><strong>{{ row.value }} ms</strong></div></details>
    </section>
  </div>
</template>
<style scoped>
.motion-page { @apply cczj-min-w-0;container-type:inline-size; }
.motion-header { @apply cczj-flex cczj-justify-between cczj-items-center cczj-gap-10;padding:8px 0 18px; }
h2 { font-size:1.35rem;margin:0 0 7px; } h3 { font-size:1rem;margin:0 0 16px; } h4 { font-size:.92rem;margin:0 0 4px;@apply cczj-font-medium; }
p { font-size:.82rem;@apply cczj-text-muted;line-height:1.65;margin:0; }
.motion-section { @apply cczj-mb-8;padding:22px;@apply cczj-border;border-radius:14px;background:var(--bg-card); }
.section-title { @apply cczj-flex cczj-items-center cczj-justify-between cczj-gap-7 cczj-mb-8; }.section-title h3 { margin:0; }
.small-button { font:inherit;font-size:.82rem;background:var(--bg-secondary);@apply cczj-text-secondary cczj-border;border-radius:7px;padding:7px 10px;@apply cczj-cursor-pointer; }
.play-preview { @apply cczj-flex;gap:7px;@apply cczj-items-center cczj-text-accent; }
.style-options { @apply cczj-grid;grid-template-columns:repeat(3,minmax(0,1fr));@apply cczj-gap-6 cczj-mb-7; }
.style-option { @apply cczj-p-7 cczj-border;border-radius:11px;background:var(--bg-secondary);@apply cczj-text-primary;font:inherit;@apply cczj-text-left cczj-cursor-pointer;transition:border-color var(--cczj-motion-fast),background-color var(--cczj-motion-fast); }
.style-option.selected { @apply cczj-border-primary;background:var(--accent-alpha-10); }
.style-option strong { @apply cczj-flex cczj-justify-between cczj-items-center;font-size:.91rem;margin:12px 0 6px; }.style-option p { font-size:.76rem; }
.style-illustration { @apply cczj-flex cczj-items-end cczj-gap-3;height:38px; }.style-illustration span { width:25%;max-width:32px;height:18px;@apply cczj-rounded-sm;background:var(--accent-alpha-20);transition:transform var(--cczj-motion-normal) var(--cczj-motion-ease-enter); }
.balanced .style-illustration span:nth-child(2) { height:27px; }.lively .style-illustration span { height:22px; }.lively .style-illustration span:nth-child(2) { height:36px; }
.style-option:hover .style-illustration span:nth-child(2),.style-option:focus-visible .style-illustration span:nth-child(2) { transform:translateY(-4px); }
.motion-control { @apply cczj-flex cczj-items-center cczj-justify-between cczj-gap-12;padding:15px 0; }.motion-control+.motion-control { @apply cczj-border-top; }.motion-control>div { @apply cczj-min-w-0 cczj-flex-1; }
.range-control { width:260px;max-width:50%;@apply cczj-flex cczj-gap-6 cczj-items-center; }.range-control input { @apply cczj-w-full cczj-min-w-0;accent-color:var(--accent); }.range-control output { font-size:.84rem;width:42px;@apply cczj-flex-shrink-0 cczj-text-right;font-variant-numeric:tabular-nums; }
.motion-toggle { @apply cczj-relative cczj-block;width:42px;@apply cczj-h-24 cczj-flex-shrink-0; }.motion-toggle input { @apply cczj-absolute cczj-opacity-0 cczj-w-full cczj-h-full;margin:0;@apply cczj-cursor-pointer; }.motion-toggle span { @apply cczj-absolute;inset:0;@apply cczj-pointer-events-none;border-radius:24px;@apply cczj-border-strong;background:var(--bg-secondary);transition:background-color var(--cczj-motion-fast); }.motion-toggle span::before { content:'';@apply cczj-absolute;left:3px;top:3px;@apply cczj-h-16 cczj-w-16 cczj-rounded-50;background:var(--text-secondary);transition:transform var(--cczj-motion-fast); }.motion-toggle input:checked+span { background:var(--accent);@apply cczj-border-primary; }.motion-toggle input:checked+span::before { transform:translateX(18px);background:var(--accent-contrast); }.motion-toggle input:focus-visible+span,button:focus-visible { outline:2px solid var(--accent);outline-offset:3px; }
.motion-notice { padding:12px 14px;@apply cczj-mb-8;background:var(--bg-card);border-left:3px solid var(--accent); }
.preview-stage { @apply cczj-grid;place-items:center;min-height:210px;margin:16px 0 10px;@apply cczj-p-8;border:1px dashed var(--border);@apply cczj-rounded-lg;background:var(--bg-secondary);@apply cczj-overflow-hidden; }
.preview-sample { @apply cczj-w-full;max-width:340px;box-sizing:border-box;padding:18px;@apply cczj-border;border-radius:11px;background:var(--bg-card);box-shadow:0 8px 20px rgba(0,0,0,.08); }.sample-heading { @apply cczj-flex cczj-items-center cczj-gap-5;font-size:.91rem; }.sample-symbol { @apply cczj-grid;place-items:center;@apply cczj-w-32 cczj-h-32 cczj-rounded-md cczj-text-accent;background:var(--accent-alpha-10); }.sample-lines { @apply cczj-grid cczj-gap-4;margin:16px 0; }.sample-lines i { @apply cczj-h-1.5;width:85%;border-radius:3px;@apply cczj-bg-border; }.sample-lines i:last-child { width:60%; }.sample-action { padding:8px 10px;border-radius:7px;background:var(--accent-alpha-10);@apply cczj-text-accent;font-size:.73rem;@apply cczj-flex cczj-justify-between; }
.sample-list { @apply cczj-grid;grid-template-columns:repeat(3,1fr);@apply cczj-gap-4; }.sample-tile { @apply cczj-grid;place-items:center;height:42px;border-radius:7px;@apply cczj-text-accent;background:var(--accent-alpha-10);animation:sample-reveal var(--cczj-motion-normal) var(--cczj-motion-ease-enter) backwards;animation-delay:calc(var(--preview-order)*var(--cczj-motion-stagger,35ms)); }
.preview-page { animation: motion-preview-page var(--cczj-motion-slow) var(--cczj-motion-ease-enter); }
.preview-dialog { animation: motion-preview-dialog var(--cczj-motion-normal) var(--cczj-motion-ease-enter); }
.preview-list { animation: cczj-fade-in var(--cczj-motion-normal) var(--cczj-motion-ease-enter); }
@keyframes motion-preview-page { from { opacity: 0; transform: translateY(var(--cczj-motion-distance,8px)); } to { opacity: 1; transform: translateY(0); } }
@keyframes motion-preview-dialog { from { opacity: 0; transform: translateY(var(--cczj-motion-distance,8px)) scale(var(--cczj-motion-enter-scale,.98)); } to { opacity: 1; transform: translateY(0) scale(1); } }
@keyframes sample-reveal { from { opacity:0;transform:translateY(var(--cczj-motion-distance,6px)); } to { opacity:1;transform:translateY(0); } }
.timing-details { @apply cczj-mt-4 cczj-border-top;padding-top:14px;font-size:.78rem;@apply cczj-text-muted; }.timing-details summary { @apply cczj-cursor-pointer; }.timing-details>div { @apply cczj-flex cczj-justify-between;gap:15px;padding:10px 0 0; }.timing-details strong { @apply cczj-text-secondary cczj-whitespace-nowrap; }
@container (max-width:570px) { .style-options { grid-template-columns:1fr; }.style-option { @apply cczj-grid;grid-template-columns:70px minmax(0,1fr);column-gap:14px; }.style-illustration { grid-row:span 2;@apply cczj-self-center; }.style-option strong { @apply cczj-mt-0; }.motion-section { @apply cczj-p-8; }.range-control { width:180px; }.motion-control { @apply cczj-flex-wrap; } }
@media (prefers-reduced-motion:reduce) { .sample-tile, .preview-sample { animation:none; } }
html[data-cczj-motion='off'] .sample-tile,
html[data-cczj-motion='off'] .preview-sample { animation:none; }
</style>
