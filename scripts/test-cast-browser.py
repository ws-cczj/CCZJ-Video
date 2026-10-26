"""Production cast panel scan ownership, errors and system-settings fallback."""
import argparse,json,re
from pathlib import Path
from playwright.sync_api import sync_playwright,expect
ROOT=Path(__file__).resolve().parent.parent

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--port',type=int,default=19320)
 parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/cast-discovery/browser')
 args=parser.parse_args();args.output.mkdir(parents=True,exist_ok=True)
 names=re.findall(r'export function (\w+)\(', (ROOT/'frontend/bindings/cczjVideo/app/service/app.js').read_text('utf-8'))
 mock="""import {CancellablePromise} from '/tests/browser/runtime.ts';
 function call(name){return new CancellablePromise((resolve,reject)=>{
   if(name==='DiscoverCastDevices'){window.__scans++;window.__scanResolve=resolve;if(!window.__hold)resolve([{id:'tv',name:'LAN receiver'}])}
   else if(name==='GetCastStatus'){window.__statusResolve=resolve;window.__statusReject=reject;if(!window.__holdStatus)resolve({state:'PLAYING',transport_status:'OK',position:12,duration:90,has_position:true})}
   else resolve('')
 },()=>{window.__cancelled++})}
 """
 mock+='\n'.join('export function '+name+'(...args){return call('+json.dumps(name)+')}' for name in names)
 errors=[]
 with sync_playwright() as p:
  browser=p.chromium.launch(headless=True,channel='chrome')
  try:
   page=browser.new_page(viewport=dict(width=375,height=812),is_mobile=True,has_touch=True)
   page.on('pageerror',lambda e:errors.append(str(e)))
   page.route('**/bindings/cczjVideo/app/service/app*',lambda route:route.fulfill(body=mock,content_type='text/javascript'))
   page.route('**/src/api/runtime.ts',lambda r:r.fulfill(body="export const Window={};export const Dialogs={};export const isMobileRuntime=()=>true;export const isIOSRuntime=()=>false;export const isDesktopRuntime=()=>false;export const initializeRuntimeEnvironment=async()=>{};export const reloadApplication=url=>location.replace(url);export const closeMobileApplication=()=>{};",content_type='text/javascript'))
   page.add_init_script("window.__scans=0;window.__cancelled=0;window.__locks=[];window.wails={playerState:()=>JSON.stringify({pip:true,volume:.5,brightness:.5,castSettings:true}),playerMulticast:enabled=>{if(window.__lockFails&&enabled)throw new Error('multicast denied');window.__locks.push(enabled)},playerOpenCastSettings:()=>{window.__settingsCalls=(window.__settingsCalls||0)+1;return window.__systemFails?'UNSUPPORTED':''}}")
   page.goto(f'http://127.0.0.1:{args.port}/tests/browser/cast.html')
   page.wait_for_load_state('networkidle')
   assert not errors,errors
   page.get_by_role('button',name='Open cast',exact=True).click()
   expect(page.locator('.cczj-mobile-cast-list')).to_contain_text('LAN receiver')
   assert page.evaluate('window.__scans===1&&window.__locks.at(-1)===false')
   page.get_by_role('button',name='关闭',exact=True).click()
   page.evaluate('window.__hold=true')
   page.get_by_role('button',name='Open cast',exact=True).click()
   expect(page.get_by_role('button',name='搜索设备中…',exact=True)).to_be_disabled()
   page.get_by_role('button',name='关闭',exact=True).click()
   assert page.evaluate('window.__cancelled===1&&window.__locks.at(-1)===false')
   page.evaluate('window.__hold=false;window.__scanResolve([{id:"late",name:"stale device"}])')
   page.get_by_role('button',name='Open cast',exact=True).click()
   expect(page.locator('.cczj-mobile-cast-list')).not_to_contain_text('stale device')
   page.evaluate('window.__systemFails=true')
   page.get_by_role('button',name='打开系统无线投屏',exact=True).click()
   expect(page.get_by_role('alert')).to_contain_text('未能打开系统投屏设置')
   page.evaluate('window.__systemFails=false')
   page.get_by_role('button',name='打开系统无线投屏',exact=True).click()
   expect(page.get_by_role('dialog')).to_have_count(0)
   page.evaluate('window.__lockFails=true')
   page.get_by_role('button',name='Open cast',exact=True).click()
   expect(page.get_by_role('alert')).to_contain_text('操作失败，请查看日志')
   expect(page.get_by_role('button',name='刷新',exact=True)).to_be_enabled()
   page.evaluate('window.__lockFails=false')
   page.get_by_role('button',name='刷新',exact=True).click()
   expect(page.locator('.cczj-mobile-cast-list')).to_contain_text('LAN receiver')
   page.screenshot(path=str(args.output/'cast-mobile.png'))
   page.evaluate('(()=>{const theme=document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("theme");theme.currentId="d-blue";theme.apply()})()')
   page.wait_for_timeout(350)
   page.screenshot(path=str(args.output/'cast-mobile-dark.png'))
   page.evaluate('(()=>{const theme=document.querySelector("#app").__vue_app__.config.globalProperties.$pinia._s.get("theme");theme.currentId="green";theme.apply()})()')
   assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
   page.set_viewport_size(dict(width=812,height=375))
   page.screenshot(path=str(args.output/'cast-landscape.png'))
   assert page.evaluate('document.documentElement.scrollWidth<=innerWidth+1')
   page.evaluate('window.__holdStatus=true')
   page.get_by_role('button',name='LAN receiver',exact=True).click()
   assert page.evaluate('(window.__connected||0)===0'),'SOAP acknowledgement paused local playback'
   expect(page.get_by_role('status')).to_contain_text('正在确认电视播放状态')
   expect(page.get_by_text('电视已确认播放',exact=True)).to_have_count(0)
   page.evaluate('window.__firstStatus=window.__statusResolve;window.__statusResolve({state:"PLAYING",transport_status:"OK",position:0,duration:0,has_position:true})')
   expect(page.get_by_text('电视已确认播放',exact=True)).to_have_count(0)
   page.wait_for_function('window.__statusResolve!==window.__firstStatus')
   page.evaluate('window.__errorStatus=window.__statusResolve;window.__statusResolve({state:"PLAYING",transport_status:"ERROR_OCCURRED",position:12,duration:90,has_position:true})')
   expect(page.get_by_text('电视报告媒体播放错误，请检查格式或更换线路。',exact=True)).to_be_visible()
   assert page.evaluate('(window.__connected||0)===0'),'receiver media error paused local playback'
   page.wait_for_function('window.__statusResolve!==window.__errorStatus')
   page.evaluate('window.__statusResolve({state:"PLAYING",transport_status:"OK",position:12,duration:90,has_position:true})')
   expect(page.get_by_text('电视已确认播放',exact=True)).to_be_visible()
   assert page.evaluate('window.__connected===1'),'confirmed playback did not notify the local player'
   expect(page.get_by_text('00:12 / 01:30',exact=True)).to_be_visible()
   page.get_by_role('button',name='暂停',exact=True).click()
   page.wait_for_timeout(100)
   page.evaluate('window.__statusReject(new Error("receiver unreachable"))')
   expect(page.get_by_text('电视已确认播放',exact=True)).to_have_count(0)
   expect(page.get_by_text(re.compile('无法取得电视状态'))).to_be_visible()
   before=page.evaluate('window.__cancelled')
   page.get_by_role('button',name='关闭',exact=True).click()
   page.wait_for_timeout(3200)
   assert page.evaluate('window.__cancelled')==before,'polling continued after closing'
   assert not errors,errors
   result=dict(passed=True,native=False,checks=['scan releases multicast lock','closing cancels the actual request and rejects stale results','system settings success and failure recover','multicast preparation error releases scanning state and can retry','portrait and landscape layout','SOAP acknowledgement does not claim playback before receiver status','status failures clear stale playback confirmation and closing stops polling'],errors=errors)
   (args.output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),'utf-8');print(json.dumps(result,ensure_ascii=False),flush=True)
  finally:browser.close()
if __name__=='__main__':main()
