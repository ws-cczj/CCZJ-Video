// 前端约定守卫：i18n 中英键位对齐 + 键引用有效性 + cczj-* 工具类存在性 + 行内样式体量。
// 独立成脚本的原因：locales 是 `export default {}` 的无类型对象，vue-tsc 看不出缺键；
// cczj-* 是手写 CSS，类名写错只会静默不生效。两者都只能靠静态扫描。
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { join, posix, relative } from 'node:path'

const ROOT = process.argv[2] ?? process.cwd()
const SRC = join(ROOT, 'frontend', 'src')
// 内置扩展包（设置页的「日志」「诊断」分组）在 Go 侧的 embed 目录里，
// 它们的 main.js 用 cczj.i18n.t(...) 引用同一批 locale 键。只扫 frontend/src
// 会把那两个键误判成「没人用」，而删掉键的人看不到任何报错——正是这个脚本要防的静默失效。
const BUILTIN_PACKS = join(ROOT, 'app', 'plugin', 'builtin')
const SKIPPED_DIRS = new Set(['node_modules', 'dist'])

const problems = []
const fail = (msg) => problems.push(msg)

function walk(dir, files = [], pattern = /\.(ts|vue)$/) {
  for (const entry of readdirSync(dir)) {
    if (entry.startsWith('.') || SKIPPED_DIRS.has(entry)) continue
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) walk(path, files, pattern)
    else if (pattern.test(entry)) files.push(path)
  }
  return files
}

function relTo(base, path) {
  return relative(base, path).replace(/\\/g, posix.sep)
}

function lineOf(text, index) {
  return text.slice(0, index).split('\n').length
}

/** 字符串感知地把 `export default {...}` 展平成叶子键 → 文案。 */
function parseLocale(file) {
  const tokens = tokenize(readFileSync(file, 'utf8'))
  const leaves = new Map()
  const stack = []
  let depth = 0
  let mode = 'key'
  let pending = null

  for (const token of tokens) {
    if (token === '{') {
      depth += 1
      if (depth > 1) stack.push(pending ?? '')
      mode = 'key'
      continue
    }
    if (token === '}') {
      depth -= 1
      stack.pop()
      mode = 'key'
      continue
    }
    if (depth === 0) continue
    if (token === ':') { mode = 'value'; continue }
    if (token === ',') { mode = 'key'; continue }
    if (mode === 'key') { pending = token; continue }
    if (mode === 'value') {
      leaves.set([...stack, pending].filter(Boolean).join('.'), token)
      mode = 'key'
    }
  }
  return leaves
}

function tokenize(text) {
  const out = []
  let i = 0
  while (i < text.length) {
    const ch = text[i]
    if (ch === '/' && text[i + 1] === '/') { while (i < text.length && text[i] !== '\n') i += 1; continue }
    if (ch === '/' && text[i + 1] === '*') {
      i += 2
      while (i < text.length && !(text[i] === '*' && text[i + 1] === '/')) i += 1
      i += 2
      continue
    }
    if (ch === "'" || ch === '"' || ch === '`') {
      i += 1
      let value = ''
      while (i < text.length && text[i] !== ch) {
        if (text[i] === '\\') { value += text[i + 1] ?? ''; i += 2; continue }
        value += text[i]
        i += 1
      }
      i += 1
      out.push(value)
      continue
    }
    if (ch === '{' || ch === '}' || ch === ':' || ch === ',') { out.push(ch); i += 1; continue }
    if (/[A-Za-z0-9_$]/.test(ch)) {
      let value = ''
      while (i < text.length && /[A-Za-z0-9_$]/.test(text[i])) { value += text[i]; i += 1 }
      out.push(value)
      continue
    }
    i += 1
  }
  return out
}

// ---------- 1. 中英键位对齐 ----------

const maps = {
  'zh-CN': parseLocale(join(SRC, 'locales', 'zh-CN.ts')),
  en: parseLocale(join(SRC, 'locales', 'en.ts')),
}
for (const [name, map] of Object.entries(maps)) {
  if (map.size === 0) fail(`locales/${name}.ts 解析出 0 个键，检查器本身需要跟着改`)
}

