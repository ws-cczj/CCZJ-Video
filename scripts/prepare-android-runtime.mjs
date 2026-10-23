import { readFileSync, writeFileSync, mkdirSync, cpSync, chmodSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { resolve, dirname } from 'node:path'

// beta.30's JNI envelope discards CallError.Cause. Keep this small, checked
// native build copy in the project instead of modifying a shared module cache.
// Go 1.26 disallows source overlays underneath GOMODCACHE.
const root=process.cwd()
const module=execFileSync('go',['list','-m','-f','{{.Dir}}','github.com/wailsapp/wails/v3'],{encoding:'utf8'}).trim()
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
            encoded, _ := json.Marshal(map[string]any{"ok":false, "error":callError})
            return string(encoded)
        }
        return fail(err.Error())
    }`)
content=content.slice(0,start)+body+content.slice(end)
if(!content.includes('\n\t"errors"\n'))content=content.replace('import (\n','import (\n\t"errors"\n')
const hash=createHash('sha256').update(content).digest('hex').slice(0,12)
const mirror=resolve(root,`build/android/gen/wails-${hash}`)
// A prior interrupted copy or generated-file cleanup can leave a partial
// directory. Reconcile missing files on every build instead of treating its
// existence as completion; retain the patched file until it is rewritten below.
cpSync(module,mirror,{recursive:true,force:false,errorOnExist:false})
const output=resolve(mirror,'pkg/application/application_android.go')
chmodSync(output,0o644);writeFileSync(output,content)
const modfile=resolve(root,'build/android/gen/go.android.mod')
mkdirSync(dirname(modfile),{recursive:true})
writeFileSync(modfile,readFileSync(resolve(root,'go.mod'),'utf8')+`\nreplace github.com/wailsapp/wails/v3 => ${JSON.stringify(mirror.replaceAll('\\','/'))}\n`)
writeFileSync(modfile.replace(/\.mod$/,'.sum'),readFileSync(resolve(root,'go.sum'),'utf8'))
console.log('Android build-local runtime preserves structured binding errors.')
