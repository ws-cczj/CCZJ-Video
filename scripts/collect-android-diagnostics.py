"""Read diagnostics from an already running CCZJ Android debug host.

No navigation, playback, input simulation, screenshots or pass/fail assertions.
Calls existing diagnostic bindings in the running WebView and optionally exports
the native report. Requires adb and websocket-client; release hosts disable CDP.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import time
from urllib.request import urlopen

import websocket


ROOT = Path(__file__).resolve().parent.parent


def compiled_binding(name):
    bindings = (ROOT / 'frontend/bindings/cczjVideo/app/service/app.js').read_text(encoding='utf-8')
    method = re.search(r'export function ' + re.escape(name) + r'\([^)]*\)\s*\{\s*return \$Call.ByID\((\d+)', bindings)
    if not method:
        raise ValueError(f'Cannot locate diagnostic binding {name}')
    for path in (ROOT / 'frontend/dist/assets').glob('app-*.js'):
        source = path.read_text(encoding='utf-8')
        local = re.search(r'function ([\w$]+)\([^)]*\)\{return [\w$]+\(' + method[1] + r'[,)]', source)
        if not local:
            continue
        # New builds export the policy wrapper, not the raw generated binding.
        # Keep diagnostic/test calls on the same request path as the actual UI.
        wrapped = re.search(r'([\w$]+)=[\w$]+\([\x22\x27\x60]' + re.escape(name) +
                            r'[\x22\x27\x60],' + re.escape(local[1]) + r'\)', source)
        symbol = wrapped[1] if wrapped else local[1]
        exported = re.search(r'(?:\{|,)' + re.escape(symbol) + r' as ([\w$]+)[,}]', source[source.rfind('export{'):])
        if exported:
            return '/assets/' + path.name, exported[1]
    raise ValueError('Matching compiled diagnostic module is unavailable; build must match the installed host')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', default='emulator-5554')
    parser.add_argument('--adb', default='adb')
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--export-report', action='store_true')
    args = parser.parse_args()
    adb = [args.adb, '-s', args.device]

    def native(*command):
        return subprocess.check_output(adb + list(command), text=True, encoding='utf-8', timeout=15).strip()

    pid = native('shell', 'pidof', 'com.cczj.video.preview').split()[0]
    port = native('forward', 'tcp:0', f'localabstract:webview_devtools_remote_{pid}')
    socket = None
    identifier = 0
    try:
        with urlopen(f'http://127.0.0.1:{port}/json/list', timeout=10) as response:
            targets = json.load(response)
        target = next(row for row in targets if row.get('url', '').startswith('https://wails.localhost'))
        socket = websocket.create_connection(target['webSocketDebuggerUrl'], timeout=30, origin='http://localhost')

        def evaluate(expression):
            nonlocal identifier
            identifier += 1
            socket.send(json.dumps({'id': identifier, 'method': 'Runtime.evaluate', 'params': {
                'expression': expression, 'awaitPromise': True, 'returnByValue': True,
            }}))
            while True:
                result = json.loads(socket.recv())
                if result.get('id') != identifier:
                    continue
                if 'error' in result or result.get('result', {}).get('exceptionDetails'):
                    raise RuntimeError(str(result)[:1200])
                return result.get('result', {}).get('result', {}).get('value')

        def binding(name, values=None):
            development = any('/@vite/client' in script or '/src/main.ts' in script for script in frontend.get('scripts', []))
            path, exported = ('/src/api/app.ts', name) if development else compiled_binding(name)
            arguments = ','.join(json.dumps(value, ensure_ascii=True) for value in (values or []))
            return evaluate(f'(async()=>{{const module=await import({json.dumps(path)});return await module[{json.dumps(exported)}]({arguments})}})()')

        frontend = evaluate('''({
          captured_at:new Date().toISOString(), presentation:document.documentElement.dataset.cczjPlatform,
          observation:'Passive snapshot; no playback or UI scenarios performed',
          share_available:typeof window.wails?.diagnosticShare==='function',
          browser_resources:{video_count:document.querySelectorAll('video').length,canvas_count:document.querySelectorAll('canvas').length},
          scripts:Array.from(document.scripts).map(script=>script.src).filter(Boolean)
        })''')
        if any('/@vite/client' in script or '/src/main.ts' in script for script in frontend.get('scripts', [])):
            frontend['architecture'] = evaluate('(async()=>{const module=await import("/src/diagnostics/architecture.ts");return module.architectureSnapshot()})()')
        resources = [binding('GetRuntimeMetrics')]
        time.sleep(5)
        resources.append(binding('GetRuntimeMetrics'))
        report = binding('ExportDiagnosticsReport', [json.dumps({'observation': frontend, 'runtime_samples': resources})]) if args.export_report else None
        result = {'device': args.device, 'app_pid': int(pid), 'frontend': frontend, 'runtime_samples': resources, 'export': report}
        args.output.mkdir(parents=True, exist_ok=True)
        (args.output / 'passive-android-diagnostics.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps({'device': args.device, 'frontend': frontend,
            'resources': [row.get('resources') for row in resources], 'export': report}, ensure_ascii=True))
    finally:
        if socket:
            socket.close()
        subprocess.run(adb + ['forward', '--remove', f'tcp:{port}'], capture_output=True, timeout=15)


if __name__ == '__main__':
    main()
