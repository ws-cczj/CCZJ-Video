/**
 * 图片画质增强工具（一次性处理静态图片）
 *
 * 用于首页轮播图、视频海报等静态图提升观感：
 *   - Canvas 2D 后备：更好的默认渲染质量（比浏览器默认更清晰）
 *   - WebGL2 增强管线：锐化 + 边缘增强 + 去色带 + 局部对比
 *
 * 纯静态图片处理，不是视频实时管线。
 */

export interface EnhanceOptions {
  /** 目标宽度，0 表示跟随源 */
  width?: number
  /** 目标高度，0 表示跟随源 */
  height?: number
  /** 锐化强度 0~1.5，默认 0.6 */
  sharpness?: number
  /** 对比度增强 0~1，默认 0.25 */
  contrast?: number
  /** 去色带强度 0~1，默认 0.35 */
  deband?: number
  /** 边缘增强 0~1，默认 0.45 */
  edgeEnhance?: number
  /** 上采样倍率 1~2，默认 1.25（比原始更清晰） */
  upscale?: number
  /** 画布的 object-fit 策略，默认 cover（适配轮播图的宽幅容器） */
  fit?: 'cover' | 'contain' | 'fill'
}

const VERTEX_SHADER = /* glsl */ `#version 300 es
in vec2 a_position;
in vec2 a_texCoord;
out vec2 v_texCoord;
void main() {
  gl_Position = vec4(a_position, 0.0, 1.0);
  v_texCoord = a_texCoord;
}`

const FRAGMENT_SHADER = /* glsl */ `#version 300 es
precision highp float;
in vec2 v_texCoord;
out vec4 fragColor;

uniform sampler2D u_source;
uniform vec2 u_texelSize;
uniform float u_sharpness;
uniform float u_contrast;
uniform float u_deband;
uniform float u_edgeEnhance;
uniform vec2 u_outputSize;
uniform vec2 u_inputSize;

float luminance(vec3 c) { return dot(c, vec3(0.2126, 0.7152, 0.0722)); }

// 3x3 Unsharp Mask
vec3 unsharpMask(sampler2D tex, vec2 uv, vec2 ts, float strength) {
  vec3 center = texture(tex, uv).rgb;
  if (strength <= 0.0) return center;
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

// 局部对比度（5x5 均值）
vec3 localContrast(sampler2D tex, vec2 uv, vec2 ts, float strength) {
  vec3 center = texture(tex, uv).rgb;
  if (strength <= 0.0) return center;
  vec3 local = vec3(0.0);
  for (int x = -2; x <= 2; x++) {
    for (int y = -2; y <= 2; y++) {
      local += texture(tex, uv + vec2(float(x) * ts.x, float(y) * ts.y)).rgb;
    }
  }
  local /= 25.0;
  return clamp(center + (center - local) * strength * 2.0, 0.0, 1.0);
}

// 边缘增强 (Sobel)
vec3 edgeEnhancement(sampler2D tex, vec2 uv, vec2 ts, float strength) {
  vec3 center = texture(tex, uv).rgb;
  if (strength <= 0.0) return center;
  float lc = luminance(center);
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
  float factor = 1.0 + edge * strength;
  float edgeMask = smoothstep(0.05, 0.2, edge);
  return mix(center, clamp(center * factor, 0.0, 1.0), edgeMask * 0.6);
}

// 去色带（高频抖动）
vec3 debanding(vec3 color, vec2 uv, float strength) {
  if (strength <= 0.0) return color;
  float noise = fract(sin(dot(uv * 1000.0, vec2(12.9898, 78.233))) * 43758.5453);
  float dither = (noise - 0.5) * strength / 255.0;
  float banding = abs(fract(color * 255.0) - 0.5) * 2.0;
  float smoothArea = 1.0 - smoothstep(0.15, 0.45, banding);
  return clamp(color + dither * smoothArea * 0.7, 0.0, 1.0);
}

// Lanczos-like 自适应上采样
vec3 upscaleFilter(sampler2D tex, vec2 uv, vec2 ts, vec2 inputSize, vec2 outputSize) {
  vec3 result = texture(tex, uv).rgb;
  float scaleX = outputSize.x / inputSize.x;
  float scaleY = outputSize.y / inputSize.y;
  if (scaleX <= 1.01 && scaleY <= 1.01) return result;
  float scale = max(scaleX, scaleY);
  vec2 sampleRadius = ts / scale;
  vec3 sum = vec3(0.0);
  float weightSum = 0.0;
  for (int i = -1; i <= 1; i++) {
    for (int j = -1; j <= 1; j++) {
      vec2 samplePos = uv + vec2(float(i), float(j)) * sampleRadius;
      vec3 sample = texture(tex, samplePos).rgb;
      float dist = length(vec2(float(i), float(j)));
      float w = dist < 1.0
        ? 1.0 - 2.0*dist*dist + dist*dist*dist
        : 4.0 - 8.0*dist + 5.0*dist*dist - dist*dist*dist;
      w = max(w, 0.0);
      sum += sample * w;
      weightSum += w;
    }
  }
  return weightSum > 0.0 ? sum / weightSum : result;
}

void main() {
  vec2 ts = u_texelSize;
  vec2 uv = v_texCoord;
  vec3 color = upscaleFilter(u_source, uv, ts, u_inputSize, u_outputSize);
  color = debanding(color, uv, u_deband);
  color = unsharpMask(u_source, uv, ts, u_sharpness);
  color = localContrast(u_source, uv, ts, u_contrast);
  color = edgeEnhancement(u_source, uv, ts, u_edgeEnhance);
  fragColor = vec4(color, 1.0);
}`

