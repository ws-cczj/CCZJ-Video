import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, writeFileSync, rmSync, readFileSync } from 'node:fs'
import { resolve, join, sep } from 'node:path'
import { tmpdir } from 'node:os'
import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'

function fixture(run) {
  const parent = resolve(tmpdir()), dir = mkdtempSync(join(parent,'cczj-release-test-'))
  try {
    const version = '2.4.0', repository = 'ws-cczj/CCZJ-Video', tag='v2.4.0'
    const names = [`cczjVideo-windows-amd64-${version}.exe`,`cczjVideo-windows-amd64-${version}-portable.zip`,`cczjVideo-android-arm64-${version}.apk`]
    const artifacts = names.map(name=>{const bytes=Buffer.from(name);writeFileSync(join(dir,name),bytes);return {name,bytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex')}})
    const android = {package:'com.cczj.video.preview',version,versionCode:20400,abi:'arm64-v8a',debuggable:false}
    writeFileSync(join(dir,'artifact-manifest.json'),JSON.stringify({version,tag,repository,artifacts,android}))
    writeFileSync(join(dir,'version.json'),JSON.stringify({version,assets:artifacts.map(a=>({name:a.name,size:a.bytes,browser_download_url:`https://github.com/${repository}/releases/download/${tag}/${a.name}`}))}))
    writeFileSync(join(dir,'android-metadata.json'),JSON.stringify(android))
    writeFileSync(join(dir,'release-notes.md'),'Notes')
    const files=[...names,'artifact-manifest.json','version.json','android-metadata.json','release-notes.md']
    writeFileSync(join(dir,'checksums.txt'),files.map(name=>createHash('sha256').update(readFileSync(join(dir,name))).digest('hex')+'  '+name).join('\n')+'\n')
    run(dir,names)
  } finally {
    // Only remove the uniquely created fixture directory inside the temp root.
    assert.ok(resolve(dir).startsWith(parent+sep) && dir.slice(parent.length+1).startsWith('cczj-release-test-'))
    rmSync(dir,{recursive:true,force:true})
  }
}
const check=dir=>spawnSync(process.execPath,['scripts/check-release-artifacts.mjs',dir],{encoding:'utf8'})
test('complete release metadata and checksums pass',()=>fixture(dir=>assert.equal(check(dir).status,0)))
test('modified APK bytes fail instead of silently replacing checksum expectations',()=>fixture((dir,names)=>{writeFileSync(join(dir,names[2]),'truncated apk');const result=check(dir);assert.notEqual(result.status,0);assert.match(result.stderr,/Checksum mismatch/)}))
test('missing phone APK fails',()=>fixture((dir,names)=>{rmSync(join(dir,names[2]));assert.notEqual(check(dir).status,0)}))
