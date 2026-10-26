"""Run the real update modal against mocked bindings and actual Wails events.

Start Vite first, then run with Python Playwright and --channel chrome locally.
No releases are downloaded or installed; only the binding seam is mocked.
"""
import argparse
import json
from pathlib import Path
import re

from playwright.sync_api import sync_playwright, expect

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--url', default='http://127.0.0.1:9247')
    parser.add_argument('--channel', default=None)
    args = parser.parse_args()
    bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    names = re.findall(r'export function (\w+)\(', bindings)
    special = {'CheckUpdate', 'DownloadUpdate', 'InstallUpdate', 'FileExists', 'GetPendingUpdateInfo',
               'GetLastInstallReport', 'ScheduleInstallOnExit', 'CancelInstallOnExit', 'GetPendingInstall'}
    mock = '\n'.join(f'export async function {name}() {{ return ""; }}' for name in names if name not in special)
    mock += '''
      export async function CheckUpdate() {
        if(window.__holdCheck)return new Promise((resolve,reject)=>{window.__finishCheck=resolve;window.__failCheck=reject});
        return window.__updateInfo;
      }
      export async function GetPendingUpdateInfo() { return null; }
      export async function GetLastInstallReport() { return {}; }
      export async function DownloadUpdate(url) {
        window.__downloadURL = url;
        return new Promise((resolve, reject) => { window.__finishDownload = resolve; window.__failDownload = reject; });
      }
      export async function FileExists() { return true; }
      export async function GetPendingInstall() { return window.__plan || {}; }
      export async function ScheduleInstallOnExit(path, version) { window.__plan = {available: true, path, version}; }
      export async function CancelInstallOnExit() { window.__plan = {}; }
      export async function InstallUpdate(path) { window.__installPath = path; throw new Error('fixture install refused'); }
    '''
    info = {'has_update': True, 'current_version': '2.3.1', 'latest_version': '9.1.0',
            'release_name': 'Test update', 'release_notes': 'Regression fixture',
            'download_url': 'https://github.com/ws-cczj/CCZJ-Video/releases/download/v9.1.0/app.exe',
            'asset_name': 'app.exe', 'asset_size': 1024, 'published_at': ''}
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True, channel=args.channel)
        try:
            page = browser.new_page(viewport={'width': 1280, 'height': 800})
            errors = []
            page.on('pageerror', lambda error: errors.append(str(error)))
            page.route('**/bindings/cczjVideo/app/service/app*', lambda route: route.fulfill(body=mock, content_type='text/javascript'))
            page.add_init_script('window.__updateInfo = ' + json.dumps(info) + ';')
            page.goto(args.url + '/tests/browser/update.html')
            page.wait_for_load_state('networkidle')
            # Inspect the mounted DOM before delivering backend events.
            assert page.get_by_role('dialog').count() == 0
            # A background result wins over a late failed manual check. Repeated
            # notifications cannot reopen a dismissed/download-in-progress modal.
            page.evaluate('window.__holdCheck=true;void window.updateTest.check()')
            page.wait_for_function('!!window.__failCheck')
            page.evaluate("window._wails.dispatchWailsEvent({name: 'update:available', data: window.__updateInfo})")
            expect(page.get_by_role('dialog')).to_be_visible()
            page.evaluate('window.__failCheck(new Error("late upstream failure"));window.__holdCheck=false')
            expect(page.get_by_text('获取更新信息失败',exact=True)).to_have_count(0)
            page.get_by_role('button',name='关闭',exact=True).click()
            page.evaluate("window._wails.dispatchWailsEvent({name:'update:available',data:window.__updateInfo})")
            expect(page.get_by_role('dialog')).to_have_count(0)
            # A stale local cache must not be accepted as the new release.
            page.evaluate("localStorage.setItem('cczj_update_download_state', JSON.stringify({path:'C:/old/CCZJ-Video-Update.exe',version:'9.1.0',url:''}))")
            page.evaluate('void window.updateTest.check()')
            expect(page.get_by_role('button', name='立即更新', exact=True)).to_have_count(0)
            page.get_by_role('button', name='下载更新', exact=True).click()
            page.wait_for_function('typeof window.__finishDownload === "function"')
            page.get_by_role('button', name='后台下载', exact=True).click()
            indicator = page.locator('.bg-download-indicator')
            expect(indicator).to_be_visible()
            page.evaluate("window._wails.dispatchWailsEvent({name:'update:download:progress',data:{downloaded:512,total:1024,speed_bps:256}})")
            expect(indicator).to_contain_text('50%')
            expect(indicator).to_contain_text('256 B/s')
            page.evaluate("window._wails.dispatchWailsEvent({name:'update:download:progress',data:{downloaded:100,total:-1,speed_bps:10}})")
            expect(indicator).to_contain_text('100 B')
            expect(indicator).not_to_contain_text('50%')
            page.evaluate("window._wails.dispatchWailsEvent({name:'update:download:progress',data:{downloaded:1024,total:1024,speed_bps:0}})")
            expect(indicator).to_contain_text('正在校验更新包')
            path = 'C:/Users/耿/OneDrive/Desktop/CC video/CCZJ-Video-Update.9.1.0.123.exe'
            page.evaluate('(path) => window.__finishDownload(path)', path)
            expect(indicator).to_contain_text('更新包已下载完成')
            expect(indicator).to_contain_text('点击选择安装时间')
            indicator.focus()
            page.keyboard.press('Enter')
            page.get_by_role('button', name='退出时安装', exact=True).click()
            expect(page.get_by_role('button', name='取消退出时安装', exact=True)).to_be_visible()
            page.get_by_role('button', name='关闭', exact=True).click()
            expect(indicator).to_contain_text('已安排退出时安装')
            indicator.click()
            page.get_by_role('button', name='取消退出时安装', exact=True).click()
            page.get_by_role('button', name='立即更新', exact=True).click()
            page.wait_for_function('!!window.__installPath')
            assert page.evaluate("!!localStorage.getItem('cczj_update_download_state')"), 'failed install erased recoverable download state'
            # Start a second download to exercise the persistent failure indicator.
            page.evaluate('void window.updateTest.check()')
            page.get_by_role('button', name='下载更新', exact=True).click()
            page.get_by_role('button', name='后台下载', exact=True).click()
            page.evaluate("window.__failDownload(new Error('fixture network failure'))")
            expect(indicator).to_contain_text('下载更新失败')
            expect(indicator).to_contain_text('点击重试下载')
            assert not errors, errors
            print('Update browser regressions passed: available event, stale cache, live progress, unknown size, verification, completion, keyboard reopen, quit install, failed install recovery and download failure')
        finally:
            browser.close()


if __name__ == '__main__':
    main()
