/**
 * 影视增强器 — FSRCNNX CNN 超分 + 全链路增强（WebGL2 实时）
 *
 * 管线：Deinterlace → Denoise(H+V) → FSRCNNX 2x → Temporal → Reinhard HDR → CAS
 * 性能自适应：根据 GPU 帧率动态降级
 */
import fsrcnnxSource from './FSRCNNX_x2_8-0-4-1.glsl?raw'

export interface FilmUpscalerOptions {
  sharpness?: number; casStrength?: number
  deinterlace?: boolean; denoise?: number
  temporalBlend?: number; hdrToneMap?: number
  autoQuality?: boolean
}
export interface FilmStats {
  fps: number; gpuEnabled: boolean
  qualityScale: number; enhancements: string[]
}

interface PassDef {
  desc: string; body: string; bindNames: string[]; saveName: string
  components: number; isAggregation: boolean; lumaBind: boolean
}

// ==================== 着色器 ====================

const VERTEX_SRC = `#version 300 es
in vec2 a_pos;
out vec2 v_uv;
void main() { gl_Position = vec4(a_pos, 0.0, 1.0); v_uv = a_pos * 0.5 + 0.5; }`

// canvas 输出专用：翻转 Y 以匹配 DOM 坐标系
const VERTEX_FLIP_SRC = `#version 300 es
in vec2 a_pos;
out vec2 v_uv;
void main() { gl_Position = vec4(a_pos.x, -a_pos.y, 0.0, 1.0); v_uv = a_pos * 0.5 + 0.5; }`

const DEINTERLACE_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input; uniform vec2 u_texelSize; uniform float u_strength;
void main() {
  vec3 c = texture(u_input, v_uv).rgb;
  vec3 p = texture(u_input, v_uv + vec2(0.0, -u_texelSize.y)).rgb;
  float m = smoothstep(0.01, 0.08, abs(dot(c,vec3(0.333)) - dot(p,vec3(0.333))));
  fragColor = vec4(mix(mix(c, p, 0.5), c, m * u_strength), 1.0);
}`

const DENOISE_H_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input; uniform vec2 u_texelSize; uniform float u_strength;
void main() {
  vec3 c = texture(u_input, v_uv).rgb; float cl = dot(c, vec3(0.299,0.587,0.114));
  vec3 s = c; float tw = 1.0; float sigma = 0.1 * (1.0 + u_strength * 5.0);
  for (int i = 1; i <= 4; i++) {
    float off = float(i) * u_texelSize.x;
    vec3 r = texture(u_input, v_uv + vec2(off, 0.0)).rgb;
    vec3 l = texture(u_input, v_uv - vec2(off, 0.0)).rgb;
    float wr = exp(-abs(dot(r,vec3(0.299,0.587,0.114)) - cl) / sigma);
    float wl = exp(-abs(dot(l,vec3(0.299,0.587,0.114)) - cl) / sigma);
    s += r * wr + l * wl; tw += wr + wl;
  }
  fragColor = vec4(s / tw, 1.0);
}`

const DENOISE_V_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input; uniform vec2 u_texelSize; uniform float u_strength;
void main() {
  vec3 c = texture(u_input, v_uv).rgb; float cl = dot(c, vec3(0.299,0.587,0.114));
  vec3 s = c; float tw = 1.0; float sigma = 0.1 * (1.0 + u_strength * 5.0);
  for (int i = 1; i <= 4; i++) {
    float off = float(i) * u_texelSize.y;
    vec3 u = texture(u_input, v_uv + vec2(0.0, off)).rgb;
    vec3 d = texture(u_input, v_uv - vec2(0.0, off)).rgb;
    float wu = exp(-abs(dot(u,vec3(0.299,0.587,0.114)) - cl) / sigma);
    float wd = exp(-abs(dot(d,vec3(0.299,0.587,0.114)) - cl) / sigma);
    s += u * wu + d * wd; tw += wu + wd;
  }
  fragColor = vec4(s / tw, 1.0);
}`

const TEMPORAL_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input; uniform sampler2D u_prev; uniform float u_strength;
void main() {
  vec3 c = texture(u_input, v_uv).rgb;
  vec3 p = texture(u_prev, v_uv).rgb;
  float diff = abs(dot(c,vec3(0.333)) - dot(p,vec3(0.333)));
  // 静止时(diff小) w接近1.0，多取上一帧；运动时(diff大) w接近0.1
  float w = mix(0.9, 0.1, smoothstep(0.02, 0.15, diff));
  float alpha = clamp(u_strength * w, 0.0, 0.9); // 限制最大混合率
  fragColor = vec4(mix(c, p, alpha), 1.0);
}`

