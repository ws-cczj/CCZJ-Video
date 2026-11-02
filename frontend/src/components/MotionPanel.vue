<script setup lang="ts">
import { computed, ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useMotionStore } from '../stores/motion'
import { useThemeStore } from '../stores/theme'
import { useViewActivity } from '../composables/useViewActivity'
import { motionVariables, type MotionTiming } from '../motion/preferences'
import { createThemeReveal } from '../motion/themeReveal'
import { routeTransition } from '../mobile/routeMotion'
import '../mobile/route-motion.css'
import { presentationProfile } from '../platform/presentation'
import { Button, Modal, NumberField } from './ui'
import CczjMobilePlayerDrawer from '../mobile/CczjMobilePlayerDrawer.vue'
import Icon from './Icon.vue'
const props=withDefaults(defineProps<{active?:boolean}>(),{active:true})
const {t}=useI18n(),router=useRouter(),motion=useMotionStore(),theme=useThemeStore()
const mobile=presentationProfile().ui==='mobile'
const active=useViewActivity(()=>props.active)
const page=ref(0),playerOpen=ref(false),dialogOpen=ref(false),drawerOpen=ref(false)
const direction=ref('forward'),night=ref(false),themeSurface=ref<HTMLElement>()
const reveal=createThemeReveal()
const variables=computed(()=>motionVariables(motion.options))
const samplePalette=computed(()=>theme.themes.find(item=>item.id===(night.value?'d-violet':'cyan'))?.palette??theme.current.palette)
const sampleStyle=computed(()=>({'--bg-card':samplePalette.value.bgCard,'--text-primary':samplePalette.value.textPrimary,'--text-secondary':samplePalette.value.textSecondary,'--text-muted':samplePalette.value.textMuted,'--accent':samplePalette.value.accent,'--btn-solid':samplePalette.value.btnSolid,'--btn-solid-text':samplePalette.value.btnSolidText,'--border':samplePalette.value.border,background:samplePalette.value.bgApp,color:samplePalette.value.textPrimary}))
const routes=['/','/search']
function resolved(path:string){const value=router.resolve(path);return {...value,name:value.name??undefined}}
const pageTransition=computed(()=>routeTransition(resolved(routes[direction.value==='forward'?0:1]!),resolved(routes[direction.value==='forward'?1:0]!),0,1,mobile,routes))
const playbackTransition=computed(()=>routeTransition(resolved(playerOpen.value?'/':'/player/preview/0/0'),resolved(playerOpen.value?'/player/preview/0/0':'/'),0,playerOpen.value?1:0,mobile))
const timings=computed(()=>[
  ...(mobile?[{key:'slow' as const,label:t('motion.mobilePageDuration')}]:[{key:'section' as const,label:t('motion.pageDuration')},{key:'dismiss' as const,label:t('motion.returnDuration')}]),
  {key:'group' as const,label:t('motion.groupDuration')},
  {key:'normal' as const,label:t('motion.overlayDuration')},
  {key:'reveal' as const,label:t('motion.themeDuration')},
])
const toggles=computed(()=>[
  {key:'pages' as const,title:t('motion.pages'),detail:t('motion.pagesDesc')},
  {key:'overlays' as const,title:t('motion.overlays'),detail:t('motion.overlaysDesc')},
  {key:'feedback' as const,title:t('motion.feedback'),detail:t('motion.feedbackDesc')},
  {key:'stagger' as const,title:t('motion.stagger'),detail:t('motion.staggerDesc')},
])
function selectPage(index:number):void {if(index===page.value)return;direction.value=index>page.value?'forward':'back';page.value=index}
function setDuration(key:MotionTiming,value:number):void {void motion.setOptions({durations:{...motion.options.durations,[key]:value}})}
function previewTheme(event:MouseEvent):void {
  const bounds=(event.currentTarget as HTMLElement).getBoundingClientRect()
  const origin=event.detail?{x:event.clientX,y:event.clientY}:{x:bounds.left+bounds.width/2,y:bounds.top+bounds.height/2}
  void reveal.run(()=>{if(active.value)night.value=!night.value},origin,motion.allows('pages')?motion.tokenMs('--cczj-motion-reveal'):0,themeSurface.value)
}
let playerOpener:HTMLElement|undefined
function openPlayer(event:MouseEvent):void {playerOpener=event.currentTarget as HTMLElement;playerOpen.value=true}
function closePlayer():void {playerOpen.value=false;void nextTick(()=>{if(active.value&&playerOpener?.isConnected)playerOpener.focus({preventScroll:true})})}
function retirePreview(element:Element):void {element.setAttribute('inert','')}
function activatePreview(element:Element):void {element.removeAttribute('inert')}
function stop():void {reveal.dispose();dialogOpen.value=false;drawerOpen.value=false;playerOpen.value=false}
watch(active,visible=>{if(!visible)stop()})
watch(()=>motion.allows('pages'),allowed=>{if(!allowed)reveal.cancel()})
onBeforeUnmount(stop)
</script>
<template>
  <div class="motion-page cczj-min-w-0">
    <header class="motion-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-8">
      <div><h2>{{t('motion.switchTitle')}}</h2></div>
      <label class="motion-toggle"><input type="checkbox" :checked="motion.enabled" :aria-label="t('motion.switchTitle')" @change="motion.setEnabled(($event.target as HTMLInputElement).checked)"/><span/></label>
    </header>
    <p v-if="motion.systemReduced" class="motion-notice">{{t('motion.systemReduced')}}</p>
    <p v-else-if="!motion.enabled" class="motion-notice">{{t('motion.switchDesc')}}</p>
    <div class="motion-control cczj-flex cczj-items-center cczj-justify-between cczj-gap-8">
      <div><h3>{{t('motion.reduceMotion')}}</h3><p>{{t('motion.reduceMotionDesc')}}</p></div>
      <label class="motion-toggle"><input type="checkbox" :checked="motion.options.reduceMotion" :aria-label="t('motion.reduceMotion')" @change="motion.setOptions({reduceMotion:($event.target as HTMLInputElement).checked})"/><span/></label>
    </div>
    <div class="scenario-grid cczj-grid cczj-gap-8">
      <section class="motion-section cczj-min-w-0 cczj-border cczj-rounded-lg cczj-p-8">
        <h3>{{t('motion.navigationScenario')}}</h3>
        <div class="cczj-flex cczj-flex-wrap cczj-gap-4 cczj-mt-6 cczj-mb-6">
          <Button v-for="(label,index) in [t('sidebar.home'),t('sidebar.search')]" :key="index" size="sm" :variant="page===index?'primary':'secondary'" :aria-pressed="page===index" @click="selectPage(index)">{{label}}</Button>
        </div>
        <div class="scenario-stage cczj-relative cczj-overflow-hidden cczj-rounded-md cczj-border">
          <Transition :name="pageTransition" :css="motion.allows('pages')" @before-leave="retirePreview" @before-enter="activatePreview">
            <div :key="page" class="scenario-screen cczj-absolute cczj-inset-0 cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-6" :inert="playerOpen">
              <Icon :name="page?'search':'home'" :size="28"/><strong>{{page?t('sidebar.search'):t('sidebar.home')}}</strong>
              <Button size="sm" @click="openPlayer"><Icon name="play" :size="16"/>{{t('motion.openPlayer')}}</Button>
            </div>
          </Transition>
          <Transition :name="playbackTransition" :css="motion.allows('pages')" @before-leave="retirePreview" @before-enter="activatePreview">
            <div v-if="playerOpen" class="scenario-player scenario-screen cczj-absolute cczj-inset-0 cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-6">
              <Icon name="play" :size="32"/><strong>{{t('player.videoPlayback')}}</strong><Button variant="overlay" size="sm" @click="closePlayer">{{t('common.back')}}</Button>
            </div>
          </Transition>
        </div>
      </section>
      <section class="motion-section cczj-min-w-0 cczj-border cczj-rounded-lg cczj-p-8">
        <h3>{{t('motion.overlayScenario')}}</h3>
        <div class="scenario-stage cczj-flex cczj-items-center cczj-justify-center cczj-flex-wrap cczj-gap-6 cczj-mt-6 cczj-rounded-md cczj-border cczj-p-8">
          <Button @click="dialogOpen=true">{{t('motion.openDialog')}}</Button><Button @click="drawerOpen=true">{{t('motion.openDrawer')}}</Button>
        </div>
      </section>
      <section class="motion-section cczj-min-w-0 cczj-border cczj-rounded-lg cczj-p-8">
        <h3>{{t('motion.themeScenario')}}</h3>
        <div ref="themeSurface" :style="sampleStyle" class="theme-scenario cczj-relative cczj-overflow-hidden cczj-rounded-md cczj-border cczj-p-8 cczj-mt-6">
          <strong>{{t('motion.previewContent')}}</strong><p class="cczj-mt-4 cczj-mb-6">{{t('motion.previewThemeLocal')}}</p>
          <Button variant="primary" @click="previewTheme">{{t('motion.changePreviewTheme')}}</Button>
        </div>
      </section>
    </div>
    <section class="motion-section cczj-border cczj-rounded-lg cczj-p-8 cczj-mt-8">
      <h3>{{t('motion.detailTitle')}}</h3>
      <div v-for="item in toggles" :key="item.key" class="motion-control cczj-flex cczj-items-center cczj-justify-between cczj-gap-8">
        <div><h4>{{item.title}}</h4><p>{{item.detail}}</p></div><label class="motion-toggle"><input type="checkbox" :checked="motion.options[item.key]" :aria-label="item.title" @change="motion.setOptions({[item.key]:($event.target as HTMLInputElement).checked})"/><span/></label>
      </div>
      <details class="cczj-mt-6"><summary>{{t('motion.adjustTimings')}}</summary><p class="cczj-mt-6">{{t(mobile?'motion.mobileTimingsHint':'motion.timingsHint')}}</p>
        <div v-for="item in timings" :key="item.key" class="motion-control cczj-flex cczj-flex-wrap cczj-items-center cczj-justify-between cczj-gap-8"><span>{{item.label}}</span><div class="cczj-flex cczj-items-center cczj-gap-4"><NumberField :model-value="parseInt(variables['--cczj-motion-'+item.key])" :min="100" :max="2500" :step="50" :ariaLabel="item.label" @update:model-value="setDuration(item.key,$event)"/><span>{{t('motion.milliseconds')}}</span></div></div>
      </details>
      <Button class="cczj-mt-6" @click="motion.reset()">{{t('motion.restoreDefaults')}}</Button>
    </section>
    <Modal v-model="dialogOpen" :title="t('motion.dialogPreview')" width="420px"><p>{{t('motion.overlayPreviewContent')}}</p><template #footer><Button @click="dialogOpen=false">{{t('common.close')}}</Button></template></Modal>
    <CczjMobilePlayerDrawer :open="drawerOpen" :title="t('motion.drawerPreview')" :fullscreen="false" :placement="mobile?'bottom':'right'" lazy @close="drawerOpen=false"><p class="cczj-p-8">{{t('motion.overlayPreviewContent')}}</p></CczjMobilePlayerDrawer>
  </div>
