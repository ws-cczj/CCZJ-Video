"""Real ShortDrama view/player and mobile shell; local portrait clip and mocked bindings.

Uses the same isolated Vite + Playwright seam as test-player-browser.py.
Screenshots and generated video stay in dist/audit/short-drama.
"""
import argparse
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.request import urlopen

from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / 'dist/audit/short-drama'


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--channel', default='chrome')
    parser.add_argument('--port', type=int, default=9259)
    parser.add_argument('--preview-image', type=Path, help='Optional local artwork to encode as the test clip for layout previews')
    args = parser.parse_args()
    OUT.mkdir(parents=True, exist_ok=True)
    clip = OUT / 'portrait.webm'
    input_args = ['-loop','1','-i',str(args.preview_image),'-vf','scale=270:480:force_original_aspect_ratio=increase,crop=270:480:iw-ow:0','-t','30','-r','15'] if args.preview_image else ['-f','lavfi','-i','testsrc2=size=270x480:rate=15:duration=30']
    subprocess.run(['ffmpeg','-hide_banner','-loglevel','error','-y',*input_args,'-an','-c:v','libvpx','-b:v','160k',str(clip)],check=True)
    media = 'data:video/webm;base64,' + base64.b64encode(clip.read_bytes()).decode()
    bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    names = re.findall(r'export function (\w+)\(', bindings)
    special = {'GetAllSources', 'GetRecommend', 'GetVideoDetail', 'GetVideoList', 'GetSetting', 'SetSetting', 'SaveWatchHistory', 'GetHistoryPosition', 'IsFavorite', 'AddFavorite', 'RemoveFavorite'}
    mock = '\n'.join(f'export async function {name}() {{return {json.dumps([] if name in ("ListPlugins", "RescanPlugins", "GetSourceStats", "GetAllSources") else False if name.startswith("WindowIs") else "")};}}' for name in names if name not in special)
    mock += '''
      export async function GetAllSources() { return window.__sources ?? [{source_key:'fixture',name:'Fixture',api_url:'https://example.invalid/',enabled:1},{source_key:'alternate',name:'Alternate',api_url:'https://example.invalid/alternate',enabled:1}]; }
      export async function GetSetting(key) { return window.__settings?.[key] || ''; }
      export async function SetSetting(key, value) { (window.__settings ||= {})[key] = value; }
      export async function SaveWatchHistory(req) { (window.__history ||= []).push(req); }
      export async function GetHistoryPosition() { return 0; }
      export async function IsFavorite(req) { return (window.__favorites||[]).some(row=>row.source_key===req.source_key && row.vod_id===req.vod_id); }
      export async function AddFavorite(req) { (window.__favorites||=[]).push(req); }
      export async function RemoveFavorite(req) { window.__favorites=(window.__favorites||[]).filter(row=>row.source_key!==req.source_key || row.vod_id!==req.vod_id); }
      export async function GetVideoList(req) {
        (window.__catalogRequests ||= []).push(req);
        if(!req.short_drama) throw Error('missing catalog short drama boundary');
        if(window.__catalogError) throw {code:'UNAVAILABLE',message:'catalog fixture outage'};
        let rows=Array.from({length:30},(_,i)=>({vod_id:String(i+1),global_id:i+1,vod_name:window.__titles[i%7]+(i>=7?' · 再续前缘':''),type_name:i%2?'都市短剧':'古装短剧',vod_year:'2026',vod_remarks:'全80集',vod_pic:window.__posters[i%window.__posters.length]}));
        if(req.keyword) rows=rows.filter(row=>row.vod_name.includes(req.keyword));
        if(req.recent_days) rows=rows.slice(0,12);
        const offset=Number(req.cursor||0), next=offset+req.page_size;
        return {videos:rows.slice(offset,next),total:rows.length,next_cursor:next<rows.length?String(next):''};
      }
      export async function GetRecommend(req) {
        (window.__recommendations ||= []).push(req);
        if(window.__recommendError) throw {code:'UNAVAILABLE',message:'fixture outage'};
        if(window.__empty) return [];
        if(!req.short_drama || req.limit!==5) throw new Error('missing short drama boundary');
        return Array.from({length:5},(_,i)=>({vod_id:String(i+1),global_id:i+1,vod_name:window.__titles[i],type_name:'短剧'}));
      }
      export async function GetVideoDetail(req) {
        (window.__details ||= []).push(req);
        if(window.__missingDetail) throw {code:'NOT_FOUND',message:'catalog lookup'};
        return {video:{vod_id:req.vod_id,global_id:Number(req.vod_id),vod_name:window.__titles[Number(req.vod_id)-1],vod_content:'一场意外，让她回到了故事的起点。面对熟悉又陌生的选择，她决定亲手改写结局。'.repeat(8)},episodes:Array.from({length:80},(_,i)=>({ep_num:i+1,ep_name:i===79?'第80集 · 所有未说出口的话终于有了答案':'第'+(i+1)+'集',ep_url:window.__media}))};
      }
    '''
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags)
    origin = f'http://127.0.0.1:{args.port}'
    results = []
    try:
        for _ in range(100):
            if server.poll() is not None:
                raise RuntimeError('Vite exited')
            try:
                with urlopen(origin+'/tests/browser/short-drama.html', timeout=1):
                    break
            except OSError:
                time.sleep(.2)
        else:
            raise RuntimeError('Vite startup timed out')
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True, channel=args.channel, args=['--autoplay-policy=no-user-gesture-required'])
            try:
                for mobile, width, height in [(False,1280,850),(True,390,844)]:
                    context = browser.new_context(viewport={'width':width,'height':height}, has_touch=mobile, is_mobile=mobile)
                    page = context.new_page()
                    errors = []
                    page.on('pageerror', lambda error: errors.append(str(error)))
                    page.add_init_script('window.__media='+json.dumps(media)+';'+('window._wails={environment:{OS:"android"}};' if mobile else ''))
                    page.add_init_script('window.__titles='+json.dumps(['雨夜重逢：这一次换我走向你','她从风中来','迟来的告白','重启人生的第一天','他站在时光深处','长夜有微光','当我们再次相遇'])+';')
                    posters=['data:image/'+('png' if path.suffix=='.png' else 'jpeg')+';base64,'+base64.b64encode(path.read_bytes()).decode() for path in [ROOT/'frontend/src/assets/theme_images'/name for name in ['jqbg.jpg','myzcbg.jpg','china_ink.jpg','xnkl.png']]]
                    page.add_init_script('window.__posters='+json.dumps(posters)+';')
                    page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
                    page.goto(origin+'/tests/browser/short-drama.html')
                    page.wait_for_load_state('networkidle')
                    chrome=page.locator('.cczj-mobile-navigation' if mobile else '.titlebar')
                    chrome_background=chrome.evaluate('(el)=>getComputedStyle(el).backgroundColor')
                    ready = "!!document.querySelector('video') && document.querySelector('video').readyState>=2 && !document.querySelector('video').paused"
                    def assert_shell_theme():
                        assert chrome.evaluate('(el)=>getComputedStyle(el).backgroundColor')==chrome_background, 'short dramas changed the navigation/title bar background'
                        expect(page.locator('.app-shell.cczj-short-drama-surface,.cczj-mobile-shell.cczj-short-drama-surface')).to_have_count(0)
                        shell=page.locator('.cczj-mobile-shell' if mobile else '.app-shell')
                        for prop in ['--accent','--text-primary','--bg-primary']:
                            assert shell.evaluate('(el,p)=>getComputedStyle(el).getPropertyValue(p).trim()',prop)==page.evaluate('(p)=>getComputedStyle(document.documentElement).getPropertyValue(p).trim()',prop), 'short dramas changed application chrome: '+prop
                        if mobile:
                            assert shell.evaluate('(el)=>getComputedStyle(el).getPropertyValue("--cczj-mobile-canvas").trim()')==page.evaluate('getComputedStyle(document.documentElement).getPropertyValue("--bg-primary").trim()')
                    assert_shell_theme()
                    def inspect_theatre():
                        theatre=page.locator('.drama-theatre')
                        expect(theatre).to_be_visible()
                        expect(theatre.locator('.video-card')).to_have_count(24)
                        expect(page.locator('video')).to_have_count(0)
                        def assert_catalog_theme():
                            expect(page.locator('.app-shell.cczj-short-drama-surface,.cczj-mobile-shell.cczj-short-drama-surface,.short-drama.cczj-short-drama-surface')).to_have_count(0)
                            assert theatre.evaluate('(el)=>getComputedStyle(el).getPropertyValue("--accent").trim()')==page.evaluate('getComputedStyle(document.documentElement).getPropertyValue("--accent").trim()')
                            assert theatre.locator('.video-card').first.evaluate('(el)=>getComputedStyle(el).getPropertyValue("--bg-card").trim()')==theatre.evaluate('(el)=>getComputedStyle(el).getPropertyValue("--bg-card").trim()')
                        assert_catalog_theme()
                        if not mobile:
                            expect(page.locator('.sidebar')).to_be_visible()
                            assert page.locator('.sidebar').evaluate('(el)=>getComputedStyle(el).getPropertyValue("--accent").trim()')==page.evaluate('getComputedStyle(document.documentElement).getPropertyValue("--accent").trim()')
                        page.get_by_role('button',name='加载更多',exact=True).click()
                        expect(theatre.locator('.video-card')).to_have_count(30)
                        form=theatre.get_by_role('search')
                        form.get_by_role('searchbox').fill('雨夜');form.get_by_role('searchbox').press('Enter')
                        expect(theatre.locator('.video-card')).to_have_count(5)
                        page.evaluate('window.__catalogError=true')
                        form.get_by_role('searchbox').fill('迟来');form.get_by_role('searchbox').press('Enter')
                        expect(page.get_by_text('catalog fixture outage',exact=True)).to_be_visible()
                        page.evaluate('window.__catalogError=false')
                        theatre.get_by_role('button',name='重试',exact=True).click()
                        expect(theatre.locator('.video-card')).to_have_count(4)
                        form.get_by_role('searchbox').fill('');form.get_by_role('searchbox').press('Enter')
                        expect(theatre.locator('.video-card')).to_have_count(24)
                        theatre.get_by_role('button',name='近七天更新',exact=True).click()
                        expect(theatre.locator('.video-card')).to_have_count(12)
                        theatre.get_by_role('button',name='全部短剧',exact=True).click()
                        expect(theatre.locator('.video-card')).to_have_count(24)
                        theatre.evaluate('(el)=>el.scrollTop=0')
                        form.get_by_role('searchbox').evaluate('(el)=>el.blur()')
                        page.mouse.move(0,0)
                        page.wait_for_function('''()=>{
                            const cards=[...document.querySelectorAll('.theatre-grid .video-card')].filter(e=>{const r=e.getBoundingClientRect();return r.top<innerHeight-80 && r.bottom>0});
                            return cards.length>0 && cards.every(e=>Number(getComputedStyle(e).opacity)>.99);
                        }''')
                        page.evaluate('''()=>Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{})))''')
                        page.screenshot(path=str(OUT/f'{"mobile" if mobile else "desktop"}-theatre.png'))
                        if mobile:
                            assert theatre.locator('.theatre-grid').evaluate('(el)=>getComputedStyle(el).gridTemplateColumns.split(" ").length')==2
                            page.set_viewport_size({'width':320,'height':640})
                            page.evaluate('window.dramaTest.locale("en")')
                            assert theatre.evaluate('(el)=>el.scrollWidth<=el.clientWidth'), 'narrow theatre must not overflow'
                            assert theatre.locator('.theatre-search').evaluate('''el=>{
                                const search=el.getBoundingClientRect(), action=el.nextElementSibling.getBoundingClientRect();
                                return search.right<=action.left && action.right<=innerWidth;
                            }'''), 'search and random action must fit at 320px'
                            page.screenshot(path=str(OUT/'mobile-narrow-theatre-english.png'))
                            page.evaluate('window.dramaTest.locale("zh-CN")')
                            page.set_viewport_size({'width':width,'height':height})
                        count=page.evaluate('(window.__recommendations||[]).length')
                        theatre.locator('.video-card').first.focus();page.keyboard.press('Enter')
                        page.wait_for_function(ready)
                        expect(page.locator('.drama-episodes')).to_contain_text('第1集')
                        expect(page.get_by_role('button',name='返回剧场',exact=True)).to_be_visible()
                        if not mobile: expect(page.locator('.sidebar')).to_have_count(0)
                        assert page.evaluate('(window.__recommendations||[]).length')==count
                        if not mobile:
                            page.locator('.portrait-status').get_by_role('button',name='选集',exact=True).click()
                            expect(page.get_by_role('dialog',name='选集',exact=True)).to_be_visible()
                            page.mouse.move(0,0)
                            page.screenshot(path=str(OUT/'desktop-entry-episodes.png'))
                            page.keyboard.press('Escape')
                        page.get_by_role('button',name='返回剧场',exact=True).click()
                        expect(theatre).to_be_visible()
                        expect(theatre.locator('.video-card')).to_have_count(24)
                        expect(page.locator('video')).to_have_count(0)
                        assert_catalog_theme()
                        page.get_by_role('button',name='随机播放',exact=True).click()
                    if not mobile:
                        assert page.evaluate('(window.__recommendations||[]).length')==0
                        inspect_theatre()
                    page.wait_for_function(ready)
                    base_theme=page.evaluate('''()=>({accent:getComputedStyle(document.documentElement).getPropertyValue('--accent'),background:getComputedStyle(document.documentElement).getPropertyValue('--bg-primary'),theme:document.documentElement.dataset.theme})''')
                    assert_shell_theme()
                    if mobile:
                        expect(page.locator('.portrait-status')).to_have_count(0)
                    else:
                        status=page.locator('.portrait-status')
                        expect(status).to_be_visible()
                        expect(status.locator('.time-bar')).to_contain_text('00:30')
                        status.get_by_role('button',name='暂停',exact=True).click()
                        page.wait_for_function('document.querySelector("video").paused')
                        status.get_by_role('button',name='播放',exact=True).click()
                        page.wait_for_function(ready)
                        page.locator('.drama-actions').get_by_role('button',name='收藏',exact=True).click()
                        favorite=page.get_by_role('dialog',name='收藏到文件夹',exact=True)
                        expect(favorite).to_be_visible()
                        favorite.get_by_role('button',name='确认收藏',exact=True).click()
                        expect(page.locator('.drama-actions').get_by_role('button',name='取消收藏',exact=True)).to_have_attribute('aria-pressed','true')
                        assert page.evaluate('window.__favorites.at(-1).source_key')=='fixture'
                        page.locator('.portrait-status [data-player-action="fullscreen"]').click()
                        page.wait_for_function('document.fullscreenElement===document.querySelector(".short-drama")')
                        expect(page.locator('.portrait-status')).to_be_visible()
                        page.screenshot(path=str(OUT/'desktop-fullscreen.png'))
                        page.locator('.portrait-status [data-player-action="fullscreen"]').click()
                        page.wait_for_function('!document.fullscreenElement')
                    page.evaluate('document.querySelector("video").loop=true')
                    assert page.evaluate('document.querySelector("video").videoHeight > document.querySelector("video").videoWidth')
                    assert page.evaluate('document.querySelector("video").playbackRate') == 1
                    assert page.locator('.ctrl-bar:not(.ctrl-bar-minimal),.cczj-mobile-player-controls,.player-drag-handle,.player-title-bar,.volume-group,.speed-group').count() == 0
                    if mobile:
                        assert page.locator('.cczj-mobile-navigation a').evaluate_all('(rows)=>rows.map(e=>e.getAttribute("href"))') == ['/', '/short-drama', '/search', '/favorites', '/settings']
                    screen = page.locator('.drama-screen').bounding_box()
                    assert screen and screen['width']>200 and screen['height']>400, screen
                    assert screen['x']>=0 and screen['y']+screen['height']<=height+1, screen
                    label = 'mobile' if mobile else 'desktop'
                    if mobile:
                        expect(page.locator('.drama-ambience')).to_have_count(0)
                    else:
                        page.wait_for_function('''()=>{
                            const canvas=document.querySelector('.drama-ambience');
                            return canvas && [...canvas.getContext('2d').getImageData(36,32,1,1).data].slice(0,3).some(v=>v>0);
                        }''')
                        expect(page.locator('video')).to_have_count(1)
                    info=page.locator('.drama-info').bounding_box()
                    assert info['height'] <= (112 if mobile else 144), 'metadata must fit its compact mobile rows or desktop three-line synopsis'
                    if not mobile: assert info['width']<=360
                    page.screenshot(path=str(OUT/f'{label}-playback.png'))
                    assert page.locator('.drama-header').evaluate('(e)=>getComputedStyle(e).position')=='absolute'
                    if mobile: expect(page.locator('.drama-switch')).to_have_count(0)
                    if not mobile:
                        expect(page.locator('.sidebar')).to_have_count(0)
                        assert_shell_theme()
                    assert page.locator('.portrait-track').evaluate('(e)=>e.getBoundingClientRect().height')<=4
                    def geometry():
                        return page.evaluate('''()=>{
                          const rect=s=>{const r=document.querySelector(s).getBoundingClientRect();return {left:r.left,right:r.right,top:r.top,bottom:r.bottom,width:r.width,height:r.height}};
                          const video=rect('video'), caption=rect('.drama-info'), track=rect('.portrait-track');
                          return {video,caption,track,card:rect('.drama-frame'),stage:rect('.drama-stage'),fit:getComputedStyle(document.querySelector('video')).objectFit};
                        }''')
                    def assert_geometry():
                        box=geometry(); card=box['card'] if mobile else box['stage']
                        for item in (box['caption'],box['track']):
                            assert item['left']>=card['left']-1 and item['right']<=card['right']+1, box
                            assert item['top']>=card['top']-1 and item['bottom']<=card['bottom']+1, box
                        if not mobile:
                            assert abs(box['card']['width']/box['card']['height']-9/16)<.01, box
                            assert abs(box['caption']['left']-card['left']-24)<1, box
                            assert abs(box['track']['width']-card['width'])<1, box
                        assert box['fit']==('cover' if mobile else 'contain'), box
                    assert_geometry()
                    if not mobile:
                        for w,h in [(1024,720),(900,600),(1280,850)]:
                            page.set_viewport_size({'width':w,'height':h});assert_geometry()
                        page.screenshot(path=str(OUT/'desktop-playback.png'))
                    if mobile:
                        page.set_viewport_size({'width':320,'height':640})
                        assert page.locator('.short-drama').evaluate('(e)=>e.scrollWidth<=e.clientWidth')
                        assert_geometry()
                        page.screenshot(path=str(OUT/'mobile-narrow-playback.png'))
                        page.evaluate('window.dramaTest.locale("en")')
                        assert_geometry()
                        page.screenshot(path=str(OUT/'mobile-narrow-english.png'))
                        page.evaluate('window.dramaTest.locale("zh-CN")')
                        page.set_viewport_size({'width':width,'height':height})
                    # Recommendation playback advances to another title, not its second episode.
                    page.evaluate('document.querySelector("video").loop=false;document.querySelector("video").currentTime=document.querySelector("video").duration-.1')
                    expect(page.locator('.drama-title')).to_have_text('她从风中来')
                    page.wait_for_function(ready)
                    expect(page.locator('.drama-episodes')).to_contain_text('第1集')
                    page.get_by_role('button',name='换一批',exact=True).click()
                    page.wait_for_function(ready)
                    expect(page.locator('.drama-title')).to_have_text('雨夜重逢：这一次换我走向你')
                    # A real held pointer boosts temporarily and cannot pause on release.
                    point = {'x':screen['x']+screen['width']*.5,'y':screen['y']+screen['height']*.3}
                    cdp = context.new_cdp_session(page)
                    def navigate(direction='next', series=False):
                        if mobile:
                            cdp.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]})
                            cdp.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[dict(point,y=point['y']+(-100 if direction=='next' else 100))]})
                            cdp.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
                        else:
                            name=('下一集' if direction=='next' else '上一集') if series else ('下一部短剧' if direction=='next' else '上一部短剧')
                            page.get_by_role('button',name=name,exact=True).click()
                    if mobile:
                        cdp.send('Input.dispatchTouchEvent', {'type':'touchStart','touchPoints':[point]})
                    else:
                        page.mouse.move(**point); page.mouse.down()
                    page.wait_for_function('document.querySelector("video").playbackRate===2')
                    expect(page.locator('.portrait-boost')).to_be_visible()
                    page.screenshot(path=str(OUT/f'{label}-hold.png'))
                    if mobile:
                        cdp.send('Input.dispatchTouchEvent', {'type':'touchEnd','touchPoints':[]})
                    else:
                        page.mouse.up()
                    page.wait_for_function('document.querySelector("video").playbackRate===1 && !document.querySelector("video").paused')
                    assert page.evaluate('(()=>{const raw=JSON.parse(localStorage.getItem("vp_settings"));return (raw.value||raw).speed})()') == '1.5'
                    # Range pointer input previews while dragging, commits on release.
                    page.locator('video').click()
                    page.wait_for_function('document.querySelector("video").paused')
                    expect(page.locator('.portrait-play')).to_be_visible()
                    slider = page.locator('.portrait-progress').bounding_box()
                    previous = page.evaluate('document.querySelector("video").currentTime')
                    page.mouse.move(slider['x']+slider['width']*.7,slider['y']+slider['height']/2);page.mouse.down()
                    page.wait_for_timeout(120)
                    assert abs(page.evaluate('document.querySelector("video").currentTime')-previous)<.5
                    page.mouse.up()
                    page.wait_for_function('document.querySelector("video").currentTime>15')
                    page.locator('.drama-episodes').click()
                    drawer = page.get_by_role('dialog', name='选集', exact=True)
                    expect(drawer).to_be_visible()
                    assert drawer.evaluate('(el)=>getComputedStyle(el).backgroundColor')=='rgb(24, 25, 28)'
                    assert drawer.evaluate('(el)=>getComputedStyle(el).getPropertyValue("--accent").trim()')=='#f4f4f5', 'teleported drawers must retain the local neutral palette'
                    expect(drawer.get_by_text('选集',exact=True)).to_have_count(1)
                    assert_geometry()
                    expect(drawer.locator('.drama-episode')).to_have_count(20)
                    expect(drawer.locator('.drama-range')).to_have_count(4)
                    drawer.get_by_role('button',name='61–80',exact=True).click()
                    drawer.get_by_role('button',name='正序',exact=True).click()
                    expect(drawer.locator('.drama-episode-number').first).to_have_text('80')
                    drawer.get_by_role('button',name='定位当前',exact=True).click()
                    expect(drawer.locator('.drama-range[aria-pressed=true]')).to_have_text('1–20')
                    expect(drawer.locator('.drama-episode[aria-current=true] .drama-episode-number')).to_have_text('1')
                    drawer.get_by_role('button',name='61–80',exact=True).click()
                    last = drawer.get_by_role('button',name='第80集 · 所有未说出口的话终于有了答案',exact=True)
                    last.scroll_into_view_if_needed()
                    assert drawer.locator('.drama-episode-name').evaluate_all('(rows)=>rows.every(e=>parseFloat(getComputedStyle(e).fontSize)>=12)')
                    assert drawer.locator('.drama-episode-name').evaluate_all('(rows)=>rows.every(e=>getComputedStyle(e).whiteSpace==="normal")')
                    page.locator('.cczj-mobile-quick-mask:visible').evaluate('(el)=>Promise.all(el.getAnimations({subtree:true}).map(a=>a.finished.catch(()=>{})))')
                    page.screenshot(path=str(OUT/f'{label}-episodes-long-name.png'))
                    last.click()
                    page.wait_for_function(ready)
                    expect(page.locator('.drama-episodes')).to_contain_text('第80集')
                    expect(drawer).not_to_be_visible()
                    page.locator('.drama-episodes').click()
                    expect(drawer).to_be_visible()
                    expect(drawer.locator('.drama-episode[aria-current=true] .drama-episode-number')).to_have_text('80')
                    page.locator('.cczj-mobile-quick-mask:visible').evaluate('(el)=>Promise.all(el.getAnimations({subtree:true}).map(a=>a.finished.catch(()=>{})))')
                    assert drawer.locator('.drama-episode[aria-current=true]').evaluate('''(el)=>{
                        const card=el.getBoundingClientRect(), body=el.closest('.drama-episode-grid').getBoundingClientRect();
                        return card.top>=body.top-1 && card.bottom<=body.bottom+1;
                    }'''), 'current episode must remain fully visible after locating its range'
                    assert drawer.locator('.drama-ranges').evaluate('''(el)=>{
                        const range=el.getBoundingClientRect(), body=el.closest('.cczj-mobile-quick-body').getBoundingClientRect();
                        return range.top>=body.top-1 && range.bottom<=body.bottom+1;
                    }'''), 'episode ranges must stay visible while scrolling the grid'
                    if mobile:
                        page.set_viewport_size({'width':320,'height':640})
                        drawer.get_by_role('button',name='定位当前',exact=True).click()
                        assert drawer.locator('.drama-episode[aria-current=true]').evaluate('''(el)=>{
                            const card=el.getBoundingClientRect(), grid=el.closest('.drama-episode-grid').getBoundingClientRect();
                            return card.top>=grid.top-1 && card.bottom<=grid.bottom+1;
                        }'''), 'current episode must fit the narrow mobile drawer'
                        assert drawer.locator('.drama-episode[aria-current=true]').evaluate('''(el)=>{
                            const card=el.getBoundingClientRect(), content=el.querySelector('.ui-btn__content').getBoundingClientRect();
                            return content.top>=card.top && content.bottom<=card.bottom;
                        }'''), 'episode text must fit inside its card'
                        page.screenshot(path=str(OUT/f'{label}-narrow-episodes.png'))
                        page.set_viewport_size({'width':390,'height':844})
                    page.screenshot(path=str(OUT/f'{label}-episodes.png'))
                    page.keyboard.press('Escape')
                    # Choosing an episode enters this series. Its last episode cannot escape to another title.
                    if mobile:
                        navigate('next',True)
                        expect(page.locator('.drama-episodes')).to_contain_text('第80集')
                    else:
                        expect(page.get_by_role('button',name='下一集',exact=True)).to_be_disabled()
                    recommendation_count=page.evaluate('window.__recommendations.length')
                    page.evaluate('document.querySelector("video").loop=false;document.querySelector("video").currentTime=document.querySelector("video").duration-.1')
                    expect(page.get_by_text('本剧已播完',exact=True)).to_be_visible()
                    expect(page.locator('.drama-title')).to_have_text('雨夜重逢：这一次换我走向你')
                    assert page.evaluate('window.__recommendations.length')==recommendation_count
                    navigate('previous',True)
                    try:
                        page.wait_for_function(ready)
                    except Exception:
                        page.screenshot(path=str(OUT/f'redesign-{label}-failure.png'))
                        print(page.evaluate('''()=>({body:document.body.innerText,media:[...document.querySelectorAll('video')].map(v=>({paused:v.paused,ready:v.readyState,time:v.currentTime,duration:v.duration,error:v.error?.message}))})'''),flush=True)
                        raise
                    expect(page.locator('.drama-episodes')).to_contain_text('第79集')
                    navigate('next',True)
                    page.wait_for_function(ready)
                    expect(page.locator('.drama-episodes')).to_contain_text('第80集')
                    page.get_by_role('button',name='返回推荐',exact=True).click()
                    # Synopsis is readable, escape closes, then a title switch retires its media.
                    page.locator('.drama-intro').click()
                    expect(page.get_by_role('dialog', name='简介', exact=True)).to_be_visible()
                    page.keyboard.press('Escape')
                    expect(page.get_by_role('dialog', name='简介', exact=True)).not_to_be_visible()
                    page.evaluate('window.__oldVideo=document.querySelector("video")')
                    navigate()
                    page.wait_for_function(ready)
                    expect(page.locator('.drama-title')).to_have_text('她从风中来')
                    assert page.evaluate('window.__oldVideo.paused && !window.__oldVideo.getAttribute("src")')
                    if not mobile:
                        page.mouse.move(screen['x']+screen['width']*.45,screen['y']+screen['height']*.35)
                        for _ in range(4): page.mouse.wheel(0,100)
                        page.wait_for_function(ready)
                        expect(page.locator('.drama-title')).to_have_text('迟来的告白')
                        page.locator('.player-wrapper').focus()
                        page.keyboard.press('PageUp')
                        page.wait_for_function(ready)
                        expect(page.locator('.drama-title')).to_have_text('她从风中来')
                        page.locator('.player-wrapper').focus();page.keyboard.press('Space')
                        page.wait_for_function('document.querySelector("video").paused')
                        page.keyboard.press('Space');page.wait_for_function(ready)
                    # Active playback is released even though the mobile tab is kept alive.
                    page.evaluate('window.__oldVideo=document.querySelector("video"); void window.dramaTest.router.push("/")')
                    page.wait_for_function('window.__oldVideo.paused && !window.__oldVideo.getAttribute("src")')
                    expect(page.locator('.drama-ambience')).to_have_count(0)
                    assert page.evaluate('''()=>({accent:getComputedStyle(document.documentElement).getPropertyValue('--accent'),background:getComputedStyle(document.documentElement).getPropertyValue('--bg-primary'),theme:document.documentElement.dataset.theme})''')==base_theme, 'immersive appearance must not overwrite the saved theme'
                    expect(page.locator('.app-shell.cczj-short-drama-surface')).to_have_count(0)
                    if not mobile:
                        assert page.locator('.sidebar').evaluate('(el)=>getComputedStyle(el).getPropertyValue("--accent")')==base_theme['accent'], 'leaving short dramas must restore sidebar theme'
                    if mobile:
                        # A partially swiped tab is a preview, not permission to
                        # fetch/play media. Cancel below the commit threshold.
                        cdp.send('Input.dispatchTouchEvent', {'type':'touchStart','touchPoints':[{'x':280,'y':300}]})
                        cdp.send('Input.dispatchTouchEvent', {'type':'touchMove','touchPoints':[{'x':210,'y':300}]})
                        page.wait_for_timeout(120)
                        assert page.locator('video').count()==0, 'tab preview started playback'
                        cdp.send('Input.dispatchTouchEvent', {'type':'touchEnd','touchPoints':[]})
                        page.wait_for_timeout(600) # Let the shell's post-swipe click guard expire.
                        assert page.evaluate('window.dramaTest.router.currentRoute.value.path')=='/'
                    page.evaluate('void window.dramaTest.router.push('+json.dumps('/short-drama' if mobile else '/short-drama?view=feed')+')')
                    page.wait_for_function(ready)
                    # A concrete entry uses its explicit source and series queue on both hosts.
                    recommendation_count=page.evaluate('window.__recommendations.length')
                    page.evaluate('void window.dramaTest.router.push({path:"/short-drama",query:{source:"alternate",vod:"7",global_id:"7",episode:"3"}})')
                    expect(page.locator('.drama-title')).to_have_text('当我们再次相遇')
                    page.wait_for_function(ready)
                    expect(page.locator('.drama-episodes')).to_contain_text('第4集')
                    assert page.evaluate('window.__details.at(-1).source_key')=='alternate'
                    assert page.evaluate('window.dramaTest.source.currentSourceKey')=='fixture'
                    if mobile:
                        cdp.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[point]})
                        cdp.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[dict(point,y=point['y']-100)]})
                        cdp.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
                    else:
                        page.locator('.player-wrapper').focus();page.keyboard.press('PageDown')
                    expect(page.locator('.drama-episodes')).to_contain_text('第5集')
                    page.wait_for_function(ready)
                    expect(page.locator('.drama-title')).to_have_text('当我们再次相遇')
                    assert page.evaluate('window.__recommendations.length')==recommendation_count
                    page.evaluate('document.querySelector("video").loop=false;document.querySelector("video").currentTime=document.querySelector("video").duration-.1')
                    expect(page.locator('.drama-episodes')).to_contain_text('第6集')
                    page.wait_for_function(ready)
                    page.screenshot(path=str(OUT/f'{label}-series-playback.png'))
                    if mobile:
                        page.set_viewport_size({'width':320,'height':640})
                        page.evaluate('window.dramaTest.locale("en")')
                        assert_geometry()
                        assert page.locator('.drama-header').evaluate('''el=>{
                            const heading=el.querySelector('.drama-heading').getBoundingClientRect(), action=el.querySelector('.drama-refresh').getBoundingClientRect();
                            return heading.right<=action.left;
                        }'''), 'series heading and return action must not overlap'
                        page.screenshot(path=str(OUT/'mobile-narrow-series-english.png'))
                        page.evaluate('window.dramaTest.locale("zh-CN")')
                        page.set_viewport_size({'width':width,'height':height})
                    page.get_by_role('button',name='返回推荐',exact=True).click()
                    page.wait_for_function(ready)
                    assert page.evaluate('window.dramaTest.router.currentRoute.value.query.source') is None
                    # Clicking the title also enters a series without reloading media.
                    page.get_by_role('button',name='观看全集',exact=True).click()
                    expect(page.get_by_role('button',name='返回推荐',exact=True)).to_be_visible()
                    page.get_by_role('button',name='返回推荐',exact=True).click()
                    page.evaluate('window.__recommendError=true')
                    page.get_by_role('button', name='换一批', exact=True).click()
                    expect(page.get_by_text('fixture outage', exact=True)).to_be_visible()
                    assert_shell_theme()
                    expect(page.locator('.short-drama.cczj-short-drama-surface')).to_have_count(0)
                    page.evaluate('window.__recommendError=false')
                    page.get_by_role('button', name='重试', exact=True).click()
                    page.wait_for_function(ready)
                    page.evaluate('window.__empty=true')
                    page.get_by_role('button', name='换一批', exact=True).click()
                    expect(page.locator('.drama-placeholder').get_by_text('还没有可播放的短剧', exact=True)).to_be_visible()
                    assert_shell_theme()
                    expect(page.locator('.short-drama.cczj-short-drama-surface')).to_have_count(0)
                    assert page.locator('.drama-placeholder .empty-title').evaluate('(el)=>getComputedStyle(el).color')==page.locator('.short-drama').evaluate('(el)=>getComputedStyle(el).color')
                    page.screenshot(path=str(OUT/f'{label}-empty-theme.png'))
                    page.evaluate('window.__empty=false')
                    page.get_by_role('button', name='重试', exact=True).click()
                    page.wait_for_function(ready)
                    if mobile:
                        page.get_by_role('button',name='剧场',exact=True).click()
                        inspect_theatre()
                        page.wait_for_function(ready)
                    # A cached feed can outlive its catalog and source. Reentry
                    # must refresh the catalog or show the true no-source state.
                    page.evaluate('window.__missingDetail=true;window.__empty=true;window.dramaTest.router.push("/settings")')
                    page.wait_for_function('window.dramaTest.router.currentRoute.value.path==="/settings"')
                    page.evaluate('window.dramaTest.router.push('+json.dumps('/short-drama' if mobile else '/short-drama?view=feed')+')')
                    expect(page.locator('.drama-placeholder').get_by_text('还没有可播放的短剧',exact=True)).to_be_visible()
                    expect(page.get_by_text('catalog lookup',exact=True)).to_have_count(0)
                    page.evaluate('window.__missingDetail=false;window.__empty=false')
                    page.get_by_role('button',name='重试',exact=True).click();page.wait_for_function(ready)
                    page.evaluate('window.__recommendError=true')
                    page.get_by_role('button',name='换一批',exact=True).click()
                    expect(page.get_by_text('fixture outage',exact=True)).to_be_visible()
                    page.evaluate('window.__sources=[];window.dramaTest.router.push("/settings")')
                    page.wait_for_function('window.dramaTest.router.currentRoute.value.path==="/settings"')
                    page.evaluate('window.dramaTest.router.push('+json.dumps('/short-drama' if mobile else '/short-drama?view=feed')+')')
                    expect(page.locator('.drama-placeholder').get_by_text('请先添加视频源并采集短剧内容。',exact=True)).to_be_visible()
                    expect(page.get_by_text('fixture outage',exact=True)).to_have_count(0)
                    expect(page.locator('video')).to_have_count(0)
                    assert not errors, errors
                    results.append({'profile':label,'checks':['portrait autoplay overrides preference','five short dramas','controls remain inside video frame','thin timeline','no landscape controls','long press and rate restoration','drag seek commits on release','80 episodes in ranges, reverse order and locate current','single drawer heading and readable wrapped episode titles','recommendation completion advances to another title','episode selection enters series and stops at its final episode','direct series uses explicit source without recommendations','series gesture and automatic playback advance episodes','title entry and return to recommendations','synopsis and keyboard dismissal','title switch disposal','route leave and reentry','refresh failure and retry','empty feed recovery']+(['320px Chinese and English layout','cancelled tab preview does not start media'] if mobile else ['900/1024/1280px video geometry','wheel burst switches once','keyboard navigation and play/pause'])})
                    if not mobile:
                        results[-1]['checks'].extend(['desktop theatre theme survives return with teleported controls','existing desktop bar provides play, time jump, episode picker, picture in picture and fullscreen','fullscreen contains the complete portrait scene','favorite folder saves the playing source identity'])
                    results[-1]['checks'].extend(['stale catalog refresh on reentry','removed source clears old failure and media'])
                    context.close()
            finally:
                browser.close()
    finally:
        server.terminate()
        try: server.wait(timeout=10)
        except subprocess.TimeoutExpired: server.kill(); server.wait()
    (OUT/'browser-result.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(results,ensure_ascii=False))


if __name__ == '__main__':
    main()
