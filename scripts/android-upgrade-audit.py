"""Read real signed preview APK data using same-certificate instrumentation.

Does not replace the target APK, enable debugging, inject business data or clear
application storage. --delete-downloads explicitly removes staged update APKs
and their digest receipts after preserving a database/log snapshot.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import zipfile

ROOT=Path(__file__).resolve().parent.parent

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--device',default='emulator-5554')
    p.add_argument('--label',required=True)
    p.add_argument('--java-home',type=Path,default=Path('C:/Environment/Java/jdk-21'))
    p.add_argument('--delete-downloads',action='store_true')
    p.add_argument('--output',type=Path,default=ROOT/'dist/audit/upgrade-244-245')
    args=p.parse_args()
    if not re.fullmatch(r'[a-zA-Z0-9_-]{1,64}',args.label):p.error('Invalid label')
    if (args.output/args.label).exists() or (args.output/(args.label+'-instrumentation.txt')).exists():
        p.error('Audit label already exists; use a new label to preserve snapshot evidence')
    sdk=Path(os.environ.get('ANDROID_HOME',Path.home()/'AppData/Local/Android/Sdk'))
    build=sdk/'build-tools/35.0.1';jar=sdk/'platforms/android-35/android.jar'
    env={**os.environ,'JAVA_HOME':str(args.java_home),'PATH':str(args.java_home/'bin')+os.pathsep+os.environ['PATH']}
    def run(command,**kwargs):return subprocess.check_output([str(x) for x in command],env=env,**kwargs)
    folder=ROOT/'.cache/upgrade-audit';folder.mkdir(parents=True,exist_ok=True)
    classes=folder/'classes';classes.mkdir(exist_ok=True)
    dex=folder/'dex';dex.mkdir(exist_ok=True)
    fixture=ROOT/'scripts/fixtures/upgrade-audit'
    run([args.java_home/'bin/javac.exe','-source','8','-target','8','-Xlint:-options','-classpath',jar,'-d',classes,fixture/'UpgradeAudit.java'])
    run([build/'d8.bat','--lib',jar,'--min-api','24','--output',dex,*classes.rglob('*.class')])
    unsigned=folder/'unsigned.apk';aligned=folder/'aligned.apk';apk=folder/'upgrade-audit.apk'
    run([build/'aapt2.exe','link','-o',unsigned,'--manifest',fixture/'AndroidManifest.xml','-I',jar])
    with zipfile.ZipFile(unsigned,'a') as archive:archive.write(dex/'classes.dex','classes.dex')
    run([build/'zipalign.exe','-f','4',unsigned,aligned])
    run([build/'apksigner.bat','sign','--ks',Path.home()/'.android/debug.keystore','--ks-pass','pass:android','--key-pass','pass:android','--ks-key-alias','androiddebugkey','--out',apk,aligned])
    adb=['adb','-s',args.device]
    args.output.mkdir(parents=True,exist_ok=True)
    run([*adb,'install','-r',apk])
    run([*adb,'shell','am','force-stop','com.cczj.video.preview'])
    output=run([*adb,'shell','am','instrument','-w','-r','-e','label',args.label,'-e','deleteDownloads',str(args.delete_downloads).lower(),'com.cczj.video.upgradeaudit/com.cczj.video.upgradeaudit.UpgradeAudit']).decode('utf-8')
    (args.output/(args.label+'-instrumentation.txt')).write_text(output,encoding='utf-8')
    if 'INSTRUMENTATION_RESULT: error=' in output or 'INSTRUMENTATION_CODE: -1' not in output:raise RuntimeError(output)
    path=re.search(r'INSTRUMENTATION_RESULT: output=(.+)',output).group(1).strip()
    run([*adb,'pull',path,args.output])
    report=json.loads((args.output/args.label/'snapshot.json').read_text(encoding='utf-8'))
    print(json.dumps(report,ensure_ascii=True,indent=2))

if __name__=='__main__':main()
