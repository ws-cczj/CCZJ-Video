"""Read-only mobile page layout checks in the installed Android preview."""
import argparse
import json
from pathlib import Path
import subprocess
import time
from urllib.request import urlopen
import websocket

ROOT = Path(__file__).resolve().parent.parent

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--output', default='mobile-layout')
    parser.add_argument('--width', type=int, default=0, help='Temporary WebView viewport width; restored after the audit')
    args = parser.parse_args()
    output = ROOT / 'dist/audit' / args.output
    output.mkdir(parents=True, exist_ok=True)
    adb = ['adb', '-s', args.device]
    subprocess.run(adb + ['shell', 'am', 'start', '-n', 'com.cczj.video.preview/com.wails.app.MainActivity'], check=True, capture_output=True)
    time.sleep(1)
    pid = subprocess.check_output(adb + ['shell', 'pidof', 'com.cczj.video.preview'], text=True).strip()
    port = subprocess.check_output(adb + ['forward', 'tcp:0', f'localabstract:webview_devtools_remote_{pid}'], text=True).strip()
    connection = None
    counter = 0
    original = None
    findings = []
    rows = []
    opened_form = False
    def command(method, params):
        nonlocal counter
        counter += 1
        connection.send(json.dumps({'id':counter,'method':method,'params':params}))
        while True:
            message = json.loads(connection.recv())
            if message.get('id') != counter: continue
            if 'error' in message: raise RuntimeError(message['error'])
            result = message['result']
            if 'exceptionDetails' in result: raise RuntimeError(result['exceptionDetails'])
            return result
    def evaluate(expression):
        return command('Runtime.evaluate', {'expression':expression,'awaitPromise':True,'returnByValue':True}).get('result', {}).get('value')
    try:
        with urlopen(f'http://127.0.0.1:{port}/json/list', timeout=10) as response: targets = json.load(response)
        target = next(row for row in targets if row.get('url','').startswith('https://wails.localhost'))
        connection = websocket.create_connection(target['webSocketDebuggerUrl'], timeout=20, origin='http://localhost')
        original = evaluate('location.hash')
        if args.width: command('Emulation.setDeviceMetricsOverride', {'width':args.width,'height':760,'deviceScaleFactor':1,'mobile':True})
        for path in ['/manage','/history','/sources','/settings','/settings?group=basic','/settings?group=theme','/settings?group=motion','/settings?group=extensions','/settings?group=advanced','/settings?group=about','/settings?group=diagnostics','/','/search','/favorites','/downloads','/recent','/video-types','/recycle']:
            evaluate('location.hash=' + json.dumps('#' + path))
            time.sleep(1.2)
            state = evaluate('''(() => {
              const root=document.querySelector('.cczj-mobile-shell');
              const main=document.querySelector('.cczj-mobile-content')||document.querySelector('.main-content');
              const visible=x=>{const r=x.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(x).visibility!=='hidden'};
              const headings=[...main.querySelectorAll('h1,h2')].filter(visible).filter(x=>!x.closest('.carousel')).map(x=>({text:x.textContent.trim(),width:x.getBoundingClientRect().width,height:x.getBoundingClientRect().height,font:parseFloat(getComputedStyle(x).fontSize)}));
              const badControls=[...main.querySelectorAll('.toolbar button,.toolbar input,.scheduler-bar button,.page-header input,.page-header button,.basic-section .select-trigger,.types-actions button,.types-actions input')].filter(visible).filter(x=>{const r=x.getBoundingClientRect();return r.left<0||r.right>innerWidth+1}).map(x=>x.textContent.trim()||x.getAttribute('placeholder'));
              return {root:!!root,width:innerWidth,overflow:main.scrollWidth>main.clientWidth+1,
                scrollbar:getComputedStyle(main,'::-webkit-scrollbar').display,headings,badControls,
                wrappedActions:[...main.querySelectorAll('.toolbar-right .ui-btn__content > span')].filter(visible).filter(x=>x.getBoundingClientRect().height>parseFloat(getComputedStyle(x).fontSize)*1.8).map(x=>x.textContent),
                manage:!!document.querySelector('.cczj-mobile-manage'),settingsLink:!!document.querySelector('.cczj-mobile-manage a[href="#/settings"]'),
                carousel:[...main.querySelectorAll('.slide-detail-wrap')].filter(visible).map(x=>({width:x.getBoundingClientRect().width,height:x.getBoundingClientRect().height})),
                basic:!!document.querySelector('.cczj-mobile-settings'),windowSettings:!!document.querySelector('#basic-window')};
            })()''')
            rows.append({'path':path,**state})
            if not state['root']: findings.append(f'{path}: mobile shell missing')
            if state['overflow'] or state['badControls']: findings.append(f'{path}: clipped controls / horizontal overflow {state["badControls"]}')
            if state['wrappedActions']: findings.append(f'{path}: toolbar button text wraps vertically {state["wrappedActions"]}')
            if state['scrollbar'] != 'none': findings.append(f'{path}: desktop scrollbar remains')
            if any(h['width']<120 or h['height']>h['font']*2.6 for h in state['headings']): findings.append(f'{path}: heading squeezed {state["headings"]}')
            if path == '/manage' and not (state['manage'] and state['settingsLink']): findings.append('management must be a full page with visible settings entry')
            if path.startswith('/settings') and not state['basic']: findings.append('mobile settings navigation missing')
            if path.startswith('/settings') and state['windowSettings']: findings.append('desktop window settings should not exist on mobile')
            if any(c['width']<200 for c in state['carousel']): findings.append('desktop split carousel squeezes mobile text')
            (output / (path.strip('/').replace('/','-').replace('?','-').replace('=','-') or 'home')).with_suffix('.png').write_bytes(subprocess.check_output(adb + ['exec-out','screencap','-p']))
        (output/'results.json').write_text(json.dumps({'pages':rows,'failures':findings},ensure_ascii=False,indent=2),encoding='utf-8')
        evaluate("document.querySelector('.cczj-mobile-navigation a[href=\"#/manage\"]').click()")
        time.sleep(.7)
        assert evaluate("!!document.querySelector('.cczj-mobile-manage') && !document.querySelector('[role=dialog]')"), 'management opens a dialog'
        evaluate("document.querySelector('.cczj-mobile-settings-entry').click()")
        time.sleep(.7)
        evaluate("document.querySelector('.cczj-mobile-settings .cczj-mobile-menu-row').click()")
        time.sleep(.7)
        assert evaluate("location.hash.includes('group=basic') && !document.querySelector('#basic-window')"), 'basic settings is not mobile-aware'
        subprocess.run(adb + ['shell','input','keyevent','KEYCODE_BACK'], check=True)
        time.sleep(.7)
        assert evaluate("location.hash === '#/settings' && !!document.querySelector('.cczj-mobile-settings .cczj-mobile-menu')"), 'Back did not restore settings list'
        subprocess.run(adb + ['shell','input','keyevent','KEYCODE_BACK'], check=True)
        time.sleep(.7)
        assert evaluate("location.hash === '#/manage'"), 'Back did not restore management page'
        evaluate("document.querySelector('.cczj-mobile-manage a[href=\"#/sources\"]').click()")
        time.sleep(.7)
        evaluate("document.querySelector('.sources-page .toolbar-right button:last-child').click()")
        opened_form = True
        time.sleep(.7)
        assert evaluate("!!document.querySelector('[role=dialog]')"), 'source form failed to open'
        form = evaluate('''[...document.querySelectorAll('[role=dialog] input,[role=dialog] button')].filter(x=>x.getBoundingClientRect().width).map(x=>{const r=x.getBoundingClientRect();return {left:r.left,right:r.right}})''')
        assert all(row['left'] >= 0 and row['right'] <= evaluate('innerWidth')+1 for row in form), ('source form controls clipped',form)
        (output/'source-form.png').write_bytes(subprocess.check_output(adb + ['exec-out','screencap','-p']))
        subprocess.run(adb + ['shell','input','keyevent','KEYCODE_BACK'], check=True)
        time.sleep(.7)
        dismissed = evaluate("({closed:!document.querySelector('[role=dialog]'),route:location.hash})")
        assert dismissed['closed'] and dismissed['route'] == '#/sources', ('Back did not dismiss source form before route', dismissed)
        opened_form = False
        evaluate("location.hash='#/'")
        time.sleep(.8)
        evaluate("document.querySelector('.cczj-mobile-content').scrollTop=0")
        carousel = evaluate('''(() => {const el=document.querySelector('.carousel');if(!el||el.querySelectorAll('.carousel-indicator').length<2)return null;const r=el.getBoundingClientRect();return {left:r.left,top:r.top,width:r.width,height:r.height,index:[...el.querySelectorAll('.carousel-indicator')].findIndex(x=>x.classList.contains('active'))}})()''')
        if carousel:
            y = carousel['top'] + carousel['height'] * .3
            x = carousel['left'] + carousel['width'] * .8
            command('Input.dispatchTouchEvent', {'type':'touchStart','touchPoints':[{'x':x,'y':y}]})
            for step in range(1,7):
                command('Input.dispatchTouchEvent', {'type':'touchMove','touchPoints':[{'x':x-carousel['width']*.1*step,'y':y}]})
                time.sleep(.04)
            command('Input.dispatchTouchEvent', {'type':'touchEnd','touchPoints':[]})
            time.sleep(3)
            current = evaluate("[...document.querySelectorAll('.carousel-indicator')].findIndex(x=>x.classList.contains('active'))")
            assert current != carousel['index'], 'horizontal touch did not advance carousel'
            print('PASS: real touch swipe advances the mobile carousel')
        (output/'results.json').write_text(json.dumps({'pages':rows,'failures':findings},ensure_ascii=False,indent=2),encoding='utf-8')
        assert not findings, '\n'.join(findings)
        print(f'PASS: {len(rows)} mobile pages, isolated shell, settings entry, headings, controls, scrollbars and native Back')
    finally:
        try:
            if connection:
                if opened_form: evaluate("document.querySelector('.ui-modal__close')?.click()")
                if args.width: command('Emulation.clearDeviceMetricsOverride', {})
                if original is not None: evaluate('location.hash=' + json.dumps(original))
                connection.close()
        finally: subprocess.run(adb+['forward','--remove',f'tcp:{port}'],capture_output=True)

if __name__ == '__main__': main()
