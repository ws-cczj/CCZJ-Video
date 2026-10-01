/**
 * 扩展包着色器引擎 — 运行用户提供的 mpv hook 着色器（WebGL2 实时）
 *
 * 与内置引擎的分工：包里的每个 pass 都在源分辨率上跑，输出放大由本引擎在最后一帧
 * 绘制时用线性过滤一次完成。因此扩展包只做同分辨率处理（锐化/去噪/去色带/调色），
 * 任何改分辨率的 pass 在 Go 侧校验阶段就会被拒。
 */
import {
  VERTEX_SRC,
  VERTEX_FLIP_SRC,
  parseMpvShader,
  buildPassShader,
  linkProgram,
  createFBO,
  deleteFBO,
  layoutCanvasToVideo,
  type FBOEntry,
  type PassDef,
} from './filmUpscaler'
import { loseGlContext } from './anime4kUpscaler'

export interface CustomShaderSpec {
  /** 扩展包声明的档位 id，只用于日志与错误归属 */
  id: string
  /** mpv hook 语法的着色器源码 */
  source: string
  /** 输出分辨率倍数：1 或 2 */
  scale: number
}

const TAG = 'CustomShader'

// 输出 pass：翻 Y 以匹配 DOM 坐标系，线性过滤顺便完成 scale 倍的放大
const OUTPUT_FLIP_FRAG = `#version 300 es
precision highp float;
in vec2 v_uv;
out vec4 fragColor;
uniform sampler2D u_input;
void main() { fragColor = texture(u_input, v_uv); }`

function compile(gl: WebGL2RenderingContext, type: number, src: string, label: string): WebGLShader | null {
  const s = gl.createShader(type)
  if (!s) return null
  gl.shaderSource(s, src)
  gl.compileShader(s)
  if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
    const info = gl.getShaderInfoLog(s) ?? ''
    gl.deleteShader(s)
    throw new Error(`${label} 编译失败: ${info.slice(0, 300)}`)
  }
  return s
}

/** 环境缺能力（没有宿主元素 / 没有 WebGL2 / 没有浮点渲染目标）。这类失败不是包写坏了：
 *  调用方据此决定别把包标成损坏，也别让用户重装。用类型而不是错误文案判定，
 *  是因为文案既中英混杂又会随实现改动，字符串匹配迟早某天悄悄失配。 */
class ShaderCapabilityError extends Error {}

export class CustomShaderUpscaler {
  private spec: CustomShaderSpec
  private canvas: HTMLCanvasElement | null = null
  private gl: WebGL2RenderingContext | null = null
  private video: HTMLVideoElement | null = null
  private running = false
  private rafId = 0
  private videoFrameCallbackId: number | null = null
  private _error: string | null = null
  private _capability = false

  private quadVAO: WebGLVertexArrayObject | null = null
  private quadVBO: WebGLBuffer | null = null
  private videoTex: WebGLTexture | null = null

  private passDefs: PassDef[] = []
  private progs: (WebGLProgram | null)[] = []
  private outputProg: WebGLProgram | null = null
  private fboReg: Map<string, FBOEntry[]> = new Map()
  private ulocs: Map<WebGLProgram, Map<string, WebGLUniformLocation | null>> = new Map()

  private frames = 0
  private lastFpsTime = 0
  private currentFps = 0
  private inputW = 0
  private inputH = 0
  private lastUploadTime = -1

  get error(): string | null { return this._error }
  get capabilityMissing(): boolean { return this._capability }

  constructor(spec: CustomShaderSpec) {
    this.spec = spec
  }

  async init(video: HTMLVideoElement, wrapper?: HTMLElement): Promise<boolean> {
    try {
      this.video = video
      this.passDefs = parseMpvShader(this.spec.source)
      if (!this.passDefs.length) throw new Error('着色器没有可运行的 //!HOOK pass')
      if (this.passDefs.some(p => p.isAggregation)) {
        throw new Error('着色器含改变分辨率的 pass，扩展包只允许同分辨率处理')
      }

      this.canvas = document.createElement('canvas')
      this.canvas.style.cssText = 'position:absolute;top:0;left:0;pointer-events:none;z-index:2'
      const host = wrapper ?? video.parentElement
      if (!host) throw new ShaderCapabilityError('No container')
      host.style.position = 'relative'
      host.appendChild(this.canvas)

      const gl = this.canvas.getContext('webgl2', {
        alpha: false, antialias: false, premultipliedAlpha: false, preserveDrawingBuffer: false,
      }) as WebGL2RenderingContext | null
      if (!gl) throw new ShaderCapabilityError('WebGL2')
      this.gl = gl
      // pass 之间用 RGBA16F 中转，没有浮点渲染目标就画不出正确结果，直接判不可用
      if (!gl.getExtension('EXT_color_buffer_float')) throw new ShaderCapabilityError('EXT_color_buffer_float 不可用')
      gl.getExtension('OES_texture_float_linear')

      this.canvas.addEventListener('webglcontextlost', e => { e.preventDefault(); this.stop() })
      this.canvas.addEventListener('webglcontextrestored', () => { this.rebuild() })

      this.inputW = video.videoWidth || 1920
      this.inputH = video.videoHeight || 1080
      const scale = this.spec.scale === 1 ? 1 : 2
      this.canvas.width = this.inputW * scale
      this.canvas.height = this.inputH * scale

      this.quadVAO = gl.createVertexArray()!
      gl.bindVertexArray(this.quadVAO)
      this.quadVBO = gl.createBuffer()!
      gl.bindBuffer(gl.ARRAY_BUFFER, this.quadVBO)
      gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW)
      gl.enableVertexAttribArray(0)
      gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0)
      gl.bindVertexArray(null)

