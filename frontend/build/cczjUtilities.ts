import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import postcss, { type Declaration, type Plugin } from 'postcss'

const defaultLibrary = fileURLToPath(new URL('../src/styles/cczj-utilities.css', import.meta.url))

/** Compose the existing utilities inside scoped, state and responsive selectors.
 * Expansion keeps declaration order and selector specificity; no runtime or
 * second utility framework is involved. The CSS file remains the only definition.
 */
export function cczjUtilities(library = defaultLibrary): Plugin {
  let previous = ''
  let definitions = new Map<string, Declaration[]>()
  function readDefinitions(): Map<string, Declaration[]> {
    const source = readFileSync(library, 'utf8')
    if (source === previous) return definitions
    const next = new Map<string, Declaration[]>()
    postcss.parse(source, { from: library }).walkRules(rule => {
      const match = rule.selector.match(/^\.((?:[\w-]|\\.)+)$/)
      if (!match) return // Pseudo classes belong in the template, never flattened.
      const name = match[1]!.replace(/\\(.)/g, '$1')
      const declarations = rule.nodes.filter((node): node is Declaration => node.type === 'decl')
      if (!name.startsWith('cczj-') || declarations.length !== rule.nodes.length || next.has(name)) {
        throw rule.error(`Invalid CCZJ utility definition: ${rule.selector}`)
      }
      next.set(name, declarations)
    })
    previous = source
    definitions = next
    return definitions
  }
  return {
    postcssPlugin: 'cczj-utilities',
    Once(root, { result }) {
      let utilities: Map<string, Declaration[]> | undefined
      root.walkAtRules('apply', rule => {
        if (rule.parent?.type !== 'rule') throw rule.error('@apply must be inside a CSS selector')
        const names = rule.params.trim().split(/\s+/)
        const available = utilities ??= readDefinitions()
        const expanded = names.flatMap(name => {
          const declarations = available.get(name)
          if (!declarations) throw rule.error(`Unknown or conditional CCZJ utility: ${name}`)
          return declarations.map(declaration => {
            const clone = declaration.clone()
            clone.source = rule.source
            return clone
          })
        })
        rule.replaceWith(...expanded)
      })
      if (utilities) result.messages.push({ type: 'dependency', plugin: 'cczj-utilities', file: library })
    },
  }
}
