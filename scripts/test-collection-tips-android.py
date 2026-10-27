"""Check collection Tips with real native collection in the isolated acceptance APK.

Requires a Debug acceptance APK containing the current frontend and a real video
API passed with --source-url. Creates and removes only its own temporary source.
Uses the existing Android WebView transport; never targets preview app storage.
"""
import argparse
import base64
import importlib.util
import json
from pathlib import Path
import time
import uuid

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location('upgrade_acceptance', ROOT/'scripts/test-upgrade-android.py')
NATIVE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(NATIVE)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--source-url', required=True)
    parser.add_argument('--output', type=Path, default=ROOT/'dist/audit/update-notifications/native')
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    device = NATIVE.Device('adb', args.device)
    key = 'tip_audit_'+uuid.uuid4().hex[:12]
    created = False
    result = {}

    def wait(expression, timeout=15):
        deadline = time.monotonic()+timeout
        while not device.js(expression):
            if time.monotonic() > deadline:
                raise AssertionError('Native condition timed out: '+expression)
            time.sleep(.1)

    def click(selector):
        point = device.js('(()=>{const e=document.querySelector('+json.dumps(selector)+');if(!e)throw Error("Missing touch target");const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
        device.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[point]))
        device.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))

    def screenshot(label):
        image = device.call('Page.captureScreenshot', dict(format='png'))['data']
        (args.output/(label+'.png')).write_bytes(base64.b64decode(image))

    # Match only this test's actionable notice, excluding outgoing animations.
    notice = '.ui-toast:not(.cczj-motion-toast-leave-active):has(.ui-btn)'
    try:
        device.connect()
        device.rpc('AddSource', dict(source_key=key, name='真实源采集 Tips 验收', api_url=args.source_url, enabled=1, kind='video'))
        created = True
        device.store('source', 's.loadSources(true)')
        device.visit('/settings')
        device.store('source', 's.loadSources(true)')
        result['sourcesBeforeCollect'] = device.store('source', 's.sources')
        assert any(row['source_key'] == key for row in result['sourcesBeforeCollect']), result
        device.store('collect', 's.startCollect('+json.dumps(key)+',"once",0)')
        wait('!!document.querySelector('+json.dumps(notice)+')')
        time.sleep(.35)
        geometry = device.js('(()=>{const r=document.querySelector('+json.dumps(notice)+').getBoundingClientRect();return {x:r.x,y:r.y,width:r.width,height:r.height,viewport:innerWidth,viewHeight:innerHeight,text:document.querySelector('+json.dumps(notice)+').innerText}})()')
        assert geometry['x'] >= 0 and geometry['x']+geometry['width'] <= geometry['viewport']+1, geometry
        assert geometry['y'] < geometry['viewHeight']/2, geometry
        assert '真实源采集 Tips 验收' in geometry['text'], geometry
        result['geometry'] = geometry
        screenshot('collection-tip')
        click(notice+' .ui-toast__close')
        wait('!document.querySelector('+json.dumps(notice)+')')
        result['manualClose'] = True
        # The real task continues after closing the tip, then publishes completion.
        wait('!document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("collect").getState('+json.dumps(key)+').running', 90)
        state = device.store('collect', 's.getState('+json.dumps(key)+')')
        assert not state['error'], state
        result['collection'] = state
        wait('!!document.querySelector('+json.dumps(notice)+')')
        wait('!document.querySelector('+json.dumps(notice)+')', 7)
        result['autoDismiss'] = True
        # Immediate second ordinary collection must remain usable within five minutes.
        device.store('collect', 's.startCollect('+json.dumps(key)+',"once",0)')
        wait('!!document.querySelector('+json.dumps(notice)+')')
        click(notice+' .ui-btn')
        wait('location.hash.includes("source='+key+'")')
        wait('document.querySelector('+json.dumps('[data-collection-source="'+key+'"]')+')?.classList.contains("expanded")')
        result['sourceNavigation'] = device.js('location.hash')
        screenshot('collection-source-expanded')
        wait('!document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("collect").getState('+json.dumps(key)+').running', 90)
        second = device.store('collect', 's.getState('+json.dumps(key)+')')
        assert not second['error'], second
        result['immediateSecondCollection'] = True
        assert device.js('!document.querySelector(".collect-overlay,.bg-download-indicator")')
        result['noBottomOverlay'] = True
        (args.output/'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(result, ensure_ascii=True))
    finally:
        if created:
            device.rpc('StopCollect', dict(source_key=key, mode='', hours=0))
            device.rpc('DeleteSource', key)
        device.close()


if __name__ == '__main__':
    main()
