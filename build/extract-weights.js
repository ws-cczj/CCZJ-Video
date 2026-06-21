/**
 * GLSL 权重提取脚本 — 从 Anime4K 官方 GLSL 文件提取 CNN 权重
 * 用法: node build/extract-weights.js
 * 输出: frontend/src/utils/anime4kWeights_{M,L,VL}.ts
 */
const fs = require('fs')
const path = require('path')

const UTILS = path.join(__dirname, '..', 'frontend', 'src', 'utils')

// ==================== 解析工具 ====================

/** 从 GLSL hook 函数体提取所有 mat4(...) 权重和最后的 vec4(...) bias */
function extractWeightsFromHook(hookBody) {
  // 找 hook() { ... } 函数体
  const hookMatch = hookBody.match(/vec4\s+hook\s*\(\s*\)\s*\{([\s\S]*)\}/)
  if (!hookMatch) throw new Error('No hook() found in pass')
  const body = hookMatch[1]

  // 提取所有 mat4(...) 参数
  const mat4Re = /mat4\s*\(([\s\S]*?)\)\s*\*/g
  const matrices = []
  let m
  while ((m = mat4Re.exec(body)) !== null) {
    const values = m[1].split(',').map(s => s.trim()).filter(s => s.length > 0).map(Number)
    if (values.length !== 16) throw new Error(`mat4 has ${values.length} values instead of 16`)
    matrices.push(values)
  }

  // 提取 bias: 找 "result += vec4(...)" 或 "vec4(...);" 在最后一个 mat4 之后
  // 策略: 找所有 vec4(...) 出现，排除 mat4(...) 中的 vec4
  const biasMatch = body.match(/(?:\+=|^\s*)\s*vec4\s*\(([^)]+)\)\s*;/gm)
  let bias = null
  if (biasMatch) {
    // 最后一个 vec4 是 bias
    const lastBias = biasMatch[biasMatch.length - 1]
    const inner = lastBias.match(/vec4\s*\(([^)]+)\)/)
    if (inner) {
      const vals = inner[1].split(',').map(s => s.trim()).map(Number)
      if (vals.length === 4 && vals.every(v => !isNaN(v))) bias = vals
    }
  }

  // 也检查 "return result + MAIN_tex(MAIN_pos)" 形式（final pass with residual）
  const hasResidual = /return\s+result\s*\+\s*MAIN_tex/.test(body)

  // 检查 g_N 模式 (1×1 conv, 无参数)
  const isPointwise = /g_\d+[^_]/.test(body) || /\bresult\s*\+=\s*mat4.*\*\s*g_\d+/.test(body)

  return { matrices, bias, hasResidual, isPointwise, body }
}

/** 分割 GLSL 文件为各个 pass */
function splitPasses(glslContent) {
  const passHeaders = []
  const descRegex = /^\/\/!DESC\s+(.+)$/gm
  let m
  while ((m = descRegex.exec(glslContent)) !== null) {
    passHeaders.push({ desc: m[1], offset: m.index })
  }

  const passes = []
  for (let i = 0; i < passHeaders.length; i++) {
    const start = passHeaders[i].offset
    const end = i + 1 < passHeaders.length ? passHeaders[i + 1].offset : glslContent.length
    const content = glslContent.substring(start, end)
    const desc = passHeaders[i].desc

    // 判断 pass 类型
    const isDepth2Space = /Depth-to-Space/i.test(desc) || /Depth.to.Space/i.test(desc)
    const isSaveMain = /\/\/!SAVE\s+MAIN/.test(content) && !isDepth2Space
    const convMatch = desc.match(/Conv-(\d+)x(\d+)x(\d+)x(\d+)/)
    let convSize = null, kernelSize = 3, inCh = 0, outCh = 0
    if (convMatch) {
      inCh = parseInt(convMatch[1])
      kernelSize = parseInt(convMatch[2])
      outCh = parseInt(convMatch[4])
      convSize = { inCh, kernelSize, outCh }
    }

    passes.push({
      desc,
      content,
      isDepth2Space,
      isSaveMain,
      convSize,
    })
  }
  return passes
}

/** 格式化 float 为紧凑字符串 */
function fmt(v) {
  if (v === 0) return '0'
  if (Math.abs(v) >= 0.001 && Math.abs(v) < 10000) return v.toString()
  return v.toExponential(6)
}

function fmtMat4(values) {
  // GLSL mat4 column-major → 16 values
  return `mat4(${values.map(fmt).join(',')})`
}

function fmtBias(values) {
  return `vec4(${values.map(fmt).join(',')})`
}

// ==================== GLSL 到 WebGL2 转换 ====================

