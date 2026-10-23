"""Audit real Android video settings; uses only the separate acceptance app.

Exports local test reports/backups, opens and cancels native choosers. Does not
import a backup, send a file, confirm an update, or clear application data.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import time
from android_webview import AndroidWebView


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    checks = []
    pages = []
    completed = False
    adb = ['adb', '-s', args.device]
    def shell(*values):
        return subprocess.check_output(adb+['shell', *values], text=True, encoding='utf-8', errors='replace', timeout=30)
    def check(name, passed, detail=None):
        checks.append(dict(name=name, passed=bool(passed), detail=detail))
        print(('PASS ' if passed else 'FAIL ')+name, flush=True)
    with AndroidWebView(args.device, application_id='com.cczj.video.acceptance') as view:
        def wait(expression, timeout=15):
            deadline = time.monotonic()+timeout
            while not view.evaluate(expression):
                if time.monotonic() > deadline: raise AssertionError(expression)
                time.sleep(.1)
        wait("!!document.querySelector('#app')?.__vue_app__")
        store = "document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('applicationMode')"
        if view.evaluate(store+'.mode') != 'video':
            view.evaluate(store+".switchMode('video',async()=>true)")
            wait("!!document.querySelector('.cczj-mobile-shell')")
        def visit(group):
            route = '/settings'+('' if group == 'index' else '?group='+group)
            view.evaluate('location.hash='+json.dumps(route))
            key = group.split('&')[0]
            wait("!!document.querySelector('.cczj-mobile-route-scene[data-screen=\"/settings:"+key+"\"]') && document.querySelectorAll('.cczj-mobile-route-scene').length===1")
            wait("!document.querySelector('.cczj-mobile-route-stage [class*=enter-active],.cczj-mobile-route-stage [class*=leave-active]')")
        def click(selector):
            view.evaluate('document.querySelector('+json.dumps(selector)+')?.scrollIntoView({block:"center",behavior:"instant"})')
            view.evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')
            wait('(()=>{const e=document.querySelector('+json.dumps(selector)+');if(!e)return false;const r=e.getBoundingClientRect();const hit=document.elementFromPoint(r.x+r.width/2,r.y+r.height/2);return !!hit&&(hit===e||e.contains(hit))})()')
            point = view.evaluate('(()=>{const e=document.querySelector('+json.dumps(selector)+');if(!e)return null;const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
            assert point, selector
            view.call('Input.dispatchTouchEvent', dict(type='touchStart', touchPoints=[point]))
            time.sleep(.06)
            view.call('Input.dispatchTouchEvent', dict(type='touchEnd', touchPoints=[]))
        def button(text):
            return view.evaluate('(()=>{const e=[...document.querySelectorAll("button")].find(e=>e.getBoundingClientRect().width&&e.innerText.trim()==='+json.dumps(text)+');if(!e)return false;e.click();return true})()')
        def native_top():
            text = shell('dumpsys', 'activity', 'activities')
            return '\n'.join(line.strip() for line in text.splitlines() if re.search(r'(mResumedActivity|topResumedActivity)',line))
        def chooser(name, expected):
            deadline=time.monotonic()+3
            top=native_top()
            while not re.search(expected,top,re.I) and time.monotonic()<deadline:
                time.sleep(.15);top=native_top()
            found=bool(re.search(expected,top,re.I))
            check(name,found,top)
            if found:
                view.screenshot(args.output/(name+'.png'))
                shell('input','keyevent','KEYCODE_BACK')
                time.sleep(.3)
            return found
        try:
            # Keep an export action present during narrow-layout checks, as it
            # remains mounted when users revisit settings after exporting.
            visit('logs')
            assert button('导出')
            wait("[...document.querySelectorAll('.settings-group-active button')].some(e=>e.innerText.trim()==='分享导出文件')")
            for width in [320,360,412]:
                view.call('Emulation.setDeviceMetricsOverride',dict(width=width,height=760,deviceScaleFactor=1,mobile=True))
                for group in ['index','basic','basic&section=home','basic&section=data','basic&section=maintenance','extensions','advanced','about','theme','motion','logs','diagnostics']:
                    visit(group)
                    row=view.evaluate('''(()=>{const vis=e=>e.getBoundingClientRect().width>0&&e.getBoundingClientRect().height>0;
                        const root=document.querySelector('.cczj-mobile-content');
                        return {text:document.body.innerText,buttons:[...document.querySelectorAll('button')].filter(vis).map(e=>e.innerText.trim()),
                        overflow:root.scrollWidth>root.clientWidth+1,
                        clipped:[...document.querySelectorAll('.settings-group-active button,.settings-group-active input')].filter(vis).filter(e=>!e.closest('.diag-nav')).filter(e=>{const r=e.getBoundingClientRect();return r.left< -1||r.right>innerWidth+1}).map(e=>e.outerHTML.slice(0,200))}})()''')
                    pages.append(dict(width=width,group=group,**row))
                    check(f'{width}-{group}-bounds', not row['overflow'] and not row['clipped'],row['clipped'])
                    bad=[text for text in row['buttons'] if re.search('打开.*(目录|文件夹)|重启应用|退回上一版',text)]
                    check(f'{width}-{group}-actions',not bad,bad)
                    if group=='extensions':
                        check(f'{width}-extensions-language',not re.search('资源管理器|拖进窗口|设置页顶部|删掉这个文件夹',row['text']))
                    if group=='diagnostics':
                        for section in ['startup','activity','playback','network','collection','douban','storage','overview']:
                            selector='.diag-nav [data-section="'+section+'"]'
                            view.evaluate('document.querySelector('+json.dumps(selector)+').scrollIntoView({block:"nearest",inline:"center",behavior:"instant"})')
                            check(f'{width}-diagnostics-nav-{section}',view.evaluate('(()=>{const e=document.querySelector('+json.dumps(selector)+');const r=e.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth})()'))
                    if width==360 and group in ['index','extensions','about']:view.screenshot(args.output/(group+'.png'))
            check('mobile-update-entry', any(r['group']=='about' and '检查更新' in r['buttons'] for r in pages))
            visit('about')
            button('检查更新')
            wait("!!document.querySelector('.about-actions [aria-busy=true]') || document.body.innerText.includes('当前已是最新版本') || !!document.querySelector('[role=dialog]')",5)
            check('mobile-update-wired', True)
            wait("!document.querySelector('.about-actions [aria-busy=true]')",45)
            check('mobile-update-finishes-or-times-out', True)
            if view.evaluate("!!document.querySelector('[role=dialog]')"):
                check('mobile-update-dialog-fit',view.evaluate("(()=>{const r=document.querySelector('[role=dialog]').getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.height<=innerHeight})()"))
                view.screenshot(args.output/'update-result.png')
                shell('input','keyevent','KEYCODE_BACK')
                wait("!document.querySelector('[role=dialog]')")
            visit('advanced')
            # Tap the visible button to exercise the native HTML file-input path.
            view.evaluate("[...document.querySelectorAll('.settings-group-active button')].find(e=>e.innerText.trim()==='导入备份').setAttribute('data-audit-picker','')")
            for _ in range(2):
                click('[data-audit-picker]')
                chooser('backup-document-picker-'+str(_),r'documentsui')
            assert button('导出全量备份')
            wait("[...document.querySelectorAll('.settings-group-active button')].some(e=>['分享备份','打开所在文件夹'].includes(e.innerText.trim()))")
            check('backup-export-mobile-action',not view.evaluate("[...document.querySelectorAll('.settings-group-active button')].some(e=>e.innerText.includes('打开所在文件夹'))"))
            if button('分享备份'):chooser('backup-share',r'ChooserActivity|ResolverActivity')
            else:check('backup-share',False)
            visit('logs')
            assert button('导出')
            wait("[...document.querySelectorAll('.settings-group-active button')].some(e=>e.innerText.trim()==='分享导出文件')")
            button('分享导出文件');chooser('log-share',r'ChooserActivity|ResolverActivity')
            visit('diagnostics')
            assert button('导出诊断报告')
            wait("[...document.querySelectorAll('.settings-group-active button')].some(e=>e.innerText.trim()==='分享导出文件')",30)
            button('分享导出文件');chooser('diagnostic-share',r'ChooserActivity|ResolverActivity')
            visit('theme')
            check('theme-edit-visible-without-hover',view.evaluate("getComputedStyle(document.querySelector('.theme-card .card-actions-top')).opacity==='1'"))
            click('.theme-card .mini-btn')
            wait("!!document.querySelector('[role=dialog]')")
            check('theme-mobile-language',not view.evaluate("document.querySelector('[role=dialog]').innerText.includes('侧边栏')"))
            view.evaluate("document.querySelector('.bg-remove')?.click()")
            click('.bg-drop-zone')
            chooser('background-image-picker',r'documentsui')
            shell('input','keyevent','KEYCODE_BACK')
            wait("!document.querySelector('[role=dialog]')")
            view.evaluate("location.hash='/downloads'")
            wait("!!document.querySelector('.downloads-page')")
            check('downloads-no-arbitrary-path',not view.evaluate("!!document.querySelector('.download-settings-block input')"))
            view.screenshot(args.output/'downloads.png')
            completed = True
        finally:
            view.call('Emulation.clearDeviceMetricsOverride')
            output=dict(passed=completed and bool(checks) and all(c['passed'] for c in checks),completed=completed,checks=checks,pages=pages)
            (args.output/'result.json').write_text(json.dumps(output,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    assert all(c['passed'] for c in checks),[c['name'] for c in checks if not c['passed']]


if __name__ == '__main__': main()
