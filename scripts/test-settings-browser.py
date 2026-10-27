"""Actual settings/frame/splash UI with an isolated Wails API seam."""
import argparse, json, os, re, subprocess, time
from pathlib import Path
from urllib.request import urlopen
from playwright.sync_api import sync_playwright
ROOT = Path(__file__).resolve().parent.parent
def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--channel', default=None)
    parser.add_argument('--port', type=int, default=9270)
    parser.add_argument('--baseline', action='store_true')
    args = parser.parse_args()
    origin = f'http://127.0.0.1:{args.port}'
    server = subprocess.Popen(['node','node_modules/vite/bin/vite.js','--host','127.0.0.1','--port',str(args.port),'--strictPort'], cwd=ROOT/'frontend',stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0)
    try:
        for _ in range(100):
            if server.poll() is not None: raise RuntimeError('Vite test server exited before becoming ready')
            try:
                with urlopen(origin+'/tests/browser/settings.html',timeout=1): break
            except OSError: time.sleep(.2)
        names = re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
        specific = {'GetSetting','SetSetting','ListPlugins','RescanPlugins','WindowGetSize','WindowSetSize','WindowGetResizable','GetCloseBehavior','GetDoubanIntervalMinutes','GetLogKeepDays','GetAppVersion','GetAllowPrivateNetwork','SetAllowPrivateNetwork','SetDoubanIntervalMinutes'}
        mock = '\n'.join(f'export async function {name}(...args) {{ return {"[]" if name.startswith(("GetAll","GetSource","GetRecent","List","GetDownload")) else "false" if name.startswith("WindowIs") else json.dumps("")}; }}' for name in names if name not in specific)
        mock += '''
        export async function GetSetting(key) { if(window.__stallTheme && key==='theme_id') return new Promise(()=>{}); return window.__preferences?.[key] || ''; }
        export async function SetSetting(key, value) { if(window.__failSave) throw new Error('test storage failure'); (window.__preferences ||= {})[key] = value; }
        export async function ListPlugins() { return [{id:'motion-effects',status:'ready',kind:'script',name:{en:'Motion'},dir:'',version:'1.0.0'}]; }
        export async function RescanPlugins() { return await ListPlugins(); }
        export async function WindowGetSize() { return window.__size || {width:1280,height:800}; }
        export async function WindowSetSize(width,height) { if(window.__failWindow) throw new Error('test window failure'); if(window.__holdFirstResize){window.__holdFirstResize=false;await new Promise(resolve=>window.__releaseResize=resolve)} window.__size = {width,height}; }
        export async function WindowGetResizable() { return true; }
        export async function GetCloseBehavior() { return true; }
        export async function GetDoubanIntervalMinutes() { return 30; }
        export async function GetLogKeepDays() { return 30; }
        export async function GetAllowPrivateNetwork() { return window.__privateAccess===true; }
        export async function SetAllowPrivateNetwork(next) { window.__privateAccess=next; }
        export async function SetDoubanIntervalMinutes(next) { window.__intervalMinutes=next; return next; }
        export async function GetAppVersion() { return '2.3.2'; }
        '''
        output = ROOT/'dist/audit/settings-20261014'
        output.mkdir(parents=True,exist_ok=True)
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True,channel=args.channel)
            try:

                errors=[]
                def make_page(init=''):
                    page=browser.new_page(viewport={'width':960,'height':600})
                    page.on('pageerror',lambda error:errors.append(str(error)))
                    page.add_init_script("window.__intervals=new Map(); const originalInterval=window.setInterval.bind(window),originalClear=window.clearInterval.bind(window);window.setInterval=(fn,ms,...args)=>{const id=originalInterval(fn,ms,...args);window.__intervals.set(id,ms);return id};window.clearInterval=id=>{window.__intervals.delete(id);originalClear(id)}; window.__native=[]; window.chrome ||= {}; window.chrome.webview={postMessage:(m)=>window.__native.push(m)}; window._wails={environment:{OS:'windows'},flags:{frameless:true}};"+init)
                    page.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=mock,content_type='text/javascript'))
                    return page
                page=make_page()
                page.goto(origin+'/tests/browser/settings.html')
                page.wait_for_load_state('networkidle')
                page.locator('#basic-appearance').wait_for(state='visible',timeout=30000)
                assert page.locator('.tab').count()==8
                page.screenshot(animations='disabled',path=str(output/('before.png' if args.baseline else 'compact.png')))
                def check_layout():
                    wrapped=page.locator('.tab span').evaluate_all('(items)=>items.filter(el=>el.getBoundingClientRect().height>parseFloat(getComputedStyle(el).fontSize)*2).map(el=>el.textContent)')
                    assert not wrapped, f'navigation wraps: {wrapped}'
                    overflow=page.evaluate("""() => {
                        const main=document.querySelector('.main-content');
                        const cards=[...document.querySelectorAll('.basic-section')].filter(el=>el.getBoundingClientRect().width);
                        return {page:document.documentElement.scrollWidth>innerWidth,main:main.scrollWidth>main.clientWidth+1,
                          controls:cards.flatMap(card=>[...card.querySelectorAll('input, .select-trigger, .ui-seg')].filter(el=>{
                            const r=el.getBoundingClientRect(),c=card.getBoundingClientRect();
                            return r.width>0&&(r.left<c.left||r.right>c.right+1);
                          }).map(el=>el.outerHTML.slice(0,180)))};
                    }""")
                    assert not overflow['page'] and not overflow['main'] and not overflow['controls'],overflow
                # First-open geometry must not move either the host or document scroll.
                menu=page.get_by_role('combobox',name='字体大小',exact=True)
                # Separate Playwright's pre-click reveal from menu positioning;
                # larger text can put the anchor one pixel below the host edge.
                menu.scroll_into_view_if_needed()
                before=page.evaluate("[document.scrollingElement.scrollTop,document.querySelector('.main-content').scrollTop]")
                menu.click()
                page.get_by_role('listbox').wait_for(state='visible')
                page.wait_for_timeout(240)
                geometry=page.evaluate("""()=>{const t=document.querySelector('#basic-appearance .item:nth-of-type(2) .select-trigger').getBoundingClientRect(),p=document.querySelector('.select-panel').getBoundingClientRect();return {trigger:[t.left,t.top,t.right,t.bottom],panel:[p.left,p.top,p.right,p.bottom],scroll:[document.scrollingElement.scrollTop,document.querySelector('.main-content').scrollTop]}}""")
                assert geometry['scroll']==before, f'first open moved page: {geometry}'
                assert min(abs(geometry['panel'][1]-geometry['trigger'][3]-4),abs(geometry['trigger'][1]-geometry['panel'][3]-4))<2, f'first short menu did not anchor below trigger: {geometry}'
                page.keyboard.press('Escape')
                for selector in ['.sidebar','.tabs']:
                    assert page.locator(selector).evaluate("el=>getComputedStyle(el).scrollbarWidth==='none' || getComputedStyle(el,'::-webkit-scrollbar').display==='none'"),f'{selector} exposes an unnecessary scrollbar'
                check_layout()
                assert page.evaluate("!window.__preferences?.font_size_key && !window.__preferences?.font_family"),'merely opening settings writes font preferences'
                if args.baseline: return
                # The original scrollbar occupied this strip and swallowed native mouse events.
                assert page.locator('.main-content').evaluate('(el)=>el.scrollHeight>el.clientHeight')
                assert page.evaluate("document.elementFromPoint(innerWidth-2,300).classList.contains('app-shell')"),'scrollbar covers resize strip'
                page.evaluate('window._wails.setResizable(true)')
                page.mouse.move(958,300); page.mouse.down(); page.mouse.move(957,302); page.mouse.up()
                assert page.evaluate("window.__native.includes('wails:resize:e-resize')"),'Wails did not start resizing'
                page.evaluate("document.body.setAttribute('data-player-fullscreen','1')")
                assert page.locator('.app-body').evaluate('(el)=>Math.abs(el.getBoundingClientRect().right-innerWidth)<1')
                page.evaluate("document.body.removeAttribute('data-player-fullscreen')")
                # Section links scroll within the page and move keyboard focus to the target.
                page.locator('.section-links button').nth(1).click()
                page.wait_for_function("document.activeElement.id==='basic-window' && document.querySelector('.main-content').scrollTop>100")
                width=page.get_by_role('textbox',name='宽',exact=True)
                width.fill('640'); width.press('Tab')
                height=page.get_by_role('textbox',name='高',exact=True)
                height.fill('360'); height.press('Tab')
                page.wait_for_function('window.__size?.width===960 && window.__size?.height===600')
                page.get_by_role('button',name='增加宽',exact=True).click()
                page.wait_for_function('window.__size?.width===970')
                page.get_by_role('button',name='减少宽',exact=True).click()
                page.wait_for_function('window.__size?.width===960')
                # A slow earlier width write must not replace the subsequently entered height.
                page.evaluate('window.__holdFirstResize=true')
                width.fill('1200'); width.press('Tab')
                page.wait_for_function('!!window.__releaseResize')
                height.fill('700'); height.press('Tab')
                page.evaluate('window.__releaseResize()')
                page.wait_for_function("window.__size?.width===1200 && window.__size?.height===700",timeout=2500)
                assert height.input_value()=='700','stale size response overwrote the current input'
                width.fill('960'); width.press('Tab')
                height.fill('600'); height.press('Tab')
                page.wait_for_function('window.__size?.width===960 && window.__size?.height===600')
                page.screenshot(animations='disabled',path=str(output/'window.png'))
                page.evaluate('window.__failWindow=true')
                width.fill('1200'); width.press('Tab')
                page.wait_for_function("window.settingsTest.errors.history.some(e=>e.source==='Settings.windowSize')")
                assert width.input_value()=='960'
                width.fill('bad'); width.press('Tab'); assert width.input_value()=='960'
                page.evaluate('window.__failWindow=false')
                # Keep the existing instances mounted when navigating away and back.
                page.locator('.tab[data-group="basic"]').focus()
                page.keyboard.press('ArrowRight')
                page.wait_for_function("document.activeElement.dataset.group==='theme'")
                page.keyboard.press('Home')
                page.wait_for_function("document.activeElement.dataset.group==='basic'")
                assert width.input_value()=='960'
                page.locator('.main-content').evaluate('(el)=>el.scrollTop=0')
                page.set_viewport_size({'width':1600,'height':900})
                check_layout()
                page.wait_for_timeout(450)
                page.screenshot(animations='disabled',path=str(output/'wide.png'))
                columns=page.locator('.basic-grid').evaluate('(el)=>({columns:getComputedStyle(el).gridTemplateColumns,width:el.getBoundingClientRect().width,root:el.parentElement.getBoundingClientRect().width})')
                assert len(columns['columns'].split(' '))==2,columns
                # Exercise real controls rather than mutating component refs.
                page.get_by_role('combobox',name='字体大小',exact=True).click()
                page.get_by_role('option',name=re.compile('20px')).click()
                page.get_by_role('combobox',name='语言',exact=True).click()
                page.get_by_role('option',name='English',exact=True).click()
                page.wait_for_function("window.__preferences?.language==='en'")
                page.evaluate("window.settingsTest.theme.setTheme('d-blue')")
                for w,h,name in [(1600,900,'wide-dark-en-large'),(960,600,'compact-dark-en-large'),(900,600,'fallback-small')]:
                    page.set_viewport_size({'width':w,'height':h})
                    page.locator('.main-content').evaluate('(el)=>el.scrollTop=0')
                    check_layout()
                    page.wait_for_timeout(400)
                    page.screenshot(animations='disabled',path=str(output/(name+'.png')))
                page.get_by_role('button',name='Scroll settings groups right',exact=True).click()
                page.wait_for_function("document.querySelector('.tabs').scrollLeft>0")
                page.get_by_role('button',name='Scroll settings groups left',exact=True).click()
                page.wait_for_function("document.querySelector('.tabs').scrollLeft===0")
                # Test every core settings group at the compact size and largest font.
                for group in ['theme','extensions','advanced','about','motion']:
                    page.locator(f'.tab[data-group="{group}"]').click()
                    page.wait_for_timeout(300)
                    assert page.locator('.main-content').evaluate('(el)=>el.scrollWidth<=el.clientWidth+1'),group
                    page.screenshot(animations='disabled',path=str(output/(group+'-compact.png')))
                assert page.evaluate('[...window.__intervals.values()].filter(ms=>ms===1000).length')==0
                page.locator('.tab[data-group="basic"]').click()
                page.wait_for_function('[...window.__intervals.values()].filter(ms=>ms===1000).length===0')
                # Data policies use the existing backend preference and show real intervals.
                page.locator('.section-links button').nth(3).click()
                page.locator('#basic-data .policy-options button').last.click()
                page.wait_for_function("window.__preferences?.detail_cache_freshness==='saver'")
                assert page.locator('#basic-player').count()==0,'duplicate playback controls remain in settings'
                # Motion preferences are real persisted controls, not timing documentation.
                page.locator('.tab[data-group="motion"]').click()
                page.locator('.style-option.lively').click()
                page.wait_for_function("JSON.parse(window.__preferences.ui_motion_options).style==='lively'")
                page.locator('.range-control input').first.fill('140')
                page.locator('.range-control input').first.dispatch_event('change')
                page.wait_for_function("JSON.parse(window.__preferences.ui_motion_options).speed===140")
                assert page.evaluate("window.settingsTest.motion.tokenMs('--cczj-motion-normal')")<220
                page.get_by_role('checkbox',name='Dialogs and menus',exact=True).uncheck()
                page.wait_for_function("document.documentElement.dataset.motionOverlays==='off'")
                old_version=int(page.locator('.preview-sample').get_attribute('data-preview-version'))
                page.locator('.small-button.play-preview').click()
                page.locator(f'.preview-sample[data-preview-version="{old_version+1}"]').wait_for(state='visible')
                assert page.locator('.preview-sample').evaluate('(el)=>getComputedStyle(el).animationName')!='none'
                page.wait_for_timeout(300)
                page.set_viewport_size({'width':1600,'height':1000})
                page.locator('.main-content').evaluate('(el)=>el.scrollTop=0')
                page.screenshot(animations='disabled',path=str(output/'motion-updated.png'))
                page.get_by_role('button',name='Restore defaults',exact=True).click()
                page.wait_for_function("JSON.parse(window.__preferences.ui_motion_options).speed===100")
                page.locator('.tab[data-group="basic"]').click()
                # Both accessibility and the application's motion preference disable ornaments.
                page.evaluate("const spinner=document.createElement('span');spinner.id='test-loading-indicator';spinner.className='cczj-motion-spin';document.querySelector('.basic-settings').append(spinner)")
                page.emulate_media(reduced_motion='reduce')
                assert page.locator('.basic-section').first.evaluate('(el)=>parseFloat(getComputedStyle(el).animationDuration)')<=.001
                assert page.locator('#test-loading-indicator').evaluate('(el)=>getComputedStyle(el).animationIterationCount')=='infinite'
                page.emulate_media(reduced_motion='no-preference')
                page.evaluate('window.settingsTest.motion.setEnabled(false)')
                page.wait_for_function("document.documentElement.dataset.cczjMotion==='off'")
                assert page.locator('.basic-section').first.evaluate('(el)=>parseFloat(getComputedStyle(el).animationDuration)')<=.001
                assert page.locator('#test-loading-indicator').evaluate('(el)=>getComputedStyle(el).animationIterationCount')=='infinite'
                page.locator('#test-loading-indicator').evaluate('(el)=>el.remove()')
                # A failed preference save is surfaced in the existing error center.
                page.locator('.section-links button').first.click()
                page.evaluate('window.__failSave=true')
                page.get_by_role('combobox',name=re.compile('font size',re.I)).click()
                page.get_by_role('option',name=re.compile('14px')).click()
                page.wait_for_function("window.settingsTest.errors.history.some(e=>e.source==='Settings.save')")
                # Verify the ink background and calligraphic font from the reported screenshots.
                page.evaluate('window.__failSave=false')
                page.get_by_role('combobox',name='Language',exact=True).click()
                page.locator('.select-panel [role="option"]').first.click()
                page.get_by_role('combobox',name='字体',exact=True).click()
                page.get_by_role('option',name='楷体',exact=True).click()
                page.evaluate("window.settingsTest.theme.setTheme('china_ink'); window.settingsTest.motion.setEnabled(true)")
                page.set_viewport_size({'width':960,'height':636})
                page.locator('.main-content').evaluate('(el)=>el.scrollTop=0')
                check_layout()
                page.screenshot(animations='disabled',path=str(output/'ink-compact.png'))
                page.locator('.section-links button').nth(3).click()
                page.wait_for_function("document.querySelector('#basic-data').getBoundingClientRect().top<180")
                page.screenshot(animations='disabled',path=str(output/'data-ink.png'))
                page.locator('.tab[data-group="motion"]').click()
                page.set_viewport_size({'width':1296,'height':1000})
                page.locator('.main-content').evaluate('(el)=>el.scrollTop=0')
                page.screenshot(animations='disabled',path=str(output/'motion-ink.png'))
                # Mount the real shared log panel in the existing settings seam.
                page.evaluate('''async()=>{
                  const [{pluginSettingsTabs},{default:LogPanel},{markRaw}]=await Promise.all([import('/src/plugins/registry.ts'),import('/src/components/LogPanel.vue'),import('/node_modules/.vite/deps/vue.js')]);
                  pluginSettingsTabs.value.find(item=>item.id==='logs').component=markRaw(LogPanel);
                }''')
                page.locator('.tab[data-group="logs"]').click()
                page.locator('.log-search input').wait_for(state='visible')
                page.evaluate('''()=>{const logs=document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('logs');logs.setPaused(true);logs.records=Array.from({length:500},(_,i)=>({key:'desktop-'+i,seq:i+1,ts:Date.now()+i,level:'INFO',origin:'app',caller:'long/path/'.repeat(40),message:'Desktop log layout '+i+' '+('diagnostic details '.repeat(80))}));}''')
                for width in [960,1280]:
                    page.set_viewport_size({'width':width,'height':800})
                    page.locator('.main-content').evaluate('(el)=>el.scrollTop=0')
                    page.wait_for_timeout(150)
                    assert page.locator('.log-row').count()<60
                    assert page.locator('.log-list').evaluate('el=>el.scrollWidth<=el.clientWidth+1')
                    page.locator('.log-row').first.focus()
                    page.keyboard.press('Enter')
                    page.locator('.log-record-message').wait_for(state='visible')
                    assert page.locator('.log-record-message').evaluate('el=>el.scrollWidth<=el.clientWidth+1')
                    page.screenshot(animations='disabled',path=str(output/f'logs-{width}.png'))
                    page.locator('.log-detail button[aria-label]').click()
                    page.locator('.log-detail').wait_for(state='detached')
                page.close()
                # Image decoding is asynchronous; switching themes cannot restart the splash image.
                splash=make_page("HTMLImageElement.prototype.decode=function(){return new Promise(resolve=>window.__finishDecode=resolve)};")
                splash.goto(origin+'/tests/browser/settings.html?splash')
                splash.locator('.splash-img').wait_for(state='attached',timeout=30000)
                splash.wait_for_function('!!window.__finishDecode')
                source=splash.locator('.splash-img').get_attribute('src')
                splash.evaluate("window.settingsTest.theme.currentId='red'")
                assert splash.locator('.splash-img').get_attribute('src')==source
                assert splash.locator('.splash-img.ready').count()==0
                splash.evaluate('window.__finishDecode()')
                splash.locator('.splash-img.ready').wait_for(state='visible')
                splash.wait_for_timeout(400)
                splash.screenshot(animations='disabled',path=str(output/'splash.png'))
                splash.locator('.splash-wrap').wait_for(state='detached',timeout=3000)
                splash.close()
                # Unmount during decode must not leave timers/watchers or resurrect the splash.
                splash=make_page("HTMLImageElement.prototype.decode=function(){return new Promise(resolve=>window.__finishDecode=resolve)};")
                splash.goto(origin+'/tests/browser/settings.html?splash')
                splash.wait_for_function('!!window.__finishDecode')
                splash.evaluate('window.settingsTest.state.splash=false')
                splash.locator('.splash-wrap').wait_for(state='detached')
                splash.evaluate('window.__finishDecode()')
                splash.wait_for_timeout(250)
                assert splash.locator('.splash-wrap').count()==0
                splash.close()
                # Stalled theme IPC and decoding still cannot trap the user behind the splash.
                splash=make_page("window.__stallTheme=true; HTMLImageElement.prototype.decode=()=>new Promise(()=>{});")
                splash.goto(origin+'/tests/browser/settings.html?splash')
                splash.locator('.splash-wrap').wait_for(state='visible',timeout=30000)
                splash.locator('.splash-wrap').wait_for(state='detached',timeout=3000)
                splash.close()
                assert not errors,errors
                print('Settings responsive layout, controls, native resize signalling and splash lifecycle passed')
            finally: browser.close()
    finally:
        server.terminate(); server.wait(timeout=10)
if __name__=='__main__': main()
