import { enhancementBufferBudget, enhancementOutputSize, sizeEnhancementBuffer, type EnhancementBufferBudget } from './videoLayout'
/** Film enhancement: FSRCNNX 2x super-resolution followed by CAS sharpening. */
import fsrcnnxSource from './FSRCNNX_x2_8-0-4-1.glsl?raw'
import { loseGlContext } from './anime4kUpscaler'

export interface FilmUpscalerOptions { casStrength?: number; sharpenOnly?: boolean }
export interface FilmStats {
  fps: number; gpuEnabled: boolean
  qualityScale: number; enhancements: string[]
}

export interface PassDef {
  desc: string; body: string; bindNames: string[]; saveName: string
  components: number; isAggregation: boolean; lumaBind: boolean
}

// ==================== 着色器 ====================

export const VERTEX_SRC = `#version 300 es
layout(location=0) in vec2 a_pos;
out vec2 v_uv;
void main() { gl_Position = vec4(a_pos, 0.0, 1.0); v_uv = a_pos * 0.5 + 0.5; }`

// canvas 输出专用：翻转 Y 以匹配 DOM 坐标系
export const VERTEX_FLIP_SRC = `#version 300 es
layout(location=0) in vec2 a_pos;
out vec2 v_uv;
void main() { gl_Position = vec4(a_pos.x, -a_pos.y, 0.0, 1.0); v_uv = a_pos * 0.5 + 0.5; }`

// Contrast-adaptive sharpening with a local limiter for high-contrast edges.
const CAS_FRAG = `#version 300 es
precision highp float;
precision highp sampler2D;
in vec2 v_uv;
out vec4 fragColor;
uniform sampler2D u_input;
uniform vec2 u_texelSize;
uniform float u_amount;
float luma(vec3 color) { return dot(color, vec3(0.2126, 0.7152, 0.0722)); }
void main() {
  vec3 center = texture(u_input, v_uv).rgb;
  vec3 north = texture(u_input, v_uv - vec2(0.0, u_texelSize.y)).rgb;
  vec3 south = texture(u_input, v_uv + vec2(0.0, u_texelSize.y)).rgb;
  vec3 west = texture(u_input, v_uv - vec2(u_texelSize.x, 0.0)).rgb;
  vec3 east = texture(u_input, v_uv + vec2(u_texelSize.x, 0.0)).rgb;
  vec3 low = min(center, min(min(north, south), min(west, east)));
  vec3 high = max(center, max(max(north, south), max(west, east)));
  float headroom = min(luma(low), 1.0 - luma(high));
  float weight = -0.19 * clamp(u_amount, 0.0, 1.0)
               * sqrt(clamp(headroom / max(luma(high), 1e-5), 0.0, 1.0));
  vec3 sharp = (center + weight * (north + south + west + east)) / (1.0 + 4.0 * weight);
  fragColor = vec4(clamp(sharp, low, high), 1.0);
}`

// SUBCONV1 packs four absolute luma samples in column-major subpixel order.
const DETAIL_COMPOSE_FRAG = `#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;
in vec2 v_uv;
out vec4 fragColor;
uniform sampler2D u_detail;
uniform sampler2D u_color;
void main() {
  ivec2 pixel = ivec2(gl_FragCoord.xy);
  int channel = (pixel.x % 2) * 2 + (pixel.y % 2);
  float enhancedLuma = texelFetch(u_detail, pixel / 2, 0)[channel];
  vec3 color = texture(u_color, v_uv).rgb;
  float delta = enhancedLuma - dot(color, vec3(0.2126, 0.7152, 0.0722));
  fragColor = vec4(clamp(color + delta, 0.0, 1.0), 1.0);
}`

// ==================== mpv parser ====================

export function parseMpvShader(src: string): PassDef[] {
  const blocks = src.split(/^\/\/!HOOK /m).slice(1)
  const passes: PassDef[] = []
  for (const block of blocks) {
    const lines = block.split('\n')
    let desc = '', body = '', saveName = '', isAgg = false, components = 4
    const bindNames: string[] = []
    let inBody = false; const bodyLines: string[] = []
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
    body = bodyLines.join('\n')
      .replace(/\s*return vec4\(res\);\s*$/m, '\n')
      .replace(/\s*return res;\s*$/m, '\n')
      .replace(/^\s*\{/, '').replace(/\}\s*$/, '').trim()
    const lumaBind = bindNames.includes('LUMA')
    if (body.trim()) passes.push({ desc, body, bindNames, saveName, components, isAggregation: isAgg, lumaBind })
  }
  return passes
}

