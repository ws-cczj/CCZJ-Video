import { describe, expect, it } from 'vitest'
import postcss from 'postcss'
import { compileStyle } from '@vue/compiler-sfc'
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { cczjUtilities } from '../build/cczjUtilities'
import { sharedLayoutViolations } from '../../scripts/audit-cczj-styles.mjs'

describe('CCZJ CSS composition', () => {
  it('rejects copied layout declarations while preserving domain dimensions and animation states', () => {
    const source = '.row{display:flex;align-items:center;min-width:0;width:393px;color:var(--novel-ink)} @media(max-width:600px){.row{display:grid}} @keyframes appear{from{display:none;opacity:0}to{opacity:1}}'
    expect(sharedLayoutViolations(source).map(item => item.utility)).toEqual(['cczj-flex','cczj-items-center','cczj-min-w-0','cczj-grid'])
    expect(sharedLayoutViolations('.row{@apply cczj-flex cczj-items-center;width:393px;display:flex!important}')).toEqual([])
  })
  it('keeps declaration order, scoped specificity and state/media boundaries', () => {
    const source = '.card { color:red; @apply cczj-flex cczj-gap-4; gap:10px } @media(max-width:600px){.card:hover{ @apply cczj-grid cczj-gap-2; }}'
    const result = compileStyle({ source, filename: 'Card.vue', id: 'data-v-test', scoped: true, postcssPlugins: [cczjUtilities()] })
    expect(result.errors).toEqual([])
    const css = postcss.parse(result.code), records: string[] = []
    css.walkDecls(declaration => { records.push(`${declaration.parent?.type === 'rule' ? declaration.parent.selector : ''}:${declaration.prop}:${declaration.value}`) })
    expect(records).toEqual(['.card[data-v-test]:color:red','.card[data-v-test]:display:flex','.card[data-v-test]:gap:8px','.card[data-v-test]:gap:10px','.card[data-v-test]:hover:display:grid','.card[data-v-test]:hover:gap:4px'])
    expect(result.code).toContain('@media(max-width:600px)')
    expect(result.code).not.toContain('@apply')
  })

  it('expands compound utilities and escaped class names without changing their units', async () => {
    const result = await postcss([cczjUtilities()]).process('.label{@apply cczj-truncate cczj-select-none cczj-h-1.5 cczj-w-8 cczj-w-16}', { from: undefined })
    const declarations: string[] = []
    postcss.parse(result.css).walkDecls(declaration => { declarations.push(`${declaration.prop}:${declaration.value}`) })
    expect(declarations).toEqual(['overflow:hidden','text-overflow:ellipsis','white-space:nowrap','user-select:none','-webkit-user-select:none','height:6px','width:32px','width:16px'])
  })

  it.each(['.x{@apply cczj-missing}', '.x{@apply cczj-hover-underline}', '@apply cczj-flex;'])('rejects unsupported composition: %s', async source => {
    await expect(postcss([cczjUtilities()]).process(source, { from: undefined })).rejects.toThrow(/CCZJ utility|inside a CSS selector/)
  })

  it('rebuilds consumers after the shared library changes and reports the dependency', async () => {
    const directory = mkdtempSync(join(tmpdir(), 'cczj-css-test-'))
    const path = join(directory, 'utilities.css')
    try {
      writeFileSync(path, '.cczj-gap-4{gap:8px}')
      const processor = postcss([cczjUtilities(path)])
      const before = await processor.process('.row{@apply cczj-gap-4}', { from: undefined })
      writeFileSync(path, '.cczj-gap-4{gap:10px}')
      const after = await processor.process('.row{@apply cczj-gap-4}', { from: undefined })
      expect(before.css).toContain('gap:8px')
      expect(after.css).toContain('gap:10px')
      expect(after.messages).toContainEqual({ type:'dependency',plugin:'cczj-utilities',file:path })
    } finally { rmSync(directory, { recursive:true }) }
  })
})
