"""Check actual font preference changes and restart persistence in acceptance."""
import argparse
import base64
import importlib.util
import json
from pathlib import Path
import time

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location('native_acceptance', ROOT/'scripts/test-upgrade-android.py')
NATIVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(NATIVE)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--baseline', action='store_true')
    args = parser.parse_args()
    out = ROOT/'dist/audit/page-polish'
    out.mkdir(parents=True, exist_ok=True)
    device = NATIVE.Device('adb', args.device)
    device.connect()
    original = dict(url=device.js('location.href'), key=device.rpc('GetSetting','font_size_key'))
    result = {}
    def wait(expression):
        deadline = time.monotonic()+15
        while not device.js(expression):
            if time.monotonic()>deadline: raise AssertionError(expression)
            time.sleep(.1)
    def choose(key, size):
        device.visit('/settings?group=basic')
        wait('!!document.querySelector(".basic-fields") && !document.querySelector(".basic-fields").disabled')
        device.js('document.querySelector(\'[role=combobox][aria-label="字体大小"]\').click()')
        wait('!!document.querySelector("[role=listbox]")')
        device.js('Array.from(document.querySelectorAll("[role=option]")).find(e=>e.textContent.includes('+json.dumps(str(size)+'px')+')).click()')
        wait('document.documentElement.style.fontSize==='+json.dumps(str(size)+'px'))
        deadline=time.monotonic()+10
        while device.rpc('GetSetting','font_size_key')!=key:
            if time.monotonic()>deadline: raise AssertionError('font preference not persisted')
            time.sleep(.1)
        screenshot=device.call('Page.captureScreenshot',dict(format='png',captureBeyondViewport=False))
        (out/f'font-{size}px.png').write_bytes(base64.b64decode(screenshot['data']))
        return device.js('(()=>{const read=s=>getComputedStyle(document.querySelector(s)).fontSize;return {root:read("html"),label:read(".item-name"),select:read("[role=combobox][aria-label=\\"字体大小\\"]"),heading:read(".cczj-mobile-heading h1"),button:read(".section-links button")}})()')
    try:
        result['small']=choose('xs',12)
        result['large']=choose('xxl',20)
        if not args.baseline:
            for name in ['label','select','heading','button']:
                assert float(result['large'][name][:-2])/float(result['small'][name][:-2])>1.5,(name,result)
            device.restart()
            device.visit('/settings?group=basic')
            wait('document.documentElement.style.fontSize==="20px"')
            result['restart']=device.js('({root:getComputedStyle(document.documentElement).fontSize,label:getComputedStyle(document.querySelector(".item-name")).fontSize})')
            assert result['restart']['label']==result['large']['label'],result
        (out/('font-baseline.json' if args.baseline else 'font-result.json')).write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
        print(json.dumps(result))
    finally:
        device.rpc('SetSetting','font_size_key',original['key'] or 'md')
        device.restart()
        device.js('location.href='+json.dumps(original['url']))
        device.close()


if __name__=='__main__': main()
