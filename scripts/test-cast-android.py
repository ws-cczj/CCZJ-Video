"""Native DLNA discovery and playback against an independently installed receiver.

Install official Kodi and enable UPnP support / remote control first. Requires
the isolated acceptance APK. Media uses the existing range-capable fixture;
discovery packets, descriptions, Go bindings and receiver responses are real.
"""
import argparse,importlib.util,json,subprocess,threading,time
from pathlib import Path
from http.server import ThreadingHTTPServer
from android_webview import AndroidWebView

ROOT=Path(__file__).resolve().parent.parent
def module(name,path):
 spec=importlib.util.spec_from_file_location(name,ROOT/path);result=importlib.util.module_from_spec(spec);spec.loader.exec_module(result);return result
compiled=module('cast_compiled','scripts/collect-android-diagnostics.py')
fixtures=module('cast_media','scripts/test-upgrade-player-android.py')

def main():
 parser=argparse.ArgumentParser(description=__doc__)
 parser.add_argument('--device',default='emulator-5554')
 parser.add_argument('--host-ip',required=True,help='LAN IPv4 reachable by the receiver; not the emulator host alias')
 parser.add_argument('--receiver-name',default='Kodi')
 parser.add_argument('--ui',action='store_true',help='Also connect through the actual mobile player panel using a disposable source')
 parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/cast-discovery/native')
 args=parser.parse_args();args.output.mkdir(parents=True,exist_ok=True)
 media=ROOT/'.cache/cast-validation';media.mkdir(parents=True,exist_ok=True)
 if not (media/'movie.mp4').exists():
  subprocess.run(['ffmpeg','-hide_banner','-loglevel','error','-f','lavfi','-i','testsrc2=size=320x180:rate=15','-f','lavfi','-i','anullsrc','-t','90','-c:v','libx264','-preset','ultrafast','-pix_fmt','yuv420p','-g','30','-c:a','aac','-movflags','+faststart',str(media/'movie.mp4')],check=True)
 requests=[]
 identity=str(time.time_ns())
 class Media(fixtures.MediaFixture):
  directory=media
  def do_GET(self,head=False):
   if self.path.startswith('/cms'):
    row=dict(vod_id=identity,vod_name='CCZJ casting audit '+identity,type_id='1',type_name='剧情片',vod_play_from='DLNA',vod_play_url=f'第01集$http://{args.host_ip}:{self.server.server_port}/media/movie.mp4')
    data=json.dumps(dict(code=1,page=1,pagecount=1,total=1,list=[row]),ensure_ascii=False).encode()
    self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data);return
   requests.append(dict(path=self.path,range=self.headers.get('Range',''),peer=self.client_address[0]));super().do_GET(head)
  def log_message(self,*_):pass
 server=ThreadingHTTPServer((args.host_ip,0),Media)
 threading.Thread(target=server.serve_forever,daemon=True).start()
 adb=['adb','-s',args.device]
 result=dict(passed=False,device=args.device,environment='emulator' if args.device.startswith('emulator-') else 'unclassified',receiver=args.receiver_name,checks=[],limitations=['Not a Hikvision physical-TV test.','A receiver on the same emulator does not validate a router or phone Wi-Fi driver.'])
 try:
  if args.receiver_name=='Kodi':
   # MuMu may freeze the background receiver after inactivity. Bring its
   # real receiving surface up before moving to the sender for discovery.
   subprocess.run(adb+['shell','am','start','-W','-n','org.xbmc.kodi/.Splash'],check=True,capture_output=True)
   time.sleep(1)
  with AndroidWebView(args.device,application_id='com.cczj.video.acceptance') as view:
   def rpc(name,*values):
    path,alias=compiled.compiled_binding(name)
    return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return await m['+json.dumps(alias)+'](...'+json.dumps(values)+')})()')
   selected=None
   try:
    view.evaluate('window.wails.playerMulticast(true)')
    try:devices=rpc('DiscoverCastDevices')
    finally:view.evaluate('window.wails.playerMulticast(false)')
    result['devices']=devices
    selected=next((device for device in devices if args.receiver_name in device['name']),None)
    assert selected,'Independent receiver not discovered: '+str(devices)
    assert len({device['id'] for device in devices})==len(devices)
    result['checks'].append('multicast discovery and validated HTTP description return the independent receiver once')
    url=f'http://{args.host_ip}:{server.server_port}/media/movie.mp4'
    if args.receiver_name=='Kodi':
     # Android needs a foreground receiver surface to create its video decoder.
     subprocess.run(adb+['shell','am','start','-n','org.xbmc.kodi/.Splash'],check=True,capture_output=True)
    rpc('PlayCastVideo',selected['id'],url,'CCZJ DLNA acceptance')
    deadline=time.monotonic()+15
    while not requests and time.monotonic()<deadline:time.sleep(.1)
    assert requests,'Receiver accepted SOAP but never fetched video'
    result['checks'].append('SetAVTransportURI and Play accepted; receiver fetched the actual MP4')
    deadline=time.monotonic()+15
    status={}
    while time.monotonic()<deadline:
     status=rpc('GetCastStatus',selected['id'])
     if status['state']=='PLAYING' and status['has_position'] and status['position']>0 and status['duration']>0:break
     time.sleep(.3)
    assert status.get('state')=='PLAYING' and status.get('position',0)>0 and status.get('duration',0)>0,status
    result['receiver_status']=status
    result['checks'].append('receiver GetTransportInfo confirms PLAYING and returns actual playback position')

    for action,seconds in [('Pause',0),('Play',0),('Seek',12)]:
     rpc('ControlCastVideo',selected['id'],action,seconds)
     deadline=time.monotonic()+6
     while True:
      state=rpc('GetCastStatus',selected['id'])
      if (action=='Pause' and state['state']=='PAUSED_PLAYBACK') or (action=='Play' and state['state']=='PLAYING') or (action=='Seek' and state['has_position'] and 9<=state['position']<=18):break
      if time.monotonic()>=deadline:break
      time.sleep(.2)
     if action=='Pause':assert state['state']=='PAUSED_PLAYBACK',state
     elif action=='Play':assert state['state']=='PLAYING',state
     elif state['has_position']:assert 9<=state['position']<=18,state
    result['checks'].append('Pause, resume and seek accepted by receiver')
    if args.receiver_name=='Kodi':
     subprocess.run(adb+['shell','am','start','-n','org.xbmc.kodi/.Splash'],check=True,capture_output=True);time.sleep(1)
     view.screenshot(args.output/'receiver-playing.png')
     log=subprocess.check_output(adb+['shell','cat','/sdcard/Android/data/org.xbmc.kodi/files/.kodi/temp/kodi.log']).decode(errors='replace')
     (args.output/'receiver-log.txt').write_text(log,'utf-8')
     assert 'movie.mp4' in log and 'VideoPlayer::OpenFile' in log,'Kodi did not open video'
    rpc('ControlCastVideo',selected['id'],'Stop',0);selected=None
    result['checks'].append('Stop accepted by receiver')
    # Launches only the system settings entry; it does not claim Miracast
    # hardware availability or a successful physical TV connection.
    subprocess.run(adb+['shell','am','start','-n','com.cczj.video.acceptance/com.wails.app.MainActivity'],check=True,capture_output=True)
    state=view.evaluate('JSON.parse(window.wails.playerState())')
    if state.get('castSettings'):
     assert view.evaluate('window.wails.playerOpenCastSettings()')==''
     time.sleep(.4)
     activities=subprocess.check_output(adb+['shell','dumpsys','activity','activities']).decode(errors='replace')
     assert 'act=android.settings.CAST_SETTINGS' in activities and 'topResumedActivity=' in activities,'Cast settings launch did not reach system UI'
     result['checks'].append('native system casting settings entry opens successfully')
     view.screenshot(args.output/'system-cast-settings.png')
     # Settings can sit above MainActivity in the same task. Starting the
     # launcher brings that whole task forward; use the user's Back action.
     subprocess.run(adb+['shell','input','keyevent','KEYCODE_BACK'],check=True,capture_output=True)
     subprocess.run(adb+['shell','am','start','-W','-n','com.cczj.video.acceptance/com.wails.app.MainActivity'],check=True,capture_output=True)
     assert view.evaluate('JSON.parse(window.wails.playerState()).castSettings') is True,'Native bridge did not recover after system settings'
     view.evaluate('window.wails.playerMulticast(true)')
     try:restored=rpc('DiscoverCastDevices')
     finally:view.evaluate('window.wails.playerMulticast(false)')
     assert any(device['id']==result['devices'][0]['id'] for device in restored),'Discovery did not recover after system settings'
     result['checks'].append('Back from system settings resumes the native bridge and discovers the receiver again')
    if args.ui:
     original_url=view.evaluate('location.href');original_private=rpc('GetAllowPrivateNetwork');source=None;enabled=[]
     def wait(expression,timeout=20):
      deadline=time.monotonic()+timeout
      while not view.evaluate(expression):
       assert time.monotonic()<deadline,expression
       time.sleep(.1)
     def tap(expression):
      point=view.evaluate('(()=>{const e='+expression+';if(!e)throw Error("missing touch target");const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()')
      view.call('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[point]));view.call('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]))
     try:
      rpc('SetAllowPrivateNetwork',True)
      enabled=[item for item in rpc('GetAllSources') if item['enabled']]
      for item in enabled:rpc('UpdateSource',dict(item,enabled=0))
      source=rpc('CreateSource',dict(name='Casting audit '+identity,api_url=f'http://{args.host_ip}:{server.server_port}/cms',enabled=1,kind='video'))
      key=source['source_key'];search=rpc('SearchSourceRequest',key,'CCZJ casting audit',1,20,'cast-ui-'+identity)
      rpc('ImportSourceVideos',key,search['videos']);global_id=rpc('GetGlobalIdForVideo',key,identity)
      view.evaluate('location.hash='+json.dumps(f'/player/{key}/{global_id}/0?vod={identity}'))
      wait('!!document.querySelector(".cczj-mobile-video video")')
      wait('document.querySelector("video").readyState>=2')
      view.evaluate('document.querySelector("video").play()');wait('document.querySelector("video").currentTime>.2')
      if not view.evaluate('document.querySelector(".cczj-mobile-video").classList.contains("show-controls")'):
       tap('document.querySelector("video")')
      wait('document.querySelector(".cczj-mobile-video").classList.contains("show-controls")');time.sleep(.35)
      tap('document.querySelector(".cczj-mobile-player-more-button")')
      wait('!!document.querySelector(".cczj-mobile-player-categories")');time.sleep(.35)
      tap('[...document.querySelectorAll(".cczj-mobile-player-categories button")].at(-1)')
      cast_button='document.querySelector('+json.dumps('.cczj-mobile-more-actions button[aria-label="投屏到电视"]')+')'
      wait('!!'+cast_button);time.sleep(.35)
      tap(cast_button)
      wait('document.querySelector(".cczj-mobile-cast-list button")?.innerText.includes("Kodi")')
      view.screenshot(args.output/'mobile-device-list.png')
      assert view.evaluate('!document.querySelector(".cczj-mobile-player-lines svg")'),'route switch still has an icon'
      assert view.evaluate('document.querySelector(".cczj-mobile-cast-help").open===false'),'help text is not collapsed'
      before_requests=len(requests)
      tap('document.querySelector(".cczj-mobile-cast-list button")')
      subprocess.run(adb+['shell','am','start','-W','-n','org.xbmc.kodi/.Splash'],check=True,capture_output=True)
      wait('document.querySelector(".cczj-mobile-cast-session")?.innerText.includes("电视已确认播放")',25)
      assert len(requests)>before_requests,'UI connection did not make the receiver fetch video'
      wait('document.querySelector("video").paused')
      result['checks'].append('actual mobile player casts through the device-list touch action; receiver playback is confirmed and local playback pauses only afterwards')
      subprocess.run(adb+['shell','am','start','-W','-n','com.cczj.video.acceptance/com.wails.app.MainActivity'],check=True,capture_output=True)
      wait('document.querySelector("video").paused')
      wait('document.querySelector(".cczj-mobile-play")?.getAttribute("aria-label")==="播放"')
      time.sleep(.5);view.screenshot(args.output/'mobile-connected.png')
      stop_button='[...document.querySelectorAll(".cczj-mobile-cast-actions button")].find(e=>e.innerText.trim()==="停止投屏")'
      tap(stop_button);wait('!document.querySelector(".cczj-mobile-cast-session")')
      result['checks'].append('actual mobile panel stops casting; help stays collapsed and the route switch contains text only')
      tap('document.querySelector(".cczj-mobile-cast").closest("section").querySelector("header button")')
      if not view.evaluate('document.querySelector(".cczj-mobile-video").classList.contains("show-controls")'):tap('document.querySelector("video")')
      wait('document.querySelector(".cczj-mobile-video").classList.contains("show-controls")');time.sleep(.35)
      tap('document.querySelector(".cczj-mobile-play")');wait('!document.querySelector("video").paused')
      result['checks'].append('returning to the sender keeps local video paused during casting; stopping releases casting and local playback can resume')
      view.evaluate('location.hash="/"');wait('!!document.querySelector(".cczj-mobile-navigation")');time.sleep(.35)
      assert view.evaluate('(()=>{const s=getComputedStyle(document.querySelector(".cczj-mobile-navigation"),"::before");return (s.getPropertyValue("mask-image")||s.getPropertyValue("-webkit-mask-image")||"none")==="none"})()'),'navigation still has a search cutout'
      view.screenshot(args.output/'mobile-navigation.png')
      result['checks'].append('mobile navigation search cutout is closed while the search action remains in place')
     finally:
      view.evaluate('location.href='+json.dumps(original_url))
      if source:rpc('DeleteSource',source['source_key'])
      for item in enabled:rpc('UpdateSource',item)
      rpc('SetAllowPrivateNetwork',original_private)
    result['passed']=True
   finally:
    if selected:
     try:rpc('ControlCastVideo',selected['id'],'Stop',0)
     except Exception:pass
    subprocess.run(adb+['shell','am','start','-n','com.cczj.video.acceptance/com.wails.app.MainActivity'],capture_output=True)
 finally:
  server.shutdown();server.server_close()
  result['mediaRequests']=requests
  (args.output/'result.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),'utf-8')
 print(json.dumps(result,ensure_ascii=False),flush=True)
if __name__=='__main__':main()
