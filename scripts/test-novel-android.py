"""Real Fanqie reading on the separately installed Android acceptance package.

Keeps preview user data untouched; restores acceptance mode, route and viewport.
Run with an explicitly identified emulator or physical device; native means the
installed host, not a claim that the device is a physical phone.
Requires a matching build in frontend/dist, adb and native-profile-requirements.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
import time

from android_webview import AndroidWebView

ROOT=Path(__file__).resolve().parent.parent
SPEC=importlib.util.spec_from_file_location('compiled',ROOT/'scripts/collect-android-diagnostics.py')
COMPILED=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(COMPILED)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device',default='emulator-5554')
    parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/novel-native')
    args=parser.parse_args()
    output=args.output
    output.mkdir(parents=True,exist_ok=True)
    result=dict(passed=False,package='com.cczj.video.acceptance',native=True,checks=[])
    with AndroidWebView(args.device,application_id='com.cczj.video.acceptance') as view:
        def rpc(name,*args):
            path,alias=COMPILED.compiled_binding(name)
            return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return await m['+json.dumps(alias)+'](...'+json.dumps(args)+')})()')
        def wait(expression,timeout=40):
            deadline=time.monotonic()+timeout
            while time.monotonic()<deadline:
                value=view.evaluate(expression)
                if value:return value
                time.sleep(.15)
            raise AssertionError(dict(wait=expression,text=view.evaluate('document.body.innerText.slice(-1800)')))
        def click(selector):
            wait('!!document.querySelector('+json.dumps(selector)+')')
            view.evaluate('document.querySelector('+json.dumps(selector)+').click()')
        def named(name):
            return view.evaluate('(()=>{const b=[...document.querySelectorAll("button")].find(b=>b.textContent.trim()==='+json.dumps(name)+');if(!b)return false;b.click();return true})()')
        def show_controls():
            view.evaluate('(()=>{const h=document.querySelector(".novel-reader-top"),v=document.querySelector(".novel-page-viewport");if(getComputedStyle(h).visibility!=="hidden")return;const r=v.getBoundingClientRect();v.dispatchEvent(new MouseEvent("click",{bubbles:true,clientX:r.left+r.width/2,clientY:r.top+100}))})()')
        def swipe(reverse=False):
            geometry=view.evaluate('(()=>{const r=document.querySelector(".novel-page-viewport").getBoundingClientRect();return {x:r.left+r.width/2,top:r.top+r.height*.3,bottom:r.top+r.height*.8}})()')
            start,end=(geometry['top'],geometry['bottom']) if reverse else (geometry['bottom'],geometry['top'])
            view.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[dict(x=geometry['x'],y=start)]))
            for step in range(1,11):
                view.call('Input.dispatchTouchEvent',dict(type='touchMove',touchPoints=[dict(x=geometry['x'],y=start+(end-start)*step/10)]))
                time.sleep(.02)
            view.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
        original_url=view.evaluate('location.href')
        original_mode=rpc('GetSetting','application_mode')
        original_preferences=None
        try:
            rpc('SetSetting','application_mode','novel')
            view.evaluate('location.href="https://wails.localhost/?application-mode=novel#/novel/sources"')
            wait('!!document.querySelector(".novel-source-directory")')
            installed=rpc('LibrarySources','novel')
            assert any(row['enabled'] and '番茄' in row['name'] for row in installed),installed
            result['checks'].append('real built-in Fanqie source is enabled')
            view.evaluate('location.hash="/novel/search"')
            wait('!!document.querySelector(".novel-search input")')
            view.evaluate('(()=>{const input=document.querySelector(".novel-search input");input.value="部族荣光";input.dispatchEvent(new Event("input",{bubbles:true}));document.querySelector(".novel-search").requestSubmit()})()')
            wait('[...document.querySelectorAll(".novel-book-card")].some(r=>r.textContent.includes("部族荣光"))',55)
            view.evaluate('[...document.querySelectorAll(".novel-book-card")].find(r=>r.textContent.includes("部族荣光")).click()')
            wait('!!document.querySelector(".novel-detail-actions a")')
            # Select the first real directory entry. The continue-reading action
            # intentionally restores prior runs, so it cannot anchor chapter-one text.
            view.evaluate('[...document.querySelectorAll("[role=tab]")].find(e=>e.textContent.trim().startsWith("目录")).click()')
            click('.novel-chapter-list a')
            wait('document.querySelector(".novel-reading-paper")?.textContent.includes("大乾皇朝")')
            assert not view.evaluate('/[\uE000-\uF8FF]/.test(document.querySelector(".novel-reading-paper").textContent)')
            result['checks'].append('actual search, directory, decoded novel text')
            result['webview']=view.evaluate('navigator.userAgent')
            original_preferences=next((row for row in rpc('ReadingStateLoad','novel','[]') if row['bucket']=='preferences'),None)
            show_controls()
            click('.novel-reader-bottom nav button:last-child')
            assert named('左右翻页')
            click('.reader-sheet-close')
            wait('!!document.querySelector(".novel-reader-paged")')
            view.call('Emulation.setDeviceMetricsOverride',dict(width=412,height=860,deviceScaleFactor=1,mobile=True))
            time.sleep(.5)
            geometry='(()=>{const v=document.querySelector(".novel-page-viewport"),b=document.querySelector(".novel-reading-paper");return {actual:v.clientWidth,column:parseFloat(b.style.columnWidth),status:document.querySelector(".novel-reading-status").textContent}})()'
            result['portrait']=view.evaluate(geometry)
            view.evaluate('(()=>{const r=document.querySelector(".novel-reader-seek input");r.value="0";r.dispatchEvent(new Event("input",{bubbles:true}));const v=document.querySelector(".novel-page-viewport"),b=v.getBoundingClientRect();v.dispatchEvent(new MouseEvent("click",{bubbles:true,clientX:b.left+b.width/2,clientY:b.top+100}))})()')
            time.sleep(.3)
            before=view.evaluate('document.querySelector(".novel-pagination-track").style.transform')
            swipe()
            wait('document.querySelector(".novel-pagination-track")?.style.transform!=='+json.dumps(before))
            result['checks'].append('vertical touch also turns pages in horizontal page mode')
            view.call('Emulation.setDeviceMetricsOverride',dict(width=860,height=412,deviceScaleFactor=1,mobile=True))
            time.sleep(.5)
            result['landscape']=view.evaluate(geometry)
            assert abs(result['landscape']['actual']-result['landscape']['column'])<=1,result
            result['checks'].append('portrait/landscape repagination')
            view.call('Emulation.setDeviceMetricsOverride',dict(width=412,height=860,deviceScaleFactor=1,mobile=True))
            time.sleep(.3)
            show_controls()
            click('.novel-reader-bottom nav button:last-child')
            wait('!!document.querySelector(".reader-sheet")')
            assert named('上下滚动')
            click('.reader-sheet-close')
            wait('!!document.querySelector(".novel-reader:not(.novel-reader-paged)")')
            view.evaluate('(()=>{const v=document.querySelector(".novel-page-viewport");v.scrollTop=(v.scrollHeight-v.clientHeight)*.55})()')
            time.sleep(.8)
            ratio=view.evaluate('(()=>{const v=document.querySelector(".novel-page-viewport");return v.scrollTop/(v.scrollHeight-v.clientHeight)})()')
            assert abs(ratio-.55)<.03,ratio
            route=view.evaluate('location.hash')
            view.evaluate('location.reload()')
            wait('!!document.querySelector(".novel-reading-paragraph")')
            time.sleep(.3)
            assert view.evaluate('location.hash')==route
            restored=view.evaluate('(()=>{const v=document.querySelector(".novel-page-viewport");return v.scrollTop/(v.scrollHeight-v.clientHeight)})()')
            assert abs(restored-ratio)<.03,dict(ratio=ratio,restored=restored)
            result['checks'].append('native SQLite reading-position persistence after reload')
            view.call('Emulation.clearDeviceMetricsOverride')
            time.sleep(.3)
            result['actual_viewport']=view.evaluate('(()=>{const v=document.querySelector(".novel-page-viewport");return {width:innerWidth,height:innerHeight,viewportWidth:v.clientWidth,contentWidth:v.scrollWidth}})()')
            assert result['actual_viewport']['contentWidth']<=result['actual_viewport']['viewportWidth']+1,result['actual_viewport']
            view.screenshot(output/'reader.png')
            old_title=view.evaluate('document.querySelector(".novel-reading-paper h1").textContent')
            view.evaluate('(()=>{const v=document.querySelector(".novel-page-viewport");v.scrollTop=v.scrollHeight})()')
            time.sleep(.3)
            swipe()
            wait('document.querySelector(".novel-reading-paper h1")?.textContent!=='+json.dumps(old_title)+'&&!!document.querySelector(".novel-reading-paragraph")')
            following=view.evaluate('location.hash')
            view.evaluate('document.querySelector(".novel-page-viewport").scrollTop=0');time.sleep(.3)
            swipe(reverse=True)
            wait('document.querySelector(".novel-reading-paper h1")?.textContent==='+json.dumps(old_title))
            time.sleep(.3)
            assert view.evaluate('(()=>{const v=document.querySelector(".novel-page-viewport");return v.scrollTop/(v.scrollHeight-v.clientHeight)})()')>.95
            result['checks'].append('trusted vertical touch crosses chapters and returns to previous chapter end')
            current_id=int(view.evaluate('location.hash').split('?')[0].split('/')[-1])
            cached=rpc('LibraryChapterText',current_id)
            assert cached['fromCache'] and len(cached['paragraphs'])>10
            context=rpc('LibraryNovelContext',dict(itemId=int(following.split('/')[-2]),chapterId=current_id,remoteId='',ord=0,first=False))
            assert 'chapters' not in context and context['chapter']['id']==current_id and context['next']
            result['compactContextBytes']=len(json.dumps(context,ensure_ascii=False).encode())
            result['checks'].append('native compact context and persistent chapter cache')
            click('.novel-scroll-chapters button:last-child')
            wait('document.querySelector(".novel-reading-paper h1")?.textContent!=='+json.dumps(old_title)+'&&!!document.querySelector(".novel-reading-paragraph")')
            next_route=view.evaluate('location.hash')
            time.sleep(.8)
            expected_id=int(next_route.split('/')[-1])
            saved=[json.loads(row['value']) for row in rpc('ReadingStateLoad','novel','[]') if row['bucket']=='progress']
            assert any(row['chapterId']==expected_id for row in saved),saved
            result['checks'].append('next chapter and saved progress remain consistent')
            show_controls()
            click('.novel-reader-bottom nav button:last-child')
            wait('!!document.querySelector(".reader-sheet")')
            subprocess.run(['adb','-s',args.device,'shell','input','keyevent','KEYCODE_BACK'],check=True,capture_output=True)
            wait('!document.querySelector(".reader-sheet")')
            assert view.evaluate('location.hash')==next_route
            result['checks'].append('Android Back closes preferences before leaving the reader')
            result['passed']=True
            print('Native novel acceptance passed: '+', '.join(result['checks']),flush=True)
        except Exception as error:
            result['error']=str(error)
            view.screenshot(output/'failure.png')
            raise
        finally:
            (output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
            view.call('Emulation.clearDeviceMetricsOverride')
            if original_preferences:rpc('ReadingStateApply','novel',json.dumps([original_preferences]))
            else:rpc('ReadingStateApply','novel',json.dumps([dict(bucket='preferences',key='default',value='',delete=True)]))
            rpc('SetSetting','application_mode',original_mode)
            view.evaluate('location.href='+json.dumps(original_url))


if __name__=='__main__':
    main()
