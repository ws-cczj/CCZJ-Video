/**
 * 从 anime4kUpscaler.ts 提取 S 模型的 shader 源码，生成 anime4kWeights_S.ts
 * 用法: node build/extract-s-weights.js
 */
const fs = require('fs')
const path = require('path')
const vm = require('vm')

const UTILS = path.join(__dirname, '..', 'frontend', 'src', 'utils')
const srcFile = path.join(UTILS, 'anime4kUpscaler.ts')
const outFile = path.join(UTILS, 'anime4kWeights_S.ts')

const src = fs.readFileSync(srcFile, 'utf-8')

// 提取 FRAG_PRELUDE 模板字符串
const preludeMatch = src.match(/const FRAG_PRELUDE = \/\* glsl \*\/ `([\s\S]*?)`/)
if (!preludeMatch) throw new Error('FRAG_PRELUDE not found')
const FRAG_PRELUDE = preludeMatch[1]

// 提取所有 FS_XX 常量（它们是 FRAG_PRELUDE + `...` 形式）
const shaderNames = ['FS_R1', 'FS_R2', 'FS_R3', 'FS_R4', 'FS_U1', 'FS_U2', 'FS_U3', 'FS_U4', 'FS_U5']
const shaders = {}

for (const name of shaderNames) {
  // Match: const FS_XX = FRAG_PRELUDE + /* glsl */ `...`
  const pattern = 'const ' + name + ' = FRAG_PRELUDE \\+ \\/\\* glsl \\*\\/ `([\\s\\S]*?)`'
  const re = new RegExp(pattern)
  const m = src.match(re)
  if (!m) throw new Error(name + ' not found')
  shaders[name] = FRAG_PRELUDE + m[1]
}

// 生成 TypeScript 输出
let ts = `/**
 * Anime4K S 模型 — 从 anime4kUpscaler.ts 提取的 CNN shader 权重
 * 来源: bloc97/Anime4K (MIT License)
 * 生成时间: ${new Date().toISOString()}
 */

import type { PassShaderDef } from './anime4kEngine'

`

const descs = {
  FS_R1: 'Restore_CNN_S Conv-4x3x3x3 (linear, video→tex0)',
  FS_R2: 'Restore_CNN_S Conv-4x3x3x8 (split ReLU, tex0→tex1)',
  FS_R3: 'Restore_CNN_S Conv-4x3x3x8 (split ReLU, tex1→tex0)',
  FS_R4: 'Restore_CNN_S Conv-3x3x3x8 (split ReLU+residual, tex0→tex1)',
  FS_U1: 'Upscale_CNN_x2_S Conv-4x3x3x3 (linear, tex1→tex2)',
  FS_U2: 'Upscale_CNN_x2_S Conv-4x3x3x8 (split ReLU, tex2→tex0)',
  FS_U3: 'Upscale_CNN_x2_S Conv-4x3x3x8 (split ReLU, tex0→tex2)',
  FS_U4: 'Upscale_CNN_x2_S Conv-4x3x3x8 (split ReLU, tex2→tex0)',
  FS_U5: 'Upscale_CNN_x2_S Depth2Space (tex0+residual+blend→canvas)',
}

for (const name of shaderNames) {
  ts += `// ${descs[name]}\n`
  ts += `export const ${name}: PassShaderDef = {\n`
  ts += `  desc: ${JSON.stringify(descs[name])},\n`
  ts += `  source: ${JSON.stringify(shaders[name])},\n`
  ts += `}\n\n`
}

fs.writeFileSync(outFile, ts, 'utf-8')
console.log(`Written ${outFile} (${(ts.length / 1024).toFixed(1)} KB, ${shaderNames.length} shaders)`)
