"""Observe a user-authorized document reload on an already running Android app.

Captures rendered frames and splash opacity, without changing settings or data.
"""
import argparse
import base64
import json
from pathlib import Path
import subprocess
import time
from urllib.request import urlopen
import websocket


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--adb', default='adb')
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--package', default='com.cczj.video.preview')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    adb = [args.adb, '-s', args.device]
    pid = subprocess.check_output(adb+['shell', 'pidof', args.package], text=True).strip()
    port = subprocess.check_output(adb+['forward', 'tcp:0', 'localabstract:webview_devtools_remote_'+pid], text=True).strip()
    socket = None
    frames = []
    identifier = 0
    def send(method, params=None):
        nonlocal identifier
        identifier += 1
        socket.send(json.dumps(dict(id=identifier, method=method, params=params or {})))
        return identifier
    def receive_until(expected):
        while True:
            row = json.loads(socket.recv())
            if row.get('method') == 'Page.screencastFrame':
                p = row['params']
                name = 'frame-%03d.jpg' % len(frames)
                (args.output/name).write_bytes(base64.b64decode(p['data']))
                frames.append(dict(file=name, **p['metadata']))
                send('Page.screencastFrameAck', {'sessionId':p['sessionId']})
            if row.get('id') == expected:
                if 'error' in row: raise RuntimeError(row['error'])
                return row.get('result', {})
    try:
        tabs = json.load(urlopen('http://127.0.0.1:'+port+'/json/list', timeout=10))
        tab = next(t for t in tabs if t.get('url', '').startswith('https://wails.localhost'))
        socket = websocket.create_connection(tab['webSocketDebuggerUrl'], timeout=15, origin='http://localhost')
        receive_until(send('Page.enable'))
        receive_until(send('Page.addScriptToEvaluateOnNewDocument', {'source': '''
          window.__startupFrames=[];
          const begin=performance.now();
          function observe(){
            const image=document.querySelector('.splash-img'), splash=document.querySelector('.splash-wrap');
            const data={ms:performance.now(),initial:!!document.querySelector('.initial-boot'),
              splash:!!splash,wrapperOpacity:splash?getComputedStyle(splash).opacity:null,
              image:!!image,ready:!!image?.classList.contains('ready'),
              imageOpacity:image?getComputedStyle(image).opacity:null,
              imageTransition:image?getComputedStyle(image).transitionDuration:null,
              motion:document.documentElement?.dataset.cczjMotion};
            window.__startupFrames.push(data);
            if(performance.now()-begin<4500)requestAnimationFrame(observe);
          } requestAnimationFrame(observe);
        '''}))
        receive_until(send('Page.startScreencast', {'format':'jpeg', 'quality':75, 'maxWidth':432, 'maxHeight':768, 'everyNthFrame':1}))
        receive_until(send('Page.reload', {'ignoreCache':False}))
        # CDP keeps delivering rendered frames while the awaited in-page timer runs.
        result = receive_until(send('Runtime.evaluate', {'expression':'new Promise(resolve=>setTimeout(()=>resolve(window.__startupFrames||[]),5000))','awaitPromise':True,'returnByValue':True}))
        samples = result.get('result', {}).get('value', [])
        receive_until(send('Page.stopScreencast'))
        result = dict(package=args.package, url=tab['url'], observation='Document reload, not a cold native launch', samples=samples, frames=frames)
        (args.output/'observation.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        visible=[s for s in samples if s['image']]
        print(json.dumps(dict(output=str(args.output), frames=len(frames), samples=len(samples),
            first_image=visible[:1], intermediate_image_frames=sum(0<float(s['imageOpacity'])<.89 for s in visible)), ensure_ascii=True))
    finally:
        if socket:socket.close()
        subprocess.run(adb+['forward','--remove','tcp:'+port], capture_output=True)


if __name__ == '__main__': main()
