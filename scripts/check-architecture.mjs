import { readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const project = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(join(project, 'frontend/package.json'))
const ts = require('typescript')
const { parse } = require('@vue/compiler-sfc')

// Parse executable code: comments and template text are not dependency edges.
export function checkSource(source, file) {
  const path = file.replaceAll('\\', '/')
  const fragments = path.endsWith('.vue')
    ? (() => { const { descriptor } = parse(source, { filename: path }); return [descriptor.script, descriptor.scriptSetup].filter(Boolean).map(block => block.content) })()
    : [source]
  const problems = new Set()
  for (const fragment of fragments) {
    const ast = ts.createSourceFile(path + '.ts', fragment, ts.ScriptTarget.Latest, true)
    function visit(node) {
      let module
      if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) module = node.moduleSpecifier
      if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) module = node.arguments[0]
      if (module && ts.isStringLiteralLike(module) && !path.startsWith('frontend/src/api/') &&
          (module.text === '@wailsio/runtime' || module.text.includes('bindings/cczjVideo/app'))) {
        problems.add(`${path}: use frontend/src/api for ${module.text}`)
      }
      if (module && ts.isStringLiteralLike(module) && path.startsWith('frontend/src/api/') &&
          path !== 'frontend/src/api/app.ts' && module.text.includes('bindings/cczjVideo/app/service/app')) {
        problems.add(`${path}: use api/app so the request policy cannot be bypassed`)
      }
      if (!path.startsWith('frontend/src/platform/')) {
        const directStorage = ts.isIdentifier(node) && node.text === 'localStorage' &&
          !(ts.isPropertyAccessExpression(node.parent) && node.parent.name === node)
        const qualifiedStorage = ts.isPropertyAccessExpression(node) && node.name.text === 'localStorage' &&
          ts.isIdentifier(node.expression) && ['window', 'globalThis'].includes(node.expression.text)
        const indexedStorage = ts.isElementAccessExpression(node) && ts.isStringLiteralLike(node.argumentExpression) &&
          node.argumentExpression.text === 'localStorage' && ts.isIdentifier(node.expression) && ['window', 'globalThis'].includes(node.expression.text)
        if (directStorage || qualifiedStorage || indexedStorage) problems.add(`${path}: use frontend/src/platform for browser persistence`)
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }
  return [...problems]
}

function checkTree(root, dir) {
  const problems = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) problems.push(...checkTree(root, path))
    else if (/\.(ts|tsx|vue)$/.test(entry.name)) problems.push(...checkSource(readFileSync(path, 'utf8'), relative(root, path)))
  }
  return problems
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = resolve(process.argv[2] ?? project)
  const problems = checkTree(root, join(root, 'frontend/src'))
  if (problems.length) { console.error(problems.join('\n')); process.exitCode = 1 }
  else console.log('Frontend dependency and storage checks passed.')
}