      this.videoTex = gl.createTexture()!
      gl.bindTexture(gl.TEXTURE_2D, this.videoTex)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
      gl.bindTexture(gl.TEXTURE_2D, null)

      this.compileAll(gl)
      this.allocFBOs()
      console.log(`[${TAG}] init: ${this.inputW}×${this.inputH} → x${scale} [${this.spec.id}] ${this.passDefs.length} pass`)
      return true
    } catch (e: any) {
      this._error = e?.message || String(e)
      this._capability = e instanceof ShaderCapabilityError
      console.error(`[${TAG}] init:`, this._error)
      // 失败也要走完整销毁：只摘 canvas 会留着 this.gl，上下文既没用又不会被回收。
      this.destroy()
      return false
    }
  }

  private compileAll(gl: WebGL2RenderingContext): void {
    const vs = compile(gl, gl.VERTEX_SHADER, VERTEX_SRC, 'VERTEX')
    if (!vs) throw new Error('顶点着色器创建失败')
    try {
      this.progs = this.passDefs.map((pass, i) => {
        const fs = compile(gl, gl.FRAGMENT_SHADER, buildPassShader(pass, this.inputW, this.inputH), `pass#${i} ${pass.desc || pass.saveName}`)
        if (!fs) throw new Error(`pass#${i} 着色器创建失败`)
        const p = linkProgram(gl, vs, fs)
        gl.deleteShader(fs)
        if (!p) throw new Error(`pass#${i} ${pass.desc || pass.saveName} 链接失败`)
        return p
      })
      const outFs = compile(gl, gl.FRAGMENT_SHADER, OUTPUT_FLIP_FRAG, 'OUTPUT')
      if (!outFs) throw new Error('输出着色器创建失败')
      const vsFlip = compile(gl, gl.VERTEX_SHADER, VERTEX_FLIP_SRC, 'VERTEX_FLIP')
      if (!vsFlip) throw new Error('输出顶点着色器创建失败')
      this.outputProg = linkProgram(gl, vsFlip, outFs)
      gl.deleteShader(outFs)
      gl.deleteShader(vsFlip)
      if (!this.outputProg) throw new Error('输出程序链接失败')
    } finally {
      gl.deleteShader(vs)
    }
  }

  private allocFBOs(): void {
    const gl = this.gl!
    for (let i = 0; i < this.passDefs.length; i++) {
      const p = this.passDefs[i]
      if (p.saveName) this.getFBO(`${p.saveName}_${i}`, this.inputW, this.inputH)
    }
  }

  private getFBO(name: string, w: number, h: number): FBOEntry {
    let arr = this.fboReg.get(name)
    if (!arr) { arr = []; this.fboReg.set(name, arr) }
    for (const f of arr) if (f.w === w && f.h === h) return f
    const fb = createFBO(this.gl!, w, h)
    arr.push(fb)
    return fb
  }

  /** 找到引用了某个 SAVE 名字的最近前置 pass 的输出纹理。 */
  private resolveTex(name: string, before: number): FBOEntry | null {
    for (let i = before - 1; i >= 0; i--) {
      if (this.passDefs[i].saveName === name) return this.getFBO(`${name}_${i}`, this.inputW, this.inputH)
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
    this.running = true
    this.frames = 0
    this.lastFpsTime = performance.now()
    this.renderLoop()
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

  /** 换集/拖动进度：扩展包是无状态单帧处理，只需要丢掉跳帧检测用的时间戳。 */
  onSeeked(): void {
    this.lastUploadTime = -1
  }

  destroy(): void {
    this.stop()
    const gl = this.gl
    if (gl) {
      for (const [, arr] of this.fboReg) for (const f of arr) deleteFBO(gl, f)
      this.fboReg.clear()
      for (const p of this.progs) if (p) gl.deleteProgram(p)
      if (this.outputProg) gl.deleteProgram(this.outputProg)
      if (this.videoTex) gl.deleteTexture(this.videoTex)
      if (this.quadVBO) gl.deleteBuffer(this.quadVBO)
      if (this.quadVAO) gl.deleteVertexArray(this.quadVAO)
      this.ulocs.clear()
      loseGlContext(gl)
    }
    this.passDefs = []
    this.progs = []
    this.outputProg = null
    this.quadVAO = null
    this.quadVBO = null
    this.videoTex = null
    if (this.canvas?.parentElement) this.canvas.parentElement.removeChild(this.canvas)
    this.canvas = null
    this.gl = null
    this.video = null
  }

  /** 仅渲染右侧增强画面，左侧保留原始 video，用于效果对比。 */
  setCompareSplit(percent: number | null): void {
    if (!this.canvas) return
    this.canvas.style.clipPath = percent == null ? '' : `inset(0 0 0 ${Math.max(0, Math.min(100, percent))}%)`
  }

  getStats(): { fps: number; gpuEnabled: boolean; qualityScale: number; enhancements: string[] } {
    return {
      fps: this.currentFps,
      gpuEnabled: true,
      qualityScale: this.spec.scale === 1 ? 1 : 2,
      enhancements: [this.spec.id],
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
    this.render()
    this.frames++
    const now = performance.now()
    if (now - this.lastFpsTime >= 2000) {
      this.currentFps = Math.round(this.frames / ((now - this.lastFpsTime) / 1000))
      this.frames = 0
      this.lastFpsTime = now
    }
  }

  private render(): void {
    const gl = this.gl!, video = this.video!, canvas = this.canvas!
    if (video.readyState < 2 || !video.videoWidth) return

    if (video.videoWidth !== this.inputW || video.videoHeight !== this.inputH) {
      this.inputW = video.videoWidth
      this.inputH = video.videoHeight
      const scale = this.spec.scale === 1 ? 1 : 2
      canvas.width = this.inputW * scale
      canvas.height = this.inputH * scale
      this.rebuild()
      return
    }

    // RAF 比视频帧快得多：没有新解出的帧就跳过整条 pass 链。
    if (video.currentTime === this.lastUploadTime) return

    const w = this.inputW, h = this.inputH
    gl.bindVertexArray(this.quadVAO)
    gl.activeTexture(gl.TEXTURE0)
    gl.bindTexture(gl.TEXTURE_2D, this.videoTex)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, video)
    this.lastUploadTime = video.currentTime

    const bind = (prog: WebGLProgram, tex: WebGLTexture, name: string, unit: number) => {
      gl.activeTexture(gl.TEXTURE0 + unit)
      gl.bindTexture(gl.TEXTURE_2D, tex)
      const loc = this.getLoc(prog, name)
      if (loc) gl.uniform1i(loc, unit)
    }

    let src: WebGLTexture = this.videoTex!
    for (let i = 0; i < this.passDefs.length; i++) {
      const pd = this.passDefs[i]
      const prog = this.progs[i]
      if (!prog) continue
      const out = this.getFBO(`${pd.saveName}_${i}`, w, h)
      gl.bindFramebuffer(gl.FRAMEBUFFER, out.fbo)
      gl.viewport(0, 0, w, h)
      gl.useProgram(prog)
      let unit = 0
      for (const bn of pd.bindNames) {
        if (bn === 'LUMA') {
          bind(prog, src, `u_${bn}`, unit)
        } else {
          const prev = this.resolveTex(bn, i)
          if (prev) bind(prog, prev.tex, `u_${bn}`, unit)
          else if (bn === 'ORIGINAL' || bn === 'SOURCE') bind(prog, src, `u_${bn}`, unit)
        }
        unit++
      }
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
      src = out.tex
    }

    // 末帧直接画到画布：viewport 是 scale 倍尺寸，线性过滤即完成放大。
    layoutCanvasToVideo(video, canvas)
    gl.bindFramebuffer(gl.FRAMEBUFFER, null)
    gl.viewport(0, 0, canvas.width, canvas.height)
    gl.useProgram(this.outputProg!)
    bind(this.outputProg!, src, 'u_input', 0)
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
    gl.bindVertexArray(null)
  }

  private rebuild(): void {
    const gl = this.gl!
    if (!gl) return
    this.stop()
    for (const [, arr] of this.fboReg) for (const f of arr) deleteFBO(gl, f)
    this.fboReg.clear()
    for (const p of this.progs) if (p) gl.deleteProgram(p)
    this.progs = []
    if (this.outputProg) { gl.deleteProgram(this.outputProg); this.outputProg = null }
    this.ulocs.clear()
    try {
      this.compileAll(gl)
    } catch (e: any) {
      // 换分辨率后按新尺寸重新编译失败（例如着色器依赖固定 texel 尺寸）：
      // 报出来并让调用方回退，而不是留一条空管线静默黑屏。
      this._error = e?.message || String(e)
      console.error(`[${TAG}] rebuild:`, this._error)
      return
    }
    this.allocFBOs()
    this.start()
  }
}