</template>
<style scoped>
.motion-page { container-type:inline-size; }
.motion-header { padding:8px 0 18px; }
h2 { font-size:1.35rem;margin:0 0 7px; } h3 { font-size:1rem;margin:0 0 12px; } h4 { font-size:.92rem;margin:0 0 4px;@apply cczj-font-medium; }
p { font-size:.82rem;@apply cczj-text-muted;line-height:1.65;margin:0; }
.scenario-grid { grid-template-columns:repeat(2,minmax(0,1fr)); }
.motion-section { background:var(--bg-card); }
.scenario-stage { min-height:210px;background:var(--bg-secondary); }
.scenario-screen { background:var(--bg-card); }
.scenario-player { z-index:5;background:#11151d;color:#e3edf6; }
.theme-scenario { min-height:184px; }
.motion-control { padding:15px 0; }.motion-control+.motion-control { @apply cczj-border-top; }.motion-control>div:first-child { @apply cczj-min-w-0; }
.motion-toggle { @apply cczj-relative cczj-block cczj-flex-shrink-0;width:42px;height:24px; }.motion-toggle input { @apply cczj-absolute cczj-opacity-0 cczj-w-full cczj-h-full cczj-cursor-pointer;margin:0; }.motion-toggle span { @apply cczj-absolute cczj-inset-0 cczj-pointer-events-none cczj-border-strong;border-radius:24px;background:var(--bg-secondary);transition:background-color var(--cczj-motion-fast); }.motion-toggle span::before { content:'';@apply cczj-absolute cczj-h-16 cczj-w-16 cczj-rounded-50;left:3px;top:3px;background:var(--text-secondary);transition:transform var(--cczj-motion-fast); }.motion-toggle input:checked+span { background:var(--accent);@apply cczj-border-primary; }.motion-toggle input:checked+span::before { transform:translateX(18px);background:var(--accent-contrast); }.motion-toggle input:focus-visible+span { outline:2px solid var(--accent);outline-offset:3px; }
.motion-notice { padding:12px 14px;@apply cczj-mb-8;background:var(--bg-card);border-left:3px solid var(--accent); }
summary { @apply cczj-cursor-pointer; }
@container(max-width:650px){.scenario-grid { grid-template-columns:1fr; }}
</style>
