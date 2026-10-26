"""Inspect the five-tab navigation in the isolated native acceptance app."""
import argparse
import base64
import importlib.util
import json
from pathlib import Path
import time

ROOT=Path(__file__).resolve().parent.parent
spec=importlib.util.spec_from_file_location('upgrade',ROOT/'scripts/test-upgrade-android.py')
upgrade=importlib.util.module_from_spec(spec);spec.loader.exec_module(upgrade)

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device',default='emulator-5554')
    args=parser.parse_args()
    out=ROOT/'dist/audit/mobile-navigation';out.mkdir(parents=True,exist_ok=True)
    d=upgrade.Device('adb',args.device);d.connect()
    app='document.querySelector("#app").__vue_app__.config.globalProperties'
    theme=app+'.$pinia._s.get("theme")'
    saved={'route':d.js('location.hash'),'theme':d.js(theme+'.currentId'),'locale':d.js(app+'.$i18n.locale'),'motion':d.js('document.documentElement.getAttribute("data-cczj-motion")'),'feedback':d.js('document.documentElement.getAttribute("data-motion-feedback")')}
    icon='.cczj-mobile-navigation-featured .cczj-mobile-navigation-icon'
    checks=[]
    geometry='''(()=>{const nav=document.querySelector('.cczj-mobile-navigation'),n=nav.getBoundingClientRect();return {background:getComputedStyle(nav).backgroundColor,items:[...nav.querySelectorAll('a')].map(e=>{const r=e.getBoundingClientRect(),i=e.firstElementChild.getBoundingClientRect(),featured=e.classList.contains('cczj-mobile-navigation-featured');return {x:r.x,y:r.y,w:r.width,h:r.height,ix:i.x,iy:i.y,iw:i.width,ih:i.height,label:featured?null:e.lastElementChild.getBoundingClientRect().y,featured,name:e.getAttribute('aria-label'),text:e.textContent.trim(),background:getComputedStyle(e).backgroundColor,color:getComputedStyle(e).color}}),centerX:n.x+n.width/2,centerY:n.y+parseFloat(getComputedStyle(nav).paddingTop)+52/2}})()'''
    def wait(expression):
        end=time.monotonic()+15
        while time.monotonic()<end:
            if d.js(expression):return
            time.sleep(.05)
        raise AssertionError(expression)
    def route(path):
        d.js('location.hash='+json.dumps(path))
        wait('location.hash==='+json.dumps('#'+path)+' && document.querySelectorAll(".cczj-mobile-navigation a").length===5')
        d.js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))')
    def shot(name,navigation=False):
        options=dict(format='png',captureBeyondViewport=False)
        if navigation:
            options['clip']=d.js('(()=>{const r=document.querySelector(".cczj-mobile-navigation").getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,scale:1}})()')
        image=d.call('Page.captureScreenshot',options)
        (out/(name+'.png')).write_bytes(base64.b64decode(image['data']))
    try:
        d.js('document.documentElement.removeAttribute("data-cczj-motion");document.documentElement.removeAttribute("data-motion-feedback")')
        for width,language,palette in [(320,'zh-CN','green'),(390,'en','d-blue'),(360,'zh-CN','china_ink')]:
            d.call('Emulation.setDeviceMetricsOverride',dict(width=width,height=740,deviceScaleFactor=2,mobile=True))
            d.js(theme+'.currentId='+json.dumps(palette));d.js(app+'.$i18n.locale='+json.dumps(language))
            route('/')
            before=d.js(geometry);layout=before['items']
            assert len(layout)==5 and layout[2]['featured'] and sum(row['featured'] for row in layout)==1,layout
            assert max(row['w'] for row in layout)-min(row['w'] for row in layout)<1 and min(row['h'] for row in layout)>=52,layout
            labels=[row['label'] for row in layout if not row['featured']]
            assert max(labels)-min(labels)<1,layout
            center=layout[2]
            assert center['name'] and not center['text'],center
            assert center['iw']==center['ih']==44,center
            assert d.js('getComputedStyle(document.querySelector('+json.dumps(icon)+')).backgroundColor')=='rgba(0, 0, 0, 0)'
            assert d.js('getComputedStyle(document.querySelector('+json.dumps(icon)+')).boxShadow')=='none'
            assert abs(center['ix']+center['iw']/2-before['centerX'])<1,center
            assert abs(center['iy']+center['ih']/2-before['centerY'])<1,center
            shot(f'{width}-{palette}-idle')
            shot(f'{width}-{palette}-bar',True)
            d.js('document.querySelector(".cczj-mobile-navigation-featured").click()')
            wait('location.hash==="#/search" && document.querySelector(".cczj-mobile-navigation-featured").getAttribute("aria-current")==="page"')
            d.js('new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)))')
            assert d.js('document.querySelector(".cczj-mobile-navigation").getAnimations({subtree:true}).length')==0
            shot(f'{width}-{palette}-active')
            wait('document.querySelector('+json.dumps(icon)+').getAnimations({subtree:true}).every(a=>a.playState!=="running")')
            wait('Array.from(document.querySelectorAll(".cczj-mobile-route-scene")).every(e=>e.getAnimations().every(a=>a.playState!=="running"))')
            shot(f'{width}-{palette}-settled')
            after=d.js(geometry)
            assert d.js('getComputedStyle(document.querySelector('+json.dumps(icon)+')).backgroundColor')=='rgba(0, 0, 0, 0)'
            assert before['background']==after['background']
            for initial,current in zip(layout,after['items']):
                for key in ['x','y','w','h','ix','iy','iw','ih','label','background']:
                    assert initial[key]==current[key],(key,initial,current)
            assert layout[0]['color']!=after['items'][0]['color']
            for path in ['/short-drama','/favorites','/settings']:
                route(path)
                assert d.js('document.querySelector(".cczj-mobile-navigation").getAnimations({subtree:true}).length')==0
                current=d.js(geometry)
                assert before['background']==current['background']
                assert [(r['x'],r['y'],r['w'],r['h'],r['ix'],r['iy']) for r in layout]==[(r['x'],r['y'],r['w'],r['h'],r['ix'],r['iy']) for r in current['items']]
            route('/search')
            assert d.js('document.querySelectorAll(".cczj-mobile-navigation a[aria-current=page]").length')==1
            assert not d.js('document.documentElement.scrollWidth>innerWidth')
            checks.append({'width':width,'language':language,'theme':palette,'center_icon_only':True,'stable_geometry_and_background':True,'color_only_selection':True})
        for attribute in ['data-cczj-motion','data-motion-feedback']:
            d.js('document.documentElement.setAttribute('+json.dumps(attribute)+',"off")')
            route('/');route('/search')
            assert d.js('getComputedStyle(document.querySelector('+json.dumps(icon)+')).animationName')=='none'
            assert d.js('getComputedStyle(document.documentElement).opacity')=='1'
            d.js('document.documentElement.removeAttribute('+json.dumps(attribute)+')')
        d.call('Emulation.setEmulatedMedia',dict(features=[dict(name='prefers-reduced-motion',value='reduce')]))
        route('/');route('/search')
        assert d.js('getComputedStyle(document.querySelector('+json.dumps(icon)+')).animationName')=='none'
        checks.append({'reduced_motion_and_disabled_feedback':True})
        (out/'result.json').write_text(json.dumps(checks,ensure_ascii=False,indent=2),encoding='utf-8');print(json.dumps(checks))
    finally:
        d.call('Emulation.clearDeviceMetricsOverride');d.call('Emulation.setEmulatedMedia',dict(features=[]))
        for key,attribute in [('motion','data-cczj-motion'),('feedback','data-motion-feedback')]:
            d.js('document.documentElement.'+('removeAttribute('+json.dumps(attribute)+')' if saved[key] is None else 'setAttribute('+json.dumps(attribute)+','+json.dumps(saved[key])+')'))
        d.js(theme+'.currentId='+json.dumps(saved['theme']));d.js(app+'.$i18n.locale='+json.dumps(saved['locale']))
        d.js('location.hash='+json.dumps(saved['route']));d.close()

if __name__=='__main__':main()
