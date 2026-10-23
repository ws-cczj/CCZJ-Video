import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, statSync } from 'node:fs'
import { join, resolve, sep } from 'node:path'
import { tmpdir } from 'node:os'
import { createHash } from 'node:crypto'
import { publishRelease } from './publish-release.mjs'
import { prepareReleaseVersion, releaseNotes } from './release-version.mjs'

const sha = bytes => createHash('sha256').update(bytes).digest('hex')
const oldHead = 'a'.repeat(40), newHead = 'b'.repeat(40)
async function fixture(work, configuration = {}) {
  const parent = resolve(tmpdir()), root = mkdtempSync(join(parent, 'cczj-publish-test-'))
  try {
    writeFileSync(join(root, 'version.json'), JSON.stringify({ version: '2.4.0', desc: 'Original 2.4.0 notes', history: [] }))
    writeFileSync(join(root, 'README.md'), 'Current release: **v2.4.0**\n')
    writeFileSync(join(root, 'CHANGELOG.md'), '## [2.4.1] - 2026-10-23\n\n### Fixed\n\n- Discover receivers.\n\n## [2.4.0] - 2026-10-22\n\nOriginal release.\n')
    const stage = join(root, 'dist/releases/v2.4.1'), commands = []
    let head = oldHead, localTag, remoteTag, release, clean = false
    const state = { commands, root, stage, configuration }
    const success = stdout => ({ code: 0, stdout: stdout ?? '', stderr: '' })
    const fail = message => ({ code: 1, stdout: '', stderr: message })
    function build() {
      mkdirSync(stage, { recursive: true })
      const version = '2.4.1', tag = `v${version}`, repository = 'owner/project'
      const names = [`cczjVideo-windows-amd64-${version}.exe`, `cczjVideo-windows-amd64-${version}-portable.zip`, `cczjVideo-android-arm64-${version}.apk`]
      const artifacts = names.map(name => { const bytes = Buffer.from('binary ' + name); writeFileSync(join(stage, name), bytes); return { name, bytes: bytes.length, sha256: sha(bytes) } })
      const android = { package: 'com.cczj.video.preview', version, versionCode: 20401, abi: 'arm64-v8a', debuggable: false }
      writeFileSync(join(stage, 'artifact-manifest.json'), JSON.stringify({ version, tag, repository, sourceCommit: configuration.wrongCommit ? oldHead : head, workingTreeDirty: false, artifacts, android }))
      writeFileSync(join(stage, 'version.json'), JSON.stringify({ version, assets: artifacts.map(a => ({ name: a.name, size: a.bytes, browser_download_url: `https://github.com/${repository}/releases/download/${tag}/${a.name}` })) }))
      writeFileSync(join(stage, 'android-metadata.json'), JSON.stringify(android))
      writeFileSync(join(stage, 'release-notes.md'), releaseNotes(readFileSync(join(root, 'CHANGELOG.md'), 'utf8'), version) + '\n')
      const files = [...names, 'version.json', 'artifact-manifest.json', 'android-metadata.json', 'release-notes.md']
      writeFileSync(join(stage, 'checksums.txt'), files.map(name => sha(readFileSync(join(stage, name))) + '  ' + name).join('\n') + '\n')
    }
    function run(program, args) {
      commands.push([program, ...args])
      if (program === 'git') {
        if (args[0] === 'remote') return success('https://github.com/owner/project.git\n')
        if (args[0] === 'symbolic-ref') return success('main\n')
        if (args[0] === 'rev-parse' && args[1] === '--git-path') return success('.git/' + args[2])
        if (args[0] === 'rev-parse' && args[1] === '--verify') return localTag ? success(localTag) : fail('tag not found')
        if (args[0] === 'rev-parse') return success(head)
        if (args[0] === 'ls-remote') {
          const tag = configuration.otherTag ? 'd'.repeat(40) : remoteTag
          return success(tag ? 'c'.repeat(40) + '\trefs/tags/v2.4.1\n' + tag + '\trefs/tags/v2.4.1^{}\n' : '')
        }
        if (args[0] === 'status') return success(clean ? '' : ' M version.json\n')
        if (args[0] === 'commit') { clean = true; head = newHead }
        if (args[0] === 'tag') localTag = head
        if (args[0] === 'push' && args[2].startsWith('refs/tags/')) remoteTag = head
        if (args[0] === 'push' && args[2].includes(':refs/heads/') && configuration.branchFailure) return fail('branch push rejected')
        return success()
      }
      if (program === 'powershell') {
        if (args.includes('scripts/verify.ps1') && configuration.verifyFailure) return fail('test failed')
        if (args.includes('build/release.ps1')) { if (configuration.buildFailure) return fail('compiler failed'); build() }
        return success()
      }
      if (program !== 'gh') throw new Error('Unexpected program: ' + program)
      if (args[0] === 'auth') return configuration.authFailure ? fail('not authenticated') : success()
      if (args[0] === 'api') {
        if (configuration.apiFailure) return { code: 1, stdout: 'HTTP/2.0 502 Bad Gateway\nContent-Type: text/plain\n\nunavailable', stderr: '' }
        return release ? success('HTTP/2.0 200 OK\nContent-Type: application/json\n\n' + JSON.stringify(release)) : { code: 1, stdout: 'HTTP/2.0 404 Not Found\nContent-Type: application/json\n\n{"message":"Not Found"}', stderr: '404' }
      }
      if (args[1] === 'create') release = { id: 7, tag_name: 'v2.4.1', draft: true, prerelease: false, assets: [], html_url: 'https://github.com/owner/project/releases/tag/v2.4.1' }
      if (args[1] === 'edit') {
        if (args.includes('--draft=false')) release.draft = false
        else release.body = readFileSync(join(stage, 'release-notes.md'), 'utf8')
      }
      if (args[1] === 'upload') {
        if (configuration.uploadFailure) return fail('connection lost during upload')
        const end = args.indexOf('--repo')
        release.assets = args.slice(3, end).map(path => ({ name: path.split(/[\\/]/).at(-1), size: statSync(path).size, state: 'uploaded', digest: 'sha256:' + sha(readFileSync(path)) }))
        if (configuration.partial) release.assets.pop()
        if (configuration.badDigest) release.assets[0].digest = 'sha256:' + '0'.repeat(64)
      }
      return success()
    }
    state.publish = (extra = {}) => publishRelease({ root, version: '2.4.1', commit: true, ...extra }, { run, log: () => {} })
    state.release = () => release
    await work(state)
  } finally {
    assert.ok(root.startsWith(parent + sep) && root.slice(parent.length + 1).startsWith('cczj-publish-test-'))
    rmSync(root, { recursive: true, force: true })
  }
}
const published = commands => commands.some(command => command.includes('--draft=false'))

