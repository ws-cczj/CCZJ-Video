"""Log layout on the isolated Android acceptance host, with temporary UI fixtures.

Native log bootstrap/history and system Back are real. The 1200 long records
are in-memory layout fixtures only; no user's app or log files are modified.
"""
import argparse
import base64
import importlib.util
import json
from pathlib import Path
import time

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location('upgrade', ROOT/'scripts/test-upgrade-android.py')
upgrade = importlib.util.module_from_spec(spec)
spec.loader.exec_module(upgrade)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    args = parser.parse_args()
    out = ROOT/'dist/audit/mobile-logs'
    out.mkdir(parents=True, exist_ok=True)
    d = upgrade.Device('adb', args.device)
    d.native('shell', 'am', 'start', '-W', '-n', upgrade.PACKAGE+'/com.wails.app.MainActivity')
    d.connect()
    app = 'document.querySelector("#app").__vue_app__.config.globalProperties'
    logs = app+'.$pinia._s.get("logs")'
    theme = app+'.$pinia._s.get("theme")'
    original = {'url': d.js('location.hash'), 'theme': d.js(theme+'.currentId'), 'locale': d.js(app+'.$i18n.locale')}
    snapshot = None
    results = []

    def wait(expression):
        end = time.monotonic()+15
        while time.monotonic() < end:
            if d.js(expression):
                return
            time.sleep(.1)
        raise AssertionError(expression)

    def click(selector):
        d.js('document.querySelector('+json.dumps(selector)+').click()')
        time.sleep(.2)

    def shot(name):
        result = d.call('Page.captureScreenshot', {'format': 'png', 'captureBeyondViewport': False})
        (out/(name+'.png')).write_bytes(base64.b64decode(result['data']))

    try:
        d.js('location.hash="/settings?group=logs"')
        wait('!!document.querySelector(".log-row")')
        # CDP returnByValue cannot faithfully serialize Vue array proxies.
        snapshot = json.loads(d.js(f'JSON.stringify({{records:{logs}.records,query:{logs}.query,level:{logs}.levelFilter,origin:{logs}.originFilter,paused:{logs}.paused}})'))
        d.js(f'{logs}.setPaused(true)')
        d.js(f'''(()=>{{const s={logs};s.query='LogLayout';s.levelFilter=['DEBUG','INFO','WARN','ERROR'];s.originFilter=['app','collect','douban','scheduler','update','ui'];s.records=Array.from({{length:1200}},(_,i)=>({{
          key:'layout-'+i,seq:i+1,ts:Date.now()+i,level:['DEBUG','INFO','WARN','ERROR'][i%4],origin:i%2?'collect':'app',
          caller:'adapter/'+('nested/'.repeat(30))+'source.go:120',sourceKey:'layout-source-'+('x'.repeat(150)),sessionId:'layout-session-'+('y'.repeat(250)),
          message:'LogLayout '+i+' 请求失败与解析失败的诊断示例 / diagnostic response '+('long-unbroken-value'.repeat(80))+'\\n第二行完整内容 / full details',persisted:false
        }}));}})()''')
        wait('document.querySelectorAll(".log-row").length>0')
        for width, language, palette in [(360, 'zh-CN', 'green'), (320, 'zh-CN', 'd-blue'), (390, 'en', 'green'), (320, 'en', 'd-blue')]:
            d.call('Emulation.setDeviceMetricsOverride', {'width': width, 'height': 740, 'deviceScaleFactor': 2, 'mobile': True})
            d.js(theme+'.currentId='+json.dumps(palette))
            d.js(app+'.$i18n.locale='+json.dumps(language))
            time.sleep(.3)
            bounds = d.js('''(()=>{const e=document.querySelector('.log-list');e.scrollTop=0;e.dispatchEvent(new Event('scroll'));return {width:innerWidth,height:innerHeight,top:e.getBoundingClientRect().top,overflow:document.documentElement.scrollWidth>innerWidth,rows:document.querySelectorAll('.log-row').length}})()''')
            assert not bounds['overflow'], bounds
            assert bounds['top'] < bounds['height']-150, bounds
            assert 1 < bounds['rows'] < 60, bounds
            compact = d.js('''(()=>{const e=document.querySelector('.log-panel > .block:first-of-type .ui-btn--secondary');return {height:e.getBoundingClientRect().height,min:parseFloat(getComputedStyle(e).minHeight)}})()''')
            assert abs(compact['min']-max(36,min(width*.1,40)))<.1 and compact['height']>=compact['min'],compact
            assert d.js('!document.querySelector(".log-grid") && !document.querySelector(".log-filter-groups")')
            shot(f'{width}-{language}-{palette}')
            click('.log-row')
            wait('!!document.querySelector("[role=dialog] .log-record-message")')
            assert d.js('''(()=>{const e=document.querySelector('.log-record-message'),r=document.querySelector('[role=dialog]').getBoundingClientRect();return e.textContent.includes('full details') && e.scrollWidth<=e.clientWidth+1 && r.left>=0 && r.right<=innerWidth})()''')
            shot(f'{width}-{language}-detail')
            d.native('shell', 'input', 'keyevent', 'KEYCODE_BACK')
            wait('!document.querySelector("[role=dialog]")')
            assert d.js('location.hash.includes("group=logs")')
            click('.log-controls > div:first-child button')
            wait('!!document.querySelector(".log-filter-groups")')
            assert not d.js('document.documentElement.scrollWidth>innerWidth')
            before = d.js(logs+'.visible.length')
            click('.log-filter-groups .log-chip')
            assert d.js(logs+'.visible.length') < before
            click('.log-filter-groups .log-chip')
            assert d.js(logs+'.visible.length') == before
            shot(f'{width}-{language}-filters')
            click('.log-controls > div:first-child button')
            results.append({'viewport': bounds, 'language': language, 'theme': palette, 'compact_control':compact, 'long_record_and_back': True, 'level_filter': True})
        # Restore actual records before checking native historical file loading.
        d.js(f'{logs}.records='+json.dumps(snapshot['records'])+f';{logs}.query=""')
        click('.log-tab:nth-child(2)')
        wait('!!document.querySelector(".log-file-picker select option") && !!document.querySelector(".log-row")')
        assert not d.js('document.documentElement.scrollWidth>innerWidth')
        shot('native-history')
        click('.log-row')
        wait('!!document.querySelector("[role=dialog]")')
        d.native('shell', 'input', 'keyevent', 'KEYCODE_BACK')
        wait('!document.querySelector("[role=dialog]")')
        click('.log-tab:nth-child(3)')
        wait('!!document.querySelector(".log-raw") && document.querySelector(".log-raw").textContent.length>100')
        assert not d.js('document.documentElement.scrollWidth>innerWidth')
        shot('native-raw')
        results.append({'native_history_and_raw': True})
        (out/'result.json').write_text(json.dumps(results, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(results, ensure_ascii=False))
    finally:
        try:
            d.call('Emulation.clearDeviceMetricsOverride')
            if snapshot:
                d.js(f'{logs}.records='+json.dumps(snapshot['records'])+f';{logs}.query='+json.dumps(snapshot['query'])+f';{logs}.levelFilter='+json.dumps(snapshot['level'])+f';{logs}.originFilter='+json.dumps(snapshot['origin'])+f';{logs}.setPaused('+json.dumps(snapshot['paused'])+')')
            d.js(theme+'.currentId='+json.dumps(original['theme']))
            d.js(app+'.$i18n.locale='+json.dumps(original['locale']))
            d.js('location.hash='+json.dumps(original['url']))
        finally:
            d.close()


if __name__ == '__main__':
    main()
