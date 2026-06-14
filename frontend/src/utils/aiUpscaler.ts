/**
 * 视频 AI 增强器 — WebGL2 实时画质增强
 *
 * 核心技术：
 *   - 锐化 (Unsharp Mask) — 让模糊的 TS 流变清晰
 *   - 局部对比度增强 (CLAHE-like) — 提升暗部细节
 *   - 边缘增强 (简版 Anime4K 思路) — 强化线条边缘
 *   - 去色带 (De-banding) — 减少 8-bit 色阶断层的条带
 *   - 自适应上采样 (Lanczos-like) — 比浏览器默认 bilinear 更清晰
 *
 * 所有计算均在 GPU 上完成（WebGL2），不影响主线程。
 * 无需下载模型、不依赖 WebGPU、WebView2 完美兼容。
 */

// ==================== 类型定义 ====================

export interface UpscalerOptions {
  /** 输出宽度（0 = 自动跟随视频宽） */
  outputWidth?: number
  /** 输出高度（0 = 自动跟随视频高） */
  outputHeight?: number
  /** 锐化强度 0.0~2.0，默认 0.6 */
  sharpness?: number
  /** 对比度增强 0.0~1.0，默认 0.3 */
  contrast?: number
  /** 去色带强度 0.0~1.0，默认 0.4 */
  deband?: number
  /** 边缘增强 0.0~1.0，默认 0.5 */
  edgeEnhance?: number
  /** 上采样倍率 1.0~2.0，默认 1.0（不上采样） */
  upscale?: number
}

export interface UpscalerStats {
  fps: number
  frameCount: number
  elapsed: number
  inputSize: string
  outputSize: string
  gpuEnabled: boolean
}

// ==================== WebGL 着色器 ====================

/** 顶点着色器 — 全屏四边形 */
const VERTEX_SHADER = /* glsl */ `#version 300 es
in vec2 a_position;
in vec2 a_texCoord;
out vec2 v_texCoord;
void main() {
  gl_Position = vec4(a_position, 0.0, 1.0);
  v_texCoord = a_texCoord;
}`

/**
 * 片段着色器 — 组合增强管线
 *
 * 在一次 pass 中完成：
 *   1. 锐化 (Unsharp Mask)
 *   2. 局部对比度增强
 *   3. 边缘增强
 *   4. 去色带
 *   5. 自适应上采样
 */
