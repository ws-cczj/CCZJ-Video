"""Render the production desktop panel; check quality order and original line IDs.
Playlist parsing and native switching are exercised by the Android companion.
"""
import json, os, subprocess, time
from pathlib import Path
from urllib.request import urlopen
from playwright.sync_api import sync_playwright

ROOT=Path(__file__).resolve().parent.parent
out=ROOT/'dist/audit/playback-routes';out.mkdir(parents=True,exist_ok=True)
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
        sources=[dict(source_key='source_a',name='源名称特别长的播放源用于检查截断而不挤出右侧面板',probe='local',qualityState='ready',lines=[quality(0,0,0),quality(1,1920,1080),quality(2,1280,720)]),dict(source_key='source_b',name='第二个源',probe='remote',qualityState='ready',lines=[quality(0,3840,2160)])]
        lines=[dict(index=i,name='HLS '+str(i),episodes=[dict(ep_num=1,ep_url='fixture')]) for i in range(3)]
        page.evaluate('(data)=>sidebarProbe.setRoutes(...data)',[sources,lines,'source_a']);page.wait_for_selector('.route-item')
        rows=page.locator('.route-item').evaluate_all('(items)=>items.map(e=>({source:e.dataset.source,line:e.dataset.line,text:e.textContent}))')
        assert [(r['source'],r['line']) for r in rows]==[('source_b','0'),('source_a','1'),('source_a','2'),('source_a','0')],rows
        page.locator('.route-item[data-source=source_a][data-line="2"]').click();assert page.evaluate('sidebarProbe.chosen()')==dict(key='source_a',line=2)
        for width in [1280,1024]:
            page.set_viewport_size(dict(width=width,height=800));page.wait_for_timeout(250)
            assert page.locator('.route-item').evaluate_all('(items)=>items.every(e=>e.getBoundingClientRect().right<=e.closest("aside").getBoundingClientRect().right+1)')
            assert page.locator('.route-quality').evaluate_all('(items)=>items.every(e=>e.getBoundingClientRect().right<=e.closest("button").getBoundingClientRect().right)'),'quality hidden behind long source name'
            page.screenshot(path=str(out/f'desktop-{width}.png'))
        page.locator('.route-item[data-source=source_b]').click();assert page.evaluate('sidebarProbe.chosen()')==dict(key='source_b',line=0)
        assert not errors,errors
        (out/'desktop.json').write_text(json.dumps(dict(rows=rows,checks=['quality descending across sources','original line IDs preserved after sorting','long source labels stay inside desktop panel','cross-source selection emits source identity'],errors=errors),ensure_ascii=False,indent=2),encoding='utf8');print('PASS: desktop quality order, line identity, long labels and source selection');browser.close()
finally:server.terminate();server.wait(timeout=10)
