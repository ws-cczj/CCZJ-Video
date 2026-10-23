"""Native source-result selection, detail metadata/episode actions and progress geometry."""
import argparse, json, time
from pathlib import Path
from android_webview import AndroidWebView

OUT = Path(__file__).resolve().parent.parent / 'dist/audit/mobile-feedback-layout'
OUT.mkdir(parents=True, exist_ok=True)
parser=argparse.ArgumentParser();parser.add_argument('--width',type=int,default=0);args=parser.parse_args()
with AndroidWebView() as p:
    original = p.evaluate('location.hash')
    storage = p.evaluate("Object.fromEntries(['search_history','source_search_mode'].map(k=>[k,localStorage.getItem(k)]))")
    def wait(expr, seconds=30):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            if p.evaluate(expr): return
            time.sleep(.1)
        raise AssertionError(expr)
    def tap(selector):
        point = p.evaluate('(()=>{const e=document.querySelector('+json.dumps(selector)+');e.scrollIntoView({block:"nearest"});const r=e.getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}})()')
        p.call('Input.dispatchTouchEvent', {'type':'touchStart','touchPoints':[point]})
        p.call('Input.dispatchTouchEvent', {'type':'touchEnd','touchPoints':[]})
        time.sleep(.3)
    try:
        if args.width:p.call('Emulation.setDeviceMetricsOverride',{'width':args.width,'height':700,'deviceScaleFactor':3,'mobile':True})
        p.evaluate("location.hash='/search'")
        wait("!!document.querySelector('.cczj-mobile-search-field input')")
        time.sleep(.5)
        if p.evaluate("document.querySelector('.source-search-toggle').getAttribute('aria-pressed')!=='true'"): tap('.source-search-toggle')
        p.evaluate("(()=>{const e=document.querySelector('.cczj-mobile-search-field input');e.value='凡人';e.dispatchEvent(new Event('input',{bubbles:true}))})()")
        tap('.cczj-mobile-search-submit')
        wait("document.querySelectorAll('.source-video-card-wrap').length>=2", 60)
        time.sleep(.7)
        p.evaluate("document.querySelector('.main-content').scrollTop=0")
        data = p.evaluate('''(()=>{const bar=document.querySelector('.cczj-mobile-selection-bar'),r=bar.getBoundingClientRect(),nav=document.querySelector('.cczj-mobile-navigation').getBoundingClientRect();return {bar:{top:r.top,bottom:r.bottom,height:r.height},navTop:nav.top,position:getComputedStyle(bar).position,cardHeights:[...document.querySelectorAll('.source-video-card-wrap .video-card')].map(e=>e.getBoundingClientRect().height),buttons:[...bar.querySelectorAll('button')].map(e=>{const b=e.getBoundingClientRect();return {left:b.left,right:b.right,height:b.height}}),width:innerWidth}})()''')
        assert data['position']=='fixed' and data['bar']['bottom']<=data['navTop'],data
        assert max(data['cardHeights'])-min(data['cardHeights'])<2,data
        assert all(b['left']>=0 and b['right']<=data['width'] and b['height']>=44 for b in data['buttons']),data
        before=p.evaluate("document.querySelector('.source-video-card-wrap').getBoundingClientRect().height")
        if p.evaluate("!!document.querySelector('.source-video-card-wrap .select-checkbox')"):
            tap('.source-video-card-wrap .select-checkbox')
            assert abs(p.evaluate("document.querySelector('.source-video-card-wrap').getBoundingClientRect().height")-before)<1
        p.screenshot(OUT/'search-selection.png')
        p.evaluate("document.querySelector('.main-content').scrollTop=600")
        time.sleep(.5)
        assert abs(p.evaluate("document.querySelector('.cczj-mobile-selection-bar').getBoundingClientRect().bottom")-data['bar']['bottom'])<1,'Floating actions scrolled away'
        flower=p.evaluate("(()=>{const e=document.querySelector('.cczj-mobile-back-to-top');if(!e||!e.getBoundingClientRect().height||getComputedStyle(e).display==='none')return null;const r=e.getBoundingClientRect();return {bottom:r.bottom,width:r.width,height:r.height,vine:!!e.querySelector('.vine-svg')}})()")
        if flower: assert flower['bottom']<=data['bar']['top'] and not flower['vine'] and flower['width']<=52,flower
        p.screenshot(OUT/'search-scrolled.png')
        p.evaluate("location.hash='/history'")
        wait("!!document.querySelector('.history-card')")
        tap('.history-card')
        wait("!!document.querySelector('.detail-header') && !!document.querySelector('.episodes-section')")
        time.sleep(.5)
        detail=p.evaluate('''(()=>{const e=document.querySelector('.episodes-section'),h=e.querySelector('.section-head-left').getBoundingClientRect(),r=e.querySelector('.section-head-right').getBoundingClientRect();return {headTop:h.top,actionsTop:r.top,actionsRight:r.right,width:innerWidth,ratings:[...document.querySelectorAll('.detail-header .meta-tags .ui-tag')].map(e=>e.textContent),tagText:document.querySelector('.meta-tags').textContent,labels:[...e.querySelectorAll('.section-head-right button')].map(e=>e.textContent.trim()),overflow:document.querySelector('.main-content').scrollWidth>innerWidth}})()''')
        assert abs(detail['headTop']-detail['actionsTop'])<2 and detail['actionsRight']<=detail['width'] and not detail['overflow'],detail
        p.screenshot(OUT/'detail.png')
        p.evaluate("document.querySelector('.episodes-section').scrollIntoView({block:'start'})")
        time.sleep(.4)
        p.screenshot(OUT/'episodes.png')
        tap('.episodes-grid button')
        wait("!!document.querySelector('.progress-bar-wrapper')")
        time.sleep(.5)
        p.evaluate("document.querySelector('video').pause()")
        geometry=p.evaluate('''(()=>{const wrap=document.querySelector('.player-wrapper').getBoundingClientRect(),track=document.querySelector('.progress-track-bg').getBoundingClientRect(),slider=document.querySelector('.progress-slider').getBoundingClientRect(),back=getComputedStyle(document.querySelector('.cczj-mobile-player-back'));return {playerWidth:wrap.width,trackWidth:track.width,sliderWidth:slider.width,left:track.left-wrap.left,right:wrap.right-track.right,progressBackground:getComputedStyle(document.querySelector('.progress-bar-wrapper')).backgroundImage,backBackground:back.backgroundColor,backBorder:back.borderTopWidth}})()''')
        assert abs(geometry['trackWidth']-(geometry['playerWidth']-24))<2 and abs(geometry['trackWidth']-geometry['sliderWidth'])<1,geometry
        assert geometry['progressBackground']=='none' and geometry['backBackground']=='rgba(0, 0, 0, 0)' and geometry['backBorder']=='0px',geometry
        p.screenshot(OUT/'portrait-progress.png')
        (OUT/'results.json').write_text(json.dumps({'search':data,'detail':detail,'progress':geometry,'flower':flower},ensure_ascii=False,indent=2),encoding='utf-8')
        print('PASS: equal cards, fixed selection actions, compact flower, detail actions and full-width progress')
    finally:
        if args.width:p.call('Emulation.clearDeviceMetricsOverride')
        p.evaluate("document.querySelector('video')?.pause();if(document.fullscreenElement)document.exitFullscreen()")
        p.evaluate('Object.entries('+json.dumps(storage)+').forEach(([k,v])=>v===null?localStorage.removeItem(k):localStorage.setItem(k,v));location.hash='+json.dumps(original)+';location.reload()')
