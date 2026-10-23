import { createRequire } from 'node:module'
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createServer } from '../frontend/node_modules/vite/dist/node/index.js'
const root=resolve(fileURLToPath(new URL('..',import.meta.url)))
const require=createRequire(resolve(process.env.CCZJ_PLAYWRIGHT_NODE_MODULES||resolve(root,'frontend'),'package.json'))
const {chromium}=require('playwright')
const names=[...readFileSync(resolve(root,'frontend/bindings/cczjVideo/app/service/app.js'),'utf8').matchAll(/export function (\w+)\(/g)].map(row=>row[1])
const unavailable=JSON.stringify({binding:null,window:null,traceId:'startup-probe-match',attempts:[{provider:'bilibili',status:'cooling_down',code:'WORK_YEAR_MISMATCH',stage:'work_cache',detail:'cached_evidence=true; year_rejected=1',retryAt:Math.floor(Date.now()/1000)+86400},{provider:'qq',status:'cooling_down',code:'PERMISSION_DENIED',retryAt:Math.floor(Date.now()/1000)+86400},{provider:'iqiyi',status:'cooling_down',code:'UNSUPPORTED',retryAt:Math.floor(Date.now()/1000)+86400}]})
const mock=names.map(name=>name==='DanmakuMatch'
 ? `export function DanmakuMatch(){return new Promise(resolve=>window.startupProbe.finishMatch=()=>resolve(${unavailable}))}`
 : name==='WriteLog'?'export async function WriteLog(entry){window.startupProbe?.logs.push(entry);return true}'
 : `export async function ${name}(){return ${name==='ListPlugins'||name==='GetRecentHistory'?'[]':name==='GetRuntimeMetrics'?'{}':'""'}}`).join('\n')+'\nexport {normalizeApiError,ApiFailure,toApiFailure} from "/src/api/errors.ts"'
const source=`export function createPlaybackSource(){return {cancelNetworkTimeout(){},dispose(){},snapshot(){return {}},async load(video){
 const probe=window.startupProbe;let paused=true;probe.video=video
 Object.defineProperties(video,{duration:{configurable:true,get:()=>1200},readyState:{configurable:true,get:()=>4},videoWidth:{configurable:true,get:()=>1920},videoHeight:{configurable:true,get:()=>1080},paused:{configurable:true,get:()=>paused}})
 video.play=async()=>{probe.plays++;paused=false;video.dispatchEvent(new Event('play'));await Promise.resolve();video.dispatchEvent(new Event('playing'))}
 video.pause=()=>{if(!paused){probe.pauses++;paused=true;video.dispatchEvent(new Event('pause'))}}
 for(const name of ['loadedmetadata','canplay','play','playing','pause'])video.addEventListener(name,()=>probe.events.push(name))
 setTimeout(()=>{video.dispatchEvent(new Event('loadedmetadata'));video.dispatchEvent(new Event('canplay'))},0)
 return true
}}}`
const server=await createServer({root:resolve(root,'frontend'),configFile:resolve(root,'frontend/vite.config.ts'),plugins:[{name:'player-startup-mocks',enforce:'pre',load(id){const path=id.replaceAll('\\','/');if(path.endsWith('/src/api/app.ts'))return mock;if(path.endsWith('/src/player/hls/playbackSource.ts'))return source}}],server:{host:'127.0.0.1',port:19274,strictPort:true}})
let browser
try{
 await server.listen();browser=await chromium.launch({channel:'msedge',headless:true})
 const page=await browser.newPage({viewport:{width:1280,height:800}})
 const errors=[];page.on('pageerror',error=>errors.push(error.message))
 await page.goto('http://127.0.0.1:19274/tests/browser/player-startup.html')
 await page.waitForFunction(()=>window.startupProbe?.finishMatch)
 const polish=process.argv.includes('--polish')
 const hoverCheck=process.argv.includes('--hover')
 const panelCheck=process.argv.includes('--panel')||hoverCheck
 const reportDir=resolve(root,hoverCheck?'reports/danmaku-hover-2026-10-19':panelCheck?'reports/danmaku-panel-cache-2026-10-19':polish?'reports/player-controls-polish-2026-10-19':'reports/player-startup-feedback-2026-10-19')
 mkdirSync(reportDir,{recursive:true})
 const before={pauseVisible:await page.locator('.pause-overlay').isVisible(),loadingVisible:await page.locator('.loading').isVisible(),plays:await page.evaluate(()=>window.startupProbe.plays)}
 let statusLayout
 if(polish){
  const tip=await page.locator('.cczj-player-preparation').boundingBox(),progress=await page.locator('.progress-bar-wrapper').boundingBox()
  statusLayout={gap:progress.y-tip.y-tip.height,font:await page.locator('.cczj-player-preparation').evaluate(el=>getComputedStyle(el).fontSize),lines:await page.locator('.cczj-player-preparation').locator('span').count()}
  await page.screenshot({path:resolve(reportDir,'preparing.png')})
 }
 await page.evaluate(()=>window.startupProbe.finishMatch())
 await page.waitForFunction(()=>window.startupProbe.plays===1)
 const toggle=page.locator('[data-player-action="danmaku-toggle"]')
 const hasToggle=await toggle.count()>0
 let controls={hasToggle}
 if(hasToggle){
  const bar=await page.locator('.ctrl-bar').boundingBox(),button=await toggle.boundingBox()
  controls.centerDistance=Math.abs(button.x+button.width/2-(bar.x+bar.width/2))
  if(polish){
   const quality=await page.locator('.quality-group').boundingBox(),settings=await toggle.boundingBox()
   controls.qualityRight=quality.x>button.x+button.width
   controls.qualityGap=quality.x-settings.x-settings.width
   controls.singleEntry=await page.locator('[data-player-action="danmaku-settings"]').count()===0
   await page.locator('.cczj-player-preparation').waitFor({state:'hidden'})
   controls.statusGone=true
  }
  await toggle.click();controls.disabled=await toggle.getAttribute('aria-pressed')==='false'
  controls.settingsHidden=await page.locator('[data-player-action="danmaku-settings"]').count()===0
  await toggle.click();controls.enabled=await toggle.getAttribute('aria-pressed')==='true'
  await page.evaluate(()=>window.startupProbe.finishMatch())
  await page.waitForTimeout(50)
  await page.screenshot({path:resolve(reportDir,'controls.png')})
  await page.mouse.move(1200,750)
  await toggle.hover()
  await page.locator('.cczj-danmaku-panel').waitFor({state:'visible'})
  controls.settingsOpened=await page.locator('.cczj-danmaku-panel').isVisible()
  const reason=page.locator('.cczj-danmaku-match-reason').first()
  if(!await page.locator('.cczj-danmaku-match-details').evaluate(el=>el.open))await page.locator('.cczj-danmaku-match-details summary').click()
  await reason.scrollIntoViewIfNeeded()
  controls.reasonVisible=await reason.isVisible()&&(await reason.innerText()).includes('WORK_YEAR_MISMATCH')
  await page.screenshot({path:resolve(reportDir,'match-reason.png')})
 }
 const result=await page.evaluate(()=>({plays:window.startupProbe.plays,pauses:window.startupProbe.pauses,events:window.startupProbe.events,errors:window.startupProbe.errors,logs:window.startupProbe.logs}))
 console.log(JSON.stringify({before,controls,...result,pageErrors:errors},null,2))
 let polishLayout
 let panels
 if(panelCheck){
  await page.locator('.cczj-danmaku-panel header button').click()
  panels=[]
  for(const width of [1280,920,800,720]){
   await page.setViewportSize({width:Math.max(1280,width),height:800})
   await page.locator('.player-wrapper').evaluate((el,width)=>el.parentElement.style.width=`${width}px`,width)
   await page.mouse.move(1200,750)
   await toggle.hover()
   await page.locator('.cczj-danmaku-panel').waitFor({state:'visible'})
   await page.waitForTimeout(200)
   const host=await page.locator('.player-wrapper').boundingBox(),panel=await page.locator('.cczj-danmaku-panel').boundingBox(),settings=await toggle.boundingBox(),quality=await page.locator('.quality-group').boundingBox()
   const checks={width,leftGap:panel.x-host.x,rightGap:host.x+host.width-panel.x-panel.width,anchorDistance:Math.abs(panel.x+panel.width/2-(settings.x+settings.width/2)),qualityGap:quality.x-settings.x-settings.width,checkboxes:await page.locator('.cczj-danmaku-panel input[type=checkbox]').count(),cacheControls:await page.locator('.cczj-danmaku-panel select').count(),detailsOpen:await page.locator('.cczj-danmaku-match-details').evaluate(el=>el.open)}
   panels.push(checks)
   await page.screenshot({path:resolve(reportDir,`panel-${width}.png`)})
   await page.locator('.cczj-danmaku-panel header button').click()
  }
  console.log(JSON.stringify({panels},null,2))
  if(panels.some(row=>row.leftGap<12||row.rightGap<12||row.anchorDistance>30||row.qualityGap<8||row.checkboxes||row.cacheControls||row.detailsOpen))process.exitCode=1
  await page.mouse.move(1200,750)
  await toggle.hover()
  await page.locator('.cczj-danmaku-panel').waitFor({state:'visible'})
  await toggle.click()
  await page.locator('.cczj-danmaku-panel').waitFor({state:'hidden'})
  if(await page.locator('.cczj-danmaku-panel').count()||await toggle.getAttribute('aria-pressed')!=='false')throw new Error('Panel blocks the control-bar toggle')
  await toggle.click()
  await page.evaluate(()=>window.startupProbe.finishMatch())
 }
 if(polish){
  const widths=[]
  await page.locator('.cczj-danmaku-panel header button').click()
  for(const width of [800,1280]){
   await page.locator('.player-wrapper').evaluate((el,width)=>el.parentElement.style.width=`${width}px`,width)
   const button=await toggle.boundingBox(),quality=await page.locator('.quality-group').boundingBox()
   widths.push({width,qualityGap:quality.x-button.x-button.width})
  }
  console.log(JSON.stringify({statusLayout,widths},null,2))
  polishLayout={statusLayout,widths}
  if(!controls.qualityRight||controls.qualityGap<8||!controls.singleEntry||!controls.statusGone||statusLayout.gap<12||parseFloat(statusLayout.font)<12||widths.some(row=>row.qualityGap<20))process.exitCode=1
 }
 let hoverBehavior
 if(hoverCheck){
  await page.mouse.move(1200,750)
  await toggle.hover()
  await page.locator('.cczj-danmaku-panel').waitFor({state:'visible'})
  const pressed=await toggle.getAttribute('aria-pressed'),button=await toggle.boundingBox(),panel=await page.locator('.cczj-danmaku-panel').boundingBox()
  // Cross the real gap from the control into the panel and adjust its range.
  await page.mouse.move(button.x+button.width/2,button.y-4)
  await page.mouse.move(panel.x+panel.width/2,panel.y+panel.height-10,{steps:4})
  await page.waitForTimeout(350)
  const retained=await page.locator('.cczj-danmaku-panel').isVisible()
  await page.locator('.cczj-danmaku-range input').first().fill('0.7')
  await page.mouse.move(1200,750)
  await page.locator('.cczj-danmaku-panel').waitFor({state:'hidden'})
  await page.mouse.move(100,100)
  await toggle.focus();await page.keyboard.press('ArrowUp')
  await page.locator('.cczj-danmaku-panel').waitFor({state:'visible'})
  await page.keyboard.press('Escape')
  await page.locator('.cczj-danmaku-panel').waitFor({state:'hidden'})
  const keyboardFocus=await toggle.evaluate(el=>document.activeElement===el)
  await toggle.dispatchEvent('pointerdown',{pointerType:'touch',clientX:button.x+5,clientY:button.y+5})
  await page.waitForTimeout(500)
  await page.locator('.cczj-danmaku-panel').waitFor({state:'visible'})
  await toggle.dispatchEvent('pointerup',{pointerType:'touch'})
  await toggle.dispatchEvent('click')
  hoverBehavior={retained,keyboardFocus,longPressDoesNotToggle:await toggle.getAttribute('aria-pressed')===pressed,singleButton:await page.locator('.cczj-danmaku-actions button').count()===1}
  console.log(JSON.stringify({hoverBehavior},null,2))
  if(Object.values(hoverBehavior).some(value=>!value))process.exitCode=1
 }
 writeFileSync(resolve(reportDir,'browser-verification.json'),JSON.stringify({before,controls,...result,...polishLayout,panels,hoverBehavior,pageErrors:errors},null,2))
 if(before.pauseVisible||!before.loadingVisible||before.plays!==0||!controls.hasToggle||!controls.disabled||!controls.settingsHidden||!controls.enabled||!controls.settingsOpened||!controls.reasonVisible||!result.logs.some(row=>row.message.includes('player_media_playing'))||result.plays!==1||result.pauses!==0||result.errors.length||errors.length)process.exitCode=1
}finally{await browser?.close();await server.close()}
