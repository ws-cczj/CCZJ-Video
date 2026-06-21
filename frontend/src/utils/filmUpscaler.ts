/**
 * 影视增强器 — FSRCNNX CNN 超分 + AMD CAS 自适应锐化（WebGL2 实时）
 *
 * FSRCNNX (Fast Super-Resolution CNN) 来自 igv/FSRCNN-TensorFlow (LGPL-3.0)
 *   - 8 通道、4 层 mapping、轻量 CNN，专为通用视频内容设计
 *   - 12 Pass 管线：Feature Extract → Mapping → Residuals → Sub-pixel Conv → 2× 聚合
 *
 * CAS (Contrast Adaptive Sharpening) 来自 AMD FidelityFX (MIT)
 *   - 极轻量的对比度自适应锐化，作为 FSRCNNX 后处理
 *
 * 外部 API: init / start / stop / destroy / updateOptions / getStats
 */

import fsrcnnxSource from './FSRCNNX_x2_8-0-4-1.glsl?raw'

// ==================== 类型定义 ====================

export interface FilmUpscalerOptions {
  sharpness?: number   // 锐化强度 0~2, 默认 0.5
  casStrength?: number // CAS 强度 0~1, 默认 0.4
}

export interface FilmStats {
  fps: number
  gpuEnabled: boolean
}

interface PassDef {
  desc: string
  body: string
  bindNames: string[]
  saveName: string
  components: number
  isAggregation: boolean
  lumaBind: boolean
}

// ==================== mpv 着色器解析器 ====================

const VERTEX_SRC = `#version 300 es
in vec2 a_pos;
out vec2 v_uv;
void main() { gl_Position = vec4(a_pos, 0.0, 1.0); v_uv = a_pos * 0.5 + 0.5; }`

function parseMpvShader(src: string): PassDef[] {
  const blocks = src.split(/^\/\/!HOOK /m).slice(1)
  const passes: PassDef[] = []
  for (const block of blocks) {
    const lines = block.split('\n')
    let desc = '', body = '', saveName = '', isAgg = false, components = 4
    const bindNames: string[] = []
    let inBody = false
    const bodyLines: string[] = []
    for (const raw of lines) {
      const line = raw.trim()
      if (line.startsWith('//!DESC ')) desc = line.slice(8)
      else if (line.startsWith('//!BIND ')) bindNames.push(line.slice(8))
      else if (line.startsWith('//!SAVE ')) saveName = line.slice(8)
      else if (line.startsWith('//!COMPONENTS ')) components = parseInt(line.slice(14))
      else if (line.startsWith('//!WIDTH ') || line.startsWith('//!HEIGHT ')) {
        if (line.includes('2 *')) isAgg = true
      } else if (line.startsWith('//!')) continue
      else if (line.includes('vec4 hook()')) inBody = true
      else if (inBody) bodyLines.push(raw)
    }
    // 移除 return 语句，因为 body 会被内联到 main() 中，用 _out0 = res 替代
    body = bodyLines.join('\n')
      .replace(/\s*return vec4\(res\);\s*$/m, '\n')
      .replace(/\s*return res;\s*$/m, '\n')
      .replace(/^\s*\{/, '') // 移除 hook() 的开花括号
      .replace(/\}\s*$/, '') // 移除 hook() 的闭合括号
      .trim()
    const lumaBind = bindNames.includes('LUMA')
    if (body.trim()) passes.push({ desc, body, bindNames, saveName, components, isAggregation: isAgg, lumaBind })
  }
  return passes
}

function buildPassShader(pass: PassDef, inputW: number, inputH: number): string {
  const ptX = (1 / inputW).toFixed(15)
  const ptY = (1 / inputH).toFixed(15)
  const samplers = pass.bindNames
    .map(n => `uniform sampler2D u_${n};`)
    .join('\n')
  // lumaBind 时 LUMA 的 texOff 由 lumaPreproc 提供（提取亮度），避免重复定义
  const defines = pass.bindNames
    .filter(n => !(pass.lumaBind && n === 'LUMA'))
    .map(n => `#define ${n}_texOff(off) texture(u_${n}, v_uv + (off) * vec2(${ptX}, ${ptY}))\n#define ${n}_tex(p) texture(u_${n}, p)`)
    .join('\n')
  const lumaPreproc = pass.lumaBind
    ? `float _fsrcnnx_luma(sampler2D t, vec2 uv) { vec3 c = texture(t, uv).rgb; return dot(c, vec3(0.2126, 0.7152, 0.0722)); }
#define LUMA_texOff(off) _fsrcnnx_luma(u_LUMA, v_uv + (off) * vec2(${ptX}, ${ptY}))`
    : ''
  return `#version 300 es
precision highp float;
in vec2 v_uv;
layout(location=0) out vec4 _out0;
${samplers}
${defines}
${lumaPreproc}
void main() {
${pass.body}
_out0 = res;
}`
}

