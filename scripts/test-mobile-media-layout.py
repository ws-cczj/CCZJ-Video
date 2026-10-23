"""Check real search/detail/player pages, without accepting terms or saving preferences."""
import argparse
import json
import time
import subprocess
from pathlib import Path
from android_webview import AndroidWebView

ROOT=Path(__file__).resolve().parent.parent

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device',default='emulator-5554')
    parser.add_argument('--output',default='mobile-media')
    parser.add_argument('--width',type=int,default=0)
    parser.add_argument('--playback',action='store_true',help='Verify real media decoding, touch controls and native fullscreen')
    parser.add_argument('--search',action='store_true',help='Submit a search for the actual opened title; updates normal search history')
    args=parser.parse_args()
    output=ROOT/'dist/audit'/args.output
    output.mkdir(parents=True,exist_ok=True)
    failures=[]
    records={}
    with AndroidWebView(args.device) as page:
        original=page.evaluate('location.hash')
        original_player_settings=page.evaluate("localStorage.getItem('vp_settings')")
        def wait(expression, seconds=20):
            until=time.monotonic()+seconds
            while time.monotonic()<until:
                if page.evaluate(expression): return
                time.sleep(.2)
            raise AssertionError('UI did not become ready: '+expression)
        def click(selector):
            box=page.evaluate('(()=>{const el=document.querySelector('+json.dumps(selector)+');if(!el)throw Error("Missing control");const r=el.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
            page.call('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[box]})
            page.call('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        def reveal_controls():
            if page.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-player-controls')).display==='none'"):
                click('.native-video')
        def back():
            subprocess.run(page.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True)
            time.sleep(.7)
        def horizontal_bounds(selector):
            return page.evaluate('Array.from(document.querySelectorAll('+json.dumps(selector)+')).flatMap(el=>{const r=el.getBoundingClientRect();return r.width&&r.height&&(r.left < -1||r.right>innerWidth+1)?[{tag:el.tagName,left:r.left,right:r.right,text:el.textContent.trim().slice(0,40)}]:[]})')
        try:
            if args.width: page.call('Emulation.setDeviceMetricsOverride',{'width':args.width,'height':760,'deviceScaleFactor':1,'mobile':True})
            page.evaluate("location.hash='#/search'")
            wait("!!document.querySelector('.search-page')")
            time.sleep(.8)
            records['search']=page.evaluate("({mobile:!!document.querySelector('.cczj-mobile-search-form'),background:getComputedStyle(document.querySelector('.cczj-mobile-shell')).backgroundImage,themeBackground:getComputedStyle(document.documentElement).getPropertyValue('--bg-image')})")
            if not records['search']['mobile']: failures.append('Search still uses desktop search form')
            if 'url(' in records['search']['themeBackground'] and 'url(' not in records['search']['background']: failures.append('Saved theme wallpaper is hidden by mobile shell')
            records['search']['clippedControls']=horizontal_bounds('.cczj-mobile-search-form button,.cczj-mobile-search-form input,.cczj-mobile-search-scope .select-trigger')
            if records['search']['clippedControls']: failures.append('Mobile search controls are clipped')
            page.screenshot(output/'search.png')
            page.evaluate("location.hash='#/history'")
            wait("!!document.querySelector('.history-card')")
            page.evaluate("document.querySelector('.history-card').click()")
            wait("!!document.querySelector('.detail-header') && !!document.querySelector('.episodes-grid button')",35)
            time.sleep(.8)
            records['detail']=page.evaluate("(()=>{const p=document.querySelector('.poster-frame').getBoundingClientRect(),t=document.querySelector('.detail-header .title').getBoundingClientRect();return {mobile:!!document.querySelector('.cczj-mobile-detail'),poster:{top:p.top,bottom:p.bottom,width:p.width},title:{top:t.top,left:t.left},width:innerWidth,overflow:document.querySelector('.main-content').scrollWidth>innerWidth}})()")
            if not records['detail']['mobile']: failures.append('Detail lacks mobile presentation')
            if records['detail']['title']['top'] >= records['detail']['poster']['bottom']: failures.append('Detail title is pushed below the entire poster')
            if records['detail']['overflow']: failures.append('Detail overflows viewport')
            records['detail']['clippedControls']=horizontal_bounds('.poster-actions button,.detail-header .title,.episodes-grid button')
            if records['detail']['clippedControls']: failures.append('Detail controls are clipped')
            video_name=page.evaluate("(()=>{const el=document.querySelector('.detail-header .title').cloneNode(true);el.querySelectorAll('span').forEach(n=>n.remove());return el.textContent.trim()})()")
            page.screenshot(output/'detail.png')
            page.evaluate("document.querySelector('.episodes-grid button').click()")
            wait("!!document.querySelector('.player-wrapper')",35)
            time.sleep(1)
            records['player']=page.evaluate("(()=>{const r=document.querySelector('.player-wrapper').getBoundingClientRect(),s=document.querySelector('.player-col-side');return {mobile:!!document.querySelector('.cczj-mobile-player'),ratio:r.width/r.height,height:r.height,width:r.width,sideVisible:s&&getComputedStyle(s).display!=='none',mobileControls:!!document.querySelector('.cczj-mobile-player-controls')}})()")
            if not records['player']['mobileControls']: failures.append('Production player still uses desktop controls')
            if not (1.7 < records['player']['ratio'] < 1.9): failures.append('Portrait player stretches across the full screen instead of 16:9')
            if not records['player']['sideVisible']: failures.append('Episodes and source selection are hidden on mobile')
            records['player']['clippedControls']=horizontal_bounds('.cczj-mobile-player-controls button,.player-title-bar button,.ep-grid button,.source-item')
            if records['player']['clippedControls']: failures.append('Player controls are clipped')
            page.screenshot(output/'player.png')
            if args.playback:
                print('Waiting for the production player to decode actual source media',flush=True)
                page.evaluate("document.querySelector('.native-video').muted=true")
                wait("(()=>{const v=document.querySelector('.native-video');return v&&v.readyState>=2&&v.videoWidth>0})()",110)
                if page.evaluate("document.querySelector('.native-video').paused"):
                    reveal_controls()
                    click('.cczj-mobile-play')
                wait("(()=>{const v=document.querySelector('.native-video');return v&&v.currentTime>2&&!v.paused})()",30)
                print('PASS: actual media decoded; checking touch controls',flush=True)
                initial=page.evaluate("document.querySelector('.native-video').currentTime")
                time.sleep(2)
                records['decoding']=page.evaluate("(()=>{const v=document.querySelector('.native-video');return {width:v.videoWidth,height:v.videoHeight,current:v.currentTime,paused:v.paused}})()")
                assert records['decoding']['current']>initial,'Production playback time did not advance'
                wait("getComputedStyle(document.querySelector('.cczj-mobile-player-controls')).display==='none'",8)
                click('.native-video')
                wait("getComputedStyle(document.querySelector('.cczj-mobile-player-controls')).display!=='none'")
                assert page.evaluate("!document.querySelector('.native-video').paused"),'Tapping video unexpectedly paused playback'
                records['touchControls']=True
                reveal_controls()
                click('.cczj-mobile-play')
                wait("document.querySelector('.native-video').paused")
                page.screenshot(output/'playing.png')
                player_route=page.evaluate('location.hash')
                assert page.evaluate("document.querySelectorAll('.cczj-mobile-player-controls button').length===3"),'Compact player must have exactly play, PiP and fullscreen'
                click('.cczj-mobile-fullscreen')
                wait('!!document.fullscreenElement && innerWidth > innerHeight')
                time.sleep(.5)
                records['fullscreen']=page.evaluate("(()=>{const r=document.querySelector('.player-wrapper').getBoundingClientRect();return {width:innerWidth,height:innerHeight,playerWidth:r.width,playerHeight:r.height,controls:document.fullscreenElement.querySelectorAll('.ctrl-bar button').length}})()")
                assert abs(records['fullscreen']['playerWidth']-records['fullscreen']['width'])<2
                assert abs(records['fullscreen']['playerHeight']-records['fullscreen']['height'])<2
                assert records['fullscreen']['controls']>=10
                page.screenshot(output/'fullscreen.png')
                click('.playback-settings-group > button')
                wait("document.querySelector('.playback-settings-popup').classList.contains('show')")
                back()
                assert page.evaluate("!!document.fullscreenElement && !document.querySelector('.playback-settings-popup').classList.contains('show')"),'Native Back must close settings first'
                click('.video-info-group > button')
                wait("!!document.querySelector('.vp-modal-info')")
                page.screenshot(output/'video-info.png')
                back()
                assert not page.evaluate("!!document.querySelector('.vp-modal-info')"),'Native Back did not close video information'
                assert page.evaluate('location.hash')==player_route,'Closing video information navigated away'
                back()
                wait('!document.fullscreenElement && innerWidth < innerHeight')
                assert page.evaluate('location.hash')==player_route,'Exiting fullscreen navigated away'
                back()
                wait("!!document.querySelector('.detail-page')")
            if args.search:
                page.evaluate("location.hash='#/search'")
                wait("!!document.querySelector('.cczj-mobile-search-form')")
                if page.evaluate("!!document.querySelector('.cczj-mobile-search-field button[type=button]')"):
                    click('.cczj-mobile-search-field button[type=button]')
                click('.cczj-mobile-search-field input')
                page.call('Input.insertText',{'text':video_name})
                click('.cczj-mobile-search-submit')
                wait("!!document.querySelector('.search-results .video-card,.source-search-results')",40)
                page.evaluate("document.activeElement?.blur()")
                wait("!document.querySelector('.search-results .video-card') || Array.from(document.querySelectorAll('.search-results .video-card')).some(el=>getComputedStyle(el).opacity==='1')")
                time.sleep(.7)
                records['searchResults']={'keyword':video_name,'clippedCards':horizontal_bounds('.search-results .video-card,.source-result-card')}
                assert not records['searchResults']['clippedCards'],'Search result cards are clipped'
                page.screenshot(output/'search-results.png')
            (output/'results.json').write_text(json.dumps({'records':records,'failures':failures},ensure_ascii=False,indent=2),encoding='utf-8')
            assert not failures, '\n'.join(failures)
            print('PASS: real mobile search, theme wallpaper, detail and production player layout')
        finally:
            page.evaluate("if(document.fullscreenElement)document.exitFullscreen();document.querySelector('.native-video')?.pause()")
            if args.width: page.call('Emulation.clearDeviceMetricsOverride')
            # volumechange persists even a temporary media-element mute. Unmount
            # first so late player callbacks cannot overwrite the saved preferences.
            page.evaluate("location.hash='#/search'")
            wait("!document.querySelector('.player-wrapper')",5)
            if original_player_settings is None:
                page.evaluate("localStorage.removeItem('vp_settings')")
            else:
                page.evaluate("localStorage.setItem('vp_settings',"+json.dumps(original_player_settings)+")")
            page.evaluate('location.hash='+json.dumps(original))

if __name__=='__main__': main()