const FRAGMENT_SHADER = /* glsl */ `#version 300 es
precision highp float;

in vec2 v_texCoord;
out vec4 fragColor;

uniform sampler2D u_source;
uniform vec2 u_texelSize;       // 1/width, 1/height
uniform float u_sharpness;      // 0.0~2.0
uniform float u_contrast;       // 0.0~1.0
uniform float u_deband;         // 0.0~1.0
uniform float u_edgeEnhance;    // 0.0~1.0
uniform vec2 u_outputSize;      // 输出尺寸
uniform vec2 u_inputSize;       // 输入尺寸

// 将 RGB 转为亮度
float luminance(vec3 c) {
  return dot(c, vec3(0.2126, 0.7152, 0.0722));
}

// 锐化 (Unsharp Mask)
// 原理：原图 + (原图 - 模糊图) * 强度
// 在 main() 中被调用，确保在低分辨率空间执行
vec3 unsharpMask(sampler2D tex, vec2 uv, vec2 ts, float strength) {
  vec3 center = texture(tex, uv).rgb;
  if (strength <= 0.0) return center;

  // 3x3 模糊核（近似高斯）
  vec3 blur = vec3(0.0);
  blur += texture(tex, uv + vec2(-ts.x, -ts.y)).rgb;
  blur += texture(tex, uv + vec2(0.0, -ts.y)).rgb;
  blur += texture(tex, uv + vec2(ts.x, -ts.y)).rgb;
  blur += texture(tex, uv + vec2(-ts.x, 0.0)).rgb;
  blur += center * 4.0;
  blur += texture(tex, uv + vec2(ts.x, 0.0)).rgb;
  blur += texture(tex, uv + vec2(-ts.x, ts.y)).rgb;
  blur += texture(tex, uv + vec2(0.0, ts.y)).rgb;
  blur += texture(tex, uv + vec2(ts.x, ts.y)).rgb;
  blur /= 12.0;

  return clamp(center + (center - blur) * strength, 0.0, 1.0);
}

// 局部对比度增强 (局部对比度拉伸)
// 原理：每个像素相对于周围局部均值的偏移量放大
// 在 main() 中被调用，确保在低分辨率空间执行
vec3 localContrast(sampler2D tex, vec2 uv, vec2 ts, float strength) {
  vec3 center = texture(tex, uv).rgb;
  if (strength <= 0.0) return center;

  // 局部平均（3x3）- 减少纹理采样提升性能
  vec3 local = vec3(0.0);
  local += texture(tex, uv + vec2(-ts.x, -ts.y)).rgb;
  local += texture(tex, uv + vec2(0.0, -ts.y)).rgb;
  local += texture(tex, uv + vec2(ts.x, -ts.y)).rgb;
  local += texture(tex, uv + vec2(-ts.x, 0.0)).rgb;
  local += center * 4.0;
  local += texture(tex, uv + vec2(ts.x, 0.0)).rgb;
  local += texture(tex, uv + vec2(-ts.x, ts.y)).rgb;
  local += texture(tex, uv + vec2(0.0, ts.y)).rgb;
  local += texture(tex, uv + vec2(ts.x, ts.y)).rgb;
  local /= 12.0;

  return clamp(center + (center - local) * strength * 3.0, 0.0, 1.0);
}

// 边缘增强 (Anime4K 简化思想)
// 原理：检测边缘 → 沿边缘方向加强对比
// 在 main() 中被调用，确保在低分辨率空间执行
vec3 edgeEnhancement(sampler2D tex, vec2 uv, vec2 ts, float strength) {
  vec3 center = texture(tex, uv).rgb;
  if (strength <= 0.0) return center;

  float lc = luminance(center);

  // Sobel 边缘检测
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

  // 沿边缘增强
  float factor = 1.0 + edge * strength * 2.0;
  float edgeMask = smoothstep(0.05, 0.25, edge);
  
  return mix(center, clamp(center * factor, 0.0, 1.0), edgeMask);
}

// 去色带 (De-banding)
// 原理：在色带区域（量化台阶处）添加高频抖动，打散色阶断层
vec3 debanding(vec3 color, vec2 uv, float strength) {
  if (strength <= 0.0) return color;

  // 基于像素位置的确定性噪声
  float noise = fract(sin(dot(uv * 1000.0, vec2(12.9898, 78.233))) * 43758.5453);
  // 增大抖动幅度，从 strength/255 提升到 strength*4/255
  float dither = (noise - 0.5) * strength * 4.0 / 255.0;

  // 判断色带区域：亮度量化误差接近 0 或 1 时为台阶（色带明显处）
  float lum = luminance(color);
  float quantError = abs(fract(lum * 255.0) - 0.5);
  // 量化误差接近 0 或 1 → isSteep = 1（需要加噪）
  float isSteep = 1.0 - smoothstep(0.1, 0.45, quantError);

  return clamp(color + dither * isSteep, 0.0, 1.0);
}

// Lanczos-like 自适应上采样
// 当输出分辨率 > 输入分辨率时，用更好的插值
// 注意：此函数现在接受已增强的颜色作为输入，避免重新从源纹理采样
vec3 upscaleFilter(sampler2D tex, vec2 uv, vec2 ts, vec2 inputSize, vec2 outputSize, vec3 enhancedColor) {
  float scaleX = outputSize.x / inputSize.x;
  float scaleY = outputSize.y / inputSize.y;

  // 如果不需要上采样，直接返回增强后的颜色
  if (scaleX <= 1.01 && scaleY <= 1.01) return enhancedColor;

  // 2-tap Lanczos 近似（比 bilinear 更清晰）
  float scale = max(scaleX, scaleY);
  // 修复：采样半径应该在输入纹理坐标空间计算
  // 输出像素相邻时，输入坐标的步长 = ts * scale
  vec2 sampleRadius = ts * scale;

  // 使用增强后的颜色作为中心点，周围从源纹理采样
  vec3 sum = vec3(0.0);
  float weightSum = 0.0;

  for (int i = -1; i <= 1; i++) {
    for (int j = -1; j <= 1; j++) {
      vec2 samplePos = uv + vec2(float(i), float(j)) * sampleRadius;
      
      // 中心点使用增强后的颜色，其他点从源纹理采样
      vec3 texSample;
      if (i == 0 && j == 0) {
        texSample = enhancedColor;  // 使用已增强的颜色
      } else {
        texSample = texture(tex, samplePos).rgb;  // 从源纹理采样
      }

      // Lanczos 核近似
      float dist = length(vec2(float(i), float(j)));
      float w = dist < 1.0
        ? 1.0 - 2.0*dist*dist + dist*dist*dist
        : 4.0 - 8.0*dist + 5.0*dist*dist - dist*dist*dist;
      w = max(w, 0.0);

      sum += texSample * w;
      weightSum += w;
    }
  }

  return weightSum > 0.0 ? sum / weightSum : enhancedColor;
}

void main() {
  vec2 ts = u_texelSize;
  vec2 uv = v_texCoord;

  // 基础采样 - 从原始低分辨率纹理采样
  vec3 color = texture(u_source, uv).rgb;

  // 1. 去色带 - 在低分辨率空间进行，色带更容易检测和修正
  color = debanding(color, uv, u_deband);

  // 2. 锐化 - 在低分辨率空间进行，邻域采样与中心颜色一致
  if (u_sharpness > 0.0) {
    color = unsharpMask(u_source, uv, ts, u_sharpness);
  }

  // 3. 局部对比度 - 在低分辨率空间进行
  if (u_contrast > 0.0) {
    color = localContrast(u_source, uv, ts, u_contrast);
  }

  // 4. 边缘增强 - 在低分辨率空间进行
  if (u_edgeEnhance > 0.0) {
    color = edgeEnhancement(u_source, uv, ts, u_edgeEnhance);
  }

  // 5. 最后做高质量上采样 - 所有增强操作已完成，传入增强后的颜色
  // upscaleFilter 会使用增强后的颜色作为中心点，周围从源纹理采样进行插值
  color = upscaleFilter(u_source, uv, ts, u_inputSize, u_outputSize, color);

  fragColor = vec4(color, 1.0);
}`