// Reinhard 色调映射
const HDR_TONE_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input; uniform float u_strength;
float luma(vec3 c) { return dot(c, vec3(0.2126, 0.7152, 0.0722)); }
void main() {
  vec3 color = texture(u_input, v_uv).rgb;
  float L = luma(color);
  vec3 mapped = color / (1.0 + L);
  fragColor = vec4(mix(color, mapped, u_strength), 1.0);
}`

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

// bilinear upsample（qualityScale < 2 时替代 FSRCNNX）
const BILINEAR_UP_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input;
void main() {
  fragColor = texture(u_input, v_uv);
}`

// 轻量锐化——Unsharp Mask（qualityScale 降级时补偿 bilinear 模糊）
const LIGHT_SHARPEN_FRAG = `#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
uniform sampler2D u_input; uniform vec2 u_texelSize; uniform float u_amount;
void main() {
  vec2 ts = u_texelSize;
  vec3 e = texture(u_input, v_uv).rgb;
  vec3 a = texture(u_input, v_uv + vec2(0,-ts.y)).rgb;
  vec3 b = texture(u_input, v_uv + vec2(-ts.x,0)).rgb;
  vec3 c = texture(u_input, v_uv + vec2(ts.x,0)).rgb;
  vec3 d = texture(u_input, v_uv + vec2(0,ts.y)).rgb;
  
  // 计算周边平均
  vec3 blur = (a + b + c + d) * 0.25;
  // 提取高频并放大
  vec3 highFreq = e - blur;
  
  fragColor = vec4(e + highFreq * u_amount * 4.0, 1.0);
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

function parseMpvShader(src: string): PassDef[] {
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

function buildPassShader(pass: PassDef, iw: number, ih: number): string {
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

function compileShader(gl: WebGL2RenderingContext, type: number, src: string): WebGLShader | null {
  const s = gl.createShader(type); if (!s) return null
  gl.shaderSource(s, src); gl.compileShader(s)
  if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
    console.error('[FilmUpscaler] compile:', gl.getShaderInfoLog(s)?.slice(0, 200))
    gl.deleteShader(s); return null
  }
  return s
}

function linkProgram(gl: WebGL2RenderingContext, vs: WebGLShader, fs: WebGLShader): WebGLProgram | null {
  const p = gl.createProgram(); if (!p) return null
  gl.attachShader(p, vs); gl.attachShader(p, fs); gl.linkProgram(p)
  if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
    console.error('[FilmUpscaler] link:', gl.getProgramInfoLog(p))
    gl.deleteProgram(p); return null
  }
  return p
}

interface FBOEntry { tex: WebGLTexture; fbo: WebGLFramebuffer; w: number; h: number }

function createFBO(gl: WebGL2RenderingContext, w: number, h: number, filter?: number): FBOEntry {
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

function deleteFBO(gl: WebGL2RenderingContext, f: FBOEntry | null): void {
  if (f) { gl.deleteTexture(f.tex); gl.deleteFramebuffer(f.fbo) }
}

// ==================== FilmUpscaler ====================

export class FilmUpscaler {
  private opts: Required<FilmUpscalerOptions>
  private canvas: HTMLCanvasElement | null = null
  private gl: WebGL2RenderingContext | null = null
  private video: HTMLVideoElement | null = null
  private running = false; private rafId = 0
  private _error: string | null = null

  private quadVAO: WebGLVertexArrayObject | null = null
  private quadVBO: WebGLBuffer | null = null
  private videoTex: WebGLTexture | null = null

  private fboReg: Map<string, FBOEntry[]> = new Map()
  private passProgs: WebGLProgram[] = []
  private aggProg: WebGLProgram | null = null
  private detailComposeProg: WebGLProgram | null = null
  private casProg: WebGLProgram | null = null
  private casFlipProg: WebGLProgram | null = null  // canvas 输出用（Y 翻转）
  private deintProg: WebGLProgram | null = null
  private denHProg: WebGLProgram | null = null
  private denVProg: WebGLProgram | null = null
  private tempProg: WebGLProgram | null = null
  private hdrProg: WebGLProgram | null = null
  private bilinearProg: WebGLProgram | null = null
  private lightSharpenProg: WebGLProgram | null = null

  // 预分配 FBO
  private preFBO: FBOEntry | null = null
  private midFBO: FBOEntry | null = null
  private prevFBO: FBOEntry | null = null
  private casFBO: FBOEntry | null = null
  private tempFBO: FBOEntry | null = null
  private hdrFBO: FBOEntry | null = null
  private lowResFBO: FBOEntry | null = null
  private detailFBO: FBOEntry | null = null
  private composeFBO: FBOEntry | null = null

  // uniform location 缓存（二级 Map，消除冲突）
  private ulocs: Map<WebGLProgram, Map<string, WebGLUniformLocation | null>> = new Map()

  private passDefs: PassDef[] = []
  private frames = 0; private lastFpsTime = 0
  private currentFps = 0; private _qualityScale = 2.0
  private inputW = 0; private inputH = 0
  private lastUploadTime = -1
  private lowFpsCounter = 0; private highFpsCounter = 0

  get error(): string | null { return this._error }

  constructor(opts: FilmUpscalerOptions = {}) {
    this.opts = {
      sharpness: opts.sharpness ?? 0.8, casStrength: opts.casStrength ?? 0.8,
      deinterlace: opts.deinterlace ?? false, denoise: opts.denoise ?? 0.0,
      temporalBlend: opts.temporalBlend ?? 0.0, hdrToneMap: opts.hdrToneMap ?? 0.0,
      autoQuality: opts.autoQuality ?? false,
    }
  }

  async init(video: HTMLVideoElement, wrapper?: HTMLElement): Promise<boolean> {
    try {
      this.video = video
      this.canvas = document.createElement('canvas')
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
      console.log(`[FilmUpscaler] init: ${this.inputW}×${this.inputH} [${this.enhancements().join(',')}]`)
      return true
    } catch (e: any) {
      this._error = e.message || String(e); console.error('[FilmUpscaler] init:', this._error)
      if (this.canvas?.parentElement) this.canvas.parentElement.removeChild(this.canvas)
      return false
    }
  }

  private enhancements(): string[] {
    const e = ['FSRCNNX', 'CAS']
    if (this.opts.deinterlace) e.push('Deint')
    if (this.opts.denoise > 0) e.push('Denoise')
    if (this.opts.temporalBlend > 0) e.push('Temp')
    if (this.opts.hdrToneMap > 0) e.push('HDR')
    if (this.opts.autoQuality) e.push('AutoQ')
    return e
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
    this.deintProg = fsProg(DEINTERLACE_FRAG)
    this.denHProg = fsProg(DENOISE_H_FRAG); this.denVProg = fsProg(DENOISE_V_FRAG)
    this.tempProg = fsProg(TEMPORAL_FRAG); this.hdrProg = fsProg(HDR_TONE_FRAG)
    this.casProg = fsProg(CAS_FRAG)
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
    this.lightSharpenProg = fsProg(LIGHT_SHARPEN_FRAG)
    gl.deleteShader(vs)
  }

  private allocFBOs(): void {
    const gl = this.gl!, w = this.inputW, h = this.inputH
    this.preFBO = createFBO(gl, w, h); this.midFBO = createFBO(gl, w, h)
    this.casFBO = createFBO(gl, w * 2, h * 2)
    this.tempFBO = createFBO(gl, w * 2, h * 2)
    this.hdrFBO = createFBO(gl, w * 2, h * 2)
    this.allocPrevFBO()
    const outW = Math.round(w * this._qualityScale), outH = Math.round(h * this._qualityScale)
    this.lowResFBO = createFBO(gl, outW, outH)
    this.detailFBO = createFBO(gl, outW, outH)
    this.composeFBO = createFBO(gl, outW, outH)

    for (let i = 0; i < this.passDefs.length; i++) {
      const p = this.passDefs[i]
      if (!p.isAggregation && p.saveName) this.getFBO(`${p.saveName}_${i}`, w, h)
    }
  }

  private allocPrevFBO(): void {
    const gl = this.gl!
    const outW = Math.round(this.inputW * this._qualityScale)
    const outH = Math.round(this.inputH * this._qualityScale)
    if (this.prevFBO) deleteFBO(gl, this.prevFBO)
    this.prevFBO = createFBO(gl, outW, outH)
  }

  private allocQualityDependentFBOs(): void {
    const gl = this.gl!; if (!gl) return
    const outW = Math.round(this.inputW * this._qualityScale)
    const outH = Math.round(this.inputH * this._qualityScale)
    if (this.lowResFBO) deleteFBO(gl, this.lowResFBO)
    if (this.detailFBO) deleteFBO(gl, this.detailFBO)
    if (this.composeFBO) deleteFBO(gl, this.composeFBO)
    this.lowResFBO = createFBO(gl, outW, outH)
    this.detailFBO = createFBO(gl, outW, outH)
    this.composeFBO = createFBO(gl, outW, outH)
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
  stop(): void { this.running = false; if (this.rafId) { cancelAnimationFrame(this.rafId); this.rafId = 0 } }

  onSeeked(): void {
    if (this.prevFBO && this.gl) {
      this.gl.deleteTexture(this.prevFBO.tex)
      this.gl.deleteFramebuffer(this.prevFBO.fbo)
    }
    this.prevFBO = null
    this.lastUploadTime = -1
  }

  destroy(): void {
    this.stop()
    const gl = this.gl
    if (gl) {
      for (const [, a] of this.fboReg) for (const f of a) { gl.deleteTexture(f.tex); gl.deleteFramebuffer(f.fbo) }
      this.fboReg.clear()
      for (const f of [this.preFBO, this.midFBO, this.casFBO, this.tempFBO, this.hdrFBO,
                        this.lowResFBO, this.detailFBO, this.composeFBO])
        deleteFBO(gl, f)
      this.onSeeked()
      for (const p of this.passProgs) if (p) gl.deleteProgram(p)
      for (const p of [this.aggProg, this.detailComposeProg, this.casProg, this.casFlipProg, this.deintProg, this.denHProg, this.denVProg,
                       this.tempProg, this.hdrProg, this.bilinearProg, this.lightSharpenProg])
        if (p) gl.deleteProgram(p)
      if (this.videoTex) gl.deleteTexture(this.videoTex)
      if (this.quadVBO) gl.deleteBuffer(this.quadVBO)
      if (this.quadVAO) gl.deleteVertexArray(this.quadVAO)
      this.ulocs.clear()
    }
    this.passProgs = []; this.passDefs = []; this.fboReg.clear()
    this.preFBO = null; this.midFBO = null; this.casFBO = null; this.tempFBO = null; this.hdrFBO = null
    this.lowResFBO = null; this.detailFBO = null; this.composeFBO = null
    this.aggProg = null; this.detailComposeProg = null; this.casProg = null; this.casFlipProg = null; this.deintProg = null
    this.denHProg = null; this.denVProg = null; this.tempProg = null; this.hdrProg = null
    this.bilinearProg = null; this.lightSharpenProg = null
    this.quadVAO = null; this.quadVBO = null; this.videoTex = null
    if (this.canvas?.parentElement) this.canvas.parentElement.removeChild(this.canvas)
    this.canvas = null; this.gl = null; this.video = null
  }

  updateOptions(opts: Partial<FilmUpscalerOptions>): void {
    const ks: (keyof FilmUpscalerOptions)[] = ['sharpness','casStrength','deinterlace','denoise','temporalBlend','hdrToneMap','autoQuality']
    for (const k of ks) if (opts[k] !== undefined) (this.opts as any)[k] = opts[k]
  }

  getStats(): FilmStats {
    return { fps: this.currentFps, gpuEnabled: true, qualityScale: this._qualityScale, enhancements: this.enhancements() }
  }

  private renderLoop = (): void => {
    if (!this.running) return
    this.rafId = requestAnimationFrame(this.renderLoop); this.render(); this.frames++
    const now = performance.now()
    if (now - this.lastFpsTime >= 2000) {
      this.currentFps = Math.round(this.frames / ((now - this.lastFpsTime) / 1000))
      this.frames = 0; this.lastFpsTime = now
      if (this.opts.autoQuality) this.adaptQuality()
    }
  }

  private adaptQuality(): void {
    const prevScale = this._qualityScale
    if (this.currentFps < 24) { this.lowFpsCounter++; this.highFpsCounter = 0 }
    else if (this.currentFps > 35) { this.highFpsCounter++; this.lowFpsCounter = 0 }
    else { this.lowFpsCounter = 0; this.highFpsCounter = 0 }

    if (this.lowFpsCounter >= 3 && this._qualityScale > 1.0) {
      this._qualityScale = Math.max(1.0, this._qualityScale - 0.5)
      this.lowFpsCounter = 0
    } else if (this.highFpsCounter >= 3 && this._qualityScale < 2.0) {
      this._qualityScale = Math.min(2.0, this._qualityScale + 0.5)
      this.highFpsCounter = 0
    }
    if (this._qualityScale !== prevScale) {
      console.log(`[FilmUpscaler] ⚡ quality: ${prevScale}x → ${this._qualityScale}x`)
      this.allocPrevFBO()
      this.allocQualityDependentFBOs()
    }
  }

  private syncCanvasToVideo(): void {
    const video = this.video!, canvas = this.canvas!
    if (!video.videoWidth || !video.videoHeight) return
    const vr = video.getBoundingClientRect()
    const cw = vr.width, ch = vr.height
    // 计算 object-fit: contain 的实际渲染区域
    const vw = video.videoWidth, vh = video.videoHeight
    const videoRatio = vw / vh
    const containerRatio = cw / ch
    let dw: number, dh: number, dx: number, dy: number
    if (videoRatio > containerRatio) {
      dw = cw; dh = cw / videoRatio; dx = 0; dy = (ch - dh) / 2
    } else {
      dh = ch; dw = ch * videoRatio; dx = (cw - dw) / 2; dy = 0
    }
    canvas.style.left = (vr.left + dx) + 'px'
    canvas.style.top = (vr.top + dy) + 'px'
    canvas.style.width = dw + 'px'
    canvas.style.height = dh + 'px'
  }

  private render(): void {
    const gl = this.gl!, video = this.video!
    if (video.readyState < 2 || !video.videoWidth) return

    if (video.videoWidth !== this.inputW || video.videoHeight !== this.inputH) {
      this.inputW = video.videoWidth; this.inputH = video.videoHeight
      this.canvas!.width = this.inputW * 2; this.canvas!.height = this.inputH * 2
      this.rebuild(); return
    }

    const w = this.inputW, h = this.inputH
    const outW = Math.round(w * this._qualityScale), outH = Math.round(h * this._qualityScale)

    gl.bindVertexArray(this.quadVAO)

    if (video.currentTime !== this.lastUploadTime) {
      gl.activeTexture(gl.TEXTURE0)
      gl.bindTexture(gl.TEXTURE_2D, this.videoTex)
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, video)
      this.lastUploadTime = video.currentTime
    }

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

    let src = this.videoTex!; let srcFBO = false

    // Step 0: Deinterlace
    if (this.opts.deinterlace && this.deintProg) {
      pass(this.preFBO!, this.deintProg, w, h, () => {
        bind(this.deintProg!, src, 'u_input', 0)
        set2f(this.deintProg!, 'u_texelSize', 1/w, 1/h)
        set1f(this.deintProg!, 'u_strength', 1.0)
      })
      src = this.preFBO!.tex; srcFBO = true
    }

    // Step 1: Denoise H+V
    if (this.opts.denoise > 0 && this.denHProg && this.denVProg) {
      pass(this.midFBO!, this.denHProg, w, h, () => {
        bind(this.denHProg!, src, 'u_input', 0)
        set2f(this.denHProg!, 'u_texelSize', 1/w, 1/h)
        set1f(this.denHProg!, 'u_strength', this.opts.denoise)
      })
      pass(this.preFBO!, this.denVProg, w, h, () => {
        bind(this.denVProg!, this.midFBO!.tex, 'u_input', 0)
        set2f(this.denVProg!, 'u_texelSize', 1/w, 1/h)
        set1f(this.denVProg!, 'u_strength', this.opts.denoise)
      })
      src = this.preFBO!.tex; srcFBO = true
    }

    // Step 2: FSRCNNX (or bilinear upsample when qualityScale < 2)
    let aggOut: FBOEntry | null = null
    if (this._qualityScale >= 2.0) {
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
    } else {
      pass(this.lowResFBO!, this.bilinearProg!, outW, outH, () => {
        bind(this.bilinearProg!, src, 'u_input', 0)
      })
      pass(this.composeFBO!, this.lightSharpenProg!, outW, outH, () => {
        bind(this.lightSharpenProg!, this.lowResFBO!.tex, 'u_input', 0)
        set2f(this.lightSharpenProg!, 'u_texelSize', 1/outW, 1/outH)
        set1f(this.lightSharpenProg!, 'u_amount', this.opts.sharpness)
      })
      aggOut = this.composeFBO!
    }

    // Step 3: Temporal
    if (this.opts.temporalBlend > 0 && this.tempProg && aggOut) {
      const ao = aggOut // TS narrowing
      pass(this.tempFBO!, this.tempProg, outW, outH, () => {
        bind(this.tempProg!, ao.tex, 'u_input', 0)
        bind(this.tempProg!, this.prevFBO?.tex ?? ao.tex, 'u_prev', 1)
        set1f(this.tempProg!, 'u_strength', this.opts.temporalBlend)
      })
      if (!this.prevFBO) this.allocPrevFBO()
      if (this.prevFBO) {
        gl.bindFramebuffer(gl.READ_FRAMEBUFFER, aggOut.fbo)
        gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, this.prevFBO.fbo)
        gl.blitFramebuffer(0, 0, outW, outH, 0, 0, outW, outH, gl.COLOR_BUFFER_BIT, gl.LINEAR)
        gl.bindFramebuffer(gl.READ_FRAMEBUFFER, null); gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, null)
      }
    }

    // Step 4: HDR
    const afterTemporal = (this.opts.temporalBlend > 0 && this.tempProg) ? this.tempFBO! : aggOut!
    if (this.opts.hdrToneMap > 0 && this.hdrProg && afterTemporal) {
      pass(this.hdrFBO!, this.hdrProg, outW, outH, () => {
        bind(this.hdrProg!, afterTemporal.tex, 'u_input', 0)
        set1f(this.hdrProg!, 'u_strength', this.opts.hdrToneMap)
      })
    }

    // Step 5: CAS → Canvas（Y 翻转 VS 匹配 DOM 坐标系）
    // 同步 canvas 布局到视频的 letterbox 区域
    this.syncCanvasToVideo()
    const finalSrc = (this.opts.hdrToneMap > 0 && this.hdrProg) ? this.hdrFBO! : afterTemporal
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
    for (const f of [this.preFBO, this.midFBO, this.casFBO, this.tempFBO, this.hdrFBO,
                      this.lowResFBO, this.detailFBO, this.composeFBO])
      deleteFBO(gl, f)
    this.onSeeked()
    this.ulocs.clear()
    this.preFBO = null; this.midFBO = null; this.casFBO = null; this.tempFBO = null; this.hdrFBO = null
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
    return { supported: true, message: 'FSRCNNX + 全链路增强可用' }
  } catch { return { supported: false, message: 'WebGL2 检测失败' } }
}

export const FILM_PRESET: Required<FilmUpscalerOptions> = {
  sharpness: 0.8, casStrength: 0.8,
  deinterlace: false, denoise: 0.0,
  temporalBlend: 0.0, hdrToneMap: 0.0,
  autoQuality: false,
}