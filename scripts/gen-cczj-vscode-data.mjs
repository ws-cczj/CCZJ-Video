import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { root, postcss, utilityRules } from './audit-cczj-styles.mjs'

const css = postcss.parse(readFileSync(join(root, 'frontend/src/styles/cczj-utilities.css'), 'utf8'))
const bySelector = new Map(utilityRules().map(rule => [rule.selector, rule]))
const sections = new Map()
let section = '通用'
for (const node of css.nodes) {
  if (node.type === 'comment') {
    const title = node.text.match(/(?:^|\n)\s*(\d+\.\s*[^\n]+)/)
    if (title) section = title[1].trim()
  }
  if (node.type !== 'rule') continue
  const rule = bySelector.get(node.selector)
  if (!rule) throw new Error(`Unrecognised CCZJ rule: ${node.selector}`)
  if (!sections.has(section)) sections.set(section, [])
  sections.get(section).push(rule)
}
const declarations = rule => rule.declarations.map(decl => `${decl.property}: ${decl.value}${decl.important ? ' !important' : ''};`).join(' ')
const values = [...sections].flatMap(([section, rules]) => rules.map(rule => ({name:rule.name,description:`**[${section}]**\n\`\`\`css\n${rule.selector} { ${declarations(rule)} }\n\`\`\`${rule.conditional ? '\n条件类仅用于模板；不可用于 @apply。' : ''}`})))
const htmlData = {version:1.1,globalAttributes:[{name:'class',valueSet:'cczj-classes'}],valueSets:[{name:'cczj-classes',values}]}
const cssData = {version:1.1,atDirectives:[{name:'@apply',description:'在 Vue / 构建期 CSS 中组合 cczj-utilities.css 的静态工具类，保持当前选择器、作用域与声明顺序。动态注入的扩展样式不经过此构建步骤。'}]}
let doc = '# CCZJ 原子样式速查手册\n\n> 由 `scripts/gen-cczj-vscode-data.mjs` 从 `frontend/src/styles/cczj-utilities.css` 生成，请修改源 CSS 后重新生成。\n\n'
doc += '模板使用 `class="cczj-flex cczj-gap-4"`；作用域、状态或响应式规则可写 `.toolbar { @apply cczj-flex cczj-gap-4; }`。两者读取同一份定义。用法及边界见 [CCZJ 样式复用](cczj-styles.md)。\n\n'
doc += '间距数字沿用 2px 单位；历史尺寸类存在固定像素与 4px 单位两套命名，表中实际声明为准，不能按类名猜测。旧类保持兼容，不改变已有页面尺寸。\n\n'
for (const [section, rules] of sections) {
  doc += `### ${section}\n\n| 类名 | 生效样式 |\n| --- | --- |\n`
  for (const rule of rules) doc += `| \`${rule.name}\`${rule.conditional ? '（条件类）' : ''} | \`${declarations(rule)}\` |\n`
  doc += '\n'
}
const outputs = [
  ['.vscode/cczj.html-data.json',JSON.stringify(htmlData,null,2)+'\n'],
  ['.vscode/cczj.css-data.json',JSON.stringify(cssData,null,2)+'\n'],
  ['docs/cczj-utilities-cheatsheet.md',doc],
]
if (process.argv.includes('--check')) {
  // Git may check out generated text with CRLF on Windows. Compare content
  // using the generator's LF convention without ignoring other differences.
  const stale = outputs.filter(([path, text])=>!existsSync(join(root,path))||readFileSync(join(root,path),'utf8').replace(/\r\n/g,'\n')!==text)
  if (stale.length) {console.error('CCZJ CSS data is stale: '+stale.map(([path])=>path).join(', ')+'. Run npm --prefix frontend run gen:css-data.');process.exitCode=1}
  else console.log(`CCZJ CSS data: ${values.length} classes, documentation and editor data are current`)
} else {
  mkdirSync(join(root,'.vscode'),{recursive:true})
  for (const [path,text] of outputs) writeFileSync(join(root,path),text)
  console.log(`Generated CCZJ CSS documentation and editor data (${values.length} classes)`)
}