// ==================== WebGL2 引擎 ====================

export class AiUpscaler {
  private gl: WebGL2RenderingContext | null = null
  private canvas: HTMLCanvasElement | null = null
  private program: WebGLProgram | null = null
  private vao: WebGLVertexArrayObject | null = null
  private sourceTexture: WebGLTexture | null = null
  private sourceVideo: HTMLVideoElement | null = null
  private uniforms: Record<string, WebGLUniformLocation | null> = {}
  private animFrameId = 0
  private running = false
  private opts: Required<UpscalerOptions>

  // 性能统计
  private frameCount = 0
  private lastFpsTime = 0
  private fps = 0
  private lastTexW = 0
  private lastTexH = 0
  private lastVideoTime = 0
  private canvasShown = false // 是否已显示 canvas（首帧渲染后显示）

  // 公开属性
  ready = false
  error: string | null = null

  constructor(options: UpscalerOptions = {}) {
    this.opts = {
      outputWidth: options.outputWidth ?? 0,
      outputHeight: options.outputHeight ?? 0,
      sharpness: options.sharpness ?? 0.6,
      contrast: options.contrast ?? 0.3,
      deband: options.deband ?? 0.4,
      edgeEnhance: options.edgeEnhance ?? 0.5,
      upscale: options.upscale ?? 1.0,
    }
  }

  // ==================== 初始化 ====================