function buildAggShader(inputW: number, inputH: number): string {
  return `#version 300 es
precision highp float;
in vec2 v_uv;
out vec4 fragColor;
uniform sampler2D u_SUBCONV1;
void main() {
  vec2 inCoord = v_uv * vec2(${inputW}.0, ${inputH}.0);
  vec2 fcoord = fract(inCoord);
  vec2 base = (floor(inCoord) + vec2(0.5)) / vec2(${inputW}.0, ${inputH}.0);
  ivec2 index = ivec2(fcoord * vec2(2));
  vec4 res = texture(u_SUBCONV1, base);
  float luma = res[index.x * 2 + index.y];
  fragColor = vec4(luma, luma, luma, 1.0);
}`
}

// ==================== CAS 着色器 ====================

const CAS_FRAG = `#version 300 es
precision highp float;
in vec2 v_uv;
out vec4 fragColor;
uniform sampler2D u_input;
uniform vec2 u_texelSize;
uniform float u_amount;

float luma(vec3 c) { return dot(c, vec3(0.299, 0.587, 0.114)); }

void main() {
  vec2 ts = u_texelSize;
  vec3 e = texture(u_input, v_uv).rgb;
  vec3 a = texture(u_input, v_uv + vec2(0,-ts.y)).rgb;
  vec3 b = texture(u_input, v_uv + vec2(-ts.x,0)).rgb;
  vec3 c = texture(u_input, v_uv + vec2(ts.x,0)).rgb;
  vec3 d = texture(u_input, v_uv + vec2(0,ts.y)).rgb;
  vec3 f = texture(u_input, v_uv + vec2(-ts.x,-ts.y)).rgb;
  vec3 g = texture(u_input, v_uv + vec2(ts.x,-ts.y)).rgb;
  vec3 h = texture(u_input, v_uv + vec2(-ts.x,ts.y)).rgb;
  vec3 i = texture(u_input, v_uv + vec2(ts.x,ts.y)).rgb;
  float mn = min(min(min(luma(a),luma(b)),min(luma(c),luma(d))),min(min(luma(f),luma(g)),min(luma(h),luma(i))));
  mn = min(mn, luma(e));
  float mx = max(max(max(luma(a),luma(b)),max(luma(c),luma(d))),max(max(luma(f),luma(g)),max(luma(h),luma(i))));
  mx = max(mx, luma(e));
  float amp = clamp(sqrt(min(mn, 1.0 - mx) / max(mx, 1e-5)), 0.0, 1.0);
  float peak = mix(-8.0, -14.0, u_amount);
  float w = amp * peak;
  float rcpW = 1.0 / (4.0 + w * 4.0);
  vec3 outColor = (e * 4.0 + (a + b + c + d) * w) * rcpW;
  fragColor = vec4(outColor, 1.0);
}`

// ==================== WebGL2 工具 ====================

function compileShader(gl: WebGL2RenderingContext, type: number, src: string): WebGLShader | null {
  const s = gl.createShader(type)
  if (!s) return null
  gl.shaderSource(s, src)
  gl.compileShader(s)
  if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
    console.error('[FilmUpscaler] Shader compile error:', gl.getShaderInfoLog(s))
    gl.deleteShader(s)
    return null
  }
  return s
}

function linkProgram(gl: WebGL2RenderingContext, vs: WebGLShader, fs: WebGLShader): WebGLProgram | null {
  const p = gl.createProgram()
  if (!p) return null
  gl.attachShader(p, vs)
  gl.attachShader(p, fs)
  gl.linkProgram(p)
  if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
    console.error('[FilmUpscaler] Program link error:', gl.getProgramInfoLog(p))
    gl.deleteProgram(p)
    return null
  }
  return p
}

