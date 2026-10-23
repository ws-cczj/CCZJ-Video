"""Actual Android long press, immediate search-history removal, source toggle and HMR."""
import argparse,json,subprocess,time
from pathlib import Path
from android_webview import AndroidWebView


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device',default='emulator-5554')
    parser.add_argument('--hmr',action='store_true')
    parser.add_argument('--output',default='mobile-search-history')
    args=parser.parse_args()
    root=Path(__file__).resolve().parent.parent
    output=root/'dist/audit'/args.output;output.mkdir(parents=True,exist_ok=True)
    with AndroidWebView(args.device) as p:
        def wait(expr,seconds=25):
            end=time.monotonic()+seconds
            while time.monotonic()<end:
                try:
                    if p.evaluate(expr):return
                except RuntimeError:pass
                time.sleep(.1)
            raise AssertionError(expr)
        def touch(kind,x=0,y=0):p.call('Input.dispatchTouchEvent',{'type':kind,'touchPoints':[] if kind in ('touchEnd','touchCancel') else [{'x':x,'y':y}]})
        def point(selector):return p.evaluate('(()=>{const r=document.querySelector('+json.dumps(selector)+').getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
        def hold(selector,cancel=False):
            at=point(selector);touch('touchStart',**at);time.sleep(.65);touch('touchCancel' if cancel else 'touchEnd');time.sleep(.25)
        def tap(selector):
            at=point(selector);touch('touchStart',**at);touch('touchEnd');time.sleep(.3)
        def back():subprocess.run(p.adb+['shell','input','keyevent','KEYCODE_BACK'],capture_output=True,check=True);time.sleep(.4)
        keys=['search_history','source_search_mode']
        saved=p.evaluate('Object.fromEntries('+json.dumps(keys)+'.map(k=>[k,localStorage.getItem(k)]))')
        route=p.evaluate('location.hash')
        try:
            p.evaluate("localStorage.setItem('search_history',JSON.stringify(['cczj-touch-a','cczj-touch-b']));location.hash='/search';location.reload()")
            wait("!!document.querySelector('.cczj-mobile-search-history-term') && !document.querySelector('.splash-wrap')")
            time.sleep(.6)
            assert p.evaluate("!document.querySelector('.cczj-mobile-history-manage') && !document.querySelector('.cczj-mobile-item-remove')"),'Management or delete controls remain in default history'
            at=point('.cczj-mobile-search-history-term');touch('touchStart',**at);touch('touchMove',at['x'],at['y']+30);time.sleep(.65);touch('touchCancel');time.sleep(.2)
            assert p.evaluate("!document.querySelector('.cczj-mobile-item-remove')"),'Scrolling triggered long press'
            stable=p.evaluate("(()=>{const r=document.querySelector('.cczj-mobile-search-history-term').getBoundingClientRect(),h=document.querySelector('.cczj-mobile-search-history-heading').getBoundingClientRect();return {w:r.width,h:r.height,top:r.top,heading:h.height}})()")
            hold('.cczj-mobile-search-history-term')
            assert p.evaluate("location.hash==='#/search' && document.querySelectorAll('.cczj-mobile-search-history-chip .cczj-mobile-item-remove').length===2"),'Hold failed to show X or searched on release'
            after=p.evaluate("(()=>{const r=document.querySelector('.cczj-mobile-search-history-term').getBoundingClientRect(),h=document.querySelector('.cczj-mobile-search-history-heading').getBoundingClientRect();return {w:r.width,h:r.height,top:r.top,heading:h.height}})()")
            assert all(abs(stable[k]-after[k])<1 for k in stable),(stable,after)
            assert p.evaluate("document.querySelectorAll('.cczj-mobile-edit-actions button').length===2 && getComputedStyle(document.querySelector('.cczj-mobile-item-remove'),'::before').display==='none'"),'Floating actions or lightweight X missing'
            p.screenshot(output/'long-press.png')
            tap('.cczj-mobile-search-history-chip .cczj-mobile-item-remove')
            assert p.evaluate("JSON.stringify(JSON.parse(localStorage.getItem('search_history')).value)==='[\"cczj-touch-b\"]' && !document.querySelector('[role=dialog]')"),'Search deletion is not immediate or requires confirmation'
            back()
            assert p.evaluate("location.hash==='#/search' && !document.querySelector('.cczj-mobile-item-remove')"),'Back did not exit editing first'
            # Toggle shares desktop tokens and reserves a fixed state-icon slot.
            before=point('.source-search-toggle')
            state=p.evaluate("document.querySelector('.source-search-toggle').getAttribute('aria-pressed')")
            tap('.source-search-toggle')
            assert p.evaluate("document.querySelector('.source-search-toggle').getAttribute('aria-pressed')")!=state
            after=point('.source-search-toggle');assert abs(before['x']-after['x'])<1,'Source toggle resized when selected'
            if p.evaluate("document.querySelector('.source-search-toggle').getAttribute('aria-pressed')")!='true':tap('.source-search-toggle')
            wait("(()=>{const e=document.querySelector('.source-search-toggle'),c=getComputedStyle(e),v=document.createElement('i');v.style.background='var(--accent)';e.append(v);const color=getComputedStyle(v).backgroundColor;v.remove();return c.backgroundColor===color})()")
            p.screenshot(output/'search.png')
            results={'longPressRevealsX':True,'releaseDoesNotSearch':True,'scrollCancelsHold':True,'immediateDeleteWithoutDialog':True,'backExitsEditing':True,'sourceToggleStableAndUsesDesktopAccent':True}
            if args.hmr:
                source=root/'frontend/src/styles/mobile/presentation.css';original=source.read_bytes();origin=p.evaluate('performance.timeOrigin');probe='hmr-'+str(time.time_ns())
                expression="getComputedStyle(document.querySelector('.cczj-mobile-shell')).getPropertyValue('--cczj-audit-hmr').trim()"
                try:
                    source.write_bytes(original+('\n.cczj-mobile-shell { --cczj-audit-hmr: '+probe+'; }\n').encode());wait(expression+'==='+json.dumps(probe))
                    assert p.evaluate('performance.timeOrigin')==origin
                finally:source.write_bytes(original)
                wait(expression+"===''");results['hmrWithoutReload']=True
            (output/'results.json').write_text(json.dumps(results,indent=2),encoding='utf-8')
            print('PASS: native search hold/X, scroll cancellation, immediate removal, Back, source toggle'+(' and CSS HMR' if args.hmr else ''))
        finally:
            p.evaluate('Object.entries('+json.dumps(saved)+').forEach(([k,v])=>v===null?localStorage.removeItem(k):localStorage.setItem(k,v));location.hash='+json.dumps(route)+';location.reload()')
            wait("!!document.querySelector('.app-shell') && !document.querySelector('.splash-wrap')")


if __name__=='__main__':main()
