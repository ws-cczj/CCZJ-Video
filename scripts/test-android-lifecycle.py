"""Verify Activity recreation keeps the process runtime; never clears app data."""
import argparse,json,subprocess,time
from pathlib import Path
from android_webview import AndroidWebView
from video_domain_probe import probe_android_video_domain

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--device',required=True)
    parser.add_argument('--application-id',default='com.cczj.video.acceptance')
    parser.add_argument('--output',default='dist/audit/lifecycle/android')
    parser.add_argument('--check-video',action='store_true')
    args=parser.parse_args()
    if args.check_video and args.application_id!='com.cczj.video.acceptance':
        parser.error('--check-video requires the isolated acceptance package')
    out=Path(args.output);out.mkdir(parents=True,exist_ok=True)
    adb=['adb','-s',args.device]
    def run(*cmd):
        return subprocess.check_output(adb+list(cmd),text=True,encoding='utf-8',errors='replace',timeout=30).strip()
    def mounted(view):
        until=time.monotonic()+25
        while not view.evaluate("!!document.querySelector('#app')?.__vue_app__"):
            if time.monotonic()>until:raise RuntimeError('Vue did not mount')
            time.sleep(.2)
    result={'device':args.device,'package':args.application_id,'scope':'same-process Activity CLEAR_TOP recreation; no data deletion','rounds':[]}
    with AndroidWebView(args.device,application_id=args.application_id) as view:
        mounted(view)
        result['initialPID']=run('shell','pidof',args.application_id)
        result['initialURL']=view.evaluate('location.href')
        if args.check_video:result['video']=probe_android_video_domain(view)
    for attempt in range(3):
        launch=run('shell','am','start','-W','--activity-clear-top','-n',args.application_id+'/com.wails.app.MainActivity')
        with AndroidWebView(args.device,application_id=args.application_id) as view:
            mounted(view)
            pid=run('shell','pidof',args.application_id)
            assert pid==result['initialPID'],f'process changed: {result["initialPID"]} -> {pid}'
            row={'attempt':attempt+1,'pid':pid,'url':view.evaluate('location.href'),'launch':launch}
            assert row['url']==result['initialURL'],'unexpected URL after recreation'
            result['rounds'].append(row)
            view.screenshot(out/f'recreated-{attempt+1}.png')
    logs=run('logcat','-d','--pid='+result['initialPID'],'-t','500')
    (out/'native.log').write_text(logs,encoding='utf-8')
    assert 'application is running or a previous run has failed' not in logs
    result['passed']=True
    (out/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
    print(json.dumps(result,ensure_ascii=True))

if __name__=='__main__':main()