  /**
   * 从 video 元素创建增强器。
   * canvas 会作为 overlay 覆盖在 video 上方。
   */
  async init(video: HTMLVideoElement, parent?: HTMLElement): Promise<boolean> {
    this.sourceVideo = video

    // 创建 canvas
    this.canvas = document.createElement('canvas')
    this.canvas.style.cssText =
      'position:absolute;inset:0;width:100%;height:100%;object-fit:contain;pointer-events:none;z-index:1;image-rendering:auto;display:none'

    const container = parent ?? video.parentElement
    if (container) {
      container.style.position = container.style.position || 'relative'
      container.appendChild(this.canvas)
    }

    // 初始化 WebGL2
    try {
      this.gl = this.canvas.getContext('webgl2', {
        alpha: false,
        antialias: false,
        powerPreference: 'high-performance',
        premultipliedAlpha: false,
        preserveDrawingBuffer: false,
      })
    } catch {
      // 忽略
    }

    if (!this.gl) {
      this.error = 'WebGL2 不可用，请检查显卡驱动'
      return false
    }

    // 检测 GPU 最大纹理尺寸
    const maxTextureSize = this.gl.getParameter(this.gl.MAX_TEXTURE_SIZE)
    const vw = video.videoWidth
    const vh = video.videoHeight
    const scale = this.opts.upscale
    const requiredWidth = this.opts.outputWidth > 0 ? this.opts.outputWidth : Math.round(vw * scale)
    const requiredHeight = this.opts.outputHeight > 0 ? this.opts.outputHeight : Math.round(vh * scale)

    if (requiredWidth > maxTextureSize || requiredHeight > maxTextureSize) {
      this.error = `输出尺寸 ${requiredWidth}x${requiredHeight} 超过 GPU 最大纹理限制 ${maxTextureSize}x${maxTextureSize}`
      console.warn('[AiUpscaler]', this.error)
      // 不阻止初始化，但在渲染时会限制尺寸
    }

    const gl = this.gl

    // 编译着色器
    const vs = this.compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER)
    const fs = this.compileShader(gl, gl.FRAGMENT_SHADER, FRAGMENT_SHADER)
    if (!vs || !fs) {
      this.error = '着色器编译失败'
      return false
    }

    this.program = this.linkProgram(gl, vs, fs)
    if (!this.program) {
      this.error = '着色器链接失败'
      return false
    }

    gl.deleteShader(vs)
    gl.deleteShader(fs)

    // 缓存 uniform 位置
    const p = this.program
    this.uniforms = {
      u_source: gl.getUniformLocation(p, 'u_source'),
      u_texelSize: gl.getUniformLocation(p, 'u_texelSize'),
      u_sharpness: gl.getUniformLocation(p, 'u_sharpness'),
      u_contrast: gl.getUniformLocation(p, 'u_contrast'),
      u_deband: gl.getUniformLocation(p, 'u_deband'),
      u_edgeEnhance: gl.getUniformLocation(p, 'u_edgeEnhance'),
      u_outputSize: gl.getUniformLocation(p, 'u_outputSize'),
      u_inputSize: gl.getUniformLocation(p, 'u_inputSize'),
    }

    // 创建全屏四边形 VAO
    this.vao = this.createQuad(gl, p)
    if (!this.vao) {
      this.error = 'VAO 创建失败'
      return false
    }

    // 创建源纹理
    this.sourceTexture = gl.createTexture()
    if (!this.sourceTexture) {
      this.error = '纹理创建失败'
      return false
    }

