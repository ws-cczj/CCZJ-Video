import { createApp, h, ref } from 'vue'
import i18n from '../../src/locales'
import PlayerPanelToggle from '../../src/player/views/PlayerPanelToggle.vue'
import PlayerInformationPanel from '../../src/player/views/PlayerInformationPanel.vue'
import type { PlayerPanelModel } from '../../src/player/page/panelModel'
import type { SourceOption } from '../../src/player/page/useSourceCatalog'
import type { PlayLine } from '../../src/types'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/views/player.css'

const collapsed=ref(false),toggle=ref<InstanceType<typeof PlayerPanelToggle>>()
const sources=ref<SourceOption[]>([]),lines=ref<PlayLine[]>([]),activeKey=ref(''),activeLine=ref(0)
const chosen=ref<{key:string;line?:number}|null>(null)
const model:PlayerPanelModel={
 details:{video:null,overview:'',sourceKey:'',vodId:'',globalId:0,get lines(){return lines.value},get activeLineIndex(){return activeLine.value}},
 catalog:{get items(){return sources.value},get activeKey(){return activeKey.value},get readyCount(){return sources.value.length},probingCount:0,loading:false,label:()=>'',selectable:()=>true,retry:()=>{}},
 episodes:{items:[],current:0,currentNumber:1,ascending:true,originalIndex:index=>index,percent:()=>0,watched:()=>false,label:()=>''},
}
const app=createApp({render:()=>h('div',{class:'player-page'},[h('div',{class:'player-layout cczj-flex'},[
 h('div',{class:'player-col-main cczj-flex-1 cczj-relative',onPointermove:(event:PointerEvent)=>toggle.value?.approach(event)},[
  h('video',{id:'stable-video'}),h(PlayerPanelToggle,{ref:toggle,collapsed:collapsed.value,onToggle:()=>collapsed.value=!collapsed.value}),
 ]),h(PlayerInformationPanel,{model,collapsed:collapsed.value,onSource:(key:string,line?:number)=>{chosen.value={key,line};activeLine.value=lines.value.findIndex(item=>item.index===line);activeKey.value=key}}),
])])})
app.use(i18n).mount('#app')
;(window as any).sidebarProbe={video:document.querySelector('video'),unmount:()=>app.unmount(),setRoutes:(items:SourceOption[],itemsLines:PlayLine[],key:string)=>{sources.value=items;lines.value=itemsLines;activeKey.value=key},chosen:()=>chosen.value}