/** 将 GLSL hook 函数体转换为 WebGL2 片段着色器主体 */
function convertHookToWebGL2(hookBody, passInfo) {
  const { body, matrices, bias, hasResidual, isPointwise } = extractWeightsFromHook(hookBody)

  // 去掉 body 中的 #define 行（我们在 prelude 中定义）
  let cleanBody = body.replace(/#define\s+go_\d+.*\n/g, '')
  cleanBody = cleanBody.replace(/#define\s+g_\d+.*\n/g, '')

  // 替换 fragColor 输出
  if (hasResidual) {
    cleanBody = cleanBody.replace(/return\s+result\s*\+\s*MAIN_tex\(MAIN_pos\)/, 'fragColor = result + MAIN_tex(MAIN_pos)')
  } else {
    cleanBody = cleanBody.replace(/return\s+result\s*;/, 'fragColor = result')
  }

  return cleanBody.trim()
}

// ==================== Shader 模板生成 ====================

const FRAG_PRELUDE_TEMPLATE = `#version 300 es
precision highp float;
precision highp int;

in vec2 v_texCoord;
out vec4 fragColor;

uniform sampler2D u_input;
uniform sampler2D u_input1;
uniform sampler2D u_main;
uniform sampler2D u_video;
uniform vec2 u_texelSize;
uniform vec2 u_inputSize;
uniform vec2 u_inputTexel;
uniform float u_strength;
uniform float u_flipY;
`

/** 生成空间 3×3 卷积 shader（线性输入，首个 pass） */
function genSpatialLinearShader(matrices, bias) {
  let body = '    vec4 result = '
  body += `${fmtMat4(matrices[0])} * go_0(-1.0, -1.0);\n`
  for (let i = 1; i < matrices.length; i++) {
    const offsets = [[- 1, 0], [-1, 1], [0, -1], [0, 0], [0, 1], [1, -1], [1, 0], [1, 1]]
    const [dx, dy] = offsets[i - 1]
    body += `    result += ${fmtMat4(matrices[i])} * go_0(${dx}.0, ${dy}.0);\n`
  }
  body += `    result += ${fmtBias(bias)};\n`
  body += `    fragColor = result;\n`

  return FRAG_PRELUDE_TEMPLATE + `
#define MAIN_texOff(off) (texture(u_input, v_texCoord + (off) * u_texelSize))
#define go_0(x_off, y_off) (MAIN_texOff(vec2(x_off, y_off)))
void main() {
${body}}`
}

/** 生成空间 3×3 卷积 shader（Split ReLU 输入） */
function genSpatialSplitShader(matrices, bias, hasResidual) {
  let body = '    vec4 result = '
  const offsets = [[-1, -1], [-1, 0], [-1, 1], [0, -1], [0, 0], [0, 1], [1, -1], [1, 0], [1, 1]]
  const halfLen = matrices.length / 2 // 9 for go_0, 9 for go_1

  for (let pass = 0; pass < 2; pass++) {
    const goName = `go_${pass}`
    const startIdx = pass * halfLen
    for (let i = 0; i < halfLen; i++) {
      const [dx, dy] = offsets[i]
      const prefix = (pass === 0 && i === 0) ? '' : '    result += '
      if (pass === 0 && i === 0) {
        body += `${fmtMat4(matrices[startIdx + i])} * ${goName}(${dx}.0, ${dy}.0);\n`
      } else {
        body += `${prefix}${fmtMat4(matrices[startIdx + i])} * ${goName}(${dx}.0, ${dy}.0);\n`
      }
    }
  }
  body += `    result += ${fmtBias(bias)};\n`
  if (hasResidual) {
    body += `    fragColor = result + MAIN_tex(MAIN_pos);\n`
  } else {
    body += `    fragColor = result;\n`
  }

  return FRAG_PRELUDE_TEMPLATE + `
#define conv2d_tf_texOff(off) (texture(u_input, v_texCoord + (off) * u_texelSize))
#define go_0(x_off, y_off) (max((conv2d_tf_texOff(vec2(x_off, y_off))), 0.0))
#define go_1(x_off, y_off) (max(-(conv2d_tf_texOff(vec2(x_off, y_off))), 0.0))
void main() {
${body}}`
}

/** 生成 16ch 双纹理空间 3×3 卷积 shader（Split ReLU, 36 matrices, 双 MRT 输出） */
function genSpatialSplit16Shader(matrices, bias) {
  const offsets = [[-1,-1],[-1,0],[-1,1],[0,-1],[0,0],[0,1],[1,-1],[1,0],[1,1]]
  const quarterLen = matrices.length / 4 // 9 per group

  let body = '    vec4 result = '
  for (let g = 0; g < 4; g++) {
    const goName = `go_${g}`
    const startIdx = g * quarterLen
    for (let i = 0; i < quarterLen; i++) {
      const [dx, dy] = offsets[i]
      const prefix = (g === 0 && i === 0) ? '' : '    result += '
      body += `${prefix}${fmtMat4(matrices[startIdx + i])} * ${goName}(${dx}.0, ${dy}.0);\n`
    }
  }
  body += `    result += ${fmtBias(bias)};\n`
  body += `    fragColor = result;\n`

  return FRAG_PRELUDE_TEMPLATE + `
#define MAIN_texOff(off) (texture(u_input, v_texCoord + (off) * u_texelSize))
#define MAIN_texOff1(off) (texture(u_input1, v_texCoord + (off) * u_texelSize))
#define go_0(x_off, y_off) (max(MAIN_texOff(vec2(x_off, y_off)), 0.0))
#define go_1(x_off, y_off) (max(MAIN_texOff1(vec2(x_off, y_off)), 0.0))
#define go_2(x_off, y_off) (max(-MAIN_texOff(vec2(x_off, y_off)), 0.0))
#define go_3(x_off, y_off) (max(-MAIN_texOff1(vec2(x_off, y_off)), 0.0))
void main() {
${body}}`
}

/** 生成 16ch 双纹理最终空间 3×3 卷积 shader（Split ReLU + residual, 36 matrices） */
function genSpatialSplit16FinalShader(matrices, bias) {
  const offsets = [[-1,-1],[-1,0],[-1,1],[0,-1],[0,0],[0,1],[1,-1],[1,0],[1,1]]
  const quarterLen = matrices.length / 4 // 9

  let body = '    vec4 result = '
  for (let g = 0; g < 4; g++) {
    const goName = `go_${g}`
    const startIdx = g * quarterLen
    for (let i = 0; i < quarterLen; i++) {
      const [dx, dy] = offsets[i]
      const prefix = (g === 0 && i === 0) ? '' : '    result += '
      body += `${prefix}${fmtMat4(matrices[startIdx + i])} * ${goName}(${dx}.0, ${dy}.0);\n`
    }
  }
  body += `    result += ${fmtBias(bias)};\n`
  body += `    fragColor = result + MAIN_tex(MAIN_pos);\n`

  return FRAG_PRELUDE_TEMPLATE + `
#define MAIN_pos v_texCoord
#define MAIN_tex(pos) texture(u_main, pos)
#define MAIN_texOff(off) (texture(u_input, v_texCoord + (off) * u_texelSize))
#define MAIN_texOff1(off) (texture(u_input1, v_texCoord + (off) * u_texelSize))
#define go_0(x_off, y_off) (max(MAIN_texOff(vec2(x_off, y_off)), 0.0))
#define go_1(x_off, y_off) (max(MAIN_texOff1(vec2(x_off, y_off)), 0.0))
#define go_2(x_off, y_off) (max(-MAIN_texOff(vec2(x_off, y_off)), 0.0))
#define go_3(x_off, y_off) (max(-MAIN_texOff1(vec2(x_off, y_off)), 0.0))
void main() {
${body}}`
}

/** 从 pass content 提取 //!BIND 指令中的纹理名称（排除 MAIN） */
function extractBindNames(passContent) {
  const bindRe = /\/\/!BIND\s+(\S+)/g
  const names = []
  let m
  while ((m = bindRe.exec(passContent)) !== null) {
    if (m[1] !== 'MAIN') names.push(m[1])
  }
  return names
}

/** 生成 1×1 pointwise 卷积 shader（Split ReLU，WebGL2 uniform sampler2D） */
function genPointwiseSplitShader(matrices, bias, numGroups, hasResidual, texNames) {
  // 生成 uniform sampler2D 声明
  let uniformDecls = ''
  for (let i = 0; i < texNames.length; i++) {
    uniformDecls += `uniform sampler2D u_pw${i};\n`
  }

  // 生成 g_N 定义: 每个纹理 → pos(+max) + neg(-max) = 2 groups
  let gDefines = ''
  for (let g = 0; g < numGroups; g++) {
    const texIdx = Math.floor(g / 2)
    const isNeg = (g % 2) === 1
    const sign = isNeg ? '-' : ''
    gDefines += `#define g_${g} (max(${sign}(texture(u_pw${texIdx}, v_texCoord)), 0.0))\n`
  }

  // 生成 MAIN 残差宏（如果需要）
  let mainDefines = ''
  if (hasResidual) {
    mainDefines += '#define MAIN_pos v_texCoord\n'
    mainDefines += '#define MAIN_tex(pos) texture(u_main, pos)\n'
  }

  // 生成 main() 函数体
  let body = '    vec4 result = '
  for (let i = 0; i < matrices.length; i++) {
    const prefix = i === 0 ? '' : '    result += '
    body += `${prefix}${fmtMat4(matrices[i])} * g_${i};\n`
  }
  body += `    result += ${fmtBias(bias)};\n`
  if (hasResidual) {
    body += `    fragColor = result + MAIN_tex(MAIN_pos);\n`
  } else {
    body += `    fragColor = result;\n`
  }

  return FRAG_PRELUDE_TEMPLATE + `
${uniformDecls}${mainDefines}${gDefines}void main() {
${body}}`
}

// ==================== 主流程 ====================

function extractAndGenerate(modelName, restoreFile, upscaleFile) {
  console.log(`\n=== Processing ${modelName} ===`)

  const restoreGlsl = fs.readFileSync(path.join(UTILS, restoreFile), 'utf-8')
  const upscaleGlsl = fs.readFileSync(path.join(UTILS, upscaleFile), 'utf-8')

  const restorePasses = splitPasses(restoreGlsl)
  const upscalePasses = splitPasses(upscaleGlsl)

  console.log(`  Restore: ${restorePasses.length} passes`)
  console.log(`  Upscale: ${upscalePasses.length} passes`)

  // 对每个 pass 提取权重并生成 shader
  const shaderDefs = []

  function processPass(pass, idx, section) {
    if (pass.isDepth2Space) {
      console.log(`  [${section}-${idx}] Depth2Space (special)`)
      return
    }

    const { matrices, bias, hasResidual, isPointwise } = extractWeightsFromHook(pass.content)
    const convSize = pass.convSize
    console.log(`  [${section}-${idx}] ${pass.desc.trim()} | ${matrices.length} matrices | residual=${hasResidual} | pointwise=${isPointwise}`)

    let shaderSource
    if (isPointwise) {
      // 1×1 pointwise conv — 从 GLSL BIND 指令提取纹理名称
      const numGroups = matrices.length
      const bindNames = extractBindNames(pass.content)
      console.log(`    pointwise: ${numGroups} groups, ${bindNames.length} source textures: ${bindNames.join(', ')}`)
      shaderSource = genPointwiseSplitShader(matrices, bias, numGroups, hasResidual, bindNames)
      shaderDefs.push({ section, idx, desc: pass.desc, source: shaderSource, pwSources: bindNames })
      return
    }
    if (convSize && convSize.kernelSize === 3) {
      if (matrices.length === 9) {
        // Linear spatial 3×3 (first pass)
        shaderSource = genSpatialLinearShader(matrices, bias)
      } else if (matrices.length === 18) {
        // Split ReLU spatial 3×3 (8ch)
        shaderSource = genSpatialSplitShader(matrices, bias, hasResidual)
      } else if (matrices.length === 36) {
        // Split ReLU spatial 3×3 (16ch dual-texture)
        if (hasResidual) {
          shaderSource = genSpatialSplit16FinalShader(matrices, bias)
        } else {
          shaderSource = genSpatialSplit16Shader(matrices, bias)
        }
      } else {
        console.log(`    WARNING: unexpected ${matrices.length} matrices for spatial conv`)
        return
      }
    } else {
      console.log(`    WARNING: unknown pass type, ${matrices.length} matrices`)
      return
    }

    shaderDefs.push({ section, idx, desc: pass.desc, source: shaderSource })
  }

  restorePasses.forEach((p, i) => processPass(p, i, 'R'))
  upscalePasses.forEach((p, i) => processPass(p, i, 'U'))

  console.log(`  Generated ${shaderDefs.length} shader sources`)

  // 写入 TypeScript 文件
  let ts = `/**\n * Anime4K ${modelName} 模型 — 自动从 GLSL 提取的 CNN shader 权重\n * 来源: bloc97/Anime4K (MIT License)\n * 生成时间: ${new Date().toISOString()}\n */\n\n`
  ts += `import type { PassShaderDef } from './anime4kModels'\n\n`

  for (const def of shaderDefs) {
    ts += `// ${def.desc.trim()}\n`
    ts += `export const FS_${def.section}${def.idx}: PassShaderDef = {\n`
    ts += `  desc: ${JSON.stringify(def.desc.trim())},\n`
    ts += `  source: ${JSON.stringify(def.source)},\n`
    if (def.pwSources) {
      ts += `  pwSources: ${JSON.stringify(def.pwSources)},\n`
    }
    ts += `}\n\n`
  }

  const outFile = path.join(UTILS, `anime4kWeights_${modelName}.ts`)
  fs.writeFileSync(outFile, ts, 'utf-8')
  console.log(`  Written to ${outFile} (${(ts.length / 1024).toFixed(1)} KB)`)
}

// 处理三个模型
extractAndGenerate('M', 'Restore_CNN_M.glsl', 'Upscale_CNN_x2_M.glsl')
extractAndGenerate('L', 'Restore_CNN_L.glsl', 'Upscale_CNN_x2_L.glsl')
extractAndGenerate('VL', 'Restore_CNN_VL.glsl', 'Upscale_CNN_x2_VL.glsl')

console.log('\nDone!')
