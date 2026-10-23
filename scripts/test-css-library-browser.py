"""Browser checks for the shared style library, real UI components and themes.

Run through the webapp-testing with_server helper or an existing Vite server.
Only the Wails transport is replaced; styles and components are production code.
"""
import argparse
import json
from pathlib import Path
import re
from playwright.sync_api import sync_playwright, expect

ROOT=Path(__file__).resolve().parent.parent


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url',default='http://127.0.0.1:9310')
    parser.add_argument('--channel',default='msedge')
    parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/css-library')
    args=parser.parse_args()
    output=args.output
    output.mkdir(parents=True,exist_ok=True)
    names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8'))
    mock="import {CancellablePromise} from '/tests/browser/runtime.ts';\n"
    mock+='\n'.join('export function '+name+'(...args){return CancellablePromise.resolve('+('[]' if name.startswith(('List','GetAll')) else "''")+')}' for name in names)
    errors=[]
    records=[]
    with sync_playwright() as p:
        browser=p.chromium.launch(headless=True,channel=args.channel)
        try:
            page=browser.new_page(viewport=dict(width=1280,height=900),reduced_motion='reduce')
            page.on('pageerror',lambda error:errors.append(str(error)))
            page.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=mock,content_type='text/javascript'))
            page.goto(args.url+'/tests/browser/style-library.html')
            page.wait_for_load_state('networkidle')
            expect(page.get_by_role('heading',name='CCZJ 样式复用验收')).to_be_visible()
            for theme in ['green','d-blue']:
                page.evaluate('(theme)=>{window.styleLibrary.theme.currentId=theme;window.styleLibrary.theme.apply()}',theme)
                for mode in ['video','novel','comic']:
                    page.evaluate('(mode)=>window.styleLibrary.state.mode=mode',mode)
                    page.wait_for_timeout(50)
                    probes=page.locator('[data-probe]').evaluate_all('nodes=>nodes.map(node=>{const s=getComputedStyle(node);return Object.fromEntries(["display","alignItems","gap","borderRadius","borderTopColor","color"].map(key=>[key,s[key]]))})')
                    assert probes[0]==probes[1],dict(theme=theme,mode=mode,probes=probes)
                    assert probes[0]['display']=='flex' and probes[0]['gap']=='8px',probes
                    expect(page.locator('.spinner-wrap .label')).to_have_text('加载内容')
                    expect(page.get_by_role('heading',name='暂无内容')).to_be_visible()
                    primary=page.locator('[data-test=primary]')
                    # Inline flex boxes become flex when they are flex items.
                    assert primary.evaluate('e=>getComputedStyle(e).display')=='flex'
                    expect(primary).to_have_class(re.compile(r'cczj-inline-flex'))
                    assert page.locator('[data-test=block]').evaluate('e=>getComputedStyle(e).display')=='flex'
                    for width,height in [(1280,900),(375,812),(812,375)]:
                        page.set_viewport_size(dict(width=width,height=height))
                        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth'),dict(mode=mode,width=width)
                    page.screenshot(path=str(output/(theme+'-'+mode+'.png')),animations='disabled',full_page=True)
                    records.append(dict(theme=theme,mode=mode,passed=True))
            page.set_viewport_size(dict(width=375,height=812))
            page.evaluate('document.documentElement.style.fontSize="24px"')
            assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
            page.get_by_role('combobox').click()
            page.get_by_role('option',name='第二个选项').click()
            assert page.evaluate('window.styleLibrary.state.selected')=='two'
            page.locator('[data-test=open-modal]').click()
            dialog=page.get_by_role('dialog')
            expect(dialog).to_be_visible()
            confirm=dialog.locator('.ui-btn--primary')
            expect(confirm).to_have_accessible_name('确定')
            # A shared Button owns its content and disabled/loading behaviour.
            expect(confirm.locator('.ui-btn__content')).to_have_count(1)
            width=confirm.bounding_box()['width']
            page.evaluate('window.styleLibrary.state.loading=true')
            expect(confirm).to_be_disabled()
            assert abs(confirm.bounding_box()['width']-width)<1
            page.evaluate('window.styleLibrary.state.loading=false')
            confirm.click()
            assert page.evaluate('window.styleLibrary.state.confirmed')==1
            page.keyboard.press('Escape')
            expect(dialog).to_have_count(0)
            page.locator('[data-test=open-custom-modal]').click()
            custom=page.get_by_role('dialog',name='自定义操作')
            custom.get_by_role('button',name='保存配置').click(timeout=2000)
            expect(custom).to_have_count(0)
            assert not errors,errors
            (output/'result.json').write_text(json.dumps(dict(passed=True,native=False,records=records,checks=['template/scoped composition equivalence','primary and block button layout','375px and landscape layouts','light/dark palettes','select interaction','shared modal buttons and stable loading width','custom modal footer slot','24px base font','Escape dismiss','reduced motion'],errors=errors),ensure_ascii=False,indent=2),encoding='utf-8')
            print('CCZJ CSS browser checks passed: 6 theme/mode combinations, responsive layouts and shared controls.')
        except Exception:
            if 'page' in locals():
                page.screenshot(path=str(output/'failure.png'),full_page=True)
                (output/'failure.json').write_text(json.dumps(dict(errors=errors,text=page.locator('body').inner_text()),ensure_ascii=False,indent=2),encoding='utf-8')
            raise
        finally:
            browser.close()


if __name__=='__main__':main()
