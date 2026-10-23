import { readFileSync, createReadStream, statSync } from 'node:fs'
import { resolve, join, basename } from 'node:path'
import { createHash } from 'node:crypto'
const dir = resolve(process.argv[2] ?? 'dist/releases/v2.4.0')
const manifest = JSON.parse(readFileSync(join(dir, 'artifact-manifest.json'), 'utf8'))
const metadata = JSON.parse(readFileSync(join(dir, 'version.json'), 'utf8'))
function requireValue(condition, message) { if (!condition) throw new Error(message) }
requireValue(/^\d+\.\d+\.\d+$/.test(manifest.version) && manifest.tag === `v${manifest.version}`, 'Invalid release version/tag')
requireValue(metadata.version === manifest.version, 'Update metadata version mismatch')
const expected = [`cczjVideo-windows-amd64-${manifest.version}.exe`, `cczjVideo-windows-amd64-${manifest.version}-portable.zip`, `cczjVideo-android-arm64-${manifest.version}.apk`]
requireValue(manifest.artifacts.length === expected.length && expected.every(name => manifest.artifacts.some(a => a.name === name)), 'Missing or unexpected binary artifacts')
const sums = new Map(readFileSync(join(dir, 'checksums.txt'), 'utf8').trim().split(/\r?\n/).map(line => {
  const match = /^([a-f0-9]{64})  ([^/\\]+)$/.exec(line)
  requireValue(match && match[2] === basename(match[2]), 'Malformed checksum entry')
  return [match[2], match[1]]
}))
for (const [name, expectedHash] of sums) {
  const hash = createHash('sha256')
  for await (const chunk of createReadStream(join(dir, name))) hash.update(chunk)
  requireValue(hash.digest('hex') === expectedHash, `Checksum mismatch: ${name}`)
}
for (const artifact of manifest.artifacts) {
  requireValue(sums.get(artifact.name) === artifact.sha256 && statSync(join(dir, artifact.name)).size === artifact.bytes, `Artifact metadata mismatch: ${artifact.name}`)
  const entry = metadata.assets?.find(a => a.name === artifact.name)
  requireValue(entry?.size === artifact.bytes && entry.browser_download_url === `https://github.com/${manifest.repository}/releases/download/${manifest.tag}/${artifact.name}`, `Update URL mismatch: ${artifact.name}`)
}
const parts = manifest.version.split('.').map(Number)
requireValue(manifest.android.package === 'com.cczj.video.preview' && manifest.android.version === manifest.version && manifest.android.versionCode === parts[0]*10000+parts[1]*100+parts[2] && manifest.android.abi === 'arm64-v8a' && manifest.android.debuggable === false, 'APK identity/version mismatch')
for (const name of ['version.json','artifact-manifest.json','android-metadata.json','release-notes.md',...expected]) requireValue(sums.has(name), `Missing checksum: ${name}`)
console.log(`Release ${manifest.tag}: EXE, portable ZIP, ARM64 APK, metadata and SHA256 checks passed.`)