const keySets = { 'zh-CN': new Set(maps['zh-CN'].keys()), en: new Set(maps.en.keys()) }
const onlyZh = [...keySets['zh-CN']].filter((k) => !keySets.en.has(k)).sort()
const onlyEn = [...keySets.en].filter((k) => !keySets['zh-CN'].has(k)).sort()
if (onlyZh.length) fail(`zh-CN 有、en 缺的键：\n  ${onlyZh.join('\n  ')}`)
if (onlyEn.length) fail(`en 有、zh-CN 缺的键：\n  ${onlyEn.join('\n  ')}`)

// 命名空间路径（player、player.sub）也算合法引用目标。
const knownPaths = new Set()
for (const key of keySets['zh-CN']) {
  const parts = key.split('.')
  for (let n = 1; n <= parts.length; n += 1) knownPaths.add(parts.slice(0, n).join('.'))
}

// ---------- 2. 引用有效性与未引用键 ----------

const referenceSites = new Map()
// t('a.b') / tr('a.b', {..}) / te('a.b')
const LITERAL_CALL = /(?:\b|\.)(?:t|tr|te)\(\s*(['"])((?:[^'"\\]|\\.)*)\1/g
// t(`a.b.${x}`) → 静态前缀 'a.b.'，前缀下的键都算被用到
const DYNAMIC_CALL = /(?:\b|\.)(?:t|tr)\(\s*`([^`$]*)\$\{/g

const referenced = new Set()
const dynamicPrefixes = []
// 有的键不直接写在 t() 里，而是当作配置字段传（VideoPlayer 的 labelKey / descriptionKey），
// 只按 t() 实参会把它们误判成未引用；这里把所有"看起来像 locale 键"的字符串字面量都算作提及。
const namespaces = new Set([...keySets['zh-CN']].map((key) => key.split('.')[0]))
const mentioned = new Set()
const KEY_LIKE = /(['"])((?:[a-z][a-zA-Z0-9]*)(?:\.[a-zA-Z0-9_]+)+)\1/g

const referenceFiles = walk(SRC)
if (existsSync(BUILTIN_PACKS)) referenceFiles.push(...walk(BUILTIN_PACKS, [], /\.js$/))

for (const file of referenceFiles) {
  const rel = relTo(SRC, file)
  const text = readFileSync(file, 'utf8')
  if (rel.startsWith('locales')) continue
  const where = relTo(ROOT, file)
  for (const match of text.matchAll(LITERAL_CALL)) {
    if (!/^[a-z][a-zA-Z0-9]*(?:\.[a-zA-Z0-9_]+)+$/.test(match[2])) continue
    referenced.add(match[2])
    if (!referenceSites.has(match[2])) referenceSites.set(match[2], `${where}:${lineOf(text, match.index)}`)
  }
  for (const match of text.matchAll(DYNAMIC_CALL)) {
    if (match[1].includes('.')) dynamicPrefixes.push(match[1])
  }
  for (const match of text.matchAll(KEY_LIKE)) {
    if (namespaces.has(match[2].split('.')[0])) mentioned.add(match[2])
  }
}

const missing = [...referenced].filter((k) => !knownPaths.has(k)).sort()
if (missing.length) fail(`引用了未定义的 i18n 键：\n  ${missing.map((k) => `${k}  ← ${referenceSites.get(k)}`).join('\n  ')}`)

const usedKey = (key) => {
  if (referenced.has(key) || mentioned.has(key)) return true
  if (dynamicPrefixes.some((prefix) => key.startsWith(prefix))) return true
  if ([...dynamicPrefixes].some((prefix) => prefix.startsWith(`${key}.`))) return true
  // 命名空间键：它下面的叶子被用到就算用到。
  return !keySets['zh-CN'].has(key) &&
    [...referenced, ...mentioned].some((ref) => ref.startsWith(`${key}.`))
}
const unused = [...keySets['zh-CN']].filter((key) => !usedKey(key)).sort()

// ---------- 3. cczj-* / bc-* 手写工具类存在性 ----------

const definedGlobally = new Set()
const definedPerFile = new Map()
const usedClasses = new Map()
const classPrefixes = new Map()

function addClasses(text, into) {
  for (const match of text.matchAll(/\.((?:cczj|bc)-[A-Za-z0-9_-]+)/g)) into.add(match[1])
  for (const match of text.matchAll(/@keyframes\s+((?:cczj|bc)-[A-Za-z0-9_-]+)/g)) into.add(match[1])
}

const cssFiles = []
;(function collectCss(dir) {
  for (const entry of readdirSync(dir)) {
    if (entry.startsWith('.') || SKIPPED_DIRS.has(entry)) continue
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) collectCss(path)
    else if (/\.css$/.test(entry)) cssFiles.push(path)
  }
})(join(SRC, 'styles'))
for (const file of [...cssFiles, join(SRC, 'App.vue'), join(ROOT, 'frontend', 'index.html')]) {
  let text = ''
  try { text = readFileSync(file, 'utf8') } catch { continue }
  addClasses(text, definedGlobally)
}

const EVENT_NAME = /(?:addEventListener|removeEventListener|CustomEvent|dispatchEvent)\(\s*['"]$/

/** 只认真正的 class 用法：排除 `--cczj-x` CSS 变量、`cczj-x.css` 路径、DOM 事件名。 */
function isClassUsage(text, match) {
  const before = text.slice(0, match.index)
  if (text[match.index - 1] === '-') return false
  if (text[match.index + match[0].length] === '.') return false
  return !EVENT_NAME.test(before)
}

for (const file of [...walk(SRC), join(ROOT, 'frontend', 'index.html')]) {
  const text = readFileSync(file, 'utf8')
  const local = new Set()
  addClasses(text, local)
  const where = relTo(ROOT, file)
  definedPerFile.set(where, local)
  // `cczj-motion-${kind}` 这类拼接：只记录前缀，不要求前缀本身是类名。
  for (const match of text.matchAll(/((?:cczj|bc)-[A-Za-z0-9_-]*)\$\{/g)) classPrefixes.set(match[1], where)
  for (const match of text.matchAll(/(?:cczj|bc)-[A-Za-z0-9_-]+[A-Za-z0-9]/g)) {
    if (!isClassUsage(text, match)) continue
    if (!usedClasses.has(match[0])) usedClasses.set(match[0], `${where}:${lineOf(text, match.index)}`)
  }
}

const undefinedClasses = [...usedClasses].filter(([name, where]) => {
  if (definedGlobally.has(name)) return false
  if (definedPerFile.get(where.split(':')[0])?.has(name)) return false
  // 拼接前缀被正则截断出来的一段（cczj-motion ← `cczj-motion-${x}`）不算引用。
  return ![...classPrefixes.keys()].some((prefix) => prefix.startsWith(`${name}-`))
}).sort()

if (undefinedClasses.length) {
  fail('用了未定义的 cczj-*/bc-* 类（手写 CSS，只会静默不生效）：\n' +
    undefinedClasses.map(([name, where]) => `  ${name}  ← ${where}`).join('\n'))
}

// ---------- 4. 单个 SFC 的行内 scoped 样式体量 ----------
// 一个 700 行的 <style scoped> 会把模板和逻辑挤出可审阅范围，也正是这一轮拆分的起因。
// 规则只管 scoped：App.vue 那份全局样式本来就该留在入口文件里。
const INLINE_STYLE_LIMIT = 260
for (const file of walk(SRC, [], /\.vue$/)) {
  const text = readFileSync(file, 'utf8')
  const open = text.lastIndexOf('\n<style scoped>')
  if (open === -1) continue // 已经搬出去的写成 <style scoped src="...">，不会命中这里
  const lines = text.slice(open).split('\n').length
  if (lines > INLINE_STYLE_LIMIT) {
    fail(`${relTo(ROOT, file)} 的行内 <style scoped> 有 ${lines} 行，搬到 styles/ 下再用 <style scoped src> 引回来`)
  }
}

// ---------- 输出 ----------

if (process.argv.includes('--report-unused')) {
  console.log(`未引用的 locale 键 ${unused.length} 个：\n  ${unused.join('\n  ')}`)
}
if (problems.length) {
  console.error(problems.join('\n'))
  console.error(`\n前端约定检查失败（${problems.length} 项）`)
  process.exit(1)
}
console.log(
  `前端约定检查通过：zh-CN ${maps['zh-CN'].size} 键 / en ${maps.en.size} 键、被引用 ${referenced.size} 键、` +
  `cczj-* 与 bc-* 定义 ${definedGlobally.size} 类、未引用键 ${unused.length} 个。`,
)
if (unused.length) process.exit(1)
