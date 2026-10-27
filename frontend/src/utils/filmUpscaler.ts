import { layoutCanvasToVideo } from './videoLayout'
/** Film enhancement: FSRCNNX 2x super-resolution followed by CAS sharpening. */
import fsrcnnxSource from './FSRCNNX_x2_8-0-4-1.glsl?raw'
import { loseGlContext } from './anime4kUpscaler'

export interface FilmUpscalerOptions { casStrength?: number }
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
in vec2 a_pos;
out vec2 v_uv;
void main() { gl_Position = vec4(a_pos, 0.0, 1.0); v_uv = a_pos * 0.5 + 0.5; }`

// canvas 输出专用：翻转 Y 以匹配 DOM 坐标系
export const VERTEX_FLIP_SRC = `#version 300 es
in vec2 a_pos;
out vec2 v_uv;
void main() { gl_Position = vec4(a_pos.x, -a_pos.y, 0.0, 1.0); v_uv = a_pos * 0.5 + 0.5; }`





// Reinhard 色调映射

// 标准 AMD CAS 锐化
const CAS_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input; uniform vec2 u_texelSize; uniform float u_amount;
float luma(vec3 c) { return dot(c, vec3(0.299,0.587,0.114)); }
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
  float amp = sqrt(min(mn, 2.0 - mx) / max(mx, 1e-5));
  float w = amp * mix(0.0, 0.5, u_amount);
  fragColor = vec4((e + w * (a + b + c + d)) / (1.0 + 4.0 * w), 1.0);
}`

// Bilinear color upsampling alongside FSRCNNX detail reconstruction.
const BILINEAR_UP_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input;
void main() {
  fragColor = texture(u_input, v_uv);
}`


// 颜色-细节合成着色器：将 FSRCNNX 提取的高频细节以加法叠加到彩色放大图上
const DETAIL_COMPOSE_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_detail;   // FSRCNNX 输出
uniform sampler2D u_color;    // bilinear 上采样的彩色图
float luma(vec3 c) { return dot(c, vec3(0.2126, 0.7152, 0.0722)); }
void main() {
  vec3 color = texture(u_color, v_uv).rgb;
  float detailL = texture(u_detail, v_uv).r;
  float baseL = luma(color);
  
  // 提取 FSRCNNX 带来的"高频细节差值"
  float detailDiff = detailL - baseL;
  
  // 将细节差值按照原色彩的亮度比例加回去，而不是直接相乘
  vec3 outColor = color + detailDiff;
  
  // 防止过曝或负值
  outColor = clamp(outColor, 0.0, 1.0);
  fragColor = vec4(outColor, 1.0);
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
    .map(n => `#define ${n}_texOff(off) texture(u_${n}, v_uv + float(off) * vec2(${px}, ${py}))\n#define ${n}_tex(p) texture(u_${n}, p)`)
    .join('\n')
  const lp = pass.lumaBind
    ? `float _luma(sampler2D t, vec2 uv) { vec3 c = texture(t, uv).rgb; return dot(c, vec3(0.2126,0.7152,0.0722)); }\n#define LUMA_texOff(off) _luma(u_LUMA, v_uv + float(off) * vec2(${px}, ${py}))`
    : ''
  const parts = [samplers, defs, lp].filter(s => s.length > 0)
  return `#version 300 es
precision highp float;
in vec2 v_uv;
layout(location=0) out vec4 _out0;
${parts.join('\n')}
void main() {
${pass.body}
_out0 = res;
}`
}

