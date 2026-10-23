import {createRequire} from 'node:module'
import {readFileSync,mkdirSync,writeFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {fileURLToPath} from 'node:url'
import {createServer} from '../frontend/node_modules/vite/dist/node/index.js'
const root=resolve(fileURLToPath(new URL('..',import.meta.url)))
const require=createRequire(resolve(process.env.CCZJ_PLAYWRIGHT_NODE_MODULES||resolve(root,'frontend'),'package.json'))
const {chromium}=require('playwright')
const names=[...readFileSync(resolve(root,'frontend/bindings/cczjVideo/app/service/app.js'),'utf8').matchAll(/export function (\w+)\(/g)].map(row=>row[1])
const mock=names.map(name=>`export async function ${name}(){return ''}`).join('\n')+'\nexport {normalizeApiError,ApiFailure,toApiFailure} from "/src/api/errors.ts"'
const server=await createServer({root:resolve(root,'frontend'),configFile:resolve(root,'frontend/vite.config.ts'),plugins:[{name:'sidebar-bindings',enforce:'pre',load(id){if(id.replaceAll('\\','/').endsWith('/src/api/app.ts'))return mock}}],server:{host:'127.0.0.1',port:19275,strictPort:true}})
let browser
try{
 await server.listen();browser=await chromium.launch({channel:'msedge',headless:true})
 const page=await browser.newPage({viewport:{width:1280,height:800}}),errors=[]
 page.on('pageerror',error=>errors.push(error.message))
 await page.clock.install()
 await page.goto('http://127.0.0.1:19275/tests/browser/player-sidebar.html')
 await page.waitForFunction(()=>window.sidebarProbe)
 const button=page.locator('.panel-toggle-btn'),side=page.locator('.player-col-side')
 const opacity=()=>button.evaluate(el=>Number(getComputedStyle(el).opacity))
 const initial=await opacity()
 const near=async()=>{const box=await page.locator('.player-col-main').boundingBox();await page.mouse.move(box.x+box.width-12,box.y+box.height/2)}
 await near();await page.waitForTimeout(260)
 const appeared=await opacity()
 const size=await button.boundingBox()
 await page.clock.fastForward(5100);await page.waitForTimeout(260)
 const expired=await opacity()
 await near();await page.waitForTimeout(260)
 const originalWidth=(await side.boundingBox()).width
 await button.click()
 await page.waitForTimeout(100)
 const intermediateWidth=(await side.boundingBox()).width
 await page.waitForTimeout(400)
 const collapsedWidth=(await side.boundingBox()).width
 const inert=await side.evaluate(el=>el.inert)
 await near();await button.click();await page.waitForTimeout(450)
 const restoredWidth=(await side.boundingBox()).width
 const stableVideo=await page.evaluate(()=>document.querySelector('video')===window.sidebarProbe.video)
 await page.emulateMedia({reducedMotion:'reduce'})
 const reduced=await side.evaluate(el=>getComputedStyle(el).transitionDuration)
 const buttonReduced=await button.evaluate(el=>getComputedStyle(el).transitionDuration)
 await page.setViewportSize({width:900,height:800})
 const narrowHidden=await button.evaluate(el=>getComputedStyle(el).display==='none')
 await page.setViewportSize({width:1280,height:800})
 const report={initial,appeared,expired,size,originalWidth,intermediateWidth,collapsedWidth,inert,restoredWidth,stableVideo,reduced,buttonReduced,narrowHidden,errors}
 const out=resolve(root,'reports/player-sidebar-hover-2026-10-19');mkdirSync(out,{recursive:true})
 await page.screenshot({path:resolve(out,'sidebar.png')})
 writeFileSync(resolve(out,'verification.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2))
 if(initial!==0||appeared<.99||expired!==0||size.width>32||size.height>64||originalWidth!==360||intermediateWidth<=0||intermediateWidth>=360||collapsedWidth!==0||!inert||restoredWidth!==360||!stableVideo||reduced!=='0s'||buttonReduced!=='0s'||!narrowHidden||errors.length)process.exitCode=1
 await page.evaluate(()=>window.sidebarProbe.unmount())
}finally{await browser?.close();await server.close()}