export function buildPassShader(pass: PassDef, iw: number, ih: number): string {
  const px = (1 / iw).toFixed(15), py = (1 / ih).toFixed(15)
  const samplers = pass.bindNames.map(n => `uniform sampler2D u_${n};`).join('\n')
  const defs = pass.bindNames.filter(n => !(pass.lumaBind && n === 'LUMA'))
    .map(n => `#define ${n}_texOff(off) texture(u_${n}, v_uv + vec2(off) * vec2(${px}, ${py}))\n#define ${n}_tex(p) texture(u_${n}, p)`)
    .join('\n')
  const lp = pass.lumaBind
    ? `float _luma(sampler2D t, vec2 uv) { vec3 c = texture(t, uv).rgb; return dot(c, vec3(0.2126,0.7152,0.0722)); }\n#define LUMA_texOff(off) _luma(u_LUMA, v_uv + vec2(off) * vec2(${px}, ${py}))`
    : ''
  const parts = [samplers, defs, lp].filter(s => s.length > 0)
  return `#version 300 es
precision highp float;
precision highp sampler2D;
in vec2 v_uv;
layout(location=0) out vec4 _out0;
${parts.join('\n')}
void main() {
${pass.body}
_out0 = res;
}`
}

// ==================== WebGL2 工具 ====================

export function compileShader(gl: WebGL2RenderingContext, type: number, src: string): WebGLShader | null {
  const s = gl.createShader(type); if (!s) return null
  gl.shaderSource(s, src); gl.compileShader(s)
  if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
    console.error('[FilmUpscaler] compile:', gl.getShaderInfoLog(s)?.slice(0, 200))
    gl.deleteShader(s); return null
  }
  return s
}

export function linkProgram(gl: WebGL2RenderingContext, vs: WebGLShader, fs: WebGLShader): WebGLProgram | null {
  const p = gl.createProgram(); if (!p) return null
  gl.attachShader(p, vs); gl.attachShader(p, fs); gl.linkProgram(p)
  if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
    console.error('[FilmUpscaler] link:', gl.getProgramInfoLog(p))
    gl.deleteProgram(p); return null
  }
  return p
}

export interface FBOEntry { tex: WebGLTexture; fbo: WebGLFramebuffer; w: number; h: number }

export class EnhancementTargetError extends Error {}

export function createFBO(gl: WebGL2RenderingContext, w: number, h: number, filter?: number): FBOEntry {
  const tex = gl.createTexture()
  if (!tex) throw new EnhancementTargetError('Cannot create texture')
  gl.bindTexture(gl.TEXTURE_2D, tex)
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA16F, w, h, 0, gl.RGBA, gl.HALF_FLOAT, null)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filter ?? gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  const fbo = gl.createFramebuffer()
  if (!fbo) {
    gl.deleteTexture(tex)
    throw new EnhancementTargetError('Cannot create framebuffer')
  }
  gl.bindFramebuffer(gl.FRAMEBUFFER, fbo)
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
  const status = gl.checkFramebufferStatus(gl.FRAMEBUFFER)
  gl.bindFramebuffer(gl.FRAMEBUFFER, null)
  if (status !== gl.FRAMEBUFFER_COMPLETE) {
    gl.deleteFramebuffer(fbo)
    gl.deleteTexture(tex)
    throw new EnhancementTargetError('Incomplete enhancement framebuffer: ' + status)
  }
  return { tex, fbo, w, h }
}

export function deleteFBO(gl: WebGL2RenderingContext, f: FBOEntry | null): void {
  if (f) { gl.deleteTexture(f.tex); gl.deleteFramebuffer(f.fbo) }
}

// ==================== FilmUpscaler ====================

export class FilmUpscaler {
  private opts: Required<FilmUpscalerOptions>
  private canvas: HTMLCanvasElement | null = null
  private gl: WebGL2RenderingContext | null = null
  private fullBudget?: EnhancementBufferBudget
  private lightBudget?: EnhancementBufferBudget
  private video: HTMLVideoElement | null = null
  private running = false; private rafId = 0; private videoFrameCallbackId: number | null = null
  private _error: string | null = null
  private casOnly = false

  private quadVAO: WebGLVertexArrayObject | null = null
  private quadVBO: WebGLBuffer | null = null
  private videoTex: WebGLTexture | null = null

  private targets = new Map<string, FBOEntry>()
  private passProgs: WebGLProgram[] = []
  private detailComposeProg: WebGLProgram | null = null
  private casFlipProg: WebGLProgram | null = null  // canvas 输出用（Y 翻转）

