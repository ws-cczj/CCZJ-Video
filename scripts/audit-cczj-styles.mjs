import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { join, relative, resolve } from 'node:path'
import { pathToFileURL, fileURLToPath } from 'node:url'

export const root = fileURLToPath(new URL('../', import.meta.url))
const require = createRequire(join(root, 'frontend/package.json'))
export const postcss = require('postcss')
export const { parse: parseSFC } = require('@vue/compiler-sfc')
export const { baseParse } = require('@vue/compiler-dom')

export function filesBelow(directory, pattern) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? filesBelow(path, pattern) : pattern.test(entry.name) ? [path] : []
  })
}

export function utilityRules() {
  const css = postcss.parse(readFileSync(join(root, 'frontend/src/styles/cczj-utilities.css'), 'utf8'))
  const out = []
  css.walkRules(rule => {
    const match = rule.selector.match(/^\.((?:[\w-]|\\.)+)(:[\w-]+)?$/)
    if (!match) throw new Error(`Unsupported utility selector: ${rule.selector}`)
    out.push({ name: match[1].replace(/\\(.)/g, '$1'), selector: rule.selector, conditional: !!match[2],
      declarations: rule.nodes.filter(node => node.type === 'decl').map(node => ({ property: node.prop, value: node.value, important: !!node.important })) })
  })
  return out
}

export function templateElements(content) {
  const out = []
  function visit(node) {
    if (node.type === 1) out.push(node)
    for (const child of node.children ?? []) visit(child)
  }
  visit(baseParse(content))
  return out
}

export function classNames(element) {
  const result = new Set()
  for (const prop of element.props) {
    let values = []
    if (prop.type === 6 && prop.name === 'class') values = [prop.value?.content ?? '']
    if (prop.type === 7 && prop.name === 'bind' && prop.arg?.content === 'class') {
      values = [...(prop.exp?.content ?? '').matchAll(/(['"`])([^'"`]*?)\1/g)].map(match => match[2])
    }
    for (const value of values) for (const name of value.split(/\s+/)) if (name) result.add(name)
  }
  return result
}

// Business dimensions, mode colours, keyframes and runtime styles keep their
// own semantics. Only exact equivalents of basic layout declarations are gated.
const sharedLayoutProperties = new Set(['display','align-items','justify-content','flex-direction','flex-wrap','flex-shrink','overflow','overflow-x','overflow-y','box-sizing'])
export function sharedLayoutViolations(source) {
  const equivalents = new Map()
  for (const rule of utilityRules()) {
    if (rule.conditional || rule.declarations.length !== 1) continue
    const declaration = rule.declarations[0]
    if (sharedLayoutProperties.has(declaration.property) ||
        (['min-width','min-height'].includes(declaration.property) && declaration.value === '0')) {
      equivalents.set(`${declaration.property}:${declaration.value}`, rule.name)
    }
  }
  const violations = []
  postcss.parse(source).walkDecls(declaration => {
    if (declaration.important || declaration.parent?.type !== 'rule') return
    for (let parent = declaration.parent; parent; parent = parent.parent) {
      if (parent.type === 'atrule' && /keyframes$/i.test(parent.name)) return
    }
    const utility = equivalents.get(`${declaration.prop}:${declaration.value}`)
    if (utility) violations.push({ declaration, utility, line: declaration.source.start.line })
  })
  return violations
}

export function audit() {
  const utilities = utilityRules(), names = new Set(utilities.map(rule => rule.name))
  const equivalents = new Map()
  for (const rule of utilities.filter(rule => !rule.conditional && rule.declarations.length === 1)) {
    const decl = rule.declarations[0]
    equivalents.set(`${decl.property}:${decl.value}`, rule.name)
  }
  const files = [], duplicates = new Map()
  const paths = filesBelow(join(root, 'frontend/src'), /\.(vue|css)$/)
  let utilityDeclarations = 0, repeatedDeclarations = 0
  for (const path of paths) {
    const source = readFileSync(path, 'utf8'), used = new Set(), composed = new Set()
    const styles = path.endsWith('.vue') ? parseSFC(source, { filename: path }).descriptor.styles.map(style => style.content) : [source]
    if (path.endsWith('.vue')) {
      const template = parseSFC(source, { filename: path }).descriptor.template?.content
      if (template) for (const element of templateElements(template)) for (const name of classNames(element)) if (names.has(name)) used.add(name)
    }
    if (!path.endsWith('cczj-utilities.css')) for (const style of styles) {
      const css = postcss.parse(style)
      css.walkAtRules('apply', rule => { for (const name of rule.params.trim().split(/\s+/)) composed.add(name) })
      css.walkDecls(decl => {
        const signature = `${decl.prop}:${decl.value}`
        if (equivalents.has(signature) && !decl.important) {
          repeatedDeclarations++
          const record = duplicates.get(signature) ?? { declaration: `${decl.prop}: ${decl.value}`, utility: equivalents.get(signature), count: 0 }
          record.count++; duplicates.set(signature, record)
        }
      })
    }
    files.push({ path: relative(root, path).replaceAll('\\', '/'), vue: path.endsWith('.vue'), utilities: [...used].sort(), composedUtilities: [...composed].sort() })
  }
  for (const rule of utilities) utilityDeclarations += rule.declarations.length
  return { utilityClasses: utilities.length, utilityDeclarations, vueFiles: files.filter(file=>file.vue).length,
    filesUsingUtilities: files.filter(file => file.utilities.length).length,
    filesUsingComposition: files.filter(file=>file.composedUtilities.length).length,
    vueFilesUsingLibrary: files.filter(file=>file.vue&&(file.utilities.length||file.composedUtilities.length)).length,
    usedUtilities: new Set(files.flatMap(file => [...file.utilities,...file.composedUtilities])).size, repeatedDeclarations,
    duplicates: [...duplicates.values()].sort((a, b) => b.count-a.count), files }
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  if (process.argv[2] === '--check-reuse') {
    const problems = []
    for (const file of filesBelow(join(root,'frontend/src'), /\.(vue|css)$/)) {
      if (file.endsWith('cczj-utilities.css')) continue
      const content = readFileSync(file,'utf8')
      const styles = file.endsWith('.vue') ? parseSFC(content).descriptor.styles.filter(style=>!style.src).map(style=>({text:style.content,offset:style.loc.start.line-1})) : [{text:content,offset:0}]
      for (const style of styles) for (const item of sharedLayoutViolations(style.text)) {
        problems.push(`${relative(root,file).replaceAll('\\','/')}:${item.line+style.offset}: reuse @apply ${item.utility} for ${item.declaration.prop}: ${item.declaration.value}`)
      }
    }
    if (problems.length) { console.error(problems.join('\n')); process.exitCode=1 }
    else console.log('CCZJ shared layout reuse passed (exact declarations; business styles and keyframes preserved).')
    process.exit(process.exitCode ?? 0)
  }
  if(process.argv[2]==='--check-build'){
    const files=filesBelow(join(root,'frontend/dist/assets'),/\.css$/)
    if(!files.length)throw new Error('Build CSS assets before checking CCZJ composition')
    for(const file of files)postcss.parse(readFileSync(file,'utf8')).walkAtRules('apply',rule=>{throw rule.error('Unexpanded CCZJ @apply in production CSS')})
    console.log(`CCZJ production CSS: ${files.length} assets contain no unexpanded composition`)
    process.exit(0)
  }
  const report = audit(), output = process.argv[2]
  if (output) writeFileSync(output, JSON.stringify(report, null, 2)+'\n')
  console.log(JSON.stringify({ ...report, duplicates: report.duplicates.slice(0, 12), files: undefined }, null, 2))
}