function hasWebGL2(): boolean {
  try {
    const c = document.createElement('canvas')
    return !!c.getContext('webgl2')
  } catch {
    return false
  }
}

function compileShader(gl: WebGL2RenderingContext, type: number, src: string): WebGLShader | null {
  const sh = gl.createShader(type)
  if (!sh) return null
  gl.shaderSource(sh, src)
  gl.compileShader(sh)
  if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
    gl.deleteShader(sh)
    return null
  }
  return sh
}

/**
 * 将 HTMLImageElement 增强并输出为 dataURL (image/png) 或直接用 URL
 * 用于轮播图、视频卡片等静态图片的画质增强
 */
export async function enhanceImage(
  img: HTMLImageElement,
  options: EnhanceOptions = {},
): Promise<string> {
  const opts: Required<EnhanceOptions> = {
    width: options.width ?? 0,
    height: options.height ?? 0,
    sharpness: options.sharpness ?? 0.6,
    contrast: options.contrast ?? 0.25,
    deband: options.deband ?? 0.35,
    edgeEnhance: options.edgeEnhance ?? 0.45,
    upscale: options.upscale ?? 1.25,
    fit: options.fit ?? 'cover',
  }

  const srcW = img.naturalWidth || img.width
  const srcH = img.naturalHeight || img.height
  if (srcW <= 0 || srcH <= 0) return ''

  // 输出尺寸（如果没指定，按 upscale 放大）
  let outW = opts.width > 0 ? opts.width : Math.round(srcW * opts.upscale)
  let outH = opts.height > 0 ? opts.height : Math.round(srcH * opts.upscale)
  // 防止过大（性能考虑）
  const MAX_SIDE = 2560
  if (outW > MAX_SIDE || outH > MAX_SIDE) {
    const s = MAX_SIDE / Math.max(outW, outH)
    outW = Math.round(outW * s)
    outH = Math.round(outH * s)
  }

  // WebGL2 路径
  if (hasWebGL2()) {
    try {
      const canvas = document.createElement('canvas')
      canvas.width = outW
      canvas.height = outH
      const gl = canvas.getContext('webgl2', {
        antialias: false,
        alpha: false,
        premultipliedAlpha: false,
        preserveDrawingBuffer: true,
      })
      if (gl) {
        const vs = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER)
        const fs = compileShader(gl, gl.FRAGMENT_SHADER, FRAGMENT_SHADER)
        if (!vs || !fs) throw new Error('shader')
        const prog = gl.createProgram()!
        gl.attachShader(prog, vs)
        gl.attachShader(prog, fs)
        gl.linkProgram(prog)
        if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) throw new Error('link')
        gl.deleteShader(vs)
        gl.deleteShader(fs)

        // VAO: fullscreen quad
        const vao = gl.createVertexArray()
        gl.bindVertexArray(vao)
        const vertices = new Float32Array([
          -1, -1, 0, 1,
          1, -1, 1, 1,
          -1, 1, 0, 0,
          -1, 1, 0, 0,
          1, -1, 1, 1,
          1, 1, 1, 0,
        ])
        const buf = gl.createBuffer()
        gl.bindBuffer(gl.ARRAY_BUFFER, buf)
        gl.bufferData(gl.ARRAY_BUFFER, vertices, gl.STATIC_DRAW)
        const posLoc = gl.getAttribLocation(prog, 'a_position')
        const texLoc = gl.getAttribLocation(prog, 'a_texCoord')
        if (posLoc >= 0) { gl.enableVertexAttribArray(posLoc); gl.vertexAttribPointer(posLoc, 2, gl.FLOAT, false, 16, 0) }
        if (texLoc >= 0) { gl.enableVertexAttribArray(texLoc); gl.vertexAttribPointer(texLoc, 2, gl.FLOAT, false, 16, 8) }

        // 上传图片纹理
        const tex = gl.createTexture()
        gl.bindTexture(gl.TEXTURE_2D, tex)
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
        gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false)
        gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, gl.BROWSER_DEFAULT_WEBGL)
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, gl.RGBA, gl.UNSIGNED_BYTE, img)

        // 设置 uniforms 并绘制
        gl.useProgram(prog)
        gl.viewport(0, 0, outW, outH)
        gl.uniform1i(gl.getUniformLocation(prog, 'u_source'), 0)
        gl.uniform2f(gl.getUniformLocation(prog, 'u_texelSize'), 1 / srcW, 1 / srcH)
        gl.uniform1f(gl.getUniformLocation(prog, 'u_sharpness'), opts.sharpness)
        gl.uniform1f(gl.getUniformLocation(prog, 'u_contrast'), opts.contrast)
        gl.uniform1f(gl.getUniformLocation(prog, 'u_deband'), opts.deband)
        gl.uniform1f(gl.getUniformLocation(prog, 'u_edgeEnhance'), opts.edgeEnhance)
        gl.uniform2f(gl.getUniformLocation(prog, 'u_outputSize'), outW, outH)
        gl.uniform2f(gl.getUniformLocation(prog, 'u_inputSize'), srcW, srcH)

        gl.bindVertexArray(vao)
        gl.drawArrays(gl.TRIANGLES, 0, 6)

        // 导出 dataURL
        const url = canvas.toDataURL('image/webp', 0.88)
        // 清理
        gl.deleteProgram(prog); gl.deleteBuffer(buf); gl.deleteVertexArray(vao); gl.deleteTexture(tex)
        return url
      }
    } catch {
      // 回落 Canvas 2D
    }
  }

  // 回落：Canvas 2D（比浏览器默认的 img 渲染更清晰一些）
  const canvas = document.createElement('canvas')
  canvas.width = outW
  canvas.height = outH
  const ctx = canvas.getContext('2d')
  if (!ctx) return ''
  ctx.imageSmoothingEnabled = true
  ;(ctx as any).imageSmoothingQuality = 'high'

  // object-fit cover
  const srcRatio = srcW / srcH
  const outRatio = outW / outH
  let sx = 0, sy = 0, sw = srcW, sh = srcH
  if (opts.fit === 'cover') {
    if (srcRatio > outRatio) {
      sw = srcH * outRatio; sx = (srcW - sw) / 2
    } else {
      sh = srcW / outRatio; sy = (srcH - sh) / 2
    }
  } else if (opts.fit === 'contain') {
    if (srcRatio > outRatio) {
      sh = outW / srcRatio; sy = (outH - sh) / 2
    } else {
      sw = outH * srcRatio; sx = (outW - sw) / 2
    }
    // contain 不需要裁剪源；调整目标区域
    let dw = outW, dh = outH, dx = 0, dy = 0
    if (srcRatio > outRatio) {
      dh = outW / srcRatio; dy = (outH - dh) / 2
    } else {
      dw = outH * srcRatio; dx = (outW - dw) / 2
    }
    ctx.drawImage(img, dx, dy, dw, dh)
    return canvas.toDataURL('image/webp', 0.88)
  }
  ctx.drawImage(img, sx, sy, sw, sh, 0, 0, outW, outH)
  return canvas.toDataURL('image/webp', 0.88)
}