test('one command synchronizes versions, builds the exact commit, verifies eight uploads and only then publishes', () => fixture(async state => {
  const result = await state.publish()
  assert.equal(result.version, '2.4.1')
  const metadata = JSON.parse(readFileSync(join(state.root, 'version.json'), 'utf8'))
  assert.equal(metadata.history[0].version, '2.4.0')
  assert.equal(metadata.history[0].desc, 'Original 2.4.0 notes')
  assert.equal(metadata.assets.length, 3)
  assert.ok(metadata.assets.every(a => a.browser_download_url.includes('/v2.4.1/')))
  assert.match(readFileSync(join(state.root, 'README.md'), 'utf8'), /v2\.4\.1/)
  assert.equal(state.release().assets.length, 8)
  const final = state.commands.findIndex(c => c.includes('--draft=false'))
  assert.ok(final > state.commands.findIndex(c => c[1] === 'release' && c[2] === 'upload'))
  assert.ok(state.commands.findIndex(c => c[0] === 'git' && c.some(a => a.includes(':refs/heads/'))) > final)
}))
test('double-click entry infers the new version from the first CHANGELOG release section', () => fixture(async state => {
  const result = await state.publish({ version: undefined })
  assert.equal(result.version, '2.4.1')
  assert.equal(JSON.parse(readFileSync(join(state.root, 'version.json'), 'utf8')).version, '2.4.1')
}))

for (const [failure, error] of [['verifyFailure', /test failed/], ['buildFailure', /compiler failed/], ['wrongCommit', /exact clean/], ['partial', /missing or unexpected assets/], ['badDigest', /SHA256 mismatch/], ['uploadFailure', /connection lost/], ['apiFailure', /GitHub API failed/], ['authFailure', /not authenticated/]]) {
  test(`${failure} never publishes an incomplete or incorrect release`, () => fixture(async state => {
    await assert.rejects(state.publish(), error)
    assert.equal(published(state.commands), false)
    assert.equal(state.commands.some(c => c[0] === 'git' && c.some(a => a.includes(':refs/heads/'))), false)
  }, { [failure]: true }))
}
test('failed upload resumes the same draft with the original verified artifacts', () => fixture(async state => {
  await assert.rejects(state.publish(), /connection lost/)
  state.configuration.uploadFailure = false
  await state.publish()
  assert.equal(state.commands.filter(c => c.includes('build/release.ps1')).length, 1)
  assert.equal(state.commands.filter(c => c[1] === 'commit').length, 1)
  assert.equal(state.release().draft, false)
}, { uploadFailure: true }))
test('retry after branch push failure verifies the published release without uploading or publishing again', () => fixture(async state => {
  await assert.rejects(state.publish(), /branch push rejected/)
  state.configuration.branchFailure = false
  await state.publish()
  assert.equal(state.commands.filter(c => c[2] === 'upload').length, 1)
  assert.equal(state.commands.filter(c => c.includes('--draft=false')).length, 1)
}, { branchFailure: true }))
test('plan is read-only and requires no GitHub authentication', () => fixture(async state => {
  const before = readFileSync(join(state.root, 'version.json'), 'utf8')
  await state.publish({ plan: true })
  assert.equal(readFileSync(join(state.root, 'version.json'), 'utf8'), before)
  assert.ok(state.commands.every(c => c[0] === 'git' && !['add', 'commit', 'push', 'tag', 'fetch'].includes(c[1])))
}))
test('a version tag belonging to another commit is never moved or overwritten', () => fixture(async state => {
  await assert.rejects(state.publish(), /already identifies another commit/)
  assert.ok(state.commands.every(c => c[0] !== 'git' || !['add', 'commit', 'push', 'tag'].includes(c[1])))
  assert.equal(published(state.commands), false)
}, { otherTag: true }))
test('version notes reject duplicate, missing and empty sections and versions that collide in Android versionCode', () => fixture(async state => {
  assert.throws(() => releaseNotes('## [2.4.1]\n\n', '2.4.1'), /nonempty/)
  assert.throws(() => releaseNotes('## [2.4.1]\nA\n## [2.4.1]\nB\n', '2.4.1'), /exactly one/)
  assert.throws(() => releaseNotes('## [2.4.1]\nA\n', '02.4.1'), /stable/)
  writeFileSync(join(state.root, 'CHANGELOG.md'), '## [2.4.100]\nA\n')
  assert.throws(() => prepareReleaseVersion(state.root, '2.4.100', 'owner/project'), /versionCode limits/)
}))
