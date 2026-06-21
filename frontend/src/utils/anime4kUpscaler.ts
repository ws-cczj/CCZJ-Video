/**
 * Anime4K 动画超分辨率增强器 — WebGL2 实时 CNN 超分（多档位版）
 *
 * 移植自 bloc97 的 Anime4K v4.0 (MIT License, Copyright (c) 2019-2021 bloc97)。
 * 支持 S / M / L / VL 四档模型，对动画/动漫做 2× CNN 超分辨率重建。
 *
 * 需要 EXT_color_buffer_float 扩展（RGBA16F 渲染目标）。
 * 所有计算在 GPU 上完成，外部 API: init/start/stop/destroy/updateOptions/getStats。
 */

import type { EnginePassDef, Anime4kModelConfig, ModelStructure } from './anime4kModels'
import { TEX_VIDEO, buildPipeline } from './anime4kModels'
import * as WS from './anime4kWeights_S'
import * as WM from './anime4kWeights_M'
import * as WL from './anime4kWeights_L'
import * as WVL from './anime4kWeights_VL'

// ==================== 类型定义 ====================

export type Anime4kTier = 'S' | 'M' | 'L' | 'VL'

export interface UpscalerOptions {
  outputWidth?: number
  outputHeight?: number
  strength?: number
  upscale?: number
  /** 模型档位: S(快/轻量) 或 M(高质量) */
  tier?: Anime4kTier
  mode?: string
  sharpness?: number
  contrast?: number
  deband?: number
  edgeEnhance?: number
  denoise?: number
  temporalBlend?: number
}

export interface UpscalerStats {
  fps: number
  frameCount: number
  elapsed: number
  inputSize: string
  outputSize: string
  gpuEnabled: boolean
}

// ==================== 预设参数 ====================

type PresetFields = Required<Pick<UpscalerOptions, 'strength' | 'upscale'>>

export const ANIME4K_PRESET: PresetFields = {
  strength: 1.0,
  upscale: 2.0,
}

// ==================== 顶点着色器 ====================

const VERTEX_SHADER = /* glsl */ `#version 300 es
layout(location = 0) in vec2 a_position;
layout(location = 1) in vec2 a_texCoord;
out vec2 v_texCoord;
uniform float u_flipY;
void main() {
  gl_Position = vec4(a_position, 0.0, 1.0);
  v_texCoord = vec2(a_texCoord.x, mix(a_texCoord.y, 1.0 - a_texCoord.y, u_flipY));
}`

// ==================== Depth2Space shaders ====================

// S/M 通用 Depth2Space（单纹理输入，4 通道 = 2×2 子像素位置，同一值应用到 RGB）
const DEPTH2SPACE_SOURCE = /* glsl */ `#version 300 es
precision highp float;
precision highp int;
in vec2 v_texCoord;
out vec4 fragColor;
uniform sampler2D u_input;
uniform sampler2D u_main;
uniform sampler2D u_video;
uniform vec2 u_texelSize;
uniform vec2 u_inputSize;
uniform vec2 u_inputTexel;
uniform float u_strength;
uniform float u_flipY;
#define MAIN_pos v_texCoord
#define MAIN_tex(pos) texture(u_main, pos)
void main() {
    // M/S 模型: pointwise 输出 4 通道 = 2x2 子像素位置的同一颜色值,
    // 按输出像素所在的子像素位置 i0 选取对应通道, 再加到 restore 残差上。
    vec2 f0 = fract(v_texCoord * u_inputSize);
    ivec2 i0 = ivec2(f0 * vec2(2.0));
    vec2 sampleCoord = (vec2(0.5) - f0) * u_inputTexel + v_texCoord;
    float c0 = texture(u_input, sampleCoord)[i0.y * 2 + i0.x];
    vec3 a4k = vec3(c0) + MAIN_tex(MAIN_pos).rgb;
    vec3 rawUpscale = texture(u_video, v_texCoord).rgb;
    fragColor = vec4(mix(rawUpscale, a4k, clamp(u_strength, 0.0, 1.0)), 1.0);
}`

