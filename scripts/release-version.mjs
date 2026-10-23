import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

export function githubRepository(remote) {
  const match = /^(?:https:\/\/github\.com\/|git@github\.com:|ssh:\/\/git@github\.com\/)([\w.-]+\/[\w.-]+?)(?:\.git)?$/.exec(remote.trim())
  if (!match) throw new Error('origin must identify a GitHub repository.')
  return match[1]
}

export function releaseNotes(changelog, version) {
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) throw new Error('Use a stable major.minor.patch version.')
  const escaped = version.replaceAll('.', '\\.')
  const matches = [...changelog.matchAll(new RegExp('^## \\[' + escaped + '\\][^\\n]*\\n([\\s\\S]*?)(?=^## |$(?![\\s\\S]))', 'gm'))]
  if (matches.length !== 1 || !matches[0][1].trim()) throw new Error(`Add exactly one nonempty CHANGELOG.md section for ${version}.`)
  return matches[0][1].trim()
}

// CHANGELOG is written once; app update text and the Release page share it.
export function prepareReleaseVersion(root, version, repository, { write = true } = {}) {
  const path = join(root, 'version.json')
  const original = readFileSync(path, 'utf8')
  const metadata = JSON.parse(original)
  const notes = releaseNotes(readFileSync(join(root, 'CHANGELOG.md'), 'utf8'), version)
  const before = metadata.version.split('.').map(Number), after = version.split('.').map(Number)
  if (after.some(n => !Number.isSafeInteger(n) || n > 65535) || after[1] > 99 || after[2] > 99 || after[0] * 10000 + after[1] * 100 + after[2] > 2100000000) throw new Error('Version exceeds Windows or Android versionCode limits (minor/patch <= 99).')
  const different = after.findIndex((n, i) => n !== before[i])
  if (different >= 0 && after[different] < before[different]) throw new Error('Refusing to lower the current version.')
  if (metadata.version !== version) {
    metadata.history = [{ version: metadata.version, desc: metadata.desc }, ...(metadata.history ?? []).filter(entry => entry.version !== metadata.version && entry.version !== version)]
    metadata.version = version
  }
  metadata.desc = notes.replace(/^### (.+)$/gm, '$1')
  const names = [`cczjVideo-windows-amd64-${version}.exe`, `cczjVideo-windows-amd64-${version}-portable.zip`, `cczjVideo-android-arm64-${version}.apk`]
  metadata.assets = names.map(name => ({ name, browser_download_url: `https://github.com/${repository}/releases/download/v${version}/${name}`, size: 0 }))
  const readmePath = join(root, 'README.md'), readme = readFileSync(readmePath, 'utf8')
  if (!/^Current release: \*\*v[^*]+\*\*/m.test(readme)) throw new Error('README current release marker is missing.')
  const updatedReadme = readme.replace(/^Current release: \*\*v[^*]+\*\*/m, `Current release: **v${version}**`)
  if (write) {
    const serialized = JSON.stringify(metadata, null, 2) + '\n'
    if (serialized !== original) writeFileSync(path, serialized, 'utf8')
    if (updatedReadme !== readme) writeFileSync(readmePath, updatedReadme, 'utf8')
  }
  return { metadata, notes }
}
