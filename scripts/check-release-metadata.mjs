import { readFileSync } from 'node:fs'
import { join } from 'node:path'

const root = process.argv[2] ?? process.cwd()
const version = JSON.parse(readFileSync(join(root, 'version.json'), 'utf8')).version
const readme = readFileSync(join(root, 'README.md'), 'utf8')
const documented = readme.match(/^Current release: \*\*v([^*]+)\*\*/m)?.[1]
if (!version || documented !== version) {
  console.error(`README release ${documented ?? '(missing)'} must match version.json ${version}`)
  process.exit(1)
}
console.log(`Release metadata matches v${version}.`)
