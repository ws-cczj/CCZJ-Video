"""Validate APK handoff using local fixtures in the separate acceptance app.

Does not download a release, confirm an installation, clear user data or change
the preview package. Restores the acceptance app's unknown-source app-op.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shlex
import subprocess
import time

from android_webview import AndroidWebView

ROOT = Path(__file__).resolve().parent.parent
PACKAGE = 'com.cczj.video.acceptance'
SPEC = importlib.util.spec_from_file_location('compiled', ROOT/'scripts/collect-android-diagnostics.py')
COMPILED = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(COMPILED)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--device', required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    adb = ['adb', '-s', args.device]

    def native(*command):
        return subprocess.check_output(adb+list(command), text=True, encoding='utf-8', errors='replace', timeout=40).strip()

    def owned(command):
        return native('shell', 'run-as', PACKAGE, 'sh', '-c', shlex.quote(command))

    appop = native('shell', 'appops', 'get', PACKAGE, 'REQUEST_INSTALL_PACKAGES')
    match = re.search(r'REQUEST_INSTALL_PACKAGES:\s*(\w+)', appop)
    previous_op = match[1] if match else 'default'
    result = dict(passed=False, package=PACKAGE, device=args.device, checks=[], scope='native local APK fixtures; system installer is opened and cancelled, no release download')
    remote_files = []
    try:
        data = native('shell', 'run-as', PACKAGE, 'pwd')
        update_dir = data+'/files/CCZJ Video/updates'
        owned('mkdir -p '+shlex.quote(update_dir))
        fixtures = {}
        for kind, local in [
            ('valid', ROOT/'bin/CCZJ-Video-android-amd64-acceptance.apk'),
            ('wrong-package', ROOT/'dist/releases/v2.4.0/cczjVideo-android-arm64-2.4.0.apk'),
        ]:
            tmp = '/data/local/tmp/cczj-update-installer-'+kind+'.apk'
            native('push', str(local), tmp)
            destination = update_dir+'/CCZJ-Video-Update.fixture-'+kind+'.apk'
            remote_files.append((tmp, destination))
            owned('cp '+shlex.quote(tmp)+' '+shlex.quote(destination))
            receipt = hashlib.sha256(local.read_bytes()).hexdigest()
            owned('printf %s '+shlex.quote(receipt)+' > '+shlex.quote(destination+'.sha256'))
            fixtures[kind] = destination

        with AndroidWebView(args.device, application_id=PACKAGE) as view:
            def rpc(name, *values):
                path, alias = COMPILED.compiled_binding(name)
                return view.evaluate('(async()=>{const m=await import('+json.dumps(path)+');return await m['+json.dumps(alias)+'](...'+json.dumps(values)+')})()')

            def install(path):
                return view.evaluate('window.wails.installApk('+json.dumps(path)+')')

            def record(text):
                result['checks'].append(text)
                print('PASS '+text, flush=True)

            def foreground():
                native('shell', 'am', 'start', '-W', '-n', PACKAGE+'/com.wails.app.MainActivity')
                time.sleep(.25)

            assert view.evaluate('typeof window.wails.installApk') == 'function'
            version_before = rpc('GetAppVersion')
            assert 'Only downloaded APK' in install(data+'/files/not-an-update.apk')
            record('arbitrary private path is rejected')
            wrong = install(fixtures['wrong-package'])
            assert 'different application' in wrong, wrong
            record('APK for preview package cannot update the acceptance package')

            owned('printf %s '+shlex.quote('0'*64)+' > '+shlex.quote(fixtures['valid']+'.sha256'))
            assert 'checksum mismatch' in install(fixtures['valid'])
            rejected = view.evaluate('(async()=>{try{const m=await import('+json.dumps(COMPILED.compiled_binding('InstallUpdate')[0])+');await m['+json.dumps(COMPILED.compiled_binding('InstallUpdate')[1])+']('+json.dumps(fixtures['valid'])+');return false}catch{return true}})()')
            assert rejected
            record('Java and Go both reject an APK with a mismatched checksum receipt')
            checksum = hashlib.sha256((ROOT/'bin/CCZJ-Video-android-amd64-acceptance.apk').read_bytes()).hexdigest()
            owned('printf %s '+shlex.quote(checksum)+' > '+shlex.quote(fixtures['valid']+'.sha256'))

            native('shell', 'appops', 'set', PACKAGE, 'REQUEST_INSTALL_PACKAGES', 'deny')
            permission = install(fixtures['valid'])
            assert 'Allow this app' in permission, permission
            time.sleep(.4)
            result['permissionActivity'] = native('shell', 'dumpsys', 'activity', 'activities')[-16000:]
            view.screenshot(args.output/'unknown-source-permission.png')
            record('missing unknown-source permission opens Android settings without claiming success')
            native('shell', 'input', 'keyevent', 'KEYCODE_BACK')
            foreground()
            native('shell', 'appops', 'set', PACKAGE, 'REQUEST_INSTALL_PACKAGES', 'allow')
            rpc('InstallUpdate', fixtures['valid'])
            assert rpc('GetAppVersion') == version_before
            assert install(fixtures['valid']) == ''
            time.sleep(.7)
            activity = native('shell', 'dumpsys', 'activity', 'activities')
            result['installerActivity'] = activity[-18000:]
            assert re.search(r'(?i)(packageinstaller|installstart|packageinstalleractivity)', activity), activity[-1800:]
            view.screenshot(args.output/'system-installer.png')
            record('same-package signed APK reaches the system installer via a read-only content URI')
            native('shell', 'input', 'keyevent', 'KEYCODE_BACK')
            foreground()
            assert rpc('GetAppVersion') == version_before
            result['versionBefore'] = version_before
            result['versionAfterCancel'] = rpc('GetAppVersion')
            result['passed'] = True
            record('cancelling installer preserves the installed version')
    finally:
        native('shell', 'appops', 'set', PACKAGE, 'REQUEST_INSTALL_PACKAGES', previous_op)
        for tmp, destination in remote_files:
            # Exact fixture paths only; no recursive cleanup of app directories.
            owned('rm -f '+shlex.quote(destination)+' '+shlex.quote(destination+'.sha256'))
            native('shell', 'rm', '-f', tmp)
        (args.output/'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2)+'\n', encoding='utf-8')
    print(json.dumps({'passed': result['passed'], 'checks': result['checks']}, ensure_ascii=False), flush=True)


if __name__ == '__main__':
    main()
