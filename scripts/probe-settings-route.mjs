import {createRequire} from 'node:module'
import {readFileSync,mkdirSync} from 'node:fs'
import {resolve} from 'node:path'
import {fileURLToPath} from 'node:url'
import {createServer} from '../frontend/node_modules/vite/dist/node/index.js'

const root=resolve(fileURLToPath(new URL('..',import.meta.url)))
// Optional external Playwright installation; no browser/dependency downloads.
const require=createRequire(process.env.CCZJ_PLAYWRIGHT_NODE_MODULES
 ? resolve(process.env.CCZJ_PLAYWRIGHT_NODE_MODULES,'package.json')
 : resolve(root,'frontend/package.json'))
const {chromium}=require('playwright')
const names=[...readFileSync(resolve(root,'frontend/bindings/cczjVideo/app/service/app.js'),'utf8').matchAll(/export function (\w+)\(/g)].map(value=>value[1])
const specific={GetSetting:'""',SetSetting:'undefined',WindowGetSize:'({width:1280,height:800})',GetCloseBehavior:'true',WindowGetResizable:'true',GetLogKeepDays:'30',GetDoubanIntervalMinutes:'30',GetAllowPrivateNetwork:'false',GetAppVersion:'"2.3.2"',ListDownloads:'[]',GetDownloadDir:'""',ListPlugins:'[]',GetDiagnostics:'({env:{app_version:"2.3.2"}})',GetRuntimeMetrics:'({})',DanmakuBinding:'null',DanmakuSearch:'[]',DanmakuMatch:'({binding:null,window:null,attempts:[]})'}
const matchPreview=process.argv.includes('--match')
if(matchPreview)specific.DanmakuMatch=JSON.stringify({
 binding:{localKey:'preview',confirmed:true,offsetMs:0,episode:{provider:'qq',episodeId:'sample01',seriesId:'sample',aid:'',title:'凡人修仙传',number:'1',durationMs:1200000,pageUrl:''}},
 window:{provider:'qq',episodeId:'sample01',items:[],warnings:[],startMs:0,endMs:75000,cached:true,stale:false,skipped:0},
 attempts:[{provider:'bilibili',status:'cooling_down',code:'NO_WORK_MATCH',retryAt:Math.floor(Date.now()/1000)+86400},{provider:'qq',status:'available',code:'',retryAt:0},{provider:'iqiyi',status:'cancelled',code:'',retryAt:0}],
})
const cacheCheck=process.argv.includes('--cache')
const mock=names.map(name=>cacheCheck&&name==='GetSetting'?`export async function GetSetting(key){window.danmakuSettingsProbe??={preferences:JSON.stringify({enabled:false,opacity:.6,freshDays:3}),writes:[]};return key==='danmaku_preferences_v1'?window.danmakuSettingsProbe.preferences:''}`
 :cacheCheck&&name==='SetSetting'?`export async function SetSetting(key,value){window.danmakuSettingsProbe.writes.push({key,value});if(key==='danmaku_preferences_v1')window.danmakuSettingsProbe.preferences=value}`
 :`export async function ${name}(){return ${specific[name]??(name.startsWith('GetAll')||name.startsWith('List')?'[]':'""')};}`).join('\n')+'\nexport {normalizeApiError,ApiFailure,toApiFailure} from "/src/api/errors.ts";'
const server=await createServer({root:resolve(root,'frontend'),configFile:resolve(root,'frontend/vite.config.ts'),plugins:[{name:'isolated-settings-bindings',enforce:'pre',load(id){if(id.replaceAll('\\','/').endsWith('/src/api/app.ts'))return mock}}],server:{host:'127.0.0.1',port:19273,strictPort:true}})
let browser
try{
 await server.listen()
 browser=await chromium.launch({channel:'msedge',headless:true})
 const page=await browser.newPage({viewport:{width:1280,height:800}})
 const errors=[],transitionWarnings=[];page.on('pageerror',error=>errors.push(error.stack||error.message))
 page.on('console',message=>{if(message.text().includes('non-element root node'))transitionWarnings.push(message.text())})
 await page.goto('http://127.0.0.1:19273/tests/browser/settings-route.html')
 await page.waitForFunction(()=>window.settingsRouteProbe)
 const visits=[]
 for(const [path,selector] of [['/settings','.settings-page'],['/','.home-probe'],['/settings','.settings-page'],['/player/source/video?ep=1','.player-probe'],['/player/source/video?ep=2','.player-probe'],['/settings','.settings-page']]){
  await page.evaluate(path=>window.settingsRouteProbe.router.push(path),path)
  await page.waitForTimeout(700)
  visits.push({path,visible:await page.locator(selector).isVisible()})
 }
 let result=await page.evaluate(()=>({errors:window.settingsRouteProbe.errors,lifecycle:window.settingsRouteProbe.lifecycle}))
 if(process.argv.includes('--controls')){
  await page.locator('[data-group="theme"]').click()
  await page.locator('.theme-grid').first().waitFor({state:'visible'})
  await page.locator('[data-group="diagnostics"]').click()
  await page.locator('.diag-nav').waitFor({state:'visible'})
  const themeHidden=!(await page.locator('.theme-grid').first().isVisible())
  const content=page.locator('.diag-content')
  await content.hover();await page.mouse.wheel(0,600);await page.waitForTimeout(150)
  const scroll=await page.evaluate(()=>({outer:document.querySelector('.main-content')?.scrollTop||0,inner:document.querySelector('.diag-content')?.scrollTop||0}))
  const nav=page.locator('.diag-nav');await nav.scrollIntoViewIfNeeded()
  const before=await nav.evaluate(element=>({left:element.scrollLeft,active:element.querySelector('.active')?.getAttribute('data-section')}))
  const box=await nav.boundingBox()
  await page.mouse.move(box.x+box.width-35,box.y+box.height/2);await page.mouse.down();await page.mouse.move(box.x+box.width-235,box.y+box.height/2,{steps:12});await page.mouse.up()
  const after=await nav.evaluate(element=>({left:element.scrollLeft,active:element.querySelector('.active')?.getAttribute('data-section')}))
  console.log(JSON.stringify({themeHidden,scroll,drag:{before,after}},null,2))
  if(!themeHidden||scroll.outer+scroll.inner<=0||after.left<=before.left||before.active!==after.active)process.exitCode=1
 }
 if(process.argv.includes('--visual')){
  const reportDir=resolve(root,cacheCheck?'reports/danmaku-panel-cache-2026-10-19':matchPreview?'reports/danmaku-parallel-match-2026-10-19':'reports/settings-danmaku-2026-10-19')
  mkdirSync(reportDir,{recursive:true})
  await page.evaluate(()=>window.settingsRouteProbe.router.push('/danmaku-sources'))
  await page.locator('.cczj-danmaku-source-card').first().waitFor({state:'visible'});await page.waitForTimeout(250)
  if(cacheCheck){
   if(await page.locator('#danmaku-cache-days').inputValue()!=='3')throw new Error('Saved cache policy was not loaded')
   await page.evaluate(()=>window.danmakuSettingsProbe.preferences=JSON.stringify({enabled:false,opacity:.4,fontSize:32,freshDays:3}))
   await page.locator('#danmaku-cache-days').selectOption('7')
   await page.getByRole('button',{name:'保存',exact:true}).click()
   await page.waitForFunction(()=>JSON.parse(window.danmakuSettingsProbe.preferences).freshDays===7)
   const preferences=await page.evaluate(()=>JSON.parse(window.danmakuSettingsProbe.preferences))
   if(preferences.enabled!==false||preferences.opacity!==.4||preferences.fontSize!==32)throw new Error('Saving cache policy overwrote display preferences')
   console.log(JSON.stringify({cachePolicySaved:preferences},null,2))
  }
  await page.screenshot({path:resolve(reportDir,'sources.png')})
  await page.evaluate(()=>window.settingsRouteProbe.router.push('/danmaku-ui'))
  await page.locator('.cczj-danmaku-panel').waitFor({state:'visible'});await page.waitForTimeout(250)
  if(await page.locator('.cczj-danmaku-panel input[type=text]').count())throw new Error('Manual source selection must not be exposed')
  if(matchPreview){
   const selected=page.locator('.cczj-danmaku-availability strong')
   await selected.waitFor({state:'visible'})
   if(!(await selected.innerText()).includes('腾讯视频'))throw new Error('Selected fallback is not visible')
   const summary=page.locator('summary').filter({hasText:'自动匹配记录'})
   await summary.click()
   const outcomes=await summary.locator('..').locator('p').allTextContents()
   console.log(JSON.stringify({selected:await selected.innerText(),outcomes},null,2))
   if(outcomes.length!==3||!outcomes[0].includes('冷却')||!outcomes[1].includes('可用')||!outcomes[2].includes('停止探测'))throw new Error('Source outcomes not displayed')
  }
  await page.screenshot({path:resolve(reportDir,'player-panel.png')})
 }
 result=await page.evaluate(()=>({errors:window.settingsRouteProbe.errors,lifecycle:window.settingsRouteProbe.lifecycle}))
 console.log(JSON.stringify({pageErrors:errors,transitionWarnings,visits,...result},null,2))
 if(errors.length||transitionWarnings.length||result.errors.length||visits.some(visit=>!visit.visible)||result.lifecycle.homeMounted!==1||result.lifecycle.homeActivated!==2||result.lifecycle.homeDeactivated!==2||result.lifecycle.playerMounted!==1)process.exitCode=1
}finally{await browser?.close();await server.close()}
