"""Select and read a disposable JSON through Android's real document picker.

Only the acceptance application is used. No backup is imported. Exact fixture
files are removed; no app data or shared-storage directories are cleared.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import time
import xml.etree.ElementTree as ET
from android_webview import AndroidWebView


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device',default='emulator-5554')
    parser.add_argument('--output',type=Path,required=True)
    args=parser.parse_args()
    args.output.mkdir(parents=True,exist_ok=True)
    name='cczj-settings-audit-'+str(time.time_ns())+'.json'
    local=args.output/name
    payload='{"cczj_file_input":"readable"}'
    local.write_text(payload,encoding='utf-8')
    remote='/sdcard/Download/'+name
    dump='/data/local/tmp/cczj-settings-file-input.xml'
    adb=['adb','-s',args.device]
    def native(*values):return subprocess.check_output(adb+list(values),text=True,encoding='utf-8',errors='replace',timeout=30)
    def nodes():
        native('shell','rm','-f',dump)
        for attempt in range(3):
            try:
                native('shell','uiautomator','dump',dump)
                break
            except subprocess.CalledProcessError as error:
                print('uiautomator attempt failed: '+str(error.output),flush=True)
                # MuMu's utility can exit 139 after successfully writing XML.
                # Only accept a newly created, parseable hierarchy, never stale data.
                if 'UI hierchary dumped to:' in error.output:break
                if attempt==2:raise
                time.sleep(1)
        return list(ET.fromstring(native('shell','cat',dump)).iter('node'))
    def tap(node):
        x1,y1,x2,y2=map(int,re.findall(r'\d+',node.attrib['bounds']))
        native('shell','input','tap',str((x1+x2)//2),str((y1+y2)//2));time.sleep(.35)
    result=dict(passed=False,fixture=name)
    try:
        native('push',str(local),remote)
        native('shell','am','force-stop','com.cczj.video.acceptance')
        native('shell','am','start','-W','-n','com.cczj.video.acceptance/com.wails.app.MainActivity')
        time.sleep(1)
        with AndroidWebView(args.device,application_id='com.cczj.video.acceptance') as view:
            end=time.monotonic()+15
            while not view.evaluate("!!document.querySelector('#app')?.__vue_app__"):
                assert time.monotonic()<end
                time.sleep(.1)
            view.evaluate("location.hash='/settings?group=advanced'")
            end=time.monotonic()+10
            while not view.evaluate("!!document.querySelector('.settings-group-active input[type=file]')"):
                assert time.monotonic()<end
                time.sleep(.1)
            view.evaluate('''(()=>{
                const input=document.querySelector('.settings-group-active input[type=file]');
                input.addEventListener('change',()=>{const file=input.files[0];if(!file)return;const reader=new FileReader();reader.onload=()=>window.__cczjFileRead={name:file.name,text:reader.result};reader.onerror=()=>window.__cczjFileRead={error:String(reader.error)};reader.readAsText(file)},{once:true,capture:true});
            })()''')
            point=view.evaluate("(()=>{const e=[...document.querySelectorAll('.settings-group-active button')].find(e=>e.innerText.trim()==='导入备份');e.scrollIntoView({block:'center',behavior:'instant'});const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()")
            view.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[point]));view.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
            time.sleep(1.5)
            current=nodes()
            selected=next((n for n in current if n.get('text')==name),None)
            if selected is None:
                nav=next(n for n in current if re.search(r'显示根目录|导航|Show roots|navigation',n.get('content-desc',''),re.I))
                tap(nav)
                current=nodes()
                downloads=next(n for n in current if re.fullmatch(r'下载|下载内容|Downloads',n.get('text',''),re.I))
                tap(downloads)
                current=nodes()
                selected=next((n for n in current if n.get('text')==name),None)
            if selected is None:
                raise AssertionError('Fixture absent from picker: '+str([n.get('text') for n in current if n.get('text')]))
            view.screenshot(args.output/'selected-document.png')
            tap(selected)
            end=time.monotonic()+10
            while not view.evaluate('window.__cczjFileRead || null'):
                assert time.monotonic()<end,'HTML file input did not receive a readable document'
                time.sleep(.1)
            result['read']=view.evaluate('window.__cczjFileRead')
            assert result['read']==dict(name=name,text=payload),result
            result['passed']=True
            # Cancel the app's import confirmation: reading bytes must not import data.
            native('shell','input','keyevent','KEYCODE_BACK')
            view.evaluate("delete window.__cczjFileRead")
            print('PASS Android document selection returns readable bytes to the HTML file input',flush=True)
    finally:
        if 'com.android.documentsui' in native('shell','dumpsys','activity','top').split('ACTIVITY')[-1]:
            native('shell','input','keyevent','KEYCODE_BACK')
        native('shell','rm','-f',remote,dump)
        (args.output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')


if __name__=='__main__':main()