interface FBOEntry { tex: WebGLTexture; fbo: WebGLFramebuffer; w: number; h: number }

function createFBO(gl: WebGL2RenderingContext, w: number, h: number): FBOEntry {
  const tex = gl.createTexture()!
  gl.bindTexture(gl.TEXTURE_2D, tex)
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA16F, w, h, 0, gl.RGBA, gl.HALF_FLOAT, null)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  const fbo = gl.createFramebuffer()!
  gl.bindFramebuffer(gl.FRAMEBUFFER, fbo)
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
  gl.bindFramebuffer(gl.FRAMEBUFFER, null)
  return { tex, fbo, w, h }
}

// ==================== FilmUpscaler 类 ====================

export class FilmUpscaler {
  private opts: Required<FilmUpscalerOptions>
  private canvas: HTMLCanvasElement | null = null
  private gl: WebGL2RenderingContext | null = null
  private video: HTMLVideoElement | null = null
  private running = false
  private rafId = 0
  private _error: string | null = null

  // 全屏四边形
  private quadVAO: WebGLVertexArrayObject | null = null
  private quadVBO: WebGLBuffer | null = null

  // 视频纹理
  private videoTex: WebGLTexture | null = null

  // FBO 注册表：纹理名 → FBO 列表
  private fboRegistry: Map<string, FBOEntry[]> = new Map()

  // 着色器程序
  private passPrograms: { prog: WebGLProgram; pass: PassDef }[] = []
  private aggProgram: WebGLProgram | null = null
  private casProgram: WebGLProgram | null = null

  // CAS FBO (2× 分辨率)
  private casFBO: FBOEntry | null = null

  // 解析后的 Pass 定义
  private passDefs: PassDef[] = []

  // 统计
  private frames = 0
  private lastFpsTime = 0
  private currentFps = 0

  // 输入尺寸
  private inputW = 0
  private inputH = 0

  get error(): string | null { return this._error }

  constructor(opts: FilmUpscalerOptions = {}) {
    this.opts = {
      sharpness: opts.sharpness ?? 0.5,
      casStrength: opts.casStrength ?? 0.4,
    }
  }

