// One-time exact-declaration migration. Each changed style is expanded by the
// production plugin and compared to its original AST before writing any file.
import assert from 'node:assert/strict'
import { readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { join, relative } from 'node:path'
import { filesBelow, postcss, parseSFC, root, utilityRules } from '../../scripts/audit-cczj-styles.mjs'
import { cczjUtilities } from '../../frontend/build/cczjUtilities.ts'

const definitions = utilityRules().filter(rule => !rule.conditional && rule.declarations.length === 1)
const equivalents = new Map()
for (const rule of definitions) {
  const decl = rule.declarations[0]
  const key = `${decl.property}:${decl.value}`
  if (!equivalents.has(key)) equivalents.set(key, rule.name)
}
function structure(node) {
  if (node.type === 'comment') return null
  return {
    type: node.type, selector: node.selector, name: node.name, params: node.params,
    property: node.prop, value: node.value, important: node.important || undefined,
    ...(node.nodes ? { nodes:node.nodes.map(structure).filter(Boolean) } : {}),
  }
}
const sha = value => createHash('sha256').update(JSON.stringify(value)).digest('hex')
const records = [], changes = []
for (const file of filesBelow(join(root, 'frontend/src'), /\.(vue|css)$/)) {
  if (file.endsWith('cczj-utilities.css') || file.endsWith('styles/animations.css')) continue
  const original = readFileSync(file, 'utf8')
  const styles = file.endsWith('.vue') ? parseSFC(original, { filename:file }).descriptor.styles : [{ content: original, loc: { start:{offset:0},end:{offset:original.length} } }]
  const replacements = []
  let count = 0
  for (const style of styles) {
    const css = postcss.parse(style.content), before = structure(css)
    const declarations = []
    css.walkDecls(decl => declarations.push(decl))
    for (const decl of declarations) {
      let ancestor = decl.parent, keyframe = false
      while (ancestor) { if (ancestor.type === 'atrule' && /keyframes$/i.test(ancestor.name)) keyframe = true; ancestor = ancestor.parent }
      const name = !decl.important && !keyframe && equivalents.get(`${decl.prop}:${decl.value}`)
      if (!name) continue
      const previous = decl.prev()
      if (previous?.type === 'atrule' && previous.name === 'apply') { previous.params += ' '+name; decl.remove() }
      else decl.replaceWith(postcss.atRule({ name:'apply', params:name, raws:{ before:decl.raws.before } }))
      count++
    }
    const output = css.toString()
    const expanded = await postcss([cczjUtilities()]).process(output, { from:file })
    assert.deepEqual(structure(postcss.parse(expanded.css)), before, `CSS cascade changed in ${file}`)
    if (output !== style.content) replacements.push({ start:style.loc.start.offset, end:style.loc.end.offset, output })
    if (output !== style.content) records.push({ file:relative(root,file).replaceAll('\\','/'), before:sha(before), after:sha(structure(postcss.parse(expanded.css))), equivalent:true })
  }
  let source = original
  for (const replacement of replacements.reverse()) source = source.slice(0,replacement.start)+replacement.output+source.slice(replacement.end)
  if (source !== original) changes.push({ file, source, declarations:count })
}
for (const change of changes) writeFileSync(change.file, change.source)
writeFileSync(join(root,'reports/css-reuse-2026-10-20/migration-equivalence.json'),JSON.stringify({ files:changes.length, declarations:changes.reduce((n,c)=>n+c.declarations,0), styles:records },null,2)+'\n')
console.log(JSON.stringify({ files:changes.length, declarations:changes.reduce((n,c)=>n+c.declarations,0), equivalentStyles:records.length }))
