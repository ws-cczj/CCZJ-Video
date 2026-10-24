"""Actual source/backup views: cancellation, stale completion and narrow layouts.

Requires Vite and Python Playwright. Mocks generated Wails bindings; never
imports into a real application database.
"""
import argparse
import json
import re
from pathlib import Path

from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', default='http://127.0.0.1:9247')
    parser.add_argument('--channel', default=None)
    args = parser.parse_args()
    output = ROOT / 'dist/audit/file-transfer'
    output.mkdir(parents=True, exist_ok=True)
    bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    names = re.findall(r'export function (\w+)\(', bindings)
    special = {'ImportSourceFromBase64', 'GetAllSources', 'GetSourceStats', 'BackupExports', 'BackupArchives', 'LegacyDataNotice', 'BackupImportFromBase64', 'BackupExport'}
    mock = '\n'.join(f'export async function {name}() {{ return ""; }}' for name in names if name not in special)
    mock += '''
      function pending(kind) {
        const calls = window.__calls ||= [];
        let resolve, reject;
        const request = new Promise((ok, fail) => { resolve = ok; reject = fail; });
        const call = {kind, resolve, reject, cancelled: false}; calls.push(call);
        request.cancel = () => { call.cancelled = true; reject({code:'CANCELLED',message:'cancelled'}); };
        return request;
      }
      export function ImportSourceFromBase64() { return pending('source'); }
      export function BackupImportFromBase64() { return pending('backup'); }
      export async function GetAllSources() { window.__sourceRefreshes = (window.__sourceRefreshes || 0) + 1; return []; }
      export async function GetSourceStats() { return []; }
      export async function BackupExports() { return [{name:'backup_20261024_very_long_existing_filename.json.br',modified_at:'2026-10-24 10:00:00',size_bytes:1234}]; }
      export async function BackupArchives() { window.__archives = (window.__archives || 0) + 1; return []; }
      export async function LegacyDataNotice() { window.__legacy = (window.__legacy || 0) + 1; return {}; }
      export async function BackupExport() { return '/data/user/0/com.cczj.video.acceptance/files/exports/backup.json.br'; }
    '''
    checks = []
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True, channel=args.channel)
        try:
            for width in (1440, 390):
                page = browser.new_page(viewport={'width': width, 'height': 900})
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
                page.goto(args.url + '/tests/browser/file-transfer.html?source')
                page.wait_for_load_state('networkidle')
                expect(page.get_by_role('dialog')).to_be_visible()
                drop = page.locator('.import-drop-zone')
                drop.focus()
                expect(drop).to_be_focused()
                file = {'name': 'source.json', 'mimeType': 'application/json', 'buffer': b'{}'}
                page.locator('input[type=file]').set_input_files(file)
                page.get_by_role('button', name='导入', exact=True).click()
                page.wait_for_function('window.__calls?.length === 1')
                page.get_by_role('button', name='取消', exact=True).click()
                page.wait_for_function('window.__calls[0].cancelled')
                page.evaluate('fileTransferTest.state.sourceOpen = true')
                expect(page.get_by_role('dialog')).to_be_visible()
                page.evaluate('window.__calls[0].resolve("late import")')
                expect(page.get_by_role('dialog')).to_be_visible()
                assert page.evaluate('window.__sourceRefreshes || 0') == 0
                page.locator('input[type=file]').set_input_files(file)
                page.get_by_role('button', name='导入', exact=True).click()
                page.wait_for_function('window.__calls.length === 2')
                page.evaluate('window.__calls[1].resolve("imported")')
                expect(page.get_by_role('dialog')).to_have_count(0)
                assert page.evaluate('window.__sourceRefreshes') == 1
                page.evaluate('fileTransferTest.state.sourceOpen = true')
                expect(page.get_by_role('dialog')).to_be_visible()
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'source import overflows'
                page.screenshot(path=str(output / f'source-{width}.png'))

                page.goto(args.url + '/tests/browser/file-transfer.html')
                page.wait_for_load_state('networkidle')
                expect(page.locator('.backup-file-name').or_(page.get_by_text('backup_20261024_very_long_existing_filename.json.br', exact=True))).to_be_visible()
                assert page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'backup panel overflows'
                page.screenshot(path=str(output / f'backup-{width}.png'))
                page.locator('input[type=file]').set_input_files({'name': 'backup.json', 'mimeType': 'application/json', 'buffer': b'{}'})
                page.wait_for_function('!!fileTransferTest.confirm.active')
                page.evaluate('fileTransferTest.confirm.handle(true)')
                page.wait_for_function('window.__calls?.length === 1')
                page.evaluate('fileTransferTest.state.mounted = false')
                page.wait_for_function('window.__calls[0].cancelled')
                page.evaluate('window.__calls[0].resolve({favorites_added:99})')
                assert page.locator('.result').count() == 0
                assert page.evaluate('fileTransferTest.errors.history.length') == 0

                page.goto(args.url + '/tests/browser/file-transfer.html?theme')
                page.wait_for_load_state('networkidle')
                page.evaluate('''() => {
                  window.FileReader = class {
                    readAsDataURL() { window.__imageReader = this; }
                    abort() { window.__imageAborts = (window.__imageAborts || 0) + 1; }
                  };
                  fileTransferTest.openTheme();
                }''')
                expect(page.get_by_role('dialog')).to_be_visible()
                picker = page.locator('.bg-drop-zone')
                picker.focus()
                expect(picker).to_be_focused()
                with page.expect_file_chooser() as choosing:
                    picker.press('Enter')
                choosing.value.set_files({'name':'wallpaper.png','mimeType':'image/png','buffer':b'image fixture'})
                page.wait_for_function('!!window.__imageReader')
                page.evaluate('window.__lateImageLoad = window.__imageReader.onload')
                page.get_by_role('button', name='取消', exact=True).click()
                expect(page.get_by_role('dialog')).to_have_count(0)
                page.evaluate('''async () => {
                  window.__imageReader.result = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7';
                  window.__lateImageLoad?.();
                  await new Promise(resolve => setTimeout(resolve, 20));
                }''')
                assert page.evaluate('fileTransferTest.theme.currentId') != '__preview__', 'closed image read resurrected the preview theme'
                assert not errors, errors
                checks.append(f'{width}px source import keyboard/fresh result/cancel/late response; backup unmount; theme close rejects late image preview')
                page.close()
            (output / 'result.json').write_text(json.dumps({'checks': checks}, ensure_ascii=False, indent=2), encoding='utf-8')
            print('\n'.join(checks))
        finally:
            browser.close()


if __name__ == '__main__':
    main()
