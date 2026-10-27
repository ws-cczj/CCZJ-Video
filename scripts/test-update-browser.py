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
    output=ROOT/'dist/audit/update-notifications';output.mkdir(parents=True,exist_ok=True)
    bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    names = re.findall(r'export function (\w+)\(', bindings)
    special = {'CheckUpdate', 'DownloadUpdate', 'InstallUpdate', 'FileExists', 'GetPendingUpdateInfo',
               'GetLastInstallReport', 'ScheduleInstallOnExit', 'CancelInstallOnExit', 'GetPendingInstall', 'GetAllSources', 'GetSourceStats', 'ProbeSources', 'SourceProbeTimeline', 'ListPlugins'}
    mock = '\n'.join(f'export async function {name}() {{ (window.__mockCalls ||= []).push("{name}"); return ""; }}' for name in names if name not in special)
    mock += '''
      export async function ListPlugins(){return [];}
      export async function GetSourceStats(){return [];}
      export async function ProbeSources(){return [];}
      export async function SourceProbeTimeline(){return [];}
      export async function GetAllSources() { return window.__notificationSources || []; }
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
            'release_name': 'Test update', 'release_notes': '## 修复内容\n\n- **更新**支持 `Markdown`\n- [发布页](https://github.com/ws-cczj/CCZJ-Video/releases)\n\n<script>window.__unsafeRelease=true</script>',
            'history': [{'version':'9.0.0','desc':'### 历史修复\n\n1. 保留**数据**'}],
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
            expect(page.locator('.update-notes-body h2')).to_have_text('修复内容')
            expect(page.locator('.update-notes-body li strong')).to_have_text('更新')
            expect(page.locator('.update-notes-body code')).to_have_text('Markdown')
            expect(page.locator('.update-notes-body a')).to_have_attribute('href','https://github.com/ws-cczj/CCZJ-Video/releases')
            expect(page.locator('.update-history-body ol strong')).to_have_text('数据')
            assert page.evaluate('window.__unsafeRelease') is None
            for width in [1280,390,320]:
                page.set_viewport_size({'width':width,'height':844})
                page.screenshot(path=str(output/f'changelog-{width}.png'))
                assert page.locator('.update-notes-body').evaluate('(e)=>e.scrollWidth<=e.clientWidth+1')
            page.set_viewport_size({'width':1280,'height':800})
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
            tip = page.locator('.ui-toast').filter(has_text='正在连接下载源')
            expect(tip).to_be_visible()
            tip.get_by_role('button',name='关闭',exact=True).click()
            expect(tip).to_have_count(0)
            page.evaluate("window._wails.dispatchWailsEvent({name:'update:download:progress',data:{downloaded:512,total:1024,speed_bps:256}})")
            expect(page.locator('.bg-download-indicator')).to_have_count(0)
            page.evaluate('window.updateTest.open()')
            expect(page.get_by_role('dialog')).to_contain_text('50%')
            expect(page.get_by_role('dialog')).to_contain_text('256 B/s')
            page.evaluate("window._wails.dispatchWailsEvent({name:'update:download:progress',data:{downloaded:100,total:-1,speed_bps:10}})")
            expect(page.get_by_role('dialog')).to_contain_text('100 B')
            expect(page.get_by_role('dialog')).not_to_contain_text('50%')
            page.evaluate("window._wails.dispatchWailsEvent({name:'update:download:progress',data:{downloaded:1024,total:1024,speed_bps:0}})")
            page.get_by_role('dialog').get_by_role('button',name='关闭',exact=True).click()
            path = 'C:/Users/耿/OneDrive/Desktop/CC video/CCZJ-Video-Update.9.1.0.123.exe'
            page.evaluate('(path) => window.__finishDownload(path)', path)
            tip = page.locator('.ui-toast').filter(has_text='更新包已下载完成')
            expect(tip).to_be_visible()
            expect(tip).to_contain_text('点击选择安装时间')
            tip.get_by_role('button',name='详细信息',exact=True).focus()
            page.keyboard.press('Enter')
            page.get_by_role('button', name='退出时安装', exact=True).click()
            expect(page.get_by_role('button', name='取消退出时安装', exact=True)).to_be_visible()
            page.get_by_role('dialog').get_by_role('button', name='关闭', exact=True).click()
            expect(tip).to_have_count(0,timeout=6000)
            page.evaluate('window.updateTest.open()')
            page.get_by_role('button', name='取消退出时安装', exact=True).click()
            page.get_by_role('button', name='立即更新', exact=True).click()
            page.wait_for_function('!!window.__installPath')
            assert page.evaluate("!!localStorage.getItem('cczj_update_download_state')"), 'failed install erased recoverable download state'
            # Failure feedback uses the same dismissible tip stack.
            page.evaluate('void window.updateTest.check()')
            page.get_by_role('button', name='下载更新', exact=True).click()
            page.get_by_role('button', name='后台下载', exact=True).click()
            page.evaluate("window.__failDownload(new Error('fixture network failure'))")
            failure=page.locator('.ui-toast').filter(has_text='下载更新失败')
            expect(failure).to_be_visible()
            page.evaluate("window.updateTest.sources.sources=window.__notificationSources=[{source_key:'long_source',name:'真实源名称很长用于窄屏提示和关闭按钮检查',api_url:'https://example.invalid/',enabled:1}]")
            for width in [1280,390,320]:
                page.set_viewport_size({'width':width,'height':844})
                page.evaluate("window.updateTest.collect.getState('long_source').startTime=Date.now();window.updateTest.collect.getState('long_source').running=true")
                notice=page.locator('.ui-toast:not(.cczj-motion-toast-leave-active)').filter(has_text='查看该源采集')
                expect(notice).to_be_visible()
                page.screenshot(path=str(output/f'collection-tip-{width}.png'),animations='disabled')
                box=notice.bounding_box()
                assert box['x']>=0 and box['x']+box['width']<=width+1,box
                assert box['y']<422,box
                if width==390:
                    notice.get_by_role('button',name='查看该源采集',exact=True).click()
                    page.wait_for_function("window.updateTest.router.currentRoute.value.query.source==='long_source'")
                    try:
                        expect(page.locator('[data-collection-source="long_source"]')).to_have_class(re.compile('expanded'))
                    except Exception:
                        print(page.evaluate("({loading:window.updateTest.sources.loading,loaded:window.updateTest.sources.loaded,sources:window.updateTest.sources.sources,calls:window.__mockCalls,errors:window.__sourceErrors})"))
                        print(errors)
                        raise
                    page.evaluate("window.updateTest.router.push('/')")
                else:
                    expect(notice).to_have_count(0,timeout=6000)
                    page.evaluate("window.updateTest.collect.getState('long_source').current++")
                    expect(notice).to_have_count(0)
                page.evaluate("window.updateTest.collect.getState('long_source').running=false;window.updateTest.collect.getState('long_source').done=false")
            expect(page.locator('.collect-overlay,.bg-download-indicator')).to_have_count(0)
            assert not errors, errors
            checks = ['Markdown release notes and history', 'escaped upstream HTML', 'available event', 'stale cache', 'live progress', 'unknown size', 'verification', 'completion', 'keyboard reopen', 'quit install', 'failed install recovery', 'download failure', 'manual tip close', 'five-second expiry', 'source navigation and expansion', '320/390/1280px layout', 'no bottom overlay']
            (output/'browser-result.json').write_text(json.dumps({'passed': checks}, indent=2), encoding='utf-8')
            print('Update browser regressions passed: '+', '.join(checks))
        finally:
            browser.close()


if __name__ == '__main__':
    main()
