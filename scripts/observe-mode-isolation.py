"""Observe actual Android mode lifetimes and document-transition timing, without changing content data."""
import importlib.util,json,sys,time,subprocess
from pathlib import Path
from android_webview import AndroidWebView
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('collector',ROOT/'scripts/collect-android-diagnostics.py')
collector=importlib.util.module_from_spec(spec);spec.loader.exec_module(collector)
output=ROOT/'reports/mode-isolation-2026-10-19'
output.mkdir(exist_ok=True)
probe="""(()=>{const out=window.__modeObservation={shell_ms:null,splash_removed_ms:null};function tick(){const main=document.querySelector('main');if(out.shell_ms===null&&main&&main.childElementCount)out.shell_ms=performance.now();if(out.shell_ms!==null&&!document.querySelector('.splash-wrap')&&out.splash_removed_ms===null)out.splash_removed_ms=performance.now();if(performance.now()<7000)requestAnimationFrame(tick)}requestAnimationFrame(tick)})()"""
with AndroidWebView() as d:
 def binding(name,*args):
  path,alias=collector.compiled_binding(name)
  return d.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return await m['+json.dumps(alias)+']('+','.join(json.dumps(a) for a in args)+')})()')
 original=binding('GetSetting','application_mode') or 'video'
 d.call('Page.enable')
 observer=d.call('Page.addScriptToEvaluateOnNewDocument',{'source':probe})['identifier']
 result={'environment':'MuMu Android 15 / actual preview APK','timing_scope':'warm document reload, shell DOM and splash removal; not content/network completion','modes':[]}
 recording=None
 try:
  for mode in ['novel','comic','video']:
   before=time.perf_counter();binding('SetSetting','application_mode',mode);native_ms=(time.perf_counter()-before)*1000
   target=d.evaluate('(()=>{const u=new URL(location.href);u.searchParams.set("application-mode",'+json.dumps(mode)+');u.hash='+json.dumps('/' if mode=='video' else '/'+mode)+';return u.href})()')
   d.call('Page.navigate',{'url':target})
   time.sleep(3.8)
   row={'mode':mode,'native_switch_ms':round(native_ms,2),'document':d.evaluate('({timing:window.__modeObservation,mode:document.querySelector("[data-application-mode]")?.dataset.applicationMode,heap:performance.memory?.usedJSHeapSize,video_count:document.querySelectorAll("video").length})'),'metrics':binding('GetRuntimeMetrics'),'diagnostics':binding('GetDiagnostics')}
   if mode!='video':
    d.evaluate('location.hash='+json.dumps('/'+mode+'/diagnostics'))
    time.sleep(1.5)
    row['panel']=d.evaluate('({tabs:Array.from(document.querySelectorAll(".cczj-reading-diagnostics [data-section]")).map(e=>e.dataset.section),video_modules:performance.getEntriesByType("resource").map(e=>e.name).filter(n=>/VideoDiagnosticsPanel|tsCache|Player-|hls-/.test(n)),text:document.querySelector(".cczj-reading-diagnostics")?.innerText})')
    d.screenshot(output/(mode+'-diagnostics.png'))
    assert row['panel']['tabs']==['overview','startup','activity','network','storage'],row['panel']
    assert not row['panel']['video_modules'],row['panel']['video_modules']
    assert not row['metrics'].get('download_executor') and not row['metrics']['background_tasks'],row['metrics']['background_tasks']
    assert 'douban' not in row['diagnostics'] and 'collect' not in row['diagnostics'],row['diagnostics']
    if mode=='novel':recording=binding('StartDiagnosticsRecording')
    else:
     row['previous_recording_visible']=binding('GetDiagnosticsRecordingStatus')
     assert not row['previous_recording_visible']['path']
   result['modes'].append(row)
  if recording:
   raw=subprocess.check_output(['adb','-s','emulator-5554','exec-out','run-as','com.cczj.video.preview','cat',recording['path']])
   (output/'novel-recording.jsonl').write_bytes(raw)
   result['recording_end']=json.loads(raw.splitlines()[-1])
   assert result['recording_end'].get('status',{}).get('reason')=='mode_changed',result['recording_end']
 finally:
  binding('SetSetting','application_mode',original)
  d.call('Page.removeScriptToEvaluateOnNewDocument',{'identifier':observer})
  home='/' if original=='video' else '/'+original
  d.call('Page.navigate',{'url':'https://wails.localhost/?application-mode='+original+'#'+home})
  (output/'observation.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8')
 print(json.dumps({'modes':[{'mode':r['mode'],'native_switch_ms':r['native_switch_ms'],'document':r['document']}for r in result['modes']],'recording_end':result.get('recording_end')},ensure_ascii=True))
