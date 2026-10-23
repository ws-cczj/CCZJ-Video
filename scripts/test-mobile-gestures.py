"""Exercise carousel taps and History long-press on the installed Android app.

Uses existing records without submitting delete/clear actions or preferences.
"""
import argparse
import json
import subprocess
import time
from pathlib import Path
from android_webview import AndroidWebView


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--output', default='mobile-gestures')
    args = parser.parse_args()
    output = Path(__file__).resolve().parent.parent/'dist/audit'/args.output
    output.mkdir(parents=True, exist_ok=True)
    results = {}
    with AndroidWebView(args.device) as page:
        original = page.evaluate('location.hash')
        def wait(expression, timeout=15):
            deadline = time.monotonic()+timeout
            while time.monotonic()<deadline:
                if page.evaluate(expression): return
                time.sleep(.1)
            raise AssertionError(expression)
        def route(path):
            page.evaluate('location.hash='+json.dumps('#'+path))
            time.sleep(.8)
            page.evaluate("document.querySelector('.cczj-mobile-content').scrollTop=0")
        def point(selector):
            return page.evaluate('(()=>{const r=document.querySelector('+json.dumps(selector)+').getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
        def touch(kind, position=None):
            page.call('Input.dispatchTouchEvent', {'type':kind, 'touchPoints':[position] if position else []})
        def tap(selector):
            touch('touchStart', point(selector))
            touch('touchEnd')
        try:
            route('/history')
            wait("!!document.querySelector('.cczj-mobile-history-card')")
            assert page.evaluate("document.querySelectorAll('.history-page .page-header-actions button').length===0"), 'Mobile manage button remains'
            card = '.cczj-mobile-history-card'
            # Moving a finger cancels selection, including when held afterward.
            start = point(card)
            touch('touchStart', start)
            time.sleep(.15)
            touch('touchMove', {**start, 'y':start['y']-35})
            time.sleep(.65)
            touch('touchEnd')
            time.sleep(.3)
            assert page.evaluate("!document.querySelector('.card-checkbox') && location.hash==='#/history'"), 'Scroll entered management or opened a record'
            page.evaluate("document.querySelector('.cczj-mobile-content').scrollTop=0")
            touch('touchStart', point(card))
            time.sleep(.8)
            touch('touchEnd')
            time.sleep(.3)
            wait("!!document.querySelector('.card-checkbox input:checked')")
            assert page.evaluate("location.hash==='#/history' && document.querySelectorAll('.card-checkbox input:checked').length===1"), 'Long press must select one record without navigating'
            assert page.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-history-card.is-selected .title')).color===getComputedStyle(document.querySelector('.cczj-mobile-shell')).color"), 'Touch hover must not replace the selected title with low-contrast accent text'
            page.screenshot(output/'history-manage.png')
            subprocess.run(page.adb+['shell','input','keyevent','KEYCODE_BACK'], check=True)
            wait("!document.querySelector('.card-checkbox')")
            assert page.evaluate("location.hash==='#/history'"), 'Back left History instead of exiting management'
            tap(card)
            wait("location.hash.startsWith('#/detail/')")
            results['history'] = {'scrollCancels':True,'longPressSelects':True,'releaseDoesNotOpen':True,'nativeBackExitsManage':True,'tapOpensDetail':True}

            route('/')
            wait("!!document.querySelector('.cczj-mobile-carousel-open:not(:disabled)')")
            assert page.evaluate("!document.querySelector('.carousel .slide-actions')"), 'Detail button is still rendered'
            assert page.evaluate("!document.querySelector('.carousel-indicators') && !document.querySelector('.cczj-mobile-header')"), 'Carousel dots or repeated primary header remain'
            assert page.evaluate("getComputedStyle(document.querySelector('.carousel .slide-image')).objectPosition==='50% 20%'"), 'Poster crop is not shifted upward'
            continue_button = '.cczj-mobile-continue-group .rec-remove-btn'
            if page.evaluate('!!document.querySelector('+json.dumps(continue_button)+')'):
                page.evaluate('document.querySelector('+json.dumps(continue_button)+').click()')
                wait("!!document.querySelector('[role=dialog]')")
                subprocess.run(page.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True)
                wait("!document.querySelector('[role=dialog]')")
                assert page.evaluate("location.hash==='#/' && !!document.querySelector('.cczj-mobile-continue-group .video-card')"), 'Cancel should preserve Continue Watching'
            page.screenshot(output/'home.png')
            # MuMu desktop input generates pointer/mouse events, not TouchEvents.
            mouse = page.evaluate("(()=>{const r=document.querySelector('.carousel').getBoundingClientRect();return {x:r.x+r.width*.8,y:r.y+r.height*.3,w:r.width,label:document.querySelector('.cczj-mobile-carousel-open:not(:disabled)').ariaLabel}})()")
            page.call('Input.dispatchMouseEvent',{'type':'mousePressed','button':'left','buttons':1,'clickCount':1,'x':mouse['x'],'y':mouse['y']})
            for i in range(1,7):
                page.call('Input.dispatchMouseEvent',{'type':'mouseMoved','button':'left','buttons':1,'x':mouse['x']-mouse['w']*.1*i,'y':mouse['y']})
                time.sleep(.04)
            transform=page.evaluate("getComputedStyle(document.querySelector('.carousel-slide')).transform")
            assert transform not in ('none','matrix(1, 0, 0, 1, 0, 0)'), 'Carousel does not track dragging before release'
            page.call('Input.dispatchMouseEvent',{'type':'mouseReleased','button':'left','buttons':0,'clickCount':1,'x':mouse['x']-mouse['w']*.6,'y':mouse['y']})
            time.sleep(.8)
            assert page.evaluate("location.hash==='#/'"), 'Mouse drag navigated to detail'
            assert page.evaluate("document.querySelector('.cczj-mobile-carousel-open:not(:disabled)').ariaLabel") != mouse['label'], 'Mouse drag did not change slide' 
            before_slide = page.evaluate("document.querySelector('.cczj-mobile-carousel-open:not(:disabled)').getAttribute('aria-label')")
            carousel = point('.carousel')
            touch('touchStart', {**carousel,'x':carousel['x']+85})
            for i in range(1,7):
                touch('touchMove', {**carousel,'x':carousel['x']+85-i*27})
                time.sleep(.04)
            touch('touchEnd')
            time.sleep(2)
            assert page.evaluate("location.hash==='#/'"), 'Swipe triggered detail navigation'
            wait("!!document.querySelector('.cczj-mobile-carousel-open:not(:disabled)')")
            assert page.evaluate("document.querySelector('.cczj-mobile-carousel-open:not(:disabled)').getAttribute('aria-label')") != before_slide, 'Swipe did not change the active slide'
            errors = "document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('error').history.length"
            old_errors = page.evaluate(errors)
            tap('.cczj-mobile-carousel-open:not(:disabled)')
            wait("location.hash.startsWith('#/detail/') || "+errors+'>'+str(old_errors), 30)
            results['carousel'] = {'tapOutcome':page.evaluate('location.hash'),'swipeDoesNotOpen':True,'separateButtonRemoved':True,'crop':'50% 20%'}
            # An unmatched chart item retains the existing no-resource feedback.
            route('/history')
            page.screenshot(output/'history.png')
            route('/favorites')
            page.screenshot(output/'favorites.png')
            route('/search')
            page.screenshot(output/'search.png')
            (output/'results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
            print('PASS: real touch carousel tap/swipe, History hold/scroll/tap and native Back; no delete actions submitted')
        finally:
            if page.evaluate("!!document.querySelector('.card-checkbox')"):
                page.evaluate("window.dispatchEvent(new Event('cczj:android-overlay-back',{cancelable:true}))")
            page.evaluate('location.hash='+json.dumps(original))


if __name__ == '__main__':
    main()
