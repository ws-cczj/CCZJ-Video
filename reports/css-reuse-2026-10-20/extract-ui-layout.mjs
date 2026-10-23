import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { root, postcss, parseSFC, templateElements, classNames, filesBelow, utilityRules } from '../../scripts/audit-cczj-styles.mjs'

const properties = new Set(['display','align-items','justify-content','gap','flex-direction','flex-wrap','flex-shrink','min-width','min-height'])
const utilities = new Map(utilityRules().filter(rule => !rule.conditional && rule.declarations.length === 1 && properties.has(rule.declarations[0].property)).map(rule => [rule.name,rule]))
let files = 0, moved = 0
for (const file of filesBelow(join(root,'frontend/src/components/ui'), /\.vue$/)) {
  const original = readFileSync(file,'utf8'), descriptor = parseSFC(original,{filename:file}).descriptor
  if (!descriptor.template) continue
  const elements = templateElements(descriptor.template.content)
  const css = descriptor.styles.map(style=>({style,css:postcss.parse(style.content)})), candidates = new Map()
  for (const {css:sheet} of css) sheet.walkRules(rule => {
    if (rule.parent.type !== 'root' || !/^\.[\w-]+$/.test(rule.selector)) return
    const name = rule.selector.slice(1), matches = elements.filter(element=>classNames(element).has(name))
    if (!matches.length || matches.some(element=>element.tagType!==0 || !element.props.some(prop=>prop.type===6&&prop.name==='class'&&prop.value?.content.split(/\s+/).includes(name)))) return
    const tokens = rule.nodes.filter(node=>node.type==='atrule'&&node.name==='apply').flatMap(node=>node.params.split(/\s+/)).filter(token=>utilities.has(token))
    if (tokens.length) candidates.set(name,{rule,matches,tokens})
  })
  if (!candidates.size) continue
  const nodeTokens = new Map()
  for (const {rule,matches,tokens} of candidates.values()) {
    for (const element of matches) nodeTokens.set(element,new Set([...(nodeTokens.get(element)??[]),...tokens]))
    rule.walkAtRules('apply',at=>{
      const kept=at.params.split(/\s+/).filter(token=>!tokens.includes(token))
      if(kept.length)at.params=kept.join(' ');else at.remove()
    })
    moved+=tokens.length
    if(!rule.nodes.length)rule.remove()
  }
  const changes=[]
  for(const [element,tokens] of nodeTokens){
    const attr=element.props.find(prop=>prop.type===6&&prop.name==='class')
    changes.push({start:descriptor.template.loc.start.offset+attr.value.loc.start.offset,end:descriptor.template.loc.start.offset+attr.value.loc.end.offset,value:JSON.stringify([...new Set([...attr.value.content.split(/\s+/),...tokens])].join(' '))})
  }
  for(const {style,css:sheet} of css)changes.push({start:style.loc.start.offset,end:style.loc.end.offset,value:sheet.toString()})
  let source=original
  for(const change of changes.sort((a,b)=>b.start-a.start))source=source.slice(0,change.start)+change.value+source.slice(change.end)
  writeFileSync(file,source);files++
}
console.log(JSON.stringify({files,layoutDeclarationsMovedToTemplates:moved}))
