/** Browser layout and interaction checks using deterministic reading transport.
 * Does not validate native Wails/Android or remote source availability.
 * Set PLAYWRIGHT_MODULE to a module path if Playwright is not installed locally.
 */
import { createRequire } from 'node:module'
import { spawn } from 'node:child_process'
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { dirname, resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(import.meta.url)
const { chromium } = (() => {
  const runtime = process.env.PLAYWRIGHT_MODULE
  const api = runtime ? require(runtime) : require('playwright')
  return { chromium: api.chromium }
})()
const output = join(root, 'reports', 'reading-redesign-2026-10-21')
mkdirSync(output, { recursive: true })
const port = 9318, origin = `http://127.0.0.1:${port}`
const server = spawn(process.execPath, ['node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', String(port), '--strictPort'], { cwd: join(root, 'frontend'), stdio: 'ignore', windowsHide: true })
const names = [...readFileSync(join(root, 'frontend/bindings/cczjVideo/app/service/app.js'), 'utf8').matchAll(/export function (\w+)\(/g)].map(match => match[1])
const mock = `import { CancellablePromise } from '/tests/browser/runtime.ts';
function invoke(method,args){const controller=new AbortController();return new CancellablePromise((resolve,reject)=>{fetch('/__redesign_fixture',{method:'POST',body:JSON.stringify({method,args}),signal:controller.signal}).then(async r=>{const data=await r.json();if(!r.ok)throw Object.assign(new Error(data.message),{code:'STORAGE'});return data}).then(resolve,error=>{if(!controller.signal.aborted)reject(error)})},()=>controller.abort())}
${names.map(name => `export function ${name}(...args){return ${name.startsWith('Library') || name.startsWith('ReadingState') ? `invoke('${name}',args)` : "CancellablePromise.resolve('')"}}`).join('\n')}`

const novelTitles = ['山海之间', '长夜行舟', '春山可望', '纸上来信', '群星的回声', '人间草木', '风起时', '远山有灯']
const comicTitles = ['夏日列车', '星海旅人', '雨中的猫', '山间小食堂', '时光邮局', '森林来信', '月与海', '不眠之城', '风的颜色', '漫长的告白']
function cover(title, index) {
  const palettes = [['#d5e3e5', '#436878', '#e4b98a'], ['#e7d8c8', '#8b6357', '#d2a271'], ['#e1e5d2', '#73876d', '#f3e8b9'], ['#dcdced', '#666b8d', '#d9b5a5']]
  const [bg, ink, sun] = palettes[index % palettes.length]
  return 'data:image/svg+xml;base64,' + Buffer.from(`<svg xmlns="http://www.w3.org/2000/svg" width="360" height="480"><rect width="360" height="480" fill="${bg}"/><circle cx="240" cy="170" r="64" fill="${sun}"/><path d="M0 340 120 190 220 310 360 230V480H0Z" fill="${ink}" opacity=".6"/><path d="M0 410 170 310 360 365V480H0Z" fill="${ink}"/><text x="30" y="55" font-family="sans-serif" font-size="14" fill="${ink}" letter-spacing="4">READING FIXTURE</text><text x="30" y="115" font-family="serif" font-size="32" fill="${ink}">${title}</text></svg>`).toString('base64')
}
const items = [...novelTitles.map((title, i) => ({ kind: 'novel', title, id: i + 1, author: ['林知远', '许清和', '沈遇', '叶川'][i % 4], cover: i % 3 === 0 ? '' : cover(title, i) })), ...comicTitles.map((title, i) => ({ kind: 'comic', title, id: i + 101, author: '青木工作室', cover: cover(title, i) }))].map(row => ({ sourceKey: `fixture-${row.kind}`, sourceName: '阅读测试源', remoteId: `book/${row.id}`, summary: '穿过山川与街巷，寻找日常生活里的微光。一段关于旅途、相遇和成长的故事。\n每一次出发，都有新的风景。', genres: '冒险,日常', status: '连载中', latest: '新的旅途', words: '32万字', updatedAt: '', available: true, ...row }))
const identity = row => [row.kind, row.sourceKey, row.remoteId].join('\0')
const records = new Map(), results = [], errors = [], calls = []
let scenario = 'populated', failResolution = false, rebaseNovel = false, browser, activePage
function seed() {
  records.clear()
  for (const kind of ['novel', 'comic']) {
    const rows = new Map()
    if (scenario !== 'empty') for (const [i, item] of items.filter(row => row.kind === kind).entries()) {
      const key = identity(item)
      const value = kind === 'novel' ? { key, itemId: item.id, addedAt: 100 - i, available: true, item } : { kind, sourceKey: item.sourceKey, remoteId: item.remoteId, itemId: item.id, title: item.title, cover: item.cover, addedAt: 100 - i }
      rows.set(`shelf:${key}`, { bucket: 'shelf', key, value: JSON.stringify(value) })
      if (i < 3) rows.set(`progress:${key}`, { bucket: 'progress', key, value: JSON.stringify(kind === 'novel' ? { itemId: item.id, title: item.title, chapterOrd: 2, chapterId: item.id * 100 + 2, chapterRemoteId: 'chapter/2', ratio: .35, updatedAt: 1000 - i } : { itemId: item.id, title: item.title, cover: item.cover, episodeOrd: 2, episodeId: item.id * 100 + 2, episodeRemoteId: 'chapter/2', page: 1, offset: 0, updatedAt: 1000 - i }) })
    }
    records.set(kind, rows)
  }
}
function rpc(method, args) {
  calls.push(method)
  if (method === 'ReadingStateLoad') return [...records.get(args[0]).values()]
  if (method === 'ReadingStateApply') { for (const row of JSON.parse(args[1])) { const key = `${row.bucket}:${row.key}`; if (row.delete) records.get(args[0]).delete(key); else records.get(args[0]).set(key, row) }; return null }
  if (method === 'LibrarySources') return scenario === 'empty' ? [] : [{ sourceKey: `fixture-${args[0]}`, kind: args[0], name: '阅读测试源', apiUrl: 'https://example.org', enabled: true, itemCount: 8, document: '{}' }]
  if (method === 'LibraryResolveRefs') { if (failResolution) throw Error('测试：书库校验失败'); return args[0].map(ref => { const item=items.find(item => identity(item) === identity(ref)); return item && rebaseNovel && item.kind==='novel' ? {...item,id:item.id+1000} : item ?? null }) }
  if (method === 'LibraryCatalogPage') return { items: items.filter(item => item.kind === args[0].kind), genres: ['冒险', '日常'], nextCursor: '' }
  if (method.startsWith('LibrarySearch')) { const [kind, keyword] = args, found = items.filter(item => item.kind === kind && item.title.includes(keyword)); return { keyword, page: 1, results: [{ sourceKey: `fixture-${kind}`, sourceName: '阅读测试源', items: found, errorCode: '', errorMessage: '', nextUrl: '', hasNext: false }], total: found.length, failedCount: 0, nextCursor: '' } }
  if (['LibraryItem', 'LibraryRefreshReadingToc', 'LibraryRefreshToc'].includes(method)) { const original = items.find(row => row.id === Number(args[0]) - (rebaseNovel && Number(args[0])>1000 ? 1000 : 0)); const item=rebaseNovel && original.kind==='novel' ? {...original,id:original.id+1000} : original; return { item, chapters: Array.from({ length: 40 }, (_, i) => ({ id: item.id * 100 + i, remoteId: `chapter/${i}`, ord: i, title: `${i + 1} · ${['启程', '山间来信', '新的旅途', '重逢'][i % 4]}`, isVolume: false, volume: '', access: 'free', available: true })), chapterCount: 40 } }
  if (['LibraryNovelContext','LibraryRefreshNovelContext'].includes(method)) {
    const location=args[0],view=rpc('LibraryItem',[location.itemId,false]),rows=view.chapters
    const chapter=location.remoteId?rows.find(row=>row.remoteId===location.remoteId):rows.find(row=>row.id===location.chapterId)??(location.first?rows[0]:null)
    const index=chapter?rows.indexOf(chapter):-1
    return {item:view.item,chapter:chapter??null,previous:rows[index-1]??null,next:index>=0?rows[index+1]??null:null,index,count:rows.length,cacheIdentity:chapter?`fixture:${chapter.id}`:''}
  }
  if (method === 'LibraryChapterText') return { chapterId: args[0], paragraphs: Array(30).fill('晨光穿过树梢，落在蜿蜒的小路上。远山在云雾中若隐若现，旅人收起地图，沿着河流继续向前。这里的每一阵风，都像在讲述一段未完的故事。'), warnings: [], fromCache: false }
  if (method === 'LibraryChapterPages') return { chapterId: args[0], pages: [0, 1, 2].map(index => ({ index, url: cover('漫画阅读测试', index) })), warnings: [], fromCache: false }
  return ''
}
const check = (condition, message) => { if (!condition) throw Error(message) }
try {
  for (let i = 0; i < 100; i++) { try { if ((await fetch(origin + '/tests/browser/reading.html')).ok) break } catch {} await new Promise(resolve => setTimeout(resolve, 100)) }
  browser = await chromium.launch({ headless: true, channel: process.env.BROWSER_CHANNEL || 'msedge' })
  for (const mobile of [false, true]) {
    seed()
    const context = await browser.newContext({ viewport: mobile ? { width: 412, height: 860 } : { width: 1440, height: 1000 } })
    const page = await context.newPage(); activePage = page
    page.setDefaultTimeout(7000)
    page.on('pageerror', error => errors.push(String(error)))
    await page.route('**/bindings/cczjVideo/app/service/app*', route => route.fulfill({ body: mock, contentType: 'text/javascript' }))
    await page.route('**/src/api/runtime.ts', route => route.fulfill({ body: `export const isMobileRuntime=()=>${mobile};export const isDesktopRuntime=()=>${!mobile};export const initializeRuntimeEnvironment=async()=>{};export const reloadApplication=url=>location.replace(url);export const closeMobileApplication=()=>{};`, contentType: 'text/javascript' }))
    await page.route('**/__redesign_fixture', route => { const { method, args } = route.request().postDataJSON(); try { return route.fulfill({ json: rpc(method, args) }) } catch (error) { return route.fulfill({ status: 500, json: { message: error.message } }) } })
    const tag = mobile ? 'mobile' : 'desktop'
    async function visit(path) { await page.goto(origin + '/tests/browser/reading.html#' + path); await page.waitForLoadState('networkidle'); const marker = path.startsWith('/novel/read/') ? '.novel-reader' : path.startsWith('/novel/settings') ? '.novel-settings' : path.startsWith('/comic/settings') ? '.comic-settings' : path.endsWith('/sources') ? '.library-sources' : path.startsWith('/comic/series/') ? '.comic-detail' : path === '/novel/search' ? '.reading-search-page' : path === '/comic/search' ? '.comic-catalog' : path === '/novel/shelf' ? '.novel-shelf' : '.comic-library'; await page.locator('.main-content').locator(marker).first().waitFor() }
    async function capture(name) { await page.waitForFunction(() => [...document.images].filter(img => { const r = img.getBoundingClientRect(); return !img.closest('[inert],[aria-hidden="true"]') && r.width > 0 && r.height > 0 && r.bottom > 0 && r.top < innerHeight && r.right > 0 && r.left < innerWidth }).every(img => img.complete && img.naturalWidth > 0)); await page.evaluate(async () => { await Promise.all(document.getAnimations().filter(a => a.effect?.getComputedTiming().endTime !== Infinity).map(a => a.finished.catch(() => {}))) }); await page.screenshot({ path: join(output, `${tag}-${name}.png`) }); const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth || [...document.querySelectorAll('.main-content')].some(el => el.scrollWidth > el.clientWidth + 1)); check(!overflow, `Horizontal overflow: ${tag}/${name}`) }
    async function step(name, action) { await action(); results.push(`${tag}: ${name}`); console.log('PASS', tag, name) }
    await step('novel personal shelf retains filtering and recent history', async () => {
      await visit('/novel/shelf'); await page.locator('.novel-shelf-item').last().waitFor(); check(await page.locator('.novel-shelf-item').count() === 8, 'Missing saved books'); await capture('novel-shelf')
      const input = page.getByRole('searchbox', { name: '搜索书架', exact: true }); await input.fill('山海'); check(await page.locator('.novel-shelf-item').count() === 1, 'Shelf filter'); await input.fill('')
      await page.getByRole('button', { name: '最近阅读', exact: true }).click(); check(await page.locator('.novel-shelf-item').count() === 3, 'History filter'); await page.getByRole('button', { name: /全部收藏/ }).click()
      check(await page.locator(mobile ? '.reading-bottom-nav a' : '.reading-desktop-nav a').count() === 3, 'Expected three primary tabs')
    })
    await step('novel resume exits to its entry', async () => {
      await page.locator('.novel-resume').click(); await page.locator('.novel-reading-paragraph').last().waitFor(); await capture('novel-reader'); await page.getByRole('button', { name: '返回', exact: true }).first().click(); await page.waitForURL('**#/novel/shelf')
    })
    await step('comic root shows followed covers and page progress', async () => {
      await visit('/comic'); await page.locator('.comic-card').last().waitFor(); check(await page.locator('.comic-card').count() === 10, 'Missing followed comics'); await capture('comic-library')
      await page.getByRole('searchbox', { name: '搜索书库', exact: true }).fill('星海'); check(await page.locator('.comic-card').count() === 1, 'Comic filter')
    })
    await step('comic episode search and current-episode navigation', async () => {
      await visit('/comic/series/101'); await page.locator('.comic-episode-grid li').last().waitFor(); await capture('comic-detail'); await page.getByRole('searchbox', { name: '搜索话名或序号', exact: true }).fill('重逢'); check(await page.locator('.comic-episode-grid li').count() === 10, 'Episode filter'); await page.getByRole('button', { name: '定位当前话' }).click(); check(await page.locator('.comic-episode-grid li').count() === 40, 'Current episode must clear filter')
    })
    await step('search forms submit with a visible action', async () => {
      for (const kind of ['novel', 'comic']) { await visit(`/${kind}/search`); await capture(`${kind}-search`); await page.locator('form input').fill(kind === 'novel' ? '山海' : '夏日'); await page.getByRole('button', { name: '搜索', exact: true }).click(); await page.locator(kind === 'novel' ? '.novel-book-card' : '.comic-card').first().waitFor(); await capture(`${kind}-search-result`) }
    })
    await step('settings show focused controls and live typography preview', async () => {
      await visit('/novel/settings?group=reading'); await page.locator('.novel-reader-preview').waitFor(); const before = await page.locator('.novel-reader-preview').evaluate(el => getComputedStyle(el).fontSize); await page.getByRole('button', { name: '放大正文字号' }).click(); const after = await page.locator('.novel-reader-preview').evaluate(el => getComputedStyle(el).fontSize); check(before !== after, 'Preview size must change'); await capture('novel-preferences'); await visit('/comic/settings'); await capture('comic-settings')
    })
    await step('source manager opens installed sources without fetching the catalog', async () => {
      calls.length = 0; await visit('/novel/sources'); await page.locator('.library-source').waitFor(); check(!calls.includes('LibraryPreviewSources'), 'Catalog should load on request'); await capture('sources')
    })
    await step('failed resolution preserves saved snapshots', async () => {
      failResolution = true; await visit('/novel/shelf'); check(await page.locator('.novel-shelf-item').count() === 8, 'Novel snapshots disappeared'); await visit('/comic'); await page.getByRole('searchbox', {name:'搜索书库',exact:true}).fill(''); check(await page.locator('.comic-card').count() === 10, 'Comic snapshots disappeared'); await capture('comic-offline'); failResolution = false
    })
    await step('dark and narrow layouts stay inside the viewport', async () => {
      await visit('/novel/shelf'); await page.evaluate(() => document.documentElement.setAttribute('data-theme-mode', 'dark')); await capture('novel-dark'); await visit('/comic'); await page.evaluate(() => document.documentElement.setAttribute('data-theme-mode', 'dark')); await capture('comic-dark'); if (mobile) { await page.setViewportSize({ width: 320, height: 740 }); await capture('comic-320'); await visit('/novel/shelf'); await capture('novel-320') }
    })
    await step('resolved source identity repairs a changed local book ID', async () => {
      rebaseNovel=true; await page.reload({waitUntil:'networkidle'}); await visit('/novel/shelf'); await page.waitForFunction(() => document.querySelector('.novel-resume')?.getAttribute('href')?.includes('/novel/read/1001/')); await page.locator('.novel-resume').click(); await page.locator('.novel-reading-paragraph').first().waitFor(); await page.waitForURL('**#/novel/read/1001/100102'); rebaseNovel=false
    })
    scenario = 'empty'; seed(); await page.reload({waitUntil:'networkidle'})
    await step('empty personal libraries offer a direct source setup action', async () => {
      for (const kind of ['novel', 'comic']) { await visit(kind === 'novel' ? '/novel/shelf' : '/comic'); await page.getByRole('link', { name: '添加源', exact: true }).waitFor(); await capture(`${kind}-empty`) }
    })
    scenario = 'populated'
    await context.close()
  }
  check(errors.length === 0, errors.join('\n'))
  writeFileSync(join(output, 'result.json'), JSON.stringify({ passed: true, native: false, results, errors }, null, 2))
} catch (error) {
  if(activePage && !activePage.isClosed()){await activePage.screenshot({path:join(output,'failure.png')}); console.log((await activePage.locator('body').innerText()).slice(0,3000), calls.slice(-12))}
  writeFileSync(join(output, 'result.json'), JSON.stringify({ passed: false, native: false, results, errors, failure: String(error) }, null, 2))
  throw error
} finally { await browser?.close(); server.kill() }
