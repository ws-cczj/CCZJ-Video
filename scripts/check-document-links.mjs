import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const files = ['README.md','AGENTS.md','CONTEXT.md','CHANGELOG.md'].map(name=>join(root,name))
function collect(directory) {
  for (const entry of readdirSync(directory,{withFileTypes:true})) {
    const path=join(directory,entry.name)
    if (entry.isDirectory()) collect(path)
    else if (entry.name.endsWith('.md')) files.push(path)
  }
}
collect(join(root,'docs'))
const problems=[]
for (const file of files) {
  let fence=false
  const text=readFileSync(file,'utf8').split('\n').map(line=>{
    if (/^\s*(```|~~~)/.test(line)) { fence=!fence;return '' }
    return fence ? '' : line.replace(/`[^`]*`/g,'')
  }).join('\n')
  for (const match of text.matchAll(/!?\[[^\]\n]*\]\((<[^>]+>|[^\s)]+)(?:\s+"[^"]*")?\)/g)) {
    const url=match[1].replace(/^<|>$/g,'')
    if (/^(?:[a-z][\w+.-]*:|#|\/\/)/i.test(url)) continue
    const path=decodeURIComponent(url.split('#')[0].split('?')[0])
    if (!path) continue
    const target=resolve(dirname(file),path)
    // Build outputs are intentionally absent from a clean checkout.
    if (/^(dist|bin|\.cache)[/\\]/.test(relative(root,target))) continue
    if (!existsSync(target)) problems.push(`${relative(root,file).replaceAll('\\','/')}: missing ${url}`)
  }
}
if (problems.length) { console.error(problems.join('\n'));process.exitCode=1 }
else console.log(`Documentation links passed (${files.length} maintained documents).`)