function buildAggShader(iw: number, ih: number): string {
  return `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_SUBCONV1;
void main() {
  vec2 ic = v_uv * vec2(${iw}.0, ${ih}.0);
  vec2 fc = fract(ic);
  vec2 bs = (floor(ic) + vec2(0.5)) / vec2(${iw}.0, ${ih}.0);
  ivec2 idx = ivec2(fc * vec2(2));
  vec4 res = texture(u_SUBCONV1, bs);
  float luma = res[idx.x * 2 + idx.y];
  fragColor = vec4(luma, luma, luma, 1.0);
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

export function createFBO(gl: WebGL2RenderingContext, w: number, h: number, filter?: number): FBOEntry {
  const tex = gl.createTexture()!
  gl.bindTexture(gl.TEXTURE_2D, tex)
  gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA16F, w, h, 0, gl.RGBA, gl.HALF_FLOAT, null)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filter ?? gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
  const fbo = gl.createFramebuffer()!
  gl.bindFramebuffer(gl.FRAMEBUFFER, fbo)
  gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
  gl.bindFramebuffer(gl.FRAMEBUFFER, null)
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
  private video: HTMLVideoElement | null = null
  private running = false; private rafId = 0; private videoFrameCallbackId: number | null = null
  private _error: string | null = null

  private quadVAO: WebGLVertexArrayObject | null = null
  private quadVBO: WebGLBuffer | null = null
  private videoTex: WebGLTexture | null = null

  private fboReg: Map<string, FBOEntry[]> = new Map()
  private passProgs: WebGLProgram[] = []
  private aggProg: WebGLProgram | null = null
  private detailComposeProg: WebGLProgram | null = null
  private casFlipProg: WebGLProgram | null = null  // canvas 输出用（Y 翻转）
  private bilinearProg: WebGLProgram | null = null

  // 预分配 FBO
  private lowResFBO: FBOEntry | null = null
  private detailFBO: FBOEntry | null = null
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
    this.opts = { casStrength: opts.casStrength ?? 0.8 }
  }

  async init(video: HTMLVideoElement, wrapper?: HTMLElement): Promise<boolean> {
    try {
      this.video = video
      this.canvas = document.createElement('canvas')
      this.canvas.dataset.cczjEnhancement = 'true'
      this.canvas.style.cssText = 'position:absolute;top:0;left:0;pointer-events:none;z-index:2'
      const c = wrapper ?? video.parentElement
      if (!c) throw new Error('No container')
      c.style.position = 'relative'; c.appendChild(this.canvas)

      const gl = this.canvas.getContext('webgl2', {
        alpha: false, antialias: false, premultipliedAlpha: false, preserveDrawingBuffer: false,
      }) as WebGL2RenderingContext | null
      if (!gl) throw new Error('WebGL2')
      this.gl = gl

      const hasFloat = !!gl.getExtension('EXT_color_buffer_float')
      gl.getExtension('OES_texture_float_linear')
      if (!hasFloat) console.warn('[FilmUpscaler] EXT_color_buffer_float 不可用，降级到 RGBA8')

      this.canvas.addEventListener('webglcontextlost', e => { e.preventDefault(); this.stop() })
      this.canvas.addEventListener('webglcontextrestored', () => { this.rebuild() })

      this.inputW = video.videoWidth || 1920; this.inputH = video.videoHeight || 1080
      this.canvas.width = this.inputW * 2; this.canvas.height = this.inputH * 2

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

      this.compileAll(gl); this.allocFBOs()
      console.log(`[FilmUpscaler] init: ${this.inputW}×${this.inputH} [${['FSRCNNX', 'CAS'].join(',')}]`)
      return true
    } catch (e: any) {
      this._error = e.message || String(e); console.error('[FilmUpscaler] init:', this._error)
      // 失败也要走完整销毁：只摘 canvas 会留着 this.gl 引用，上下文既没法用又不会被回收。
      this.destroy()
      return false
    }
  }

  private compileAll(gl: WebGL2RenderingContext): void {
    const vs = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SRC)
    if (!vs) throw new Error('VS')
    const fsProg = (src: string) => {
      const fs = compileShader(gl, gl.FRAGMENT_SHADER, src); if (!fs) throw new Error('FS')
      const p = linkProgram(gl, vs, fs)!; gl.deleteShader(fs); return p
    }
    this.passDefs = parseMpvShader(fsrcnnxSource)
    this.passProgs = []
    for (let i = 0; i < this.passDefs.length; i++) {
      const pass = this.passDefs[i]
      if (pass.isAggregation) { this.passProgs.push(null!); continue }
      const fs = compileShader(gl, gl.FRAGMENT_SHADER, buildPassShader(pass, this.inputW, this.inputH))
      if (!fs) throw new Error(`Pass ${pass.desc}`)
      const p = linkProgram(gl, vs, fs)!; gl.deleteShader(fs)
      this.passProgs.push(p)
    }
    const aggFs = compileShader(gl, gl.FRAGMENT_SHADER, buildAggShader(this.inputW, this.inputH))
    if (!aggFs) throw new Error('Agg'); this.aggProg = linkProgram(gl, vs, aggFs)!; gl.deleteShader(aggFs)

    this.detailComposeProg = fsProg(DETAIL_COMPOSE_FRAG)
    // canvas 输出用 flipped VS 重新链接 CAS
    {
      const vsFlip = compileShader(gl, gl.VERTEX_SHADER, VERTEX_FLIP_SRC)
      const casFs = compileShader(gl, gl.FRAGMENT_SHADER, CAS_FRAG)
      if (vsFlip && casFs) {
        this.casFlipProg = linkProgram(gl, vsFlip, casFs)
        gl.deleteShader(casFs)
      }
      gl.deleteShader(vsFlip)
    }
    this.bilinearProg = fsProg(BILINEAR_UP_FRAG)
    gl.deleteShader(vs)
  }

  private allocFBOs(): void {
    const gl = this.gl!, w = this.inputW, h = this.inputH
    const outW = w * 2, outH = h * 2
    this.lowResFBO = createFBO(gl, outW, outH)
    this.detailFBO = createFBO(gl, outW, outH)
    this.composeFBO = createFBO(gl, outW, outH)

    for (let i = 0; i < this.passDefs.length; i++) {
      const p = this.passDefs[i]
      if (!p.isAggregation && p.saveName) this.getFBO(`${p.saveName}_${i}`, w, h)
    }
  }

  private getFBO(name: string, w: number, h: number): FBOEntry {
    let a = this.fboReg.get(name); if (!a) { a = []; this.fboReg.set(name, a) }
    for (const f of a) if (f.w === w && f.h === h) return f
    const fb = createFBO(this.gl!, w, h); a.push(fb); return fb
  }

  private resolveTex(name: string, before: number): FBOEntry | null {
    for (let i = before - 1; i >= 0; i--) {
      if (this.passDefs[i].saveName === name && !this.passDefs[i].isAggregation) {
        return this.getFBO(`${name}_${i}`, this.inputW, this.inputH)
      }
    }
    return null
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
    if (this.rafId) { cancelAnimationFrame(this.rafId); this.rafId = 0 }
    const video = this.video as (HTMLVideoElement & { cancelVideoFrameCallback?: (id: number) => void }) | null
    if (video && this.videoFrameCallbackId !== null && video.cancelVideoFrameCallback) {
      video.cancelVideoFrameCallback(this.videoFrameCallbackId)
    }
    this.videoFrameCallbackId = null
  }

  onSeeked(): void { this.lastUploadTime = -1 }

  destroy(): void {
    this.stop()
    const gl = this.gl
    if (gl) {
      for (const [, a] of this.fboReg) for (const f of a) { gl.deleteTexture(f.tex); gl.deleteFramebuffer(f.fbo) }
      this.fboReg.clear()
      for (const f of [this.lowResFBO, this.detailFBO, this.composeFBO])
        deleteFBO(gl, f)
      this.onSeeked()
      for (const p of this.passProgs) if (p) gl.deleteProgram(p)
      for (const p of [this.aggProg, this.detailComposeProg, this.casFlipProg, this.bilinearProg])
        if (p) gl.deleteProgram(p)
      if (this.videoTex) gl.deleteTexture(this.videoTex)
      if (this.quadVBO) gl.deleteBuffer(this.quadVBO)
      if (this.quadVAO) gl.deleteVertexArray(this.quadVAO)
      this.ulocs.clear()
      loseGlContext(gl)
    }
    this.passProgs = []; this.passDefs = []; this.fboReg.clear()
    this.lowResFBO = null; this.detailFBO = null; this.composeFBO = null
    this.aggProg = null; this.detailComposeProg = null; this.casFlipProg = null
    this.bilinearProg = null
    this.quadVAO = null; this.quadVBO = null; this.videoTex = null
    if (this.canvas?.parentElement) this.canvas.parentElement.removeChild(this.canvas)
    this.canvas = null; this.gl = null; this.video = null
  }

  setCompareSplit(percent: number | null): void {
    if (!this.canvas) return
    this.canvas.style.clipPath = percent == null ? '' : `inset(0 0 0 ${Math.max(0, Math.min(100, percent))}%)`
  }

  getStats(): FilmStats {
    return { fps: this.currentFps, gpuEnabled: true, qualityScale: 2, enhancements: ['FSRCNNX', 'CAS'] }
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
    this.render(); this.frames++
    const now = performance.now()
    if (now - this.lastFpsTime >= 2000) {
      this.currentFps = Math.round(this.frames / ((now - this.lastFpsTime) / 1000))
      this.frames = 0; this.lastFpsTime = now
    }
  }

  private syncCanvasToVideo(): void {
    layoutCanvasToVideo(this.video!, this.canvas!)
  }

  private render(): void {
    const gl = this.gl!, video = this.video!
    if (video.readyState < 2 || !video.videoWidth) return

    if (video.videoWidth !== this.inputW || video.videoHeight !== this.inputH) {
      this.inputW = video.videoWidth; this.inputH = video.videoHeight
      this.canvas!.width = this.inputW * 2; this.canvas!.height = this.inputH * 2
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

    // FSRCNNX detail reconstruction plus bilinear color upsampling
    let aggOut: FBOEntry | null = null
    {
      // 细节+颜色合成管线：bilinear 彩色放大 + FSRCNNX 细节亮度 → 合成
      pass(this.lowResFBO!, this.bilinearProg!, outW, outH, () => {
        bind(this.bilinearProg!, src, 'u_input', 0)
      })
      for (let i = 0; i < this.passDefs.length; i++) {
        const pd = this.passDefs[i]
        if (pd.isAggregation) {
          pass(this.detailFBO!, this.aggProg!, outW, outH, () => {
            const s = this.resolveTex('SUBCONV1', i)
            if (s) bind(this.aggProg!, s.tex, 'u_SUBCONV1', 0)
          })
          continue
        }
        const out = this.getFBO(`${pd.saveName}_${i}`, w, h)
        const prog = this.passProgs[i]
        if (!prog) continue
        pass(out, prog, w, h, () => {
          let u = 0
          for (const bn of pd.bindNames) {
            if (bn === 'LUMA') { bind(prog, src, `u_${bn}`, u) }
            else {
              const s = this.resolveTex(bn, i)
              if (s) bind(prog, s.tex, `u_${bn}`, u)
              else if (bn === 'ORIGINAL' || bn === 'SOURCE') bind(prog, src, `u_${bn}`, u)
            }
            u++
          }
        })
      }
      pass(this.composeFBO!, this.detailComposeProg!, outW, outH, () => {
        bind(this.detailComposeProg!, this.detailFBO!.tex, 'u_detail', 0)
        bind(this.detailComposeProg!, this.lowResFBO!.tex, 'u_color', 1)
      })
      aggOut = this.composeFBO!
    }

    // Step 5: CAS → Canvas（Y 翻转 VS 匹配 DOM 坐标系）
    // 同步 canvas 布局到视频的 letterbox 区域
    this.syncCanvasToVideo()
    const finalSrc = aggOut
    if (finalSrc && this.casFlipProg) {
      gl.bindFramebuffer(gl.FRAMEBUFFER, null)
      gl.viewport(0, 0, this.canvas!.width, this.canvas!.height)
      gl.useProgram(this.casFlipProg)
      bind(this.casFlipProg, finalSrc.tex, 'u_input', 0)
      set2f(this.casFlipProg, 'u_texelSize', 1/finalSrc.w, 1/finalSrc.h)
      set1f(this.casFlipProg, 'u_amount', this.opts.casStrength)
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
    }

    gl.bindVertexArray(null)
  }

  private rebuild(): void {
    const gl = this.gl!; this.stop()
    const vs = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SRC)!

    for (const p of this.passProgs) if (p) gl.deleteProgram(p); this.passProgs = []
    for (const [, a] of this.fboReg) for (const f of a) { gl.deleteTexture(f.tex); gl.deleteFramebuffer(f.fbo) }
    this.fboReg.clear()
    for (const f of [this.lowResFBO, this.detailFBO, this.composeFBO])
      deleteFBO(gl, f)
    this.onSeeked()
    this.ulocs.clear()
    this.lowResFBO = null; this.detailFBO = null; this.composeFBO = null

    for (let i = 0; i < this.passDefs.length; i++) {
      const pass = this.passDefs[i]
      if (pass.isAggregation) { this.passProgs.push(null!); continue }
      const fs = compileShader(gl, gl.FRAGMENT_SHADER, buildPassShader(pass, this.inputW, this.inputH))!
      const p = linkProgram(gl, vs, fs)!; gl.deleteShader(fs); this.passProgs.push(p)
    }
    if (this.aggProg) { gl.deleteProgram(this.aggProg); this.aggProg = null }
    const aggFs = compileShader(gl, gl.FRAGMENT_SHADER, buildAggShader(this.inputW, this.inputH))!
    this.aggProg = linkProgram(gl, vs, aggFs); gl.deleteShader(aggFs)

    if (this.detailComposeProg) gl.deleteProgram(this.detailComposeProg)
    const composeFs = compileShader(gl, gl.FRAGMENT_SHADER, DETAIL_COMPOSE_FRAG)!
    this.detailComposeProg = linkProgram(gl, vs, composeFs); gl.deleteShader(composeFs)

    gl.deleteShader(vs)
    this.allocFBOs(); this.start()
  }
}

export function checkFilmSupport(): { supported: boolean; message: string } {
  try {
    const c = document.createElement('canvas'); const gl = c.getContext('webgl2')
    if (!gl) return { supported: false, message: 'WebGL2 不可用' }
    loseGlContext(gl) // 探测 canvas 同样占上下文名额，用完就还
    return { supported: true, message: 'FSRCNNX 2x + CAS available' }
  } catch { return { supported: false, message: 'WebGL2 检测失败' } }
}

export const FILM_PRESET: Required<FilmUpscalerOptions> = { casStrength: 0.8 }
