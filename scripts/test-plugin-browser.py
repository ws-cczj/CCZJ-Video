"""Exercise actual plugin API/runtime in Chrome; stub only Wails bindings."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import time
from urllib.request import urlopen
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--channel', default=None)
    parser.add_argument('--port', type=int, default=9265)
    args = parser.parse_args()
    origin = f'http://127.0.0.1:{args.port}'
    flags = subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0
    server = subprocess.Popen(['node', 'node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', str(args.port), '--strictPort'], cwd=ROOT/'frontend', stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=flags)
    try:
        for _ in range(100):
            if server.poll() is not None: raise RuntimeError('Vite test server exited')
            try:
                with urlopen(origin+'/tests/browser/plugins.html', timeout=1): break
            except OSError: time.sleep(.2)
        else: raise RuntimeError('Vite did not start')
        bindings = (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
        names = re.findall(r'export function (\w+)\(', bindings)
        overrides = {'ListPlugins', 'RescanPlugins', 'ReadPluginFile', 'ReadPluginAsset', 'GetSetting', 'SetSetting', 'SetPluginEnabled', 'UninstallPlugin'}
        mock = '\n'.join(f'export async function {name}(...args) {{ return ""; }}' for name in names if name not in overrides)
        mock += '''
        export async function ListPlugins() { return structuredClone(window.__packs || []); }
        export async function RescanPlugins() { return structuredClone(window.__packs || []); }
        export async function ReadPluginFile(id, path) { return window.__files?.[id]?.[path] || ''; }
        export async function ReadPluginAsset(id, path) {
          window.__assetReads = (window.__assetReads || 0) + 1;
          if (window.__delayAsset) return await new Promise(resolve => { window.__finishAsset = resolve; });
          return window.__assets?.[id]?.[path] || '';
        }
        export async function GetSetting(key) { return window.__settings?.[key] || ''; }
        export async function SetSetting(key, value) {
          if (key === 'plugin_script_trust' && window.__failTrustSave) throw new Error('settings write failed');
          (window.__settings ||= {})[key] = value;
        }
        export async function SetPluginEnabled(id, enabled) { const pack = window.__packs?.find(p => p.id === id); if (pack) pack.status = enabled ? 'ready' : 'disabled'; }
        export async function UninstallPlugin(id) { window.__packs = (window.__packs || []).filter(p => p.id !== id); }
        '''
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(headless=True, channel=args.channel)
            try:
                page = browser.new_page()
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
                page.goto(origin+'/tests/browser/plugins.html')
                page.wait_for_function('!!window.pluginTest')
                results = page.evaluate('window.pluginTest.runtimeChecks()')
                assert all(results.values()), results
                page.evaluate('window.pluginTest.startPendingSetup()')
                page.wait_for_function('window.__setupEntered')
                page.evaluate('void window.pluginTest.disablePendingSetup()')
                page.wait_for_function('window.__disabledReturned')
                page.wait_for_timeout(300)
                assert page.evaluate('!!window.__syncDisabled'), 'disabled plugin setup blocked the global synchronization queue'
                assert page.locator('style[data-cczj-pack="test-pending"]').count() == 0
                page.evaluate('window.pluginTest.prepareTheme()')
                page.wait_for_timeout(100)
                assert page.evaluate('window.__assetReads') == 0, 'unused plugin theme eagerly loaded its full background'
                page.evaluate('window.__delayAsset = true; window.pluginTest.themeBackground()')
                page.wait_for_function('!!window.__finishAsset')
                page.evaluate('window.pluginTest.disableTheme()')
                page.evaluate('window.__finishAsset("data:image/png;base64,old")')
                page.wait_for_timeout(100)
                assert page.evaluate('window.pluginTest.themeBackground()') is None, 'disabled theme resurrected a late background image'
                repeated = page.evaluate('window.pluginTest.repeatLifecycle()')
                assert repeated == {'effects': 60, 'events': 30, 'cleanups': 30, 'noLateTimer': True, 'styles': 0}, repeated
                page.evaluate('window.pluginTest.startUninstallScript()')
                assert page.locator('style[data-cczj-pack="test-uninstall"]').count() == 1
                warning = page.evaluate('window.pluginTest.uninstallWithFailedTrustCleanup()')
                assert warning, 'failed trust cleanup was hidden'
                assert page.locator('style[data-cczj-pack="test-uninstall"]').count() == 0
                assert page.evaluate('window.pluginTest.active()') == []
                page.evaluate('window.__finishSetup()')
                page.wait_for_timeout(100)
                assert page.evaluate('window.pluginTest.active()') == []
                assert page.locator('style[data-cczj-pack="test-pending"]').count() == 0
                assert not errors, errors
                print('Plugin browser regressions passed: '+', '.join(results)+', cancel pending setup, discard late setup, lazy theme backgrounds, discard disabled theme image')
            finally: browser.close()
    finally:
        server.terminate()
        server.wait(timeout=10)

if __name__ == '__main__': main()
