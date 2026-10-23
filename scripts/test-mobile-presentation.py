"""Inspect the installed mobile presentation using temporary in-memory themes."""
import argparse
import json
import re
import subprocess
import time
from pathlib import Path
from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
STORE = "document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('theme')"


def rgba(value):
    numbers = [float(v) for v in re.findall(r'[\d.]+', value)]
    if len(numbers) < 3:
        raise ValueError('Unresolved CSS color: ' + value)
    return numbers[:3], numbers[3] if len(numbers) > 3 else 1


def contrast(foreground, background, underlay):
    back, alpha = rgba(background)
    back = [v * alpha + underlay * (1-alpha) for v in back]
    fore, alpha = rgba(foreground)
    fore = [v * alpha + b * (1-alpha) for v, b in zip(fore, back)]
    def luminance(color):
        linear = [v/255/12.92 if v/255 <= .04045 else ((v/255+.055)/1.055)**2.4 for v in color]
        return sum(v*w for v,w in zip(linear, [.2126,.7152,.0722]))
    values = sorted([luminance(fore), luminance(back)])
    return (values[1]+.05)/(values[0]+.05)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--output', default='mobile-presentation')
    parser.add_argument('--width', type=int, default=0)
    args = parser.parse_args()
    output = ROOT/'dist/audit'/args.output
    output.mkdir(parents=True, exist_ok=True)
    results = []
    failures = []
    with AndroidWebView(args.device) as page:
        original_route = page.evaluate('location.hash')
        original_theme = page.evaluate(STORE+'.currentId')
        try:
            if args.width:
                page.call('Emulation.setDeviceMetricsOverride', {'width':args.width,'height':760,'deviceScaleFactor':1,'mobile':True})
            # Direct reactive state changes only: do not call setTheme/saveCustom,
            # which would persist preferences to the user's settings database.
            for theme in dict.fromkeys([original_theme, 'green', 'd-blue', 'black']):
                page.evaluate(STORE+'.currentId='+json.dumps(theme))
                for route in ['/', '/search', '/favorites']:
                    page.evaluate('location.hash='+json.dumps('#'+route))
                    deadline = time.monotonic()+15
                    selector = {'/':'.home','/search':'.search-page','/favorites':'.favorites-page'}[route]
                    while not page.evaluate('!!document.querySelector('+json.dumps(selector)+')'):
                        assert time.monotonic() < deadline, 'Route did not mount: '+route
                        time.sleep(.1)
                    time.sleep(.65)
                    data = page.evaluate('''(() => {
                      const root=document.querySelector('.cczj-mobile-shell'), main=document.querySelector('.cczj-mobile-content');
                      const header=document.querySelector('.cczj-mobile-header'), nav=document.querySelector('.cczj-mobile-navigation');
                      const styles=getComputedStyle(root);
                      const pair=(name,fg,bg)=>{const e=document.querySelector(fg),b=document.querySelector(bg);return e&&b?{name,foreground:getComputedStyle(e).color,background:getComputedStyle(b).backgroundColor}:null};
                      const panels=[...main.querySelectorAll('.recommend-group,.recommend-section,.empty-state,.folder-row')];
                      return {route:location.hash,width:innerWidth,theme:document.documentElement.dataset.theme,
                        headerHeight:header?header.getBoundingClientRect().height:0,homeHeading:!!header?.querySelector('h1'),brand:!!header?.querySelector('.cczj-mobile-brand'),
                        wallpaper:styles.backgroundImage,bodyText:main.innerText.includes('片库'),overflow:main.scrollWidth>main.clientWidth+1,
                        clipped:[...main.querySelectorAll('.cczj-mobile-search-form button,.folder-row,.manage-actions button')].filter(e=>{const r=e.getBoundingClientRect();return r.width&&r.height&&(r.left<0||r.right>innerWidth+1)}).map(e=>e.textContent.trim()),
                        iconTargets:[...(header?.querySelectorAll('.cczj-mobile-icon-button')||[])].map(e=>({width:e.getBoundingClientRect().width,height:e.getBoundingClientRect().height})),
                        navColor:getComputedStyle(nav.querySelector('[aria-current=page]')||nav.querySelector('a')).color,
                        neutralPanels:panels.map(e=>getComputedStyle(e).backgroundColor),
                        contrast:[pair('header','.cczj-mobile-heading','.cczj-mobile-header'),pair('navigation','.cczj-mobile-navigation-active','.cczj-mobile-navigation'),pair('empty','.empty-desc','.empty-state'),pair('recommendation','.group-desc','.recommend-group'),pair('search','.cczj-mobile-search-submit','.cczj-mobile-search-submit')].filter(Boolean)};
                    })()''')
                    data['contrastRatios'] = {p['name']:round(min(contrast(p['foreground'],p['background'],u) for u in [0,255]),2) for p in data['contrast']}
                    results.append(data)
                    prefix = theme+'-'+route.strip('/').replace('/','-') if route != '/' else theme+'-home'
                    page.screenshot(output/(prefix+'.png'))
                    if data['bodyText'] or data['overflow'] or data['clipped']:
                        failures.append(prefix+': naming or clipped controls')
                    if data['headerHeight'] != 0:
                        failures.append(prefix+': primary page header should be removed')
                    if any(min(v['width'],v['height']) < 48 for v in data['iconTargets']):
                        failures.append(prefix+': small header touch target')
                    if any(v < 4.5 for v in data['contrastRatios'].values()):
                        failures.append(prefix+': text contrast '+str(data['contrastRatios']))
                    if route == '/search':
                        page.evaluate("document.querySelector('.cczj-mobile-search-field input').focus()")
                        assert page.evaluate("getComputedStyle(document.querySelector('.cczj-mobile-search-field input')).outlineStyle==='none'"), 'Input has duplicate focus rectangle'
                        page.evaluate('document.activeElement.blur()')
                    if route == '/favorites':
                        page.evaluate("document.querySelector('.cczj-mobile-folder-create').click()")
                        time.sleep(.4)
                        form = page.evaluate('''(() => {
                          const dialog=document.querySelector('[role=dialog]');
                          return dialog && [...dialog.querySelectorAll('input,button')].filter(e=>e.getBoundingClientRect().width).every(e=>{const r=e.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth+1});
                        })()''')
                        assert form, 'New folder form is missing or clipped'
                        # Android consumes Back to dismiss an IME opened by autofocus
                        # before it delivers Back to the WebView overlay handler.
                        keyboard = subprocess.check_output(page.adb+['shell','dumpsys','input_method'],text=True,encoding='utf-8')
                        if re.search(r'\bmInputShown=true\b', keyboard):
                            subprocess.run(page.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True)
                            time.sleep(.6)
                            assert page.evaluate("location.hash==='#/favorites'"), 'Keyboard Back left Favorites'
                        if not page.evaluate("document.querySelector('[role=dialog]')!==null"):
                            continue
                        subprocess.run(page.adb+['shell','input','keyevent','KEYCODE_BACK'],check=True)
                        deadline = time.monotonic()+3
                        while page.evaluate("!!document.querySelector('[role=dialog]')") and time.monotonic()<deadline:
                            time.sleep(.1)
                        dismissed = page.evaluate("({closed:!document.querySelector('[role=dialog]'),route:location.hash,focus:document.activeElement.tagName,height:innerHeight,viewport:visualViewport.height})")
                        if not (dismissed['closed'] and dismissed['route']=='#/favorites'):
                            page.screenshot(output/(prefix+'-back-failed.png'))
                            raise AssertionError('Back must close the folder form before leaving Favorites: '+json.dumps(dismissed))
            (output/'results.json').write_text(json.dumps({'pages':results,'failures':failures},ensure_ascii=False,indent=2),encoding='utf-8')
            assert not failures, '\n'.join(failures)
            print(f'PASS: {len(results)} mobile theme/page presentations, naming, touch targets, text contrast and folder form Back')
        finally:
            page.evaluate("document.querySelector('.ui-modal__close')?.click()")
            page.evaluate(STORE+'.currentId='+json.dumps(original_theme))
            if args.width: page.call('Emulation.clearDeviceMetricsOverride')
            page.evaluate('location.hash='+json.dumps(original_route))


if __name__ == '__main__':
    main()
