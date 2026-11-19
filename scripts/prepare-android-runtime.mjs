import { readFileSync, writeFileSync, mkdirSync, cpSync, chmodSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { execFileSync, spawnSync } from 'node:child_process'
import { resolve, dirname } from 'node:path'

// The JNI envelope discards CallError.Cause. Keep the checked native build
// copy in the project instead of modifying a shared module cache.
// Go 1.26 disallows source overlays underneath GOMODCACHE.
const root=process.cwd()
// Resolve the same module graph as the Android compiler's -modfile build.
const sdk=JSON.parse(execFileSync('go',['list','-m','-json','github.com/wailsapp/wails/v3'],{encoding:'utf8',env:{...process.env,GOWORK:'off'}}))
const version=sdk.Version?.replace(/^v/,'')
// The CLI writes its version to stderr on 3.0.0. Check both captured streams
// while retaining its exit status and an exact version comparison.
const cliResult=spawnSync('wails3',['version'],{encoding:'utf8'})
if(cliResult.error)throw cliResult.error
if(cliResult.status!==0)throw new Error(`Wails CLI version failed: ${cliResult.stderr}`)
const cli=(cliResult.stdout+cliResult.stderr).trim()
const declared=JSON.parse(readFileSync(resolve(root,'frontend/package.json'),'utf8')).dependencies['@wailsio/runtime']
const lock=JSON.parse(readFileSync(resolve(root,'frontend/package-lock.json'),'utf8')).packages['node_modules/@wailsio/runtime'].version
const installed=JSON.parse(readFileSync(resolve(root,'frontend/node_modules/@wailsio/runtime/package.json'),'utf8')).version
if(!version||cli!==`v${version}`||declared!==version||lock!==version||installed!==version){
  throw new Error(`Wails Android version mismatch: Go=${sdk.Version}, CLI=${cli}, JS declared/locked/installed=${declared}/${lock}/${installed}`)
}
const module=sdk.Dir
const source=resolve(module,'pkg/application/application_android.go')
let content=readFileSync(source,'utf8').replaceAll('\r\n','\n')
const start=content.indexOf('func handleRuntimeCallForAndroid(')
const end=content.indexOf('// handleMessageForAndroid',start)
if(start<0||end<0)throw new Error('Wails Android runtime changed: review structured error overlay')
let body=content.slice(start,end)
const old='if err != nil {\n\t\treturn fail(err.Error())\n\t}'
if(body.split(old).length!==2)throw new Error('Wails Android error branch changed: review overlay')
body=body.replace(old,`if err != nil {
        var callError *CallError
        if errors.As(err, &callError) {
            encoded, encodeErr := json.Marshal(map[string]any{"ok":false, "error":callError})
            if encodeErr != nil { return fail("unable to marshal binding error: " + encodeErr.Error()) }
            return string(encoded)
        }
        return fail(err.Error())
    }`)
content=content.slice(0,start)+body+content.slice(end)
if(!content.includes('\n\t"errors"\n'))content=content.replace('import (\n','import (\n\t"errors"\n')
// Android destroy is empty. Explicit application exit must run
// service cleanup, then ask the Java process owner to finish; Activity detach
// never calls this path. Recreating the host keeps the existing Go runtime.
const destroy='func (a *androidApp) destroy() {\n}'
if(content.split(destroy).length!==2)throw new Error('Wails Android destroy changed: review process shutdown')
content=content.replace(destroy,`func (a *androidApp) destroy() {
    a.parent.cleanup()
    androidBridgeVoid("finishApplication")
}`)
// Java dispatch can deliver cancellation just before processCallMethod registers
// its Go cancel function. Retain bounded, expiring early cancellations so that
// handoff cannot lose the stop signal. This copy is used only by Android builds.
let processor=readFileSync(resolve(module,'pkg/application/messageprocessor.go'),'utf8').replaceAll('\r\n','\n')
let calls=readFileSync(resolve(module,'pkg/application/messageprocessor_call.go'),'utf8').replaceAll('\r\n','\n')
function replaceOnce(text,old,replacement,label){
  if(text.split(old).length!==2)throw new Error(`Wails runtime changed: review ${label}`)
  return text.replace(old,replacement)
}
processor=replaceOnce(processor,'runningCalls map[string]context.CancelFunc','earlyCancellations map[string]time.Time\n\trunningCalls map[string]context.CancelFunc','cancellation state')
processor=processor.replace('import (\n','import (\n\t"time"\n')
calls=calls.replace('import (\n','import (\n\t"time"\n')
calls=replaceOnce(calls,'cancel = m.runningCalls[*callID]',`cancel = m.runningCalls[*callID]
        if cancel == nil {
            now := time.Now()
            if m.earlyCancellations == nil { m.earlyCancellations = make(map[string]time.Time) }
            for id, until := range m.earlyCancellations { if !now.Before(until) { delete(m.earlyCancellations, id) } }
            if len(m.earlyCancellations) < 128 { m.earlyCancellations[*callID] = now.Add(time.Minute) }
        }`,'early cancellation recording')
calls=replaceOnce(calls,'m.runningCalls[*callID] = cancel',`m.runningCalls[*callID] = cancel
                if until, exists := m.earlyCancellations[*callID]; exists {
                    delete(m.earlyCancellations, *callID)
                    if time.Now().Before(until) { cancel() }
                }`,'cancellation handoff')
const hash=createHash('sha256').update(sdk.Version).update(readFileSync(resolve(module,'go.mod'))).update(content).update(processor).update(calls).digest('hex').slice(0,12)
const mirror=resolve(root,`build/android/gen/wails-${hash}`)
// A prior interrupted copy or generated-file cleanup can leave a partial
// directory. Reconcile missing files on every build instead of treating its
// existence as completion; retain the patched file until it is rewritten below.
cpSync(module,mirror,{recursive:true,force:false,errorOnExist:false})
const output=resolve(mirror,'pkg/application/application_android.go')
chmodSync(output,0o644);writeFileSync(output,content)
for(const [name,text]of [['messageprocessor.go',processor],['messageprocessor_call.go',calls]]){
  const target=resolve(mirror,'pkg/application',name)
  chmodSync(target,0o644);writeFileSync(target,text)
}
const modfile=resolve(root,'build/android/gen/go.android.mod')
mkdirSync(dirname(modfile),{recursive:true})
writeFileSync(modfile,readFileSync(resolve(root,'go.mod'),'utf8')+`\nreplace github.com/wailsapp/wails/v3 => ${JSON.stringify(mirror.replaceAll('\\','/'))}\n`)
writeFileSync(modfile.replace(/\.mod$/,'.sum'),readFileSync(resolve(root,'go.sum'),'utf8'))
console.log('Android build-local runtime preserves structured binding errors and cancellation handoff.')