    gl.bindTexture(gl.TEXTURE_2D, this.sourceTexture)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
    // 使用 NEAREST 过滤，让着色器完全控制采样位置
    // 避免双重插值（硬件 bilinear + 着色器内 Lanczos）导致效果衰减
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)

    this.ready = true
    return true
  }

  // ==================== 运⾏控制 ====================

  start(): void {
    if (!this.ready || this.running) return
    this.running = true
    this.frameCount = 0
    this.lastFpsTime = performance.now()
    this.renderLoop()
  }

  stop(): void {
    this.running = false
    this.canvasShown = false
    if (this.canvas) this.canvas.style.display = 'none'
    if (this.animFrameId) {
      cancelAnimationFrame(this.animFrameId)
      this.animFrameId = 0
    }
  }

  destroy(): void {
    this.stop()
    this.ready = false

    if (this.gl && this.program) {
      this.gl.deleteProgram(this.program)
    }
    if (this.gl && this.sourceTexture) {
      this.gl.deleteTexture(this.sourceTexture)
    }
    if (this.vao && this.gl) {
      this.gl.deleteVertexArray(this.vao)
    }
    if (this.canvas && this.canvas.parentNode) {
      this.canvas.parentNode.removeChild(this.canvas)
    }

    this.canvas = null
    this.gl = null
    this.program = null
    this.sourceTexture = null
    this.sourceVideo = null
    this.vao = null
    this.uniforms = {}
  }

  // ==================== 更新参数 ====================

  updateOptions(opts: Partial<UpscalerOptions>): void {
    if (opts.sharpness !== undefined) this.opts.sharpness = opts.sharpness
    if (opts.contrast !== undefined) this.opts.contrast = opts.contrast
    if (opts.deband !== undefined) this.opts.deband = opts.deband
    if (opts.edgeEnhance !== undefined) this.opts.edgeEnhance = opts.edgeEnhance
    if (opts.upscale !== undefined) this.opts.upscale = opts.upscale
    if (opts.outputWidth !== undefined) this.opts.outputWidth = opts.outputWidth
    if (opts.outputHeight !== undefined) this.opts.outputHeight = opts.outputHeight
  }

  getStats(): UpscalerStats {
    const v = this.sourceVideo
    const c = this.canvas
    return {
      fps: this.fps,
      frameCount: this.frameCount,
      elapsed: this.lastFpsTime > 0 ? (performance.now() - this.lastFpsTime) / 1000 : 0,
      inputSize: v ? `${v.videoWidth}x${v.videoHeight}` : 'N/A',
      outputSize: c ? `${c.width}x${c.height}` : 'N/A',
      gpuEnabled: this.gl !== null,
    }
  }

  // ==================== 内部方法 ====================

  private renderLoop = (): void => {
    if (!this.running) return
    this.animFrameId = requestAnimationFrame(this.renderLoop)
    this.render()
  }

  private render(): void {
    const gl = this.gl
    const video = this.sourceVideo
    const canvas = this.canvas
    if (!gl || !video || !canvas || !this.program || !this.sourceTexture) return

    // 检查视频元素有效性
    if (video.readyState < 2) return // HAVE_CURRENT_DATA
    if (video.paused || video.ended) return

    const vw = video.videoWidth
    const vh = video.videoHeight
    if (vw <= 0 || vh <= 0) return

    // 跳过重复帧（检测视频时间是否变化）
    const currentTime = video.currentTime
    if (Math.abs(currentTime - this.lastVideoTime) < 0.001) return
    this.lastVideoTime = currentTime

    // 计算输出尺寸
    const scale = this.opts.upscale
    const outW = this.opts.outputWidth > 0
      ? this.opts.outputWidth
      : Math.round(vw * scale)
    const outH = this.opts.outputHeight > 0
      ? this.opts.outputHeight
      : Math.round(vh * scale)

    if (canvas.width !== outW || canvas.height !== outH) {
      canvas.width = outW
      canvas.height = outH
      gl.viewport(0, 0, outW, outH)
    }

    gl.useProgram(this.program)

    // 更新视频纹理
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, this.sourceTexture)

    // 只在尺寸变化时重新分配纹理（性能优化）
    if (this.lastTexW !== vw || this.lastTexH !== vh) {
      this.lastTexW = vw
      this.lastTexH = vh
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, vw, vh, 0, gl.RGBA, gl.UNSIGNED_BYTE, null)
    }

    // 上传视频帧到纹理
    gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, vw, vh, gl.RGBA, gl.UNSIGNED_BYTE, video)

    // 设置 uniforms
    gl.uniform1i(this.uniforms.u_source, 0)
    gl.uniform2f(this.uniforms.u_texelSize, 1 / vw, 1 / vh)
    gl.uniform1f(this.uniforms.u_sharpness, this.opts.sharpness)
    gl.uniform1f(this.uniforms.u_contrast, this.opts.contrast)
    gl.uniform1f(this.uniforms.u_deband, this.opts.deband)
    gl.uniform1f(this.uniforms.u_edgeEnhance, this.opts.edgeEnhance)
    gl.uniform2f(this.uniforms.u_outputSize, outW, outH)
    gl.uniform2f(this.uniforms.u_inputSize, vw, vh)

    // 绘制
    gl.bindVertexArray(this.vao)
    gl.drawArrays(gl.TRIANGLES, 0, 6)

    // 首帧渲染成功后显示 canvas（避免初始黑屏遮盖视频）
    if (!this.canvasShown && this.canvas) {
      this.canvasShown = true
      this.canvas.style.display = ''
    }

    // 帧率统计
    this.frameCount++
    const now = performance.now()
    if (now - this.lastFpsTime >= 1000) {
      this.fps = Math.round(this.frameCount / ((now - this.lastFpsTime) / 1000))
      this.frameCount = 0
      this.lastFpsTime = now
    }
  }

  private compileShader(
    gl: WebGL2RenderingContext,
    type: number,
    source: string,
  ): WebGLShader | null {
    const shader = gl.createShader(type)
    if (!shader) return null
    gl.shaderSource(shader, source)
    gl.compileShader(shader)
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      console.error('[AiUpscaler] 着色器编译错误:', gl.getShaderInfoLog(shader))
      gl.deleteShader(shader)
      return null
    }
    return shader
  }

  private linkProgram(
    gl: WebGL2RenderingContext,
    vs: WebGLShader,
    fs: WebGLShader,
  ): WebGLProgram | null {
    const program = gl.createProgram()
    if (!program) return null
    gl.attachShader(program, vs)
    gl.attachShader(program, fs)
    gl.linkProgram(program)
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.error('[AiUpscaler] 着色器链接错误:', gl.getProgramInfoLog(program))
      gl.deleteProgram(program)
      return null
    }
    return program
  }

  private createQuad(
    gl: WebGL2RenderingContext,
    program: WebGLProgram,
  ): WebGLVertexArrayObject | null {
    const vao = gl.createVertexArray()
    if (!vao) return null
    gl.bindVertexArray(vao)

    // 全屏四边形 (NDC: -1 ~ 1)
    const vertices = new Float32Array([
      // 位置 (x, y)    纹理坐标 (u, v)
      -1, -1, 0, 1,  // 左下
      1, -1, 1, 1,  // 右下
      -1, 1, 0, 0,  // 左上
      -1, 1, 0, 0,  // 左上
      1, -1, 1, 1,  // 右下
      1, 1, 1, 0,  // 右上
    ])

    const buf = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, buf)
    gl.bufferData(gl.ARRAY_BUFFER, vertices, gl.STATIC_DRAW)

    const posLoc = gl.getAttribLocation(program, 'a_position')
    const texLoc = gl.getAttribLocation(program, 'a_texCoord')

    if (posLoc >= 0) {
      gl.enableVertexAttribArray(posLoc)
      gl.vertexAttribPointer(posLoc, 2, gl.FLOAT, false, 16, 0)
    }
    if (texLoc >= 0) {
      gl.enableVertexAttribArray(texLoc)
      gl.vertexAttribPointer(texLoc, 2, gl.FLOAT, false, 16, 8)
    }

    gl.bindVertexArray(null)
    return vao
  }
}

/**
 * 检查当前环境是否支持实时增强
 */
export function checkUpscalerSupport(): {
  webgl2: boolean
  webgpu: boolean
  recommended: 'webgl2' | 'webgpu' | 'none'
  message: string
} {
  const hasWebGL2 = (() => {
    try {
      const c = document.createElement('canvas')
      return !!c.getContext('webgl2')
    } catch { return false }
  })()

  const hasWebGPU = (() => {
    try {
      return !!(navigator as any).gpu
    } catch { return false }
  })()

  let recommended: 'webgl2' | 'webgpu' | 'none' = 'none'
  let message = ''

  if (hasWebGL2) {
    recommended = 'webgl2'
    message = 'WebGL2 可用，将使用 GPU 加速增强'
  }
  if (hasWebGPU) {
    recommended = 'webgpu'
    message = 'WebGPU 可用，未来可启用 AI 超分模型'
  }

  if (!hasWebGL2 && !hasWebGPU) {
    message = '当前设备不支持 GPU 加速，AI 增强不可用'
  }

  return { webgl2: hasWebGL2, webgpu: hasWebGPU, recommended, message }
}