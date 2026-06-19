/**
 * 视频 AI 增强器 — WebGL2 双 Pass 实时画质增强
 *
 * 支持两种增强模式：
 *   - 动画增强 (anime) — 针对动画/动漫优化：去色带、线条增强、平坦区域降噪
 *   - 影视增强 (film)  — 针对真人影视优化：纹理锐化、暗部细节提升、压缩噪声抑制
 *
 * 双 Pass 管线：
 *   Pass1（输入分辨率 → FBO）：保边降噪 + 智能去色带
 *   Pass2（FBO → Canvas）：受限锐化 + 安全边缘增强 + 上采样 + 时序一致性
 *
 * 所有计算均在 GPU 上完成（WebGL2），不影响主线程。
 * 无需下载模型、不依赖 WebGPU、WebView2 完美兼容。
 */

// ==================== 类型定义 ====================

export type EnhanceMode = 'anime' | 'film'

export interface UpscalerOptions {
  /** 增强模式 */
  mode?: EnhanceMode
  /** 输出宽度（0 = 自动跟随视频宽） */
  outputWidth?: number
  /** 输出高度（0 = 自动跟随视频高） */
  outputHeight?: number
  /** 锐化强度 0.0~2.0 */
  sharpness?: number
  /** 对比度增强 0.0~1.0 */
  contrast?: number
  /** 去色带强度 0.0~1.0 */
  deband?: number
  /** 边缘增强 0.0~1.0 */
  edgeEnhance?: number
  /** 上采样倍率 1.0~2.0 */
  upscale?: number
  /** 降噪强度 0.0~1.0 */
  denoise?: number
  /** 时序混合 0.0~1.0（0=关闭） */
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

type PresetFields = Required<Omit<UpscalerOptions, 'outputWidth' | 'outputHeight'>>

/** 动画增强预设 — 大色块去色带、线条增强、轻降噪 */
export const ANIME_PRESET: PresetFields = {
  mode: 'anime',
  sharpness: 0.45,
  contrast: 0.15,
  deband: 0.65,
  edgeEnhance: 0.6,
  upscale: 1.0,
  denoise: 0.25,
  temporalBlend: 0.3,
}

/** 影视增强预设 — 纹理锐化、暗部细节、强降噪 */
export const FILM_PRESET: PresetFields = {
  mode: 'film',
  sharpness: 0.6,
  contrast: 0.35,
  deband: 0.25,
  edgeEnhance: 0.3,
  upscale: 1.0,
  denoise: 0.5,
  temporalBlend: 0.15,
}

// ==================== WebGL 着色器 ====================

/** 顶点着色器 — 全屏四边形（两个 Pass 共用） */
const VERTEX_SHADER = /* glsl */ `#version 300 es
in vec2 a_position;
in vec2 a_texCoord;
out vec2 v_texCoord;
uniform float u_flipY;
void main() {
  gl_Position = vec4(a_position, 0.0, 1.0);
  v_texCoord = vec2(a_texCoord.x, mix(a_texCoord.y, 1.0 - a_texCoord.y, u_flipY));
}`

/**
 * Pass1 片段着色器 — 预处理（降噪 + 去色带）
 * 在输入分辨率空间执行，结果写入 FBO
 */
const PASS1_FRAGMENT_SHADER = /* glsl */ `#version 300 es
precision highp float;

in vec2 v_texCoord;
out vec4 fragColor;

uniform sampler2D u_source;
uniform vec2 u_texelSize;
uniform float u_denoise;
uniform float u_deband;
uniform float u_mode;          // 0.0=anime, 1.0=film
uniform float u_frameTime;

float luminance(vec3 c) {
  return dot(c, vec3(0.2126, 0.7152, 0.0722));
}

// 保边降噪 (Bilateral-like 3x3)
vec3 edgePreservingDenoise(sampler2D tex, vec2 uv, vec2 ts, float strength, float mode) {
  if (strength <= 0.0) return texture(tex, uv).rgb;

  vec3 center = texture(tex, uv).rgb;
  float centerLum = luminance(center);

  float sigma = mix(0.03, 0.12, strength);
  sigma *= mix(1.0, 1.5, mode);
  float inv2Sigma2 = 1.0 / (2.0 * sigma * sigma);

  vec3 sum = vec3(0.0);
  float wSum = 0.0;

  for (int x = -1; x <= 1; x++) {
    for (int y = -1; y <= 1; y++) {
      vec2 offset = vec2(float(x), float(y)) * ts;
      vec3 s = texture(tex, uv + offset).rgb;
      float lumDiff = luminance(s) - centerLum;
      float w = exp(-(lumDiff * lumDiff) * inv2Sigma2);
      sum += s * w;
      wSum += w;
    }
  }

  vec3 denoised = sum / wSum;

  // 动画模式: 在平坦区域做额外均值滤波，消除色块基底
  if (mode < 0.5) {
    float localVar = 0.0;
    for (int x = -1; x <= 1; x++) {
      for (int y = -1; y <= 1; y++) {
        float ld = luminance(texture(tex, uv + vec2(float(x), float(y)) * ts).rgb) - centerLum;
        localVar += ld * ld;
      }
    }
    localVar /= 9.0;
    float flatMask = 1.0 - smoothstep(0.0005, 0.003, localVar);
    vec3 avg = vec3(0.0);
    for (int x = -1; x <= 1; x++) {
      for (int y = -1; y <= 1; y++) {
        avg += texture(tex, uv + vec2(float(x), float(y)) * ts).rgb;
      }
    }
    avg /= 9.0;
    denoised = mix(denoised, avg, flatMask * 0.6);
  }

  return denoised;
}

// 智能去色带 — 仅在平坦区域施加抖动
vec3 smartDeband(vec3 color, sampler2D tex, vec2 uv, vec2 ts, float strength, float mode, float time) {
  if (strength <= 0.0) return color;

  float flatThreshold = mix(0.012, 0.03, mode);
  float ditherRange = mix(2.5, 1.0, mode) / 255.0;

  float lum = luminance(color);
  float variance = 0.0;
  for (int x = -1; x <= 1; x++) {
    for (int y = -1; y <= 1; y++) {
      float nl = luminance(texture(tex, uv + vec2(float(x), float(y)) * ts).rgb);
      float d = nl - lum;
      variance += d * d;
    }
  }
  variance /= 9.0;
  float flatMask = 1.0 - smoothstep(flatThreshold * 0.5, flatThreshold, variance);

  float noise = fract(sin(dot(uv * 1000.0 + time * 0.01, vec2(12.9898, 78.233))) * 43758.5453);
  float dither = (noise - 0.5) * ditherRange * strength;

  return clamp(color + dither * flatMask, 0.0, 1.0);
}

void main() {
  vec2 ts = u_texelSize;
  vec2 uv = v_texCoord;
  vec3 color = edgePreservingDenoise(u_source, uv, ts, u_denoise, u_mode);
  color = smartDeband(color, u_source, uv, ts, u_deband, u_mode, u_frameTime);
  fragColor = vec4(color, 1.0);
}`

/**
 * Pass2 片段着色器 — 增强+输出
 * 从已预处理的 FBO 纹理读取，完成锐化、边缘增强、上采样、时序混合
 */
const PASS2_FRAGMENT_SHADER = /* glsl */ `#version 300 es
precision highp float;

in vec2 v_texCoord;
out vec4 fragColor;

uniform sampler2D u_preprocessed;
uniform sampler2D u_prevFrame;
uniform vec2 u_texelSize;
uniform float u_sharpness;
uniform float u_contrast;
uniform float u_edgeEnhance;
uniform float u_mode;
uniform float u_temporalBlend;
uniform vec2 u_outputSize;
uniform vec2 u_inputSize;

float luminance(vec3 c) {
  return dot(c, vec3(0.2126, 0.7152, 0.0722));
}

// 受限 USM 锐化 — clamp diff 防止过冲
vec3 clampedUSM(sampler2D tex, vec2 uv, vec2 ts, float strength, float mode, vec3 inColor) {
  if (strength <= 0.0) return inColor;
  vec3 blur = vec3(0.0);
  blur += texture(tex, uv + vec2(-ts.x, -ts.y)).rgb;
  blur += texture(tex, uv + vec2(0.0, -ts.y)).rgb * 2.0;
  blur += texture(tex, uv + vec2(ts.x, -ts.y)).rgb;
  blur += texture(tex, uv + vec2(-ts.x, 0.0)).rgb * 2.0;
  blur += inColor * 4.0;
  blur += texture(tex, uv + vec2(ts.x, 0.0)).rgb * 2.0;
  blur += texture(tex, uv + vec2(-ts.x, ts.y)).rgb;
  blur += texture(tex, uv + vec2(0.0, ts.y)).rgb * 2.0;
  blur += texture(tex, uv + vec2(ts.x, ts.y)).rgb;
  blur /= 16.0;
  vec3 diff = inColor - blur;
  float clampRange = mix(0.1, 0.2, mode);
  diff = clamp(diff, -clampRange, clampRange);
  return clamp(inColor + diff * strength, 0.0, 1.0);
}

// 局部对比度增强
vec3 localContrast(sampler2D tex, vec2 uv, vec2 ts, float strength, float mode, vec3 inColor) {
  if (strength <= 0.0) return inColor;
  vec3 local = vec3(0.0);
  local += texture(tex, uv + vec2(-ts.x, -ts.y)).rgb;
  local += texture(tex, uv + vec2(0.0, -ts.y)).rgb * 2.0;
  local += texture(tex, uv + vec2(ts.x, -ts.y)).rgb;
  local += texture(tex, uv + vec2(-ts.x, 0.0)).rgb * 2.0;
  local += inColor * 4.0;
  local += texture(tex, uv + vec2(ts.x, 0.0)).rgb * 2.0;
  local += texture(tex, uv + vec2(-ts.x, ts.y)).rgb;
  local += texture(tex, uv + vec2(0.0, ts.y)).rgb * 2.0;
  local += texture(tex, uv + vec2(ts.x, ts.y)).rgb;
  local /= 16.0;
  float multiplier = mix(2.0, 3.0, mode);
  vec3 diff = inColor - local;
  float maxDiff = mix(0.12, 0.18, mode);
  diff = clamp(diff, -maxDiff, maxDiff);
  return clamp(inColor + diff * strength * multiplier, 0.0, 1.0);
}

// 安全边缘增强 — luminance-only，不改变色相
vec3 safeEdgeEnhance(sampler2D tex, vec2 uv, vec2 ts, float strength, float mode, vec3 inColor) {
  if (strength <= 0.0) return inColor;
  float lc = luminance(inColor);
  float tl = luminance(texture(tex, uv + vec2(-ts.x, ts.y)).rgb);
  float t  = luminance(texture(tex, uv + vec2(0.0, ts.y)).rgb);
  float tr = luminance(texture(tex, uv + vec2(ts.x, ts.y)).rgb);
  float l  = luminance(texture(tex, uv + vec2(-ts.x, 0.0)).rgb);
  float r  = luminance(texture(tex, uv + vec2(ts.x, 0.0)).rgb);
  float bl = luminance(texture(tex, uv + vec2(-ts.x, -ts.y)).rgb);
  float b  = luminance(texture(tex, uv + vec2(0.0, -ts.y)).rgb);
  float br = luminance(texture(tex, uv + vec2(ts.x, -ts.y)).rgb);
  float gx = -tl + tr - 2.0*l + 2.0*r - bl + br;
  float gy = tl + 2.0*t + tr - bl - 2.0*b - br;
  float edge = sqrt(gx*gx + gy*gy);
  float edgeSensitivity = mix(0.04, 0.1, mode);
  float maxEnhance = mix(0.5, 0.25, mode);
  float edgeMask = smoothstep(edgeSensitivity, edgeSensitivity + 0.2, edge);
  float enhanceAmt = edgeMask * strength * maxEnhance;
  float newLum = lc + enhanceAmt * 0.3;
  newLum = min(newLum, lc + 0.35);
  float ratio = (lc > 0.001) ? newLum / lc : 1.0;
  return clamp(inColor * ratio, 0.0, 1.0);
}

// 上采样 — 修复版 (sampleRadius = ts / scale)
vec3 upscaleFilter(sampler2D tex, vec2 uv, vec2 ts, vec2 inputSize, vec2 outputSize,
                   float mode, vec3 enhancedColor) {
  float scaleX = outputSize.x / inputSize.x;
  float scaleY = outputSize.y / inputSize.y;
  if (scaleX <= 1.01 && scaleY <= 1.01) return enhancedColor;
  vec2 sampleRadius = ts / max(scaleX, scaleY);
  if (mode < 0.5) {
    // 动画: NNEA 边缘导向插值
    float dH = abs(luminance(texture(tex, uv + vec2(-sampleRadius.x, 0.0)).rgb)
                 - luminance(texture(tex, uv + vec2(sampleRadius.x, 0.0)).rgb));
    float dV = abs(luminance(texture(tex, uv + vec2(0.0, -sampleRadius.y)).rgb)
                 - luminance(texture(tex, uv + vec2(0.0, sampleRadius.y)).rgb));
    float dD1 = abs(luminance(texture(tex, uv + vec2(-sampleRadius.x, sampleRadius.y)).rgb)
                  - luminance(texture(tex, uv + vec2(sampleRadius.x, -sampleRadius.y)).rgb));
    float dD2 = abs(luminance(texture(tex, uv + vec2(sampleRadius.x, sampleRadius.y)).rgb)
                  - luminance(texture(tex, uv + vec2(-sampleRadius.x, -sampleRadius.y)).rgb));
    float minGrad = min(min(dH, dV), min(dD1, dD2));
    vec3 result = enhancedColor;
    if (minGrad == dH) {
      result = (texture(tex, uv + vec2(-sampleRadius.x, 0.0)).rgb
              + texture(tex, uv + vec2(sampleRadius.x, 0.0)).rgb) * 0.5;
    } else if (minGrad == dV) {
      result = (texture(tex, uv + vec2(0.0, -sampleRadius.y)).rgb
              + texture(tex, uv + vec2(0.0, sampleRadius.y)).rgb) * 0.5;
    } else if (minGrad == dD1) {
      result = (texture(tex, uv + vec2(-sampleRadius.x, sampleRadius.y)).rgb
              + texture(tex, uv + vec2(sampleRadius.x, -sampleRadius.y)).rgb) * 0.5;
    } else {
      result = (texture(tex, uv + vec2(sampleRadius.x, sampleRadius.y)).rgb
              + texture(tex, uv + vec2(-sampleRadius.x, -sampleRadius.y)).rgb) * 0.5;
    }
    return mix(enhancedColor, result, 0.5);
  } else {
    // 影视: Lanczos-2 近似
    vec3 sum = vec3(0.0);
    float wSum = 0.0;
    for (int i = -1; i <= 1; i++) {
      for (int j = -1; j <= 1; j++) {
        vec2 samplePos = uv + vec2(float(i), float(j)) * sampleRadius;
        vec3 s;
        if (i == 0 && j == 0) { s = enhancedColor; } else { s = texture(tex, samplePos).rgb; }
        float dist = length(vec2(float(i), float(j)));
        float w = dist < 1.0
          ? 1.0 - 2.0*dist*dist + dist*dist*dist
          : 4.0 - 8.0*dist + 5.0*dist*dist - dist*dist*dist;
        w = max(w, 0.0);
        sum += s * w;
        wSum += w;
      }
    }
    return wSum > 0.0 ? sum / wSum : enhancedColor;
  }
}

// 时序一致性 — 运动自适应帧混合
vec3 temporalConsistency(vec3 current, vec2 uv, sampler2D prevTex,
                         float blendStrength, float mode) {
  if (blendStrength <= 0.0) return current;
  vec3 prev = texture(prevTex, uv).rgb;
  float prevLum = luminance(prev);
  if (prevLum < 0.001) return current;
  float curLum = luminance(current);
  float diff = abs(curLum - prevLum);
  float motionMask = smoothstep(0.02, 0.12, diff);
  float weight = blendStrength * (1.0 - motionMask);
  weight *= mix(1.0, 0.5, mode);
  return mix(current, prev, weight);
}

void main() {
  vec2 ts = u_texelSize;
  vec2 uv = v_texCoord;
  vec3 color = texture(u_preprocessed, uv).rgb;
  color = clampedUSM(u_preprocessed, uv, ts, u_sharpness, u_mode, color);
  color = localContrast(u_preprocessed, uv, ts, u_contrast, u_mode, color);
  color = safeEdgeEnhance(u_preprocessed, uv, ts, u_edgeEnhance, u_mode, color);
  color = upscaleFilter(u_preprocessed, uv, ts, u_inputSize, u_outputSize, u_mode, color);
  if (u_temporalBlend > 0.0) {
    color = temporalConsistency(color, uv, u_prevFrame, u_temporalBlend, u_mode);
  }
  fragColor = vec4(color, 1.0);
}`

// ==================== WebGL2 引擎 ====================

interface UniformMap {
  [key: string]: WebGLUniformLocation | null
}

export class AiUpscaler {
  private gl: WebGL2RenderingContext | null = null
  private canvas: HTMLCanvasElement | null = null
  private pass1Program: WebGLProgram | null = null
  private pass2Program: WebGLProgram | null = null
  private vao: WebGLVertexArrayObject | null = null
  private sourceTexture: WebGLTexture | null = null
  private fboTexture: WebGLTexture | null = null
  private prevFrameTexture: WebGLTexture | null = null
  private fbo: WebGLFramebuffer | null = null
  private pass1Uniforms: UniformMap = {}
  private pass2Uniforms: UniformMap = {}
  private sourceVideo: HTMLVideoElement | null = null
  private animFrameId = 0
  private running = false
  private lastVideoTime = 0
  private fboWidth = 0
  private fboHeight = 0
  private prevWidth = 0
  private prevHeight = 0
  private canvasShown = false
  private isFirstFrame = true
  private frameCount = 0
  private lastFpsTime = 0
  private fps = 0
  private opts: Required<UpscalerOptions>
  ready = false
  error: string | null = null

  constructor(options: UpscalerOptions = {}) {
    this.opts = {
      mode: options.mode ?? 'anime',
      outputWidth: options.outputWidth ?? 0,
      outputHeight: options.outputHeight ?? 0,
      sharpness: options.sharpness ?? ANIME_PRESET.sharpness,
      contrast: options.contrast ?? ANIME_PRESET.contrast,
      deband: options.deband ?? ANIME_PRESET.deband,
      edgeEnhance: options.edgeEnhance ?? ANIME_PRESET.edgeEnhance,
      upscale: options.upscale ?? 1.0,
      denoise: options.denoise ?? ANIME_PRESET.denoise,
      temporalBlend: options.temporalBlend ?? ANIME_PRESET.temporalBlend,
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
    const scale = this.opts.upscale
    const outW = this.opts.outputWidth > 0 ? this.opts.outputWidth : Math.round(vw * scale)
    const outH = this.opts.outputHeight > 0 ? this.opts.outputHeight : Math.round(vh * scale)
    const maxTex = gl.getParameter(gl.MAX_TEXTURE_SIZE)
    if (outW > maxTex || outH > maxTex) {
      this.error = `输出尺寸 ${outW}x${outH} 超过 GPU 最大纹理限制 ${maxTex}x${maxTex}`
      console.warn('[AiUpscaler]', this.error)
    }

    const vs = this.compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER)
    const fs1 = this.compileShader(gl, gl.FRAGMENT_SHADER, PASS1_FRAGMENT_SHADER)
    const fs2 = this.compileShader(gl, gl.FRAGMENT_SHADER, PASS2_FRAGMENT_SHADER)
    if (!vs || !fs1 || !fs2) {
      this.error = '着色器编译失败'
      if (vs) gl.deleteShader(vs); if (fs1) gl.deleteShader(fs1); if (fs2) gl.deleteShader(fs2)
      return false
    }
    this.pass1Program = this.linkProgram(gl, vs, fs1)
    this.pass2Program = this.linkProgram(gl, vs, fs2)
    gl.deleteShader(vs); gl.deleteShader(fs1); gl.deleteShader(fs2)
    if (!this.pass1Program || !this.pass2Program) { this.error = '着色器链接失败'; return false }

    const p1 = this.pass1Program
    this.pass1Uniforms = {
      u_source: gl.getUniformLocation(p1, 'u_source'),
      u_texelSize: gl.getUniformLocation(p1, 'u_texelSize'),
      u_denoise: gl.getUniformLocation(p1, 'u_denoise'),
      u_deband: gl.getUniformLocation(p1, 'u_deband'),
      u_mode: gl.getUniformLocation(p1, 'u_mode'),
      u_frameTime: gl.getUniformLocation(p1, 'u_frameTime'),
      u_flipY: gl.getUniformLocation(p1, 'u_flipY'),
    }
    const p2 = this.pass2Program
    this.pass2Uniforms = {
      u_preprocessed: gl.getUniformLocation(p2, 'u_preprocessed'),
      u_prevFrame: gl.getUniformLocation(p2, 'u_prevFrame'),
      u_texelSize: gl.getUniformLocation(p2, 'u_texelSize'),
      u_sharpness: gl.getUniformLocation(p2, 'u_sharpness'),
      u_contrast: gl.getUniformLocation(p2, 'u_contrast'),
      u_edgeEnhance: gl.getUniformLocation(p2, 'u_edgeEnhance'),
      u_mode: gl.getUniformLocation(p2, 'u_mode'),
      u_temporalBlend: gl.getUniformLocation(p2, 'u_temporalBlend'),
      u_outputSize: gl.getUniformLocation(p2, 'u_outputSize'),
      u_inputSize: gl.getUniformLocation(p2, 'u_inputSize'),
      u_flipY: gl.getUniformLocation(p2, 'u_flipY'),
    }

    this.vao = this.createQuad(gl, this.pass1Program)
    if (!this.vao) { this.error = 'VAO 创建失败'; return false }
    this.sourceTexture = this.createTexture(gl, gl.NEAREST)
    if (!this.sourceTexture) { this.error = '源纹理创建失败'; return false }
    this.fboTexture = this.createTexture(gl, gl.LINEAR)
    if (!this.fboTexture) { this.error = 'FBO 纹理创建失败'; return false }
    this.fbo = gl.createFramebuffer()
    if (!this.fbo) { this.error = 'FBO 创建失败'; return false }
    this.prevFrameTexture = this.createTexture(gl, gl.LINEAR)
    if (!this.prevFrameTexture) { this.error = '历史帧纹理创建失败'; return false }
    this.ready = true
    return true
  }

  start(): void {
    if (!this.ready || this.running) return
    this.running = true; this.frameCount = 0
    this.lastFpsTime = performance.now(); this.isFirstFrame = true
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
      if (this.pass1Program) gl.deleteProgram(this.pass1Program)
      if (this.pass2Program) gl.deleteProgram(this.pass2Program)
      if (this.sourceTexture) gl.deleteTexture(this.sourceTexture)
      if (this.fboTexture) gl.deleteTexture(this.fboTexture)
      if (this.prevFrameTexture) gl.deleteTexture(this.prevFrameTexture)
      if (this.fbo) gl.deleteFramebuffer(this.fbo)
      if (this.vao) gl.deleteVertexArray(this.vao)
    }
    if (this.canvas?.parentNode) this.canvas.parentNode.removeChild(this.canvas)
    this.canvas = this.gl = this.pass1Program = this.pass2Program = null
    this.sourceTexture = this.fboTexture = this.prevFrameTexture = this.fbo = null
    this.sourceVideo = this.vao = null
    this.pass1Uniforms = {}; this.pass2Uniforms = {}
  }

  updateOptions(opts: Partial<UpscalerOptions>): void {
    const keys = ['mode','sharpness','contrast','deband','edgeEnhance','upscale','outputWidth','outputHeight','denoise','temporalBlend'] as const
    for (const k of keys) { if (opts[k] !== undefined) (this.opts as any)[k] = opts[k] }
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

  private renderLoop = (): void => {
    if (!this.running) return
    this.animFrameId = requestAnimationFrame(this.renderLoop); this.render()
  }

  private render(): void {
    const gl = this.gl, video = this.sourceVideo, canvas = this.canvas
    if (!gl || !video || !canvas || !this.pass1Program || !this.pass2Program) return
    if (!this.sourceTexture || !this.fboTexture || !this.prevFrameTexture || !this.fbo || !this.vao) return
    if (video.readyState < 2 || video.paused || video.ended) return
    const vw = video.videoWidth, vh = video.videoHeight
    if (vw <= 0 || vh <= 0) return
    const ct = video.currentTime
    if (Math.abs(ct - this.lastVideoTime) < 0.001) return
    this.lastVideoTime = ct
    const scale = this.opts.upscale
    const outW = this.opts.outputWidth > 0 ? this.opts.outputWidth : Math.round(vw * scale)
    const outH = this.opts.outputHeight > 0 ? this.opts.outputHeight : Math.round(vh * scale)
    if (canvas.width !== outW || canvas.height !== outH) { canvas.width = outW; canvas.height = outH }
    if (this.fboWidth !== vw || this.fboHeight !== vh) {
      this.fboWidth = vw; this.fboHeight = vh
      gl.bindTexture(gl.TEXTURE_2D, this.fboTexture)
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, vw, vh, 0, gl.RGBA, gl.UNSIGNED_BYTE, null)
    }
    const modeVal = this.opts.mode === 'film' ? 1.0 : 0.0
    // 上传视频帧：直接用 texImage2D(video) 一步分配+上传
    // 避免 texSubImage2D 在 Chromium 中触发 glCopySubTextureCHROMIUM 维度越界
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, this.sourceTexture)
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false)
    try {
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, video)
    } catch (e) {
      console.warn('[AiUpscaler] texImage2D(video) 失败，跳过本帧:', e)
      return
    }
    // 更新 sourceTexture 实际尺寸（texImage2D 自动分配，无需跟踪）
    // === Pass 1: 预处理 → FBO ===
    gl.bindFramebuffer(gl.FRAMEBUFFER, this.fbo)
    gl.viewport(0, 0, vw, vh)
    gl.useProgram(this.pass1Program)
    // 注意：不绑定 fboTexture 到纹理单元，仅作为 FBO 附件
    // sourceTexture 保持在 TEXTURE0 供着色器采样，避免 feedback loop
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, this.fboTexture, 0)
    gl.uniform1i(this.pass1Uniforms.u_source, 0)
    gl.uniform2f(this.pass1Uniforms.u_texelSize, 1 / vw, 1 / vh)
    gl.uniform1f(this.pass1Uniforms.u_denoise, this.opts.denoise)
    gl.uniform1f(this.pass1Uniforms.u_deband, this.opts.deband)
    gl.uniform1f(this.pass1Uniforms.u_mode, modeVal)
    gl.uniform1f(this.pass1Uniforms.u_frameTime, performance.now())
    gl.uniform1f(this.pass1Uniforms.u_flipY, 1.0)
    gl.bindVertexArray(this.vao)
    gl.drawArrays(gl.TRIANGLES, 0, 6)
    // Pass1 诊断
    const fbStatus = gl.checkFramebufferStatus(gl.FRAMEBUFFER)
    if (fbStatus !== gl.FRAMEBUFFER_COMPLETE) {
      console.error('[AiUpscaler] Pass1 FBO 不完整:', fbStatus)
    }
    this.checkGLErrors('Pass1')
    // === Pass 2: 增强+输出 → Canvas ===
    gl.bindFramebuffer(gl.FRAMEBUFFER, null)
    gl.viewport(0, 0, outW, outH)
    gl.useProgram(this.pass2Program)
    gl.activeTexture(gl.TEXTURE0); gl.bindTexture(gl.TEXTURE_2D, this.fboTexture)
    gl.activeTexture(gl.TEXTURE1); gl.bindTexture(gl.TEXTURE_2D, this.prevFrameTexture)
    if (this.prevWidth !== outW || this.prevHeight !== outH) {
      this.prevWidth = outW; this.prevHeight = outH
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGB8, outW, outH, 0, gl.RGB, gl.UNSIGNED_BYTE, null)
      this.isFirstFrame = true
    }
    gl.uniform1i(this.pass2Uniforms.u_preprocessed, 0)
    gl.uniform1i(this.pass2Uniforms.u_prevFrame, 1)
    gl.uniform2f(this.pass2Uniforms.u_texelSize, 1 / vw, 1 / vh)
    gl.uniform1f(this.pass2Uniforms.u_sharpness, this.opts.sharpness)
    gl.uniform1f(this.pass2Uniforms.u_contrast, this.opts.contrast)
    gl.uniform1f(this.pass2Uniforms.u_edgeEnhance, this.opts.edgeEnhance)
    gl.uniform1f(this.pass2Uniforms.u_mode, modeVal)
    gl.uniform1f(this.pass2Uniforms.u_temporalBlend, this.isFirstFrame ? 0.0 : this.opts.temporalBlend)
    gl.uniform2f(this.pass2Uniforms.u_outputSize, outW, outH)
    gl.uniform2f(this.pass2Uniforms.u_inputSize, vw, vh)
    gl.uniform1f(this.pass2Uniforms.u_flipY, 0.0)
    gl.bindVertexArray(this.vao)
    gl.drawArrays(gl.TRIANGLES, 0, 6)
    this.checkGLErrors('Pass2')
    // 保存历史帧
    gl.activeTexture(gl.TEXTURE1); gl.bindTexture(gl.TEXTURE_2D, this.prevFrameTexture)
    gl.copyTexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, 0, 0, outW, outH)
    this.checkGLErrors('copyTexSubImage2D')
    if (this.isFirstFrame) this.isFirstFrame = false
    if (!this.canvasShown && this.canvas) { this.canvasShown = true; this.canvas.style.display = '' }
    this.frameCount++
    const now = performance.now()
    if (now - this.lastFpsTime >= 1000) {
      this.fps = Math.round(this.frameCount / ((now - this.lastFpsTime) / 1000))
      this.frameCount = 0; this.lastFpsTime = now
    }
  }

  private glErrorCount = 0
  private checkGLErrors(stage: string): void {
    const gl = this.gl; if (!gl) return
    let err = gl.getError()
    while (err !== gl.NO_ERROR) {
      this.glErrorCount++
      if (this.glErrorCount <= 5) {
        console.error(`[AiUpscaler] GL error at ${stage}: 0x${err.toString(16)} (count: ${this.glErrorCount})`)
      } else if (this.glErrorCount === 6) {
        console.warn('[AiUpscaler] GL 错误已抑制，后续不再逐条打印')
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
      console.error('[AiUpscaler] 着色器编译错误:', gl.getShaderInfoLog(shader))
      gl.deleteShader(shader); return null
    }
    return shader
  }

  private linkProgram(gl: WebGL2RenderingContext, vs: WebGLShader, fs: WebGLShader): WebGLProgram | null {
    const program = gl.createProgram()
    if (!program) return null
    gl.attachShader(program, vs); gl.attachShader(program, fs); gl.linkProgram(program)
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.error('[AiUpscaler] 着色器链接错误:', gl.getProgramInfoLog(program))
      gl.deleteProgram(program); return null
    }
    return program
  }

  private createQuad(gl: WebGL2RenderingContext, program: WebGLProgram): WebGLVertexArrayObject | null {
    const vao = gl.createVertexArray()
    if (!vao) return null
    gl.bindVertexArray(vao)
    const vertices = new Float32Array([
      -1,-1, 0,1,  1,-1, 1,1,  -1,1, 0,0,
      -1,1, 0,0,   1,-1, 1,1,   1,1, 1,0,
    ])
    const buf = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, buf)
    gl.bufferData(gl.ARRAY_BUFFER, vertices, gl.STATIC_DRAW)
    const posLoc = gl.getAttribLocation(program, 'a_position')
    const texLoc = gl.getAttribLocation(program, 'a_texCoord')
    if (posLoc >= 0) { gl.enableVertexAttribArray(posLoc); gl.vertexAttribPointer(posLoc, 2, gl.FLOAT, false, 16, 0) }
    if (texLoc >= 0) { gl.enableVertexAttribArray(texLoc); gl.vertexAttribPointer(texLoc, 2, gl.FLOAT, false, 16, 8) }
    gl.bindVertexArray(null); return vao
  }
}

/** 检查当前环境是否支持实时增强 */
export function checkUpscalerSupport(): {
  webgl2: boolean; webgpu: boolean; recommended: 'webgl2' | 'webgpu' | 'none'; message: string
} {
  const hasWebGL2 = (() => { try { const c = document.createElement('canvas'); return !!c.getContext('webgl2') } catch { return false } })()
  const hasWebGPU = (() => { try { return !!(navigator as any).gpu } catch { return false } })()
  let recommended: 'webgl2' | 'webgpu' | 'none' = 'none'
  let message = ''
  if (hasWebGL2) { recommended = 'webgl2'; message = 'WebGL2 可用，将使用 GPU 加速增强' }
  if (hasWebGPU) { recommended = 'webgpu'; message = 'WebGPU 可用，未来可启用 AI 超分模型' }
  if (!hasWebGL2 && !hasWebGPU) { message = '当前设备不支持 GPU 加速，AI 增强不可用' }
  return { webgl2: hasWebGL2, webgpu: hasWebGPU, recommended, message }
}
