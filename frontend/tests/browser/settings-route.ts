import {createApp,defineComponent,h,onMounted,onActivated,onDeactivated,ref,nextTick} from 'vue'
import {createPinia} from 'pinia'
import {createMemoryHistory,createRouter} from 'vue-router'
import i18n from '../../src/locales'
import DesktopVideoShell from '../../src/modes/DesktopVideoShell.vue'
import Settings from '../../src/views/Settings.vue'
import VideoDiagnosticsPanel from '../../src/components/VideoDiagnosticsPanel.vue'
import DanmakuSources from '../../src/views/DanmakuSources.vue'
import PlayerDanmaku from '../../src/player/danmaku/PlayerDanmaku.vue'
import {addPluginSettingsTab} from '../../src/plugins/registry'
import {useThemeStore} from '../../src/stores/theme'
import {useMotionStore} from '../../src/stores/motion'
import App from '../../src/App.vue'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'
void App
// Home legitimately has a page and a sibling overlay. Reproduce its fragment
// without invoking its source requests; the real desktop shell owns transition.
const lifecycle={homeMounted:0,homeActivated:0,homeDeactivated:0,playerMounted:0}
const Home=defineComponent({name:'Home',setup(){
 onMounted(()=>lifecycle.homeMounted++);onActivated(()=>lifecycle.homeActivated++);onDeactivated(()=>lifecycle.homeDeactivated++)
 return()=>[h('div',{class:'home-probe'},'home'),h('div',{class:'home-overlay-probe'})]
}})
const Player=defineComponent({name:'Player',setup(){onMounted(()=>lifecycle.playerMounted++);return()=>h('div',{class:'player-probe'},'player')}})
const DanmakuPreview=defineComponent({setup(){const panel=ref<InstanceType<typeof PlayerDanmaku>>(),host=ref<HTMLElement>();onMounted(async()=>{await nextTick();await panel.value?.open()});return()=>h('div',{ref:host,style:'height:590px;width:900px;max-width:100%;position:relative;background:#10141c'},[h(PlayerDanmaku,{ref:panel,host:host.value,mediaKey:'preview',mediaTitle:'凡人修仙传',episodeLabel:'第01集',current:0,duration:1200,playing:false,fullscreen:false,mobile:false,video:()=>null})])}})
const pinia=createPinia(),router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:Home},{path:'/settings',component:Settings},{path:'/danmaku-sources',component:DanmakuSources},{path:'/danmaku-ui',component:DanmakuPreview},{path:'/player/:sourceKey/:globalId',name:'player',component:Player}]})
const errors:string[]=[]
addPluginSettingsTab({packId:'diagnostics-panel',id:'diagnostics',label:'诊断',icon:'monitor',component:VideoDiagnosticsPanel,order:50})
const app=createApp({render:()=>h('div',{class:'app-shell'},[h(DesktopVideoShell)])})
app.use(pinia).use(router).use(i18n)
app.config.errorHandler=error=>errors.push(error instanceof Error?error.stack||error.message:String(error))
await useThemeStore(pinia).load();await useMotionStore(pinia).load();await router.isReady()
app.mount('#app')
;(window as any).settingsRouteProbe={router,errors,lifecycle}
