import { readFileSync, writeFileSync, renameSync, rmSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolve, dirname } from 'node:path'
import { gzipSync, gunzipSync } from 'node:zlib'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const check = process.argv.includes('--check')
const inputs = [
  ['app/library/quality/bulk.v1.json', 64 * 1024 * 1024],
  ['app/library/source_library_novel.json', 32 * 1024 * 1024],
  ['app/library/source_library_comic.json', 32 * 1024 * 1024],
]
let rawBytes = 0, packedBytes = 0
for (const [name, maximum] of inputs) {
  // Git checkouts can use CRLF or LF; embedded JSON uses canonical LF bytes.
  const source = Buffer.from(readFileSync(resolve(root, name), 'utf8').replace(/\r\n/g, '\n'))
  if (source.length > maximum) throw new Error(`${name} exceeds its decoded size limit`)
  JSON.parse(source.toString('utf8'))
  const output = resolve(root, `${name}.gz`)
  let packed
  try {
    const candidate = readFileSync(output)
    if (gunzipSync(candidate, { maxOutputLength: maximum }).equals(source)) packed = candidate
  } catch (error) {
    if (error.code && error.code !== 'ENOENT' && !String(error.code).startsWith('Z_') && error.code !== 'ERR_BUFFER_TOO_LARGE') throw error
  }
  if (!packed) {
    if (check) throw new Error(`${name}.gz is stale; run node scripts/compress-embedded-json.mjs`)
    packed = gzipSync(source, { level: 9 })
    const temporary = `${output}.${process.pid}.tmp`
    try { writeFileSync(temporary, packed); renameSync(temporary, output) }
    finally { rmSync(temporary, { force: true }) }
  }
  rawBytes += source.length; packedBytes += packed.length
}
console.log(`Embedded JSON ${check ? 'checked' : 'generated'}: ${rawBytes} -> ${packedBytes} bytes (${rawBytes - packedBytes} saved)`)
