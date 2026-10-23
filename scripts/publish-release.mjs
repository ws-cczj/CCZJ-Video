import { spawnSync } from 'node:child_process'
import { readFileSync, existsSync, mkdtempSync, rmSync, statSync, createReadStream } from 'node:fs'
import { createHash } from 'node:crypto'
import { resolve, join, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { githubRepository, prepareReleaseVersion } from './release-version.mjs'
import { checkReleaseArtifacts } from './check-release-artifacts.mjs'

function execute(program, args, { cwd, stream = false, timeout = 120000 } = {}) {
  const result = spawnSync(program, args, { cwd, encoding: 'utf8', stdio: stream ? 'inherit' : 'pipe', timeout,
    env: { ...process.env, GIT_TERMINAL_PROMPT: '0', GCM_INTERACTIVE: 'never', GH_PROMPT_DISABLED: '1' } })
  if (result.error?.code === 'ENOENT' && program === 'gh') throw new Error('GitHub CLI is missing. Install once: winget install --id GitHub.cli --exact ; then gh auth login --hostname github.com')
  if (result.error) throw new Error(`Cannot run ${program}: ${result.error.message}`)
  return { code: result.status ?? 1, stdout: result.stdout ?? '', stderr: result.stderr ?? '' }
}

export async function publishRelease(options, { run = execute, log = console.log, check = checkReleaseArtifacts } = {}) {
  const root = resolve(options.root ?? process.cwd())
  const command = (program, args, extra = {}) => {
    const result = run(program, args, { cwd: root, ...extra })
    if (result.code !== 0 && !extra.allowFailure) throw new Error(`${program} ${args.slice(0, 2).join(' ')} failed (${result.code}). ${result.stderr || result.stdout}`)
    return result
  }
  const git = (...args) => command('git', args).stdout.trim()
  const repository = githubRepository(git('remote', 'get-url', 'origin'))
  const version = options.version || readFileSync(join(root, 'CHANGELOG.md'), 'utf8').match(/^## \[(\d+\.\d+\.\d+)\]/m)?.[1]
  if (!version) throw new Error('CHANGELOG.md needs a version section, or pass -Version explicitly.')
  prepareReleaseVersion(root, version, repository, { write: false })
  const branch = git('symbolic-ref', '--short', 'HEAD')
  command('git', ['check-ref-format', '--branch', branch])
  for (const marker of ['MERGE_HEAD', 'rebase-merge', 'rebase-apply', 'CHERRY_PICK_HEAD', 'REVERT_HEAD']) {
    if (existsSync(resolve(root, git('rev-parse', '--git-path', marker)))) throw new Error('Finish the active Git operation before releasing.')
  }
  const tag = `v${version}`, stage = resolve(root, options.outDir ?? 'dist/releases', tag)
  if (options.plan) {
    log(`Plan: ${repository} ${tag} from ${branch}\nSynchronize version.json and README from CHANGELOG; verify; ${options.commit ? 'commit all nonignored changes; ' : 'require a clean commit; '}build EXE/ZIP/ARM64 APK; push exact tag; upload and verify draft assets; publish; push ${branch}.\nArtifacts: ${stage}\nNo files, commits, tags or remote releases were changed.`)
    return { planned: true, version, repository }
  }
  // Authenticate before touching files or creating a local commit.
  command('gh', ['auth', 'status', '--hostname', 'github.com'])
  command('git', ['fetch', '--no-tags', 'origin', branch])
  command('git', ['merge-base', '--is-ancestor', 'FETCH_HEAD', 'HEAD'])
  const remoteTag = git('ls-remote', '--tags', 'origin', `refs/tags/${tag}`, `refs/tags/${tag}^{}`)
  const headBefore = git('rev-parse', 'HEAD')
  const localTagResult = command('git', ['rev-parse', '--verify', `refs/tags/${tag}^{commit}`], { allowFailure: true })
  const localTag = localTagResult.code === 0 ? localTagResult.stdout.trim() : undefined
  const remoteCommit = remoteTag.split(/\r?\n/).filter(Boolean).sort((a, b) => Number(a.endsWith('^{}')) - Number(b.endsWith('^{}'))).at(-1)?.split(/\s+/)[0]
  if ((localTag && localTag !== headBefore) || (remoteCommit && remoteCommit !== headBefore)) throw new Error(`${tag} already identifies another commit; choose a new version. Tags will not be moved.`)

  const api = (path, args = [], optional = false) => {
    const response = command('gh', ['api', '--include', path, ...args], { allowFailure: true })
    const match = /^HTTP\/[\d.]+\s+(\d+)[^\r\n]*\r?\n[\s\S]*?\r?\n\r?\n([\s\S]*)$/.exec(response.stdout)
    if (optional && match?.[1] === '404') return undefined
    if (response.code !== 0 || !match || !/^2\d\d$/.test(match[1])) throw new Error(`GitHub API failed: ${path}. ${response.stderr || response.stdout}`)
    return JSON.parse(match[2])
  }
  let release = api(`repos/${repository}/releases/tags/${tag}`, [], true)
  if ((remoteCommit || localTag || release) && git('status', '--porcelain').length) throw new Error(`${tag} already exists. Commit changes under a new version instead of changing this release.`)
  prepareReleaseVersion(root, version, repository)
  const dirty = () => git('status', '--porcelain').length > 0
  if (dirty() && !options.commit) throw new Error('Uncommitted changes found. Review them, then pass -Commit to include all nonignored changes or commit them yourself.')
  // Verification happens before an automatic commit, so a failed check leaves
  // edits reviewable without manufacturing a release commit.
  log(`Verifying ${tag}...`)
  command('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', 'scripts/verify.ps1'], { stream: true, timeout: 1800000 })
  if (dirty()) {
    command('git', ['add', '--all'])
    command('git', ['commit', '-m', `Release ${tag}`], { stream: true })
  }
  const head = git('rev-parse', 'HEAD')
  if ((localTag && localTag !== head) || (remoteCommit && remoteCommit !== head)) throw new Error('The release commit no longer matches the existing tag.')
  let checked
  if (options.skipBuild || (release && existsSync(join(stage, 'artifact-manifest.json')))) {
    checked = await check(stage)
    log('Reusing verified artifacts for this exact release commit.')
  } else {
    command('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', 'build/release.ps1', '-Version', version,
      '-OutDir', options.outDir ?? 'dist/releases', '-AndroidSigning', options.androidSigning ?? 'ExistingPreview', '-SkipVerify', '-NoOpen'], { stream: true, timeout: 1800000 })
    checked = await check(stage)
  }
  const manifest = checked.manifest
  if (manifest.version !== version || manifest.repository !== repository || manifest.sourceCommit !== head || manifest.workingTreeDirty !== false || dirty()) throw new Error('Artifacts must come from the exact clean release commit. Rebuild before publishing.')
  // Tags make the source available first. main/version.json is pushed only
  // after publishing, so update fallbacks cannot advertise unavailable assets.
  if (!localTag) command('git', ['tag', '-a', tag, '-m', `Release ${tag}`, head])
  if (!remoteCommit) command('git', ['push', 'origin', `refs/tags/${tag}`], { stream: true })
  const base = `repos/${repository}/releases`
  const notes = join(stage, 'release-notes.md')
  if (!release) {
    command('gh', ['release', 'create', tag, '--repo', repository, '--verify-tag', '--draft', '--title', tag, '--notes-file', notes], { stream: true })
    release = api(`${base}/tags/${tag}`)
  }
  if (release.tag_name !== tag || release.prerelease) throw new Error('Unexpected remote release identity.')
  if (release.draft) {
    command('gh', ['release', 'edit', tag, '--repo', repository, '--title', tag, '--notes-file', notes], { stream: true })
    command('gh', ['release', 'upload', tag, ...checked.files.map(name => join(stage, name)), '--repo', repository, '--clobber'], { stream: true, timeout: 1800000 })
    release = api(`${base}/${release.id}`)
  }
  if (release.body?.replaceAll('\r\n', '\n').trim() !== readFileSync(notes, 'utf8').replaceAll('\r\n', '\n').trim()) throw new Error('Remote release notes do not match this build.')
  if (release.assets?.length !== checked.files.length) throw new Error('Remote release has missing or unexpected assets; it remains unpublished if still a draft.')
  for (const name of checked.files) {
    const local = join(stage, name), expected = await hashFile(local)
    const asset = release.assets.find(entry => entry.name === name)
    if (!asset || asset.state !== 'uploaded' || asset.size !== statSync(local).size) throw new Error(`Incomplete remote asset: ${name}`)
    if (asset.digest) {
      if (asset.digest !== `sha256:${expected}`) throw new Error(`Remote SHA256 mismatch: ${name}`)
    } else {
      // Older GitHub deployments may omit digest; verify downloaded bytes.
      const temporary = mkdtempSync(join(tmpdir(), 'cczj-release-verify-'))
      try {
        command('gh', ['release', 'download', tag, '--repo', repository, '--pattern', name, '--dir', temporary], { timeout: 1800000 })
        if (await hashFile(join(temporary, name)) !== expected) throw new Error(`Downloaded remote asset differs: ${name}`)
      } finally {
        if (temporary.startsWith(resolve(tmpdir()) + sep) && temporary.slice(resolve(tmpdir()).length + 1).startsWith('cczj-release-verify-')) rmSync(temporary, { recursive: true, force: true })
      }
    }
  }
  const finalTag = git('ls-remote', '--tags', 'origin', `refs/tags/${tag}`, `refs/tags/${tag}^{}`)
  const finalCommit = finalTag.split(/\r?\n/).filter(Boolean).sort((a, b) => Number(a.endsWith('^{}')) - Number(b.endsWith('^{}'))).at(-1)?.split(/\s+/)[0]
  if (finalCommit !== head || git('rev-parse', 'HEAD') !== head || dirty()) throw new Error('Source or remote tag changed while uploading; publication stopped.')
  if (release.draft) {
    command('gh', ['release', 'edit', tag, '--repo', repository, '--draft=false', '--latest'], { stream: true })
    release = api(`${base}/${release.id}`)
    if (release.draft || release.prerelease) throw new Error('GitHub did not publish the stable release.')
  }
  command('git', ['push', 'origin', `${head}:refs/heads/${branch}`], { stream: true })
  log(`Published ${release.html_url}`)
  return { version, url: release.html_url }
}

async function hashFile(path) {
  const hash = createHash('sha256')
  for await (const chunk of createReadStream(path)) hash.update(chunk)
  return hash.digest('hex')
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const options = {}
  const names = { '--version': 'version', '--out-dir': 'outDir', '--android-signing': 'androidSigning' }
  try {
    for (let i = 2; i < process.argv.length; i++) {
      const key = process.argv[i]
      if (names[key]) { if (!process.argv[i + 1]) throw new Error(`Missing value for ${key}`); options[names[key]] = process.argv[++i] }
      else if (key === '--commit') options.commit = true
      else if (key === '--plan') options.plan = true
      else if (key === '--skip-build') options.skipBuild = true
      else throw new Error(`Unknown option: ${key}`)
    }
    await publishRelease(options)
  } catch (error) { console.error(error.message); process.exitCode = 1 }
}