  async init(video: HTMLVideoElement, wrapper?: HTMLElement): Promise<boolean> {
    try {
      this.video = video

      // 创建离屏 Canvas
      this.canvas = document.createElement('canvas')
      this.canvas.style.cssText = 'position:absolute;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:2'
      const container = wrapper ?? video.parentElement
      if (!container) throw new Error('No container element')
      container.style.position = 'relative'
      container.appendChild(this.canvas)

      const gl = this.canvas.getContext('webgl2', {
        alpha: false, antialias: false, premultipliedAlpha: false, preserveDrawingBuffer: false,
      }) as WebGL2RenderingContext | null
      if (!gl) throw new Error('WebGL2 not available')
      this.gl = gl

      // 检查浮点渲染目标
      const ext = gl.getExtension('EXT_color_buffer_float')
      if (!ext) throw new Error('EXT_color_buffer_float not available')
      gl.getExtension('OES_texture_float_linear')

      // 输入分辨率
      this.inputW = video.videoWidth || 1920
      this.inputH = video.videoHeight || 1080
      this.canvas.width = this.inputW * 2
      this.canvas.height = this.inputH * 2

      // 全屏四边形
      this.quadVAO = gl.createVertexArray()!
      gl.bindVertexArray(this.quadVAO)
      this.quadVBO = gl.createBuffer()!
      gl.bindBuffer(gl.ARRAY_BUFFER, this.quadVBO)
      gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1, 1,-1, -1,1, 1,1]), gl.STATIC_DRAW)
      gl.enableVertexAttribArray(0)
      gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0)
      gl.bindVertexArray(null)

      // 视频纹理（参数只需设置一次）
      this.videoTex = gl.createTexture()!
      gl.bindTexture(gl.TEXTURE_2D, this.videoTex)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
      gl.bindTexture(gl.TEXTURE_2D, null)

      // 编译顶点着色器（所有 Pass 共用）
      const vs = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SRC)
      if (!vs) throw new Error('Vertex shader compile failed')

      // 解析 FSRCNNX
      this.passDefs = parseMpvShader(fsrcnnxSource)
      if (this.passDefs.length < 10) throw new Error(`FSRCNNX parse error: only ${this.passDefs.length} passes`)

      // 编译每个 Pass 的片段着色器
      for (const pass of this.passDefs) {
        if (pass.isAggregation) continue // 聚合 Pass 单独处理
        const fragSrc = buildPassShader(pass, this.inputW, this.inputH)
        const fs = compileShader(gl, gl.FRAGMENT_SHADER, fragSrc)
        if (!fs) throw new Error(`Pass "${pass.desc}" compile failed`)
        const prog = linkProgram(gl, vs, fs)
        if (!prog) throw new Error(`Pass "${pass.desc}" link failed`)
        gl.deleteShader(fs) // link 后片段着色器可安全删除
        this.passPrograms.push({ prog, pass })
      }

      // 聚合 Pass 着色器
      const aggFragSrc = buildAggShader(this.inputW, this.inputH)
      const aggFs = compileShader(gl, gl.FRAGMENT_SHADER, aggFragSrc)
      if (!aggFs) throw new Error('Aggregation shader compile failed')
      this.aggProgram = linkProgram(gl, vs, aggFs)
      if (!this.aggProgram) throw new Error('Aggregation shader link failed')
      gl.deleteShader(aggFs)

      // CAS 着色器
      const casFs = compileShader(gl, gl.FRAGMENT_SHADER, CAS_FRAG)
      if (!casFs) throw new Error('CAS shader compile failed')
      this.casProgram = linkProgram(gl, vs, casFs)
      if (!this.casProgram) throw new Error('CAS shader link failed')
      gl.deleteShader(casFs)

      // 顶点着色器已被所有 program 引用，link 后可安全删除
      gl.deleteShader(vs)

      // 预分配 FBO
      this.allocateFBOs()

      console.log(`[FilmUpscaler] 初始化完成: ${this.passDefs.length} passes, ${this.inputW}×${this.inputH} → ${this.inputW*2}×${this.inputH*2}`)
      return true
    } catch (e: any) {
      this._error = e.message || String(e)
      console.error('[FilmUpscaler] Init failed:', this._error)
      return false
    }
  }

  private allocateFBOs(): void {
    const gl = this.gl!
    const w = this.inputW, h = this.inputH
    const outW = w * 2, outH = h * 2

    // 为每个 Pass 的输出分配 FBO
    for (let i = 0; i < this.passDefs.length; i++) {
      const pass = this.passDefs[i]
      if (pass.isAggregation) {
        // 聚合 Pass 输出到 CAS FBO（2× 分辨率）
        this.casFBO = createFBO(gl, outW, outH)
      } else {
        this.getOrCreateFBO(`${pass.saveName}_${i}`, w, h)
      }
    }
  }

  private getOrCreateFBO(name: string, w: number, h: number): FBOEntry {
    let arr = this.fboRegistry.get(name)
    if (!arr) { arr = []; this.fboRegistry.set(name, arr) }
    // 复用同尺寸的 FBO
    for (const f of arr) if (f.w === w && f.h === h) return f
    const fbo = createFBO(this.gl!, w, h)
    arr.push(fbo)
    return fbo
  }

  /** 获取某个纹理名在 passIndex 之前最新写入的 FBO */
  private resolveTexture(name: string, beforePass: number): FBOEntry | null {
    // 按顺序扫描，找到在 beforePass 之前最后一次写入 saveName === name 的 FBO
    let result: FBOEntry | null = null
    for (let i = 0; i < beforePass; i++) {
      if (this.passDefs[i].saveName === name && !this.passDefs[i].isAggregation) {
        result = this.getOrCreateFBO(`${name}_${i}`, this.inputW, this.inputH)
      }
    }
    return result
  }

  start(): void {
    if (this.running || !this.gl) return
    this.running = true
    this.frames = 0
    this.lastFpsTime = performance.now()
    this.renderLoop()
  }

  stop(): void {
    this.running = false
    if (this.rafId) { cancelAnimationFrame(this.rafId); this.rafId = 0 }
  }

  destroy(): void {
    this.stop()
    const gl = this.gl
    if (gl) {
      // 清理 FBO
      for (const [, arr] of this.fboRegistry) {
        for (const f of arr) { gl.deleteTexture(f.tex); gl.deleteFramebuffer(f.fbo) }
      }
      if (this.casFBO) { gl.deleteTexture(this.casFBO.tex); gl.deleteFramebuffer(this.casFBO.fbo) }
      this.fboRegistry.clear()
      // 清理程序
      for (const { prog } of this.passPrograms) gl.deleteProgram(prog)
      if (this.aggProgram) gl.deleteProgram(this.aggProgram)
      if (this.casProgram) gl.deleteProgram(this.casProgram)
      if (this.videoTex) gl.deleteTexture(this.videoTex)
      if (this.quadVBO) gl.deleteBuffer(this.quadVBO)
      if (this.quadVAO) gl.deleteVertexArray(this.quadVAO)
    }
    if (this.canvas?.parentElement) this.canvas.parentElement.removeChild(this.canvas)
    this.canvas = null; this.gl = null; this.video = null
  }

  updateOptions(opts: Partial<FilmUpscalerOptions>): void {
    if (opts.sharpness !== undefined) this.opts.sharpness = opts.sharpness
    if (opts.casStrength !== undefined) this.opts.casStrength = opts.casStrength
  }

  getStats(): FilmStats {
    return { fps: this.currentFps, gpuEnabled: true }
  }

  // ==================== 渲染循环 ====================

  private renderLoop = (): void => {
    if (!this.running) return
    this.rafId = requestAnimationFrame(this.renderLoop)
    this.render()
    // FPS 统计
    this.frames++
    const now = performance.now()
    if (now - this.lastFpsTime >= 2000) {
      this.currentFps = Math.round(this.frames / ((now - this.lastFpsTime) / 1000))
      this.frames = 0
      this.lastFpsTime = now
    }
  }

  private render(): void {
    const gl = this.gl!
    const video = this.video!
    if (!video.videoWidth) return

    // 更新输入尺寸（视频可能动态变化）
    if (video.videoWidth !== this.inputW || video.videoHeight !== this.inputH) {
      this.inputW = video.videoWidth
      this.inputH = video.videoHeight
      this.canvas!.width = this.inputW * 2
      this.canvas!.height = this.inputH * 2
      // 重新编译着色器（texelSize 变化）
      this.rebuildPipeline()
    }

    const w = this.inputW, h = this.inputH

    // 上传视频帧到纹理
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, this.videoTex)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, video)

    gl.bindVertexArray(this.quadVAO)

    // ---- 执行 FSRCNNX 全部 Pass ----
    let aggInputFBO: FBOEntry | null = null
    for (let i = 0; i < this.passDefs.length; i++) {
      const pass = this.passDefs[i]

      if (pass.isAggregation) {
        // 聚合 Pass：2× 分辨率输出到 CAS FBO
        const aggFBO = this.casFBO!
        gl.bindFramebuffer(gl.FRAMEBUFFER, aggFBO.fbo)
        gl.viewport(0, 0, aggFBO.w, aggFBO.h)
        gl.useProgram(this.aggProgram!)
        const subconvFBO = this.resolveTexture('SUBCONV1', i)
        if (subconvFBO) {
          gl.activeTexture(gl.TEXTURE0)
          gl.bindTexture(gl.TEXTURE_2D, subconvFBO.tex)
          gl.uniform1i(gl.getUniformLocation(this.aggProgram!, 'u_SUBCONV1'), 0)
        }
        gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
        aggInputFBO = aggFBO
        continue
      }

      // 获取输出 FBO
      const outFBO = this.getOrCreateFBO(`${pass.saveName}_${i}`, w, h)

      // 找到对应的编译好的程序
      const progEntry = this.passPrograms.find(p => p.pass === pass)
      if (!progEntry) continue
      const prog = progEntry.prog

      this.renderToFBO(outFBO, prog, (p) => {
        let unit = 0
        for (const bindName of pass.bindNames) {
          let srcTex: WebGLTexture | null = null
          if (bindName === 'LUMA') {
            // 亮度绑定 Pass：直接使用视频 RGB 纹理，着色器内部提取亮度
            srcTex = this.videoTex!
          } else {
            const srcFBO = this.resolveTexture(bindName, i)
            if (srcFBO) srcTex = srcFBO.tex
          }
          if (srcTex) {
            gl.activeTexture(gl.TEXTURE0 + unit)
            gl.bindTexture(gl.TEXTURE_2D, srcTex)
            const loc = gl.getUniformLocation(p, `u_${bindName}`)
            if (loc) gl.uniform1i(loc, unit)
          }
          unit++
        }
      })
    }

    // ---- Step N+1: CAS 后处理 → Canvas ----
    if (aggInputFBO && this.casProgram) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, null) // 绘制到 Canvas
      gl.viewport(0, 0, this.canvas!.width, this.canvas!.height)
      gl.useProgram(this.casProgram)
      gl.activeTexture(gl.TEXTURE0)
      gl.bindTexture(gl.TEXTURE_2D, aggInputFBO.tex)
      gl.uniform1i(gl.getUniformLocation(this.casProgram, 'u_input'), 0)
      gl.uniform2f(gl.getUniformLocation(this.casProgram, 'u_texelSize'),
        1 / aggInputFBO.w, 1 / aggInputFBO.h)
      gl.uniform1f(gl.getUniformLocation(this.casProgram, 'u_amount'), this.opts.casStrength)
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
    }

    gl.bindVertexArray(null)
  }

  private renderToFBO(fbo: FBOEntry, prog: WebGLProgram, setUniforms: (prog: WebGLProgram) => void): void {
    const gl = this.gl!
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo.fbo)
    gl.viewport(0, 0, fbo.w, fbo.h)
    gl.useProgram(prog)
    setUniforms(prog)
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
  }

  /** 视频分辨率变化时重新编译着色器并重建 FBO */
  private rebuildPipeline(): void {
    const gl = this.gl!
    const vs = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SRC)!

    // 删除旧 Pass 程序
    for (const entry of this.passPrograms) gl.deleteProgram(entry.prog)
    this.passPrograms = []

    // 删除旧 FBO
    for (const [, arr] of this.fboRegistry) {
      for (const f of arr) { gl.deleteTexture(f.tex); gl.deleteFramebuffer(f.fbo) }
    }
    this.fboRegistry.clear()
    if (this.casFBO) { gl.deleteTexture(this.casFBO.tex); gl.deleteFramebuffer(this.casFBO.fbo); this.casFBO = null }

    // 重建 Pass 着色器
    for (const pass of this.passDefs) {
      if (pass.isAggregation) continue
      const fragSrc = buildPassShader(pass, this.inputW, this.inputH)
      const fs = compileShader(gl, gl.FRAGMENT_SHADER, fragSrc)!
      const prog = linkProgram(gl, vs, fs)!
      gl.deleteShader(fs)
      this.passPrograms.push({ prog, pass })
    }

    // 重建聚合着色器
    if (this.aggProgram) { gl.deleteProgram(this.aggProgram); this.aggProgram = null }
    const aggFs = compileShader(gl, gl.FRAGMENT_SHADER, buildAggShader(this.inputW, this.inputH))!
    this.aggProgram = linkProgram(gl, vs, aggFs)
    gl.deleteShader(aggFs)

    // 重建 FBO
    this.allocateFBOs()

    // 顶点着色器已被所有 program 引用，link 后可安全删除
    gl.deleteShader(vs)
  }
}

// ==================== 导出函数 ====================

export function checkFilmSupport(): { supported: boolean; message: string } {
  try {
    const c = document.createElement('canvas')
    const gl = c.getContext('webgl2')
    if (!gl) return { supported: false, message: 'WebGL2 不可用' }
    const ext = gl.getExtension('EXT_color_buffer_float')
    if (!ext) return { supported: false, message: 'EXT_color_buffer_float 不可用' }
    return { supported: true, message: 'FSRCNNX + CAS 可用' }
  } catch {
    return { supported: false, message: 'WebGL2 检测失败' }
  }
}

/** 影视增强预设 */
export const FILM_PRESET: Required<FilmUpscalerOptions> = {
  sharpness: 0.5,
  casStrength: 0.4,
}
