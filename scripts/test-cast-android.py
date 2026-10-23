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
 parser.add_argument('--output',type=Path,default=ROOT/'dist/audit/cast-discovery/native')
 args=parser.parse_args();args.output.mkdir(parents=True,exist_ok=True)
 media=ROOT/'.cache/cast-validation';media.mkdir(parents=True,exist_ok=True)
 if not (media/'movie.mp4').exists():
  subprocess.run(['ffmpeg','-hide_banner','-loglevel','error','-f','lavfi','-i','testsrc2=size=320x180:rate=15','-f','lavfi','-i','anullsrc','-t','90','-c:v','libx264','-preset','ultrafast','-pix_fmt','yuv420p','-g','30','-c:a','aac','-movflags','+faststart',str(media/'movie.mp4')],check=True)
 requests=[]
 class Media(fixtures.MediaFixture):
  directory=media
  def do_GET(self,head=False):
   requests.append(dict(path=self.path,range=self.headers.get('Range',''),peer=self.client_address[0]));super().do_GET(head)
  def log_message(self,*_):pass
 server=ThreadingHTTPServer((args.host_ip,0),Media)
 threading.Thread(target=server.serve_forever,daemon=True).start()
 adb=['adb','-s',args.device]
 result=dict(passed=False,device=args.device,environment='emulator' if args.device.startswith('emulator-') else 'unclassified',receiver=args.receiver_name,checks=[],limitations=['Not a Hikvision physical-TV test.','A receiver on the same emulator does not validate a router or phone Wi-Fi driver.'])
 try:
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
    rpc('PlayCastVideo',selected['id'],url,'CCZJ DLNA acceptance')
    deadline=time.monotonic()+15
    while not requests and time.monotonic()<deadline:time.sleep(.1)
    assert requests,'Receiver accepted SOAP but never fetched video'
    result['checks'].append('SetAVTransportURI and Play accepted; receiver fetched the actual MP4')
    for action,seconds in [('Pause',0),('Play',0),('Seek',12)]:
     rpc('ControlCastVideo',selected['id'],action,seconds);time.sleep(.3)
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