// L 模型 Depth2Space（三纹理输入 u_input + u_input1 + u_input2，RGB 通道分离）
const DEPTH2SPACE_L_SOURCE = /* glsl */ `#version 300 es
precision highp float;
precision highp int;
in vec2 v_texCoord;
out vec4 fragColor;
uniform sampler2D u_input;
uniform sampler2D u_input1;
uniform sampler2D u_input2;
uniform sampler2D u_main;
uniform sampler2D u_video;
uniform vec2 u_texelSize;
uniform vec2 u_inputSize;
uniform vec2 u_inputTexel;
uniform float u_strength;
uniform float u_flipY;
#define MAIN_pos v_texCoord
#define MAIN_tex(pos) texture(u_main, pos)
void main() {
    vec2 f0 = fract(v_texCoord * u_inputSize);
    ivec2 i0 = ivec2(f0 * vec2(2.0));
    vec2 sampleCoord = (vec2(0.5) - f0) * u_inputTexel + v_texCoord;
    float c0 = texture(u_input, sampleCoord)[i0.y * 2 + i0.x];
    float c1 = texture(u_input1, sampleCoord)[i0.y * 2 + i0.x];
    float c2 = texture(u_input2, sampleCoord)[i0.y * 2 + i0.x];
    float c3 = c2;
    vec3 a4k = vec4(c0, c1, c2, c3).rgb + MAIN_tex(MAIN_pos).rgb;
    vec3 rawUpscale = texture(u_video, v_texCoord).rgb;
    fragColor = vec4(mix(rawUpscale, a4k, clamp(u_strength, 0.0, 1.0)), 1.0);
}`

// ==================== 模型配置 ====================

function getSModel(): ModelStructure {
  return {
    restore: {
      linear: [WS.FS_R1],
      split: [WS.FS_R2, WS.FS_R3],
      final: WS.FS_R4,
    },
    upscale: {
      linear: [WS.FS_U1],
      split: [WS.FS_U2, WS.FS_U3, WS.FS_U4],
    },
    depth2Space: { desc: 'Depth2Space', source: DEPTH2SPACE_SOURCE },
  }
}

function getMModel(): ModelStructure {
  return {
    restore: {
      linear: [WM.FS_R0],
      split: [WM.FS_R1, WM.FS_R2, WM.FS_R3, WM.FS_R4, WM.FS_R5, WM.FS_R6],
      final: WM.FS_R7,
    },
    upscale: {
      linear: [WM.FS_U0],
      split: [WM.FS_U1, WM.FS_U2, WM.FS_U3, WM.FS_U4, WM.FS_U5, WM.FS_U6],
      pointwise: [WM.FS_U7],
    },
    depth2Space: { desc: 'Depth2Space', source: DEPTH2SPACE_SOURCE },
  }
}

function getLModel(): ModelStructure {
  return {
    restore: {
      linear: [WL.FS_R0, WL.FS_R1],
      split16: [WL.FS_R2, WL.FS_R3, WL.FS_R4, WL.FS_R5, WL.FS_R6, WL.FS_R7],
      final: WL.FS_R8,
    },
    upscale: {
      linear: [WL.FS_U0, WL.FS_U1],
      split16: [WL.FS_U2, WL.FS_U3, WL.FS_U4, WL.FS_U5, WL.FS_U6, WL.FS_U7],
      finalSpatial: WL.FS_U8,
    },
    depth2Space: { desc: 'Depth2Space-L', source: DEPTH2SPACE_L_SOURCE },
  }
}

function getVLModel(): ModelStructure {
  return {
    restore: {
      linear: [WVL.FS_R0, WVL.FS_R1],
      split16: [
        WVL.FS_R2, WVL.FS_R3, WVL.FS_R4, WVL.FS_R5, WVL.FS_R6, WVL.FS_R7,
        WVL.FS_R8, WVL.FS_R9, WVL.FS_R10, WVL.FS_R11, WVL.FS_R12, WVL.FS_R13, WVL.FS_R14,
      ],
      final: WVL.FS_R15,
    },
    upscale: {
      linear: [WVL.FS_U0, WVL.FS_U1],
      split16: [
        WVL.FS_U2, WVL.FS_U3, WVL.FS_U4, WVL.FS_U5, WVL.FS_U6, WVL.FS_U7,
        WVL.FS_U8, WVL.FS_U9, WVL.FS_U10, WVL.FS_U11, WVL.FS_U12, WVL.FS_U13,
      ],
      pointwise: [WVL.FS_U14, WVL.FS_U15, WVL.FS_U16],
    },
    // VL 的 Depth2Space 读取 3 个 conv2d_last_tf/tf1/tf2 (3 个 pointwise 输出),
    // 必须用三纹理版本, 否则 c1/c2 退化为 c0 导致输出偏暗。
    depth2Space: { desc: 'Depth2Space-L', source: DEPTH2SPACE_L_SOURCE },
  }
}

function getModelStructure(tier: Anime4kTier): ModelStructure {
  switch (tier) {
    case 'M': return getMModel()
    case 'L': return getLModel()
    case 'VL': return getVLModel()
    default: return getSModel()
  }
}