  // 预分配 FBO
  private composeFBO: FBOEntry | null = null

  // uniform location 缓存（二级 Map，消除冲突）
  private ulocs: Map<WebGLProgram, Map<string, WebGLUniformLocation | null>> = new Map()

  private passDefs: PassDef[] = []
  private frames = 0; private lastFpsTime = 0
  private currentFps = 0
  private inputW = 0; private inputH = 0
  private lastUploadTime = -1

  get error(): string | null { return this._error }

  constructor(opts: FilmUpscalerOptions = {}) {
    this.opts = { casStrength: opts.casStrength ?? 0.8, sharpenOnly: opts.sharpenOnly ?? false }
  }
  get isSharpenOnly(): boolean { return this.casOnly }

  async init(video: HTMLVideoElement, wrapper?: HTMLElement): Promise<boolean> {
    try {
      this.video = video
      this.canvas = document.createElement('canvas')
      this.canvas.hidden = true
      this.canvas.dataset.cczjEnhancement = 'true'
      this.canvas.style.cssText = 'position:absolute;top:0;left:0;pointer-events:none;z-index:2'
      const c = wrapper ?? video.parentElement
      if (!c) throw new Error('No container')
      c.appendChild(this.canvas)

      const gl = this.canvas.getContext('webgl2', {
        alpha: false, antialias: false, premultipliedAlpha: false, preserveDrawingBuffer: false,
      }) as WebGL2RenderingContext | null
      if (!gl) throw new Error('WebGL2')
      this.gl = gl

      gl.getExtension('OES_texture_float_linear')
      this.inputW = video.videoWidth || 1920; this.inputH = video.videoHeight || 1080

      this.quadVAO = gl.createVertexArray()!
      gl.bindVertexArray(this.quadVAO)
      this.quadVBO = gl.createBuffer()!
      gl.bindBuffer(gl.ARRAY_BUFFER, this.quadVBO)
      gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1,1,-1,-1,1,1,1]), gl.STATIC_DRAW)
      gl.enableVertexAttribArray(0); gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0)
      gl.bindVertexArray(null)

      this.videoTex = gl.createTexture()!
      gl.bindTexture(gl.TEXTURE_2D, this.videoTex)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
      gl.bindTexture(gl.TEXTURE_2D, null)

      this.configure(gl)
      console.log(`[FilmUpscaler] init: ${this.inputW}×${this.inputH} [${this.casOnly ? 'CAS' : 'FSRCNNX + CAS'}]`)
      return true
    } catch (e: any) {
      this._error = e.message || String(e); console.error('[FilmUpscaler] init:', this._error)
      // 失败也要走完整销毁：只摘 canvas 会留着 this.gl 引用，上下文既没法用又不会被回收。
      this.destroy()
      return false
    }
  }

  private configure(gl: WebGL2RenderingContext): void {
    this.passDefs = parseMpvShader(fsrcnnxSource).filter(pass => !pass.isAggregation)
    const targets = new Set(this.passDefs.map(pass => pass.saveName)).size
    this.lightBudget = enhancementBufferBudget(gl, this.inputW, this.inputH, 0)
    if (!this.lightBudget) throw new Error('Video texture exceeds GPU budget')
    this.fullBudget = !this.opts.sharpenOnly && gl.getExtension('EXT_color_buffer_float')
      ? enhancementBufferBudget(gl, this.inputW, this.inputH, targets, 1) : undefined
    this.configureOutput()
  }

  // The bundled model requests reconstruction above 1.3x in both dimensions.
  private configureOutput(): boolean {
    const video = this.video!, canvas = this.canvas!, gl = this.gl!
    const output = this.fullBudget && enhancementOutputSize(video, canvas, 2, this.fullBudget)
    const light = !output || output.width <= this.inputW * 1.3 || output.height <= this.inputH * 1.3
    const changedMode = !this.casFlipProg || this.casOnly !== light
    if (changedMode) {
      this.releasePasses(gl)
      this.casOnly = light
      this.compileAll(gl)
      if (!light) this.allocFBOs()
    }
    if (light) return sizeEnhancementBuffer(video, canvas, 1, this.lightBudget!) || changedMode
    if (canvas.width === output.width && canvas.height === output.height) return changedMode
    canvas.width = output.width
    canvas.height = output.height
    return true
  }

  private compileAll(gl: WebGL2RenderingContext): void {
    const vsFlip = compileShader(gl, gl.VERTEX_SHADER, VERTEX_FLIP_SRC)
    const casFs = compileShader(gl, gl.FRAGMENT_SHADER, CAS_FRAG)
    if (vsFlip && casFs) this.casFlipProg = linkProgram(gl, vsFlip, casFs)
    if (casFs) gl.deleteShader(casFs)
    if (vsFlip) gl.deleteShader(vsFlip)
    if (!this.casFlipProg) throw new Error('CAS shader')
    if (this.casOnly) return
    const vs = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SRC)
    if (!vs) throw new Error('VS')
    const program = (source: string, label: string): WebGLProgram => {
      const fs = compileShader(gl, gl.FRAGMENT_SHADER, source)
      if (!fs) throw new Error(label)
      const linked = linkProgram(gl, vs, fs)
      gl.deleteShader(fs)
      if (!linked) throw new Error(label)
      return linked
    }
    try {
      for (const pass of this.passDefs) {
        this.passProgs.push(program(buildPassShader(pass, this.inputW, this.inputH), pass.desc))
      }
      this.detailComposeProg = program(DETAIL_COMPOSE_FRAG, 'FSRCNNX composition')
    } finally {
      gl.deleteShader(vs)
    }
  }

  private allocFBOs(): void {
    const gl = this.gl!, w = this.inputW, h = this.inputH
    this.composeFBO = createFBO(gl, w * 2, h * 2)
    // Repeated SAVE names in this model replace features after their last read.
    for (const pass of this.passDefs) {
      if (!this.targets.has(pass.saveName)) this.targets.set(pass.saveName, createFBO(gl, w, h))
    }
  }

  private releasePasses(gl: WebGL2RenderingContext): void {
    for (const target of this.targets.values()) deleteFBO(gl, target)
    this.targets.clear()
    deleteFBO(gl, this.composeFBO)
    this.composeFBO = null
    for (const program of this.passProgs) gl.deleteProgram(program)
    this.passProgs = []
    gl.deleteProgram(this.casFlipProg)
    gl.deleteProgram(this.detailComposeProg)
    this.casFlipProg = this.detailComposeProg = null
    this.ulocs.clear()
  }

  private getLoc(prog: WebGLProgram, name: string): WebGLUniformLocation | null {
    let map = this.ulocs.get(prog)
    if (!map) { map = new Map(); this.ulocs.set(prog, map) }
    if (map.has(name)) return map.get(name)!
    const loc = this.gl!.getUniformLocation(prog, name)
    map.set(name, loc)
    return loc
  }

  start(): void {
    if (this.running || !this.gl) return
    this.running = true; this.frames = 0; this.lastFpsTime = performance.now(); this.renderLoop()
  }
  stop(): void {
    this.running = false
    if (this.canvas) this.canvas.hidden = true
    if (this.rafId) { cancelAnimationFrame(this.rafId); this.rafId = 0 }
    const video = this.video as (HTMLVideoElement & { cancelVideoFrameCallback?: (id: number) => void }) | null
    if (video && this.videoFrameCallbackId !== null && video.cancelVideoFrameCallback) {
      video.cancelVideoFrameCallback(this.videoFrameCallbackId)
    }
    this.videoFrameCallbackId = null
  }

  onSeeked(): void {
    this.lastUploadTime = -1
    if (this.running && this.video?.paused) this.renderCurrentFrame()
  }

  destroy(): void {
    this.stop()
    this.lastUploadTime = -1
    const gl = this.gl
    if (gl) {
      this.releasePasses(gl)
      if (this.videoTex) gl.deleteTexture(this.videoTex)
      if (this.quadVBO) gl.deleteBuffer(this.quadVBO)
      if (this.quadVAO) gl.deleteVertexArray(this.quadVAO)
      this.ulocs.clear()
      loseGlContext(gl)
    }
    this.passDefs = []
    this.quadVAO = null; this.quadVBO = null; this.videoTex = null
    if (this.canvas?.parentElement) this.canvas.parentElement.removeChild(this.canvas)
    this.canvas = null; this.gl = null; this.video = null; this.lightBudget = this.fullBudget = undefined
  }

  setCompareSplit(percent: number | null): void {
    if (!this.canvas) return
    this.canvas.style.clipPath = percent == null ? '' : `inset(0 0 0 ${Math.max(0, Math.min(100, percent))}%)`
  }

  getStats(): FilmStats {
    return { fps: this.currentFps, gpuEnabled: this.running, qualityScale: this.casOnly ? 1 : 2, enhancements: this.casOnly ? ['CAS'] : ['FSRCNNX', 'CAS'] }
  }

  resizeOutput(): void {
    if (!this.running || !this.video || !this.canvas) return
    try {
      if (this.video.videoWidth !== this.inputW || this.video.videoHeight !== this.inputH || this.configureOutput()) {
        this.lastUploadTime = -1
        this.renderCurrentFrame()
      }
    } catch (error) {
      this._error = error instanceof Error ? error.message : String(error)
      this.stop()
    }
  }

  private renderLoop = (): void => {
    if (!this.running) return
    const video = this.video as (HTMLVideoElement & {
      requestVideoFrameCallback?: (callback: () => void) => number
    }) | null
    if (video?.requestVideoFrameCallback) {
      this.videoFrameCallbackId = video.requestVideoFrameCallback(() => {
        this.videoFrameCallbackId = null
        this.renderLoop()
      })
    } else {
      this.rafId = requestAnimationFrame(this.renderLoop)
    }
    this.renderCurrentFrame()
    const now = performance.now()
    if (now - this.lastFpsTime >= 2000) {
      this.currentFps = Math.round(this.frames / ((now - this.lastFpsTime) / 1000))
      this.frames = 0; this.lastFpsTime = now
    }
  }

  private renderCurrentFrame(): void {
    try {
      this.render()
    } catch (error) {
      this._error = error instanceof Error ? error.message : String(error)
      this.stop()
    }
  }

  private render(): void {
    const gl = this.gl!, video = this.video!
    if (video.readyState < 2 || !video.videoWidth) return

    if (video.videoWidth !== this.inputW || video.videoHeight !== this.inputH) {
      this.inputW = video.videoWidth; this.inputH = video.videoHeight
      this.rebuild(); return
    }

    // RAF often runs faster than the source video. Skip the expensive
    // multi-pass pipeline when no decoded frame has arrived.
    if (video.currentTime === this.lastUploadTime) return

    const w = this.inputW, h = this.inputH
    const outW = w * 2, outH = h * 2

    gl.bindVertexArray(this.quadVAO)

    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, this.videoTex)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, video)
    this.lastUploadTime = video.currentTime

    const bind = (prog: WebGLProgram, tex: WebGLTexture, name: string, unit: number) => {
      gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, tex)
      const loc = this.getLoc(prog, name); if (loc) gl.uniform1i(loc, unit)
    }
    const set1f = (prog: WebGLProgram, name: string, v: number) => {
      const loc = this.getLoc(prog, name); if (loc) gl.uniform1f(loc, v)
    }
    const set2f = (prog: WebGLProgram, name: string, x: number, y: number) => {
      const loc = this.getLoc(prog, name); if (loc) gl.uniform2f(loc, x, y)
    }
    const pass = (fbo: FBOEntry, prog: WebGLProgram, vw: number, vh: number, cb: () => void) => {
      gl.bindFramebuffer(gl.FRAMEBUFFER, fbo.fbo); gl.viewport(0, 0, vw, vh); gl.useProgram(prog); cb()
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
    }

    const src = this.videoTex!

    if (!this.casOnly) {
      for (const [i, pd] of this.passDefs.entries()) {
        const prog = this.passProgs[i]
        pass(this.targets.get(pd.saveName)!, prog, w, h, () => {
          pd.bindNames.forEach((name, unit) => {
            const texture = name === 'LUMA' ? src : this.targets.get(name)!.tex
            bind(prog, texture, 'u_' + name, unit)
          })
        })
      }
      pass(this.composeFBO!, this.detailComposeProg!, outW, outH, () => {
        bind(this.detailComposeProg!, this.targets.get('SUBCONV1')!.tex, 'u_detail', 0)
        bind(this.detailComposeProg!, src, 'u_color', 1)
      })
    }

    const finalSrc = this.casOnly ? { tex: src, w, h } : this.composeFBO
    if (finalSrc && this.casFlipProg) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, null)
      gl.viewport(0, 0, this.canvas!.width, this.canvas!.height)
      gl.useProgram(this.casFlipProg)
      bind(this.casFlipProg, finalSrc.tex, 'u_input', 0)
      set2f(this.casFlipProg, 'u_texelSize', 1/finalSrc.w, 1/finalSrc.h)
      set1f(this.casFlipProg, 'u_amount', this.opts.casStrength)
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
      this.canvas!.hidden = false
      this.frames++
    }

    gl.bindVertexArray(null)
  }

  private rebuild(): void {
    const gl = this.gl!
    this.stop()
    this.releasePasses(gl)
    this.onSeeked()
    try {
      this.configure(gl)
      this.start()
    } catch (error) {
      this._error = error instanceof Error ? error.message : String(error)
      this.destroy()
    }
  }
}

export const FILM_PRESET = { casStrength: 0.8 } satisfies FilmUpscalerOptions
