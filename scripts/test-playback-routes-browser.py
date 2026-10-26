"""Render the production desktop panel; check quality order and original line IDs.
Playlist parsing and native switching are exercised by the Android companion.
"""
import argparse, io, json, os, re, subprocess, time
from PIL import Image
from pathlib import Path
from urllib.request import urlopen
from playwright.sync_api import sync_playwright

ROOT=Path(__file__).resolve().parent.parent
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--output',default='dist/audit/playback-routes')
args=parser.parse_args()
out=ROOT/args.output;out.mkdir(parents=True,exist_ok=True)
origin='http://127.0.0.1:9359'
server=subprocess.Popen(['node','node_modules/vite/bin/vite.js','--port','9359','--strictPort'],cwd=ROOT/'frontend',stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,creationflags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0)
try:
    for _ in range(100):
        try:urlopen(origin+'/tests/browser/player-sidebar.html',timeout=1).close();break
        except OSError:time.sleep(.1)
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True,channel='msedge');page=browser.new_page(viewport={'width':1280,'height':800});errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
        page.add_init_script("window._wails={environment:{OS:'windows'},flags:{frameless:true}};window.chrome={webview:{postMessage:()=>{}}};")
        page.goto(origin+'/tests/browser/player-sidebar.html');page.wait_for_function('!!window.sidebarProbe')
        def quality(index,width,height):return dict(index=index,name='HLS '+str(index),ep_num=1,ok=True,width=width,height=height,bandwidth=1000000)
        sources=[dict(source_key='source_a',name='源名称特别长的播放源用于检查截断而不挤出右侧面板',probe='local',qualityState='ready',lines=[quality(0,0,0),quality(1,1920,1080),quality(2,1080,608)]),dict(source_key='source_b',name='第二个源',probe='remote',qualityState='ready',lines=[quality(0,3840,2160)])]
        lines=[dict(index=i,name='HLS '+str(i),episodes=[dict(ep_num=1,ep_url='fixture')]) for i in range(3)]
        page.evaluate('(data)=>sidebarProbe.setRoutes(...data)',[sources,lines,'source_a']);page.wait_for_selector('.route-item')
        rows=page.locator('.route-item').evaluate_all('(items)=>items.map(e=>({source:e.dataset.source,line:e.dataset.line,text:e.textContent}))')
        assert [(r['source'],r['line']) for r in rows]==[('source_b','0'),('source_a','1'),('source_a','2'),('source_a','0')],rows
        assert '720P' in page.locator('.route-item[data-source=source_a][data-line="2"]').inner_text(),'608-pixel encoded height must use a conventional tier'
        assert '1080' in page.locator('.route-item[data-source=source_a][data-line="2"]').get_attribute('title') and '608' in page.locator('.route-item[data-source=source_a][data-line="2"]').get_attribute('title'),'original dimensions must remain available'
        page.locator('.route-item[data-source=source_a][data-line="2"]').click();assert page.evaluate('sidebarProbe.chosen()')==dict(key='source_a',line=2)
        watched=page.locator('.ep-item.watched:not(.active)').first
        def contrast():
            watched.scroll_into_view_if_needed()
            box=watched.bounding_box()
            rgb=[float(v) for v in re.findall(r'[\d.]+',watched.locator('.ep-item-num').evaluate('(e)=>getComputedStyle(e).color'))][:3]
            background=Image.open(io.BytesIO(page.screenshot())).convert('RGB').getpixel((int(box['x']+10),int(box['y']+10)))
            def lum(values):
                values=[v/255 for v in values]
                return sum((v/12.92 if v<=.04045 else ((v+.055)/1.055)**2.4)*weight for v,weight in zip(values,[.2126,.7152,.0722]))
            a,b=lum(rgb),lum(background)
            ratio=(max(a,b)+.05)/(min(a,b)+.05)
            assert ratio>=4.5,dict(foreground=rgb,background=background,ratio=ratio)
        contrast();watched.hover();contrast()
        for width in [1280,1024]:
            page.set_viewport_size(dict(width=width,height=800));page.wait_for_timeout(250)
            assert page.locator('.route-item').evaluate_all('(items)=>items.every(e=>e.getBoundingClientRect().right<=e.closest("aside").getBoundingClientRect().right+1)')
            assert page.locator('.route-quality').evaluate_all('(items)=>items.every(e=>e.getBoundingClientRect().right<=e.closest("button").getBoundingClientRect().right)'),'quality hidden behind long source name'
            page.screenshot(path=str(out/f'desktop-{width}.png'))
        page.locator('.route-item[data-source=source_b]').click();assert page.evaluate('sidebarProbe.chosen()')==dict(key='source_b',line=0)
        many=[dict(source_key=f'source_{i}',name=f'播放源 {i} Long source name that must not hide quality',probe='remote',qualityState='pending') for i in range(12)]
        page.evaluate('(data)=>sidebarProbe.setRoutes(...data)',[many,lines,'source_0'])
        page.wait_for_timeout(100)
        before=page.locator('.ep-grid').bounding_box()['y']
        for source in many:
            source.update(qualityState='ready',lines=[quality(0,1920,800),quality(1,1280,720),quality(2,640,360)])
        for _ in range(3):
            page.evaluate('(data)=>sidebarProbe.setRoutes(...data)',[many,lines,'source_0']);page.wait_for_timeout(50)
            assert page.locator('.route-item').count()==36,'probe refresh appended old rows'
        assert page.locator('.playback-routes').evaluate('(e)=>getComputedStyle(e).overflowY==="auto" && e.scrollHeight>e.clientHeight'),'expanded routes need their own bounded scroll area'
        after=page.locator('.ep-grid').bounding_box()['y']
        assert abs(after-before)<2 and after<650,dict(before=before,after=after)
        page.screenshot(path=str(out/'desktop-many-routes.png'))
        for width in [390,320]:
            page.set_viewport_size(dict(width=width,height=844));page.goto(origin+'/tests/browser/player-sidebar.html?mobile=1');page.wait_for_function('!!window.sidebarProbe')
            page.evaluate('(data)=>sidebarProbe.setRoutes(...data)',[many,lines,'source_0']);page.wait_for_timeout(100)
            assert page.locator('.route-item').evaluate_all('(items)=>items.every(e=>e.getBoundingClientRect().right<=innerWidth && e.getBoundingClientRect().width>=innerWidth-40)'),'mobile routes must use a full row'
            assert page.locator('.route-quality').evaluate_all('(items)=>items.every(e=>e.getBoundingClientRect().right<=e.closest("button").getBoundingClientRect().right && e.getBoundingClientRect().width>20)'),'mobile quality clipped'
            page.screenshot(path=str(out/f'mobile-{width}.png'))
            last=page.locator('.route-item[data-source=source_11][data-line="2"]');last.focus();last.press('Enter')
            assert page.evaluate('sidebarProbe.chosen()')==dict(key='source_11',line=2),'last discovered mobile route is not keyboard reachable'
            pending=[dict(source_key='pending',name='A very long source name while probing',probe='probing',qualityState='pending'),dict(source_key='failed',name='Source with a failed probe',probe='failed',qualityState='failed')]
            page.evaluate('sidebarProbe.setLocale("en")');page.evaluate('(data)=>sidebarProbe.setRoutes(...data)',[pending,[],'other']);page.wait_for_timeout(50)
            assert page.locator('.route-label').evaluate_all('(items)=>items.every(e=>e.getBoundingClientRect().width>60)'),'long English status squeezed out source names'
        assert not errors,errors
        (out/'desktop.json').write_text(json.dumps(dict(rows=rows,checks=['conventional quality tiers and exact declared dimensions','original line IDs preserved after sorting','bounded list during pending and completed probes','repeated results replace rows without growth','episodes remain visible','desktop and narrow mobile labels stay inside rows','cross-source selection emits source identity'],errors=errors),ensure_ascii=False,indent=2),encoding='utf8');print('PASS: quality tiers, stable bounded discovery, desktop/mobile layout and source selection');browser.close()
finally:server.terminate();server.wait(timeout=10)