// ==================== WebGL2 引擎 ====================

interface UniformMap {
  [key: string]: WebGLUniformLocation | null
}

export class Anime4kUpscaler {
  private gl: WebGL2RenderingContext | null = null
  private canvas: HTMLCanvasElement | null = null
  private programs: WebGLProgram[] = []
  private uniforms: UniformMap[] = []
  private vao: WebGLVertexArrayObject | null = null
  private textures: (WebGLTexture | null)[] = []
  private fbo: WebGLFramebuffer | null = null
  private sourceVideo: HTMLVideoElement | null = null
  private animFrameId = 0
  private running = false
  private lastVideoTime = 0
  private texW = 0
  private texH = 0
  private outW = 0
  private outH = 0
  private canvasShown = false
  private frameCount = 0
  private lastFpsTime = 0
  private fps = 0
  private useFloat = false
  private passes: EnginePassDef[] = []
  private tier: Anime4kTier = 'S'
  private opts: Required<Pick<UpscalerOptions, 'strength' | 'upscale' | 'outputWidth' | 'outputHeight'>>
  ready = false
  error: string | null = null

  // ===== 诊断 =====
  private diagEnabled = true
  private diagReadFbo: WebGLFramebuffer | null = null
  private diagSampleThisSecond = false
  private diagSampled = false
  private _lastDiagTime = 0
  private _diagPassSums = new Float32Array(40) // 最大 VL 38+2 passes
  private _diagCanvasSum = 0
  private _diagVideoSum = 0
  private _diagBilinearDiff = 0

  private _diagVideoCanvas: HTMLCanvasElement | null = null
  private _diagBilinCanvas: HTMLCanvasElement | null = null
  private _diagCanvasSnap: HTMLCanvasElement | null = null
  private _renderErrorCount = 0
  private glErrorCount = 0

  constructor(options: UpscalerOptions = {}) {
    this.tier = options.tier ?? 'S'
    this.opts = {
      strength: options.strength ?? ANIME4K_PRESET.strength,
      upscale: 2.0,
      outputWidth: options.outputWidth ?? 0,
      outputHeight: options.outputHeight ?? 0,
    }
  }

  async init(video: HTMLVideoElement, parent?: HTMLElement): Promise<boolean> {
    this.sourceVideo = video
    this.canvas = document.createElement('canvas')
    this.canvas.style.cssText =
      'position:absolute;inset:0;width:100%;height:100%;object-fit:contain;pointer-events:none;z-index:1;image-rendering:auto;display:none'
    const container = parent ?? video.parentElement
    if (container) {
      container.style.position = container.style.position || 'relative'
      container.appendChild(this.canvas)
    }
    try {
      this.gl = this.canvas.getContext('webgl2', {
        alpha: false, antialias: false, powerPreference: 'high-performance',
        premultipliedAlpha: false, preserveDrawingBuffer: false,
      })
    } catch { /* ignore */ }
    if (!this.gl) { this.error = 'WebGL2 不可用，请检查显卡驱动'; return false }

    const gl = this.gl
    const vw = video.videoWidth, vh = video.videoHeight
    if (!vw || !vh) { this.error = '视频尚未就绪'; return false }

    const outW = this.opts.outputWidth > 0 ? this.opts.outputWidth : vw * 2
    const outH = this.opts.outputHeight > 0 ? this.opts.outputHeight : vh * 2
    const maxTex = gl.getParameter(gl.MAX_TEXTURE_SIZE) as number
    if (outW > maxTex || outH > maxTex) {
      this.error = `输出尺寸 ${outW}x${outH} 超过 GPU 最大纹理限制 ${maxTex}x${maxTex}`
      console.warn('[Anime4K]', this.error)
      return false
    }
    this.outW = outW; this.outH = outH
    this.canvas.width = outW; this.canvas.height = outH

    const extFloat = gl.getExtension('EXT_color_buffer_float')
    const extHalf = gl.getExtension('EXT_color_buffer_half_float')
    if (!extFloat && !extHalf) {
      this.error = 'GPU 不支持浮点渲染目标(EXT_color_buffer_float)，Anime4K 无法运行'
      console.warn('[Anime4K]', this.error)
      return false
    }
    this.useFloat = true

    // 构建 pass 管线
    const structure = getModelStructure(this.tier)
    this.passes = buildPipeline(structure)
    const numPasses = this.passes.length

    // 计算需要的中间纹理数量（所有 pass 中最大的 write 索引 + 1，加上视频纹理）
    let maxTexIdx = TEX_VIDEO
    for (const p of this.passes) {
      if (typeof p.write === 'number' && p.write > maxTexIdx) maxTexIdx = p.write

    }
    const numTextures = maxTexIdx + 1
    console.log(`[Anime4K] ${this.tier} model: ${numPasses} passes, ${numTextures} textures`)

    // 编译所有 pass 的 program
    const vs = this.compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER)
    if (!vs) { this.error = '顶点着色器编译失败'; return false }
    for (let i = 0; i < numPasses; i++) {
      const def = this.passes[i]
      const fs = this.compileShader(gl, gl.FRAGMENT_SHADER, def.fs)
      if (!fs) {
        this.error = `片段着色器编译失败 (pass ${i}: ${def.fs.substring(0, 60)}...)`
        gl.deleteShader(vs)
        return false
      }
      const prog = this.linkProgram(gl, vs, fs)
      gl.deleteShader(fs)
      if (!prog) { this.error = '着色器链接失败'; gl.deleteShader(vs); return false }
      this.programs.push(prog)
      this.uniforms.push({
        u_input: gl.getUniformLocation(prog, 'u_input'),
        u_input0: gl.getUniformLocation(prog, 'u_input0'),
        u_input1: gl.getUniformLocation(prog, 'u_input1'),
        u_input2: gl.getUniformLocation(prog, 'u_input2'),
        u_main: gl.getUniformLocation(prog, 'u_main'),
        u_video: gl.getUniformLocation(prog, 'u_video'),
        u_texelSize: gl.getUniformLocation(prog, 'u_texelSize'),
        u_inputSize: gl.getUniformLocation(prog, 'u_inputSize'),
        u_inputTexel: gl.getUniformLocation(prog, 'u_inputTexel'),
        u_strength: gl.getUniformLocation(prog, 'u_strength'),
        u_flipY: gl.getUniformLocation(prog, 'u_flipY'),
      })
      // Pointwise uniform 查找 (u_pw0..u_pwN)
      if (def.pwSources && def.pwSources.length > 0) {
        const pwU: (WebGLUniformLocation | null)[] = []
        for (let j = 0; j < def.pwSources.length; j++) {
          pwU.push(gl.getUniformLocation(prog, `u_pw${j}`))
        }
        (this.uniforms[this.uniforms.length - 1] as any).u_pw = pwU
      }
    }
    gl.deleteShader(vs)

    this.vao = this.createQuad(gl)
    if (!this.vao) { this.error = 'VAO 创建失败'; return false }

    // 创建所有中间纹理 + 视频纹理
    for (let i = 0; i < numTextures; i++) {
      this.textures[i] = this.createTexture(gl, gl.LINEAR)
      if (!this.textures[i]) { this.error = `纹理 ${i} 创建失败`; return false }
    }

    this.fbo = gl.createFramebuffer()
    if (!this.fbo) { this.error = 'FBO 创建失败'; return false }
    this.diagReadFbo = gl.createFramebuffer()

    this.texW = vw; this.texH = vh
    this.allocateTextures(gl)

    this.ready = true
    return true
  }

  start(): void {
    if (!this.ready || this.running) return
    this.running = true; this.frameCount = 0
    this.lastFpsTime = performance.now()
    this._lastDiagTime = performance.now()
    this.diagSampleThisSecond = false
    this.renderLoop()
  }

  stop(): void {
    this.running = false; this.canvasShown = false
    if (this.canvas) this.canvas.style.display = 'none'
    if (this.animFrameId) { cancelAnimationFrame(this.animFrameId); this.animFrameId = 0 }
  }

  destroy(): void {
    this.stop(); this.ready = false
    const gl = this.gl
    if (gl) {
      for (const p of this.programs) if (p) gl.deleteProgram(p)
      for (const t of this.textures) if (t) gl.deleteTexture(t)
      if (this.fbo) gl.deleteFramebuffer(this.fbo)
      if (this.diagReadFbo) gl.deleteFramebuffer(this.diagReadFbo)
      if (this.vao) gl.deleteVertexArray(this.vao)
    }
    if (this.canvas?.parentNode) this.canvas.parentNode.removeChild(this.canvas)
    this.canvas = this.gl = null
    this.programs = []; this.uniforms = []
    this.textures = []
    this.fbo = this.diagReadFbo = this.vao = this.sourceVideo = null
  }

  updateOptions(opts: Partial<UpscalerOptions>): void {
    if (opts.strength !== undefined) this.opts.strength = opts.strength
    if (opts.outputWidth !== undefined) this.opts.outputWidth = opts.outputWidth
    if (opts.outputHeight !== undefined) this.opts.outputHeight = opts.outputHeight
  }

  getStats(): UpscalerStats {
    const v = this.sourceVideo, c = this.canvas
    return {
      fps: this.fps, frameCount: this.frameCount,
      elapsed: this.lastFpsTime > 0 ? (performance.now() - this.lastFpsTime) / 1000 : 0,
      inputSize: v ? `${v.videoWidth}x${v.videoHeight}` : 'N/A',
      outputSize: c ? `${c.width}x${c.height}` : 'N/A',
      gpuEnabled: this.gl !== null,
    }
  }

  // ==================== 渲染 ====================

  private renderLoop = (): void => {
    if (!this.running) return
    this.animFrameId = requestAnimationFrame(this.renderLoop)
    try {
      this.render()
    } catch (e) {
      console.error('[Anime4K] render() 异常:', e)
      if (++this._renderErrorCount >= 3) {
        console.error('[Anime4K] 连续渲染异常，停止管线')
        this.stop()
      }
    }
  }

  private render(): void {
    const gl = this.gl, video = this.sourceVideo, canvas = this.canvas
    if (!gl || !video || !canvas || !this.vao) return
    if (video.readyState < 2 || video.paused || video.ended) return
    const vw = video.videoWidth, vh = video.videoHeight
    if (vw <= 0 || vh <= 0) return
    const ct = video.currentTime
    if (Math.abs(ct - this.lastVideoTime) < 0.001) return
    this.lastVideoTime = ct

    if (this.texW !== vw || this.texH !== vh) {
      this.texW = vw; this.texH = vh
      if (this.opts.outputWidth <= 0 || this.opts.outputHeight <= 0) {
        this.outW = vw * 2; this.outH = vh * 2
        canvas.width = this.outW; canvas.height = this.outH
      }
      this.allocateTextures(gl)
    }

    // 上传视频帧
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, this.textures[TEX_VIDEO])
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false)
    try {
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, video)
    } catch (e) {
      console.warn('[Anime4K] texImage2D(video) 失败，跳过本帧:', e)
      return
    }

    const texelX = 1 / vw, texelY = 1 / vh

    // 执行所有 pass
    for (let i = 0; i < this.passes.length; i++) {
      const def = this.passes[i]
      const prog = this.programs[i]
      const u = this.uniforms[i]
      const isCanvasOut = def.write === 'canvas'
      const renderW = def.resScale === 2 ? this.outW : vw
      const renderH = def.resScale === 2 ? this.outH : vh

      // 设置渲染目标
      if (isCanvasOut) {
        gl.bindFramebuffer(gl.FRAMEBUFFER, null)
      } else {
        const writeIdx = def.write as number
        gl.bindFramebuffer(gl.FRAMEBUFFER, this.fbo)
        // 单输出 (split16 已拆成成对独立 pass, 不再走 MRT)
        gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, this.textures[writeIdx], 0)
        gl.drawBuffers([gl.COLOR_ATTACHMENT0])
      }
      gl.viewport(0, 0, renderW, renderH)

      gl.useProgram(prog)

      // 主输入 → unit 0
      if (def.read2 !== undefined && def.read2 >= 0) {
        // 双纹理输入 (16ch)
        gl.activeTexture(gl.TEXTURE0)
        gl.bindTexture(gl.TEXTURE_2D, this.textures[def.read])
        if (u.u_input0) gl.uniform1i(u.u_input0, 0)
        else if (u.u_input) gl.uniform1i(u.u_input, 0)

        gl.activeTexture(gl.TEXTURE4)
        gl.bindTexture(gl.TEXTURE_2D, this.textures[def.read2])
        if (u.u_input1) gl.uniform1i(u.u_input1, 4)
      } else {
        // 单纹理输入
        gl.activeTexture(gl.TEXTURE0)
        gl.bindTexture(gl.TEXTURE_2D, this.textures[def.read])
        if (u.u_input) gl.uniform1i(u.u_input, 0)
      }

      // 残差纹理 → unit 1
      if (def.mainTex !== undefined && u.u_main) {
        gl.activeTexture(gl.TEXTURE1)
        gl.bindTexture(gl.TEXTURE_2D, this.textures[def.mainTex])
        gl.uniform1i(u.u_main, 1)
      }

      // 原始视频 → unit 2
      if (def.useVideo && u.u_video) {
        gl.activeTexture(gl.TEXTURE2)
        gl.bindTexture(gl.TEXTURE_2D, this.textures[TEX_VIDEO])
        gl.uniform1i(u.u_video, 2)
      }

      // Pointwise 源纹理 → unit 5+j
      if (def.pwSources && def.pwSources.length > 0) {
        const pwU = (u as any).u_pw as (WebGLUniformLocation | null)[] | undefined
        if (pwU) {
          for (let j = 0; j < def.pwSources.length; j++) {
            gl.activeTexture(gl.TEXTURE5 + j)
            gl.bindTexture(gl.TEXTURE_2D, this.textures[def.pwSources[j]])
            if (pwU[j]) gl.uniform1i(pwU[j], 5 + j)
          }
        }
      }

      // 第三输入纹理 → unit 6 (L Depth2Space)
      if (def.read3 !== undefined && def.read3 >= 0 && u.u_input2) {
        gl.activeTexture(gl.TEXTURE6)
        gl.bindTexture(gl.TEXTURE_2D, this.textures[def.read3])
        gl.uniform1i(u.u_input2, 6)
      }

      // 公共 uniforms
      if (u.u_texelSize) gl.uniform2f(u.u_texelSize, texelX, texelY)
      if (u.u_inputSize) gl.uniform2f(u.u_inputSize, vw, vh)
      if (u.u_inputTexel) gl.uniform2f(u.u_inputTexel, texelX, texelY)
      if (u.u_strength) gl.uniform1f(u.u_strength, this.opts.strength)
      if (u.u_flipY) gl.uniform1f(u.u_flipY, def.flipY)

      gl.bindVertexArray(this.vao)
      gl.drawArrays(gl.TRIANGLES, 0, 6)

      if (!isCanvasOut) {
        const status = gl.checkFramebufferStatus(gl.FRAMEBUFFER)
        if (status !== gl.FRAMEBUFFER_COMPLETE && this.glErrorCount < 3) {
          console.error(`[Anime4K] Pass ${i} FBO 不完整: 0x${status.toString(16)}`)
        }
      }

      // ===== 诊断 =====
      if (this.diagEnabled && this.diagSampleThisSecond && !this.diagSampled) {
        const writeIdx = isCanvasOut ? -1 : (def.write as number)
        const readFbo = this.diagReadFbo
        if (readFbo) {
          try {
            if (isCanvasOut) {
              // 用 2D canvas drawImage 采样 WebGL canvas，避免 ANGLE readPixels 兼容性问题
              try {
                if (!this._diagCanvasSnap) this._diagCanvasSnap = document.createElement('canvas')
                const sc = this._diagCanvasSnap
                sc.width = 4; sc.height = 4
                const sctx = sc.getContext('2d', { willReadFrequently: true })
                if (sctx) {
                  const cx = Math.floor(canvas.width / 2 - 2)
                  const cy = Math.floor(canvas.height / 2 - 2)
                  sctx.drawImage(canvas, cx, cy, 4, 4, 0, 0, 4, 4)
                  const d = sctx.getImageData(0, 0, 4, 4).data
                  const mid = 2 * 4
                  this._diagCanvasSum = d[mid] + d[mid + 1] + d[mid + 2]
                }
              } catch { /* 2D canvas 采样失败不影响渲染 */ }
              const vid = this.sampleVideoCenter(gl)
              this._diagVideoSum = vid[0] + vid[1] + vid[2]
              this._diagBilinearDiff = this.compareBilinearVsCanvas(gl)
              this.diagSampled = true
            } else {
              gl.bindFramebuffer(gl.READ_FRAMEBUFFER, readFbo)
              gl.framebufferTexture2D(gl.READ_FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, this.textures[writeIdx], 0)
              // RGBA16F 纹理直接用 RGBA + HALF_FLOAT 读取, 避免 ANGLE 的
              // IMPLEMENTATION_COLOR_READ_FORMAT/TYPE 返回无效组合
              const buf = new Uint16Array(4)
              gl.readPixels(Math.floor(renderW / 2), Math.floor(renderH / 2), 1, 1,
                gl.RGBA, gl.HALF_FLOAT, buf)
              if (gl.getError() === gl.NO_ERROR) {
                this._diagPassSums[i] = this.halfToFloat(buf[0]) + this.halfToFloat(buf[1])
                  + this.halfToFloat(buf[2]) + this.halfToFloat(buf[3])
              }
            }
          } catch { /* readPixels 异常不影响主渲染 */ }
        }
      }
    }

    this.checkGLErrors('frame')

    // 每秒诊断输出
    if (this.diagEnabled) {
      const now = performance.now()
      if (!this.diagSampleThisSecond) {
        this.diagSampleThisSecond = true
        this.diagSampled = false
      }
      if (now - this._lastDiagTime >= 1000) {
        this._lastDiagTime = now
        this.diagSampleThisSecond = false
        const ps = Array.from(this._diagPassSums).slice(0, this.passes.length)
        const cnnWorking = ps.some(v => Math.abs(v) > 0.0001)
        const diff = Math.abs(this._diagCanvasSum - this._diagVideoSum)
        console.log(
          `[A4K-${this.tier}] fps=${this.fps} ${this.texW}x${this.texH}->${this.outW}x${this.outH}` +
          ` | P0..${Math.min(ps.length - 1, 7)}=[${ps.slice(0, 8).map(v => Math.abs(v) > 1e-6 ? v.toExponential(1) : '0').join(',')}]` +
          ` | canvas=${this._diagCanvasSum} vid=${this._diagVideoSum} diff=${diff}` +
          ` | bilinDiff=${this._diagBilinearDiff}` +
          ` | cnn=${cnnWorking ? 'OK' : 'BROKEN'}`
        )
        this._diagPassSums.fill(0)
        this._diagCanvasSum = 0
        this._diagVideoSum = 0
      }
    }

    if (!this.canvasShown) { this.canvasShown = true; canvas.style.display = '' }
    this._renderErrorCount = 0
    this.frameCount++
    const now = performance.now()
    if (now - this.lastFpsTime >= 1000) {
      this.fps = Math.round(this.frameCount / ((now - this.lastFpsTime) / 1000))
      this.frameCount = 0; this.lastFpsTime = now
    }
  }

  // ==================== 辅助方法 ====================

  private allocateTextures(gl: WebGL2RenderingContext): void {
    const internal = this.useFloat ? gl.RGBA16F : gl.RGBA8
    for (let i = 0; i < this.textures.length; i++) {
      if (!this.textures[i]) continue
      gl.bindTexture(gl.TEXTURE_2D, this.textures[i]!)
      gl.texImage2D(gl.TEXTURE_2D, 0, internal, this.texW, this.texH, 0, gl.RGBA, this.useFloat ? gl.HALF_FLOAT : gl.UNSIGNED_BYTE, null)
    }
  }

  private sampleVideoCenter(gl: WebGL2RenderingContext): number[] {
    const v = this.sourceVideo
    if (!v || !v.videoWidth) return [-1, -1, -1, -1]
    try {
      if (!this._diagVideoCanvas) this._diagVideoCanvas = document.createElement('canvas')
      const tc = this._diagVideoCanvas
      const sz = 4
      tc.width = sz; tc.height = sz
      const tctx = tc.getContext('2d', { willReadFrequently: true })
      if (!tctx) return [-2, -2, -2, -2]
      tctx.drawImage(v, v.videoWidth / 2 - sz / 2, v.videoHeight / 2 - sz / 2, sz, sz, 0, 0, sz, sz)
      const d = tctx.getImageData(0, 0, sz, sz).data
      return [d[(sz / 2 | 0) * 4], d[(sz / 2 | 0) * 4 + 1], d[(sz / 2 | 0) * 4 + 2], d[(sz / 2 | 0) * 4 + 3]]
    } catch { return [-3, -3, -3, -3] }
  }

  private halfToFloat(h: number): number {
    const s = (h >> 15) & 0x1
    const e = (h >> 10) & 0x1F
    const m = h & 0x3FF
    if (e === 0) return (s ? -1 : 1) * (m / 1024) * Math.pow(2, -14)
    if (e === 31) return m ? NaN : (s ? -Infinity : Infinity)
    return (s ? -1 : 1) * Math.pow(2, e - 15) * (1 + m / 1024)
  }

  private compareBilinearVsCanvas(_gl: WebGL2RenderingContext): number {
    const v = this.sourceVideo, c = this.canvas
    if (!v || !c || !v.videoWidth) return -1
    try {
      if (!this._diagBilinCanvas) this._diagBilinCanvas = document.createElement('canvas')
      const bc = this._diagBilinCanvas
      const sampleW = 32, sampleH = 18
      bc.width = sampleW; bc.height = sampleH
      const bctx = bc.getContext('2d', { willReadFrequently: true })
      if (!bctx) return -2
      const scale = this.texW / this.outW
      const sx = v.videoWidth / 2 - (sampleW * scale) / 2
      const sy = v.videoHeight / 2 - (sampleH * scale) / 2
      bctx.drawImage(v, sx, sy, sampleW * scale, sampleH * scale, 0, 0, sampleW, sampleH)
      const bilinData = bctx.getImageData(0, 0, sampleW, sampleH).data

      if (!this._diagCanvasSnap) this._diagCanvasSnap = document.createElement('canvas')
      const sc = this._diagCanvasSnap
      sc.width = sampleW; sc.height = sampleH
      const sctx = sc.getContext('2d', { willReadFrequently: true })
      if (!sctx) return -3
      const cx = Math.floor(c.width / 2 - sampleW / 2)
      const cy = Math.floor(c.height / 2 - sampleH / 2)
      sctx.drawImage(c, cx, cy, sampleW, sampleH, 0, 0, sampleW, sampleH)
      const canvasData = sctx.getImageData(0, 0, sampleW, sampleH).data

      let totalDiff = 0
      for (let p = 0; p < sampleW * sampleH; p++) {
        const off = p * 4
        totalDiff += Math.abs(canvasData[off] - bilinData[off])
          + Math.abs(canvasData[off + 1] - bilinData[off + 1])
          + Math.abs(canvasData[off + 2] - bilinData[off + 2])
      }
      return totalDiff
    } catch { return -99 }
  }

  private checkGLErrors(stage: string): void {
    const gl = this.gl; if (!gl) return
    let err = gl.getError()
    while (err !== gl.NO_ERROR) {
      this.glErrorCount++
      if (this.glErrorCount <= 5) {
        console.error(`[Anime4K] GL error at ${stage}: 0x${err.toString(16)} (count: ${this.glErrorCount})`)
      } else if (this.glErrorCount === 6) {
        console.warn('[Anime4K] GL 错误已抑制，后续不再逐条打印')
      }
      err = gl.getError()
    }
  }

  private createTexture(gl: WebGL2RenderingContext, filter: number): WebGLTexture | null {
    const tex = gl.createTexture()
    if (!tex) return null
    gl.bindTexture(gl.TEXTURE_2D, tex)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filter)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, filter)
    return tex
  }

  private compileShader(gl: WebGL2RenderingContext, type: number, source: string): WebGLShader | null {
    const shader = gl.createShader(type)
    if (!shader) return null
    gl.shaderSource(shader, source); gl.compileShader(shader)
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      console.error('[Anime4K] 着色器编译错误:', gl.getShaderInfoLog(shader))
      gl.deleteShader(shader); return null
    }
    return shader
  }

  private linkProgram(gl: WebGL2RenderingContext, vs: WebGLShader, fs: WebGLShader): WebGLProgram | null {
    const program = gl.createProgram()
    if (!program) return null
    gl.attachShader(program, vs); gl.attachShader(program, fs); gl.linkProgram(program)
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.error('[Anime4K] 着色器链接错误:', gl.getProgramInfoLog(program))
      gl.deleteProgram(program); return null
    }
    return program
  }

  private createQuad(gl: WebGL2RenderingContext): WebGLVertexArrayObject | null {
    const vao = gl.createVertexArray()
    if (!vao) return null
    gl.bindVertexArray(vao)
    const vertices = new Float32Array([
      -1, -1, 0, 1,  1, -1, 1, 1,  -1, 1, 0, 0,
      -1, 1, 0, 0,   1, -1, 1, 1,   1, 1, 1, 0,
    ])
    const buf = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, buf)
    gl.bufferData(gl.ARRAY_BUFFER, vertices, gl.STATIC_DRAW)
    gl.enableVertexAttribArray(0)
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 16, 0)
    gl.enableVertexAttribArray(1)
    gl.vertexAttribPointer(1, 2, gl.FLOAT, false, 16, 8)
    gl.bindVertexArray(null)
    return vao
  }
}

/** 检查当前环境是否支持 Anime4K */
export function checkAnime4kSupport(): {
  webgl2: boolean; floatBuffer: boolean; recommended: boolean; message: string
} {
  let gl2: WebGL2RenderingContext | null = null
  try {
    const c = document.createElement('canvas')
    gl2 = c.getContext('webgl2')
  } catch { /* ignore */ }
  if (!gl2) {
    return { webgl2: false, floatBuffer: false, recommended: false, message: 'WebGL2 不可用，Anime4K 无法运行' }
  }
  const hasFloat = !!(gl2.getExtension('EXT_color_buffer_float') || gl2.getExtension('EXT_color_buffer_half_float'))
  if (!hasFloat) {
    return { webgl2: true, floatBuffer: false, recommended: false, message: 'GPU 不支持浮点渲染目标，Anime4K 不可用' }
  }
  return { webgl2: true, floatBuffer: true, recommended: true, message: 'Anime4K 可用，将启用 CNN 2× 超分辨率' }
}
