/**
 * Anime4K 模型配置定义 — S / M / L 三档
 *
 * 每档模型由 restore + upscale 两组 shader 组成，
 * buildPipeline() 根据模型配置自动计算纹理分配和 pass 流程。
 */

// ==================== 类型定义 ====================

/** 单个 shader pass 定义 */
export interface PassShaderDef {
  desc: string
  source: string
}

/** 模型结构描述 */
export interface ModelStructure {
  /** Restore_CNN 各阶段的 shader 源 */
  restore: {
    /** 线性 3×3 卷积 (video → 4ch features) */
    linear: PassShaderDef[]
    /** Split ReLU 3×3 卷积 (4ch → 4ch, 18 mat4 per pass) */
    split?: PassShaderDef[]
    /** 16ch Split ReLU 3×3 卷积 (dual-tex input, 36 mat4, 仅 L) */
    split16?: PassShaderDef[]
    /** 最终 pass: pointwise 或 spatial+residual */
    final: PassShaderDef
    /** pointwise 是否包含 linear 纹理 (M=true) */
    pwIncludeLinear?: boolean
  }
  /** Upscale_CNN_x2 各阶段的 shader 源 */
  upscale: {
    /** 线性 3×3 卷积 */
    linear: PassShaderDef[]
    /** Split ReLU 3×3 卷积 */
    split?: PassShaderDef[]
    /** 16ch Split ReLU 3×3 卷积 (仅 L) */
    split16?: PassShaderDef[]
    /** 可选的 pointwise 1×1 卷积 (M 有) */
    pointwise?: PassShaderDef[]
    /** 最终空间卷积 pass (L 有, 在 Depth2Space 前) */
    finalSpatial?: PassShaderDef
    /** pointwise 是否包含 linear 纹理 (M=true) */
    pwIncludeLinear?: boolean
  }
  /** Depth2Space shader (所有模型共用逻辑, 引擎内置) */
  depth2Space: PassShaderDef
}

/** 引擎渲染用的单 pass 配置 */
export interface EnginePassDef {
  /** 片段着色器完整源码 */
  fs: string
  /** 主输入纹理索引 (0..N = 中间纹理, TEX_VIDEO = 视频) */
  read: number
  /** 第二输入纹理索引 (16ch 双纹理 pass, -1 = 无) */
  read2?: number
  /** 第三输入纹理索引 (L Depth2Space, -1 = 无) */
  read3?: number
  /** 残差/主纹理索引 (可选) */
  mainTex?: number
  /** 是否额外绑定原始视频 (Depth2Space 强度混合) */
  useVideo?: boolean
  /** 输出: 纹理索引 或 'canvas' */
  write: number | 'canvas'
  /** 渲染分辨率: 1=输入, 2=输出(2x) */
  resScale: 1 | 2
  /** Y 翻转 */
  flipY: number
  /** Pointwise 源纹理索引列表（所有中间层输出，引擎绑定为 u_pw0..u_pwN） */
  pwSources?: number[]
}

// ==================== 纹理索引常量 ====================

export const TEX_VIDEO = 0

// ==================== 管线构建 ====================

/** Build model dependencies before assigning reusable source-sized GPU textures. */
export function buildPipeline(structure: ModelStructure): EnginePassDef[] {
  const passes: EnginePassDef[] = []
  const r = structure.restore
  const u = structure.upscale

  // 动态纹理索引分配器。
  // TEX_VIDEO 占用索引 0 作为视频纹理，中间纹理从 1 开始分配。
  // 中间 pass 的输出绝不能写到 TEX_VIDEO 上，否则会覆盖视频帧
  // (后续 MAIN_tex 残差取到错误数据)，且 RGBA8 截断会把 CNN 浮点特征归零导致画面近黑。
  let nextTex = 1

  // === Restore 阶段 ===

  // 线性 passes (video → features)
  const rLinearTex: number[] = []
  for (let i = 0; i < r.linear.length; i++) {
    const writeTex = nextTex++
    passes.push({
      fs: r.linear[i].source,
      read: TEX_VIDEO,
      write: writeTex,
      resScale: 1,
      flipY: 1,
    })
    rLinearTex.push(writeTex)
  }
  const lastLinearTex = rLinearTex[rLinearTex.length - 1]

  // 8ch Split ReLU passes
  const rSplitTex: number[] = []
  if (r.split) {
    let readTex = lastLinearTex
    for (let i = 0; i < r.split.length; i++) {
      const writeTex = nextTex++
      passes.push({
        fs: r.split[i].source,
        read: readTex,
        write: writeTex,
        resScale: 1,
        flipY: 1,
      })
      rSplitTex.push(writeTex)
      readTex = writeTex
    }
  }

  // 16ch Split ReLU passes (双纹理输入, L)
  // 每两个连续 shader 是一对 (tf + tf1)，权重不同但共享同一对输入纹理。
  // 拆成两个独立的单输出 pass，绝不能用 MRT (shader 只声明了单个 fragColor 输出)。
  const rSplit16Tex: number[] = []
  let rDualA = -1, rDualB = -1

  if (r.split16) {
    let readA = rLinearTex[0]
    let readB = rLinearTex[1]
    for (let i = 0; i < r.split16.length; i += 2) {
      const fsA = r.split16[i].source
      const fsB = (i + 1 < r.split16.length) ? r.split16[i + 1].source : fsA
      const outA = nextTex++, outB = nextTex++
      passes.push({ fs: fsA, read: readA, read2: readB, write: outA, resScale: 1, flipY: 1 })
      passes.push({ fs: fsB, read: readA, read2: readB, write: outB, resScale: 1, flipY: 1 })
      rSplit16Tex.push(outA, outB)
      readA = outA; readB = outB
    }
    rDualA = readA; rDualB = readB
  }

  // 最终 Restore pass (残差 + video)
  // 所有 Restore 中间层输出，供 M 模型的 pointwise final 读取
  // M 的 FS_R7 是 Conv-3x1x1x56 pointwise (需要 1 linear + 6 split = 7 个纹理)
  const allRestoreTex = r.pwIncludeLinear
    ? [...rLinearTex, ...rSplitTex, ...rSplit16Tex]
    : [...rSplit16Tex]
  let restoreWrite: number
  if (r.split16 && r.split16.length > 0) {
    const writeTex = nextTex++
    passes.push({
      fs: r.final.source,
      read: rDualA,
      read2: rDualB,
      mainTex: TEX_VIDEO,
      write: writeTex,
      resScale: 1,
      flipY: 1,
      pwSources: r.pwIncludeLinear !== undefined ? [...allRestoreTex] : undefined,
    })
    restoreWrite = writeTex
  } else {
    const readTex = r.split && r.split.length > 0
      ? rSplitTex[rSplitTex.length - 1]
      : lastLinearTex
    const writeTex = nextTex++
    passes.push({
      fs: r.final.source,
      read: readTex,
      mainTex: TEX_VIDEO,
      write: writeTex,
      resScale: 1,
      flipY: 1,
      pwSources: r.pwIncludeLinear !== undefined ? [...allRestoreTex] : undefined,
    })
    restoreWrite = writeTex
  }

  // === Upscale 阶段 ===

  // 线性 passes
  const uLinearTex: number[] = []
  for (let i = 0; i < u.linear.length; i++) {
    const writeTex = nextTex++
    passes.push({
      fs: u.linear[i].source,
      read: restoreWrite,
      write: writeTex,
      resScale: 1,
      flipY: 1,
    })
    uLinearTex.push(writeTex)
  }

  // 8ch Split ReLU passes
  const uSplitTex: number[] = []
  if (u.split) {
    let readTex = uLinearTex.length > 0 ? uLinearTex[uLinearTex.length - 1] : restoreWrite
    for (let i = 0; i < u.split.length; i++) {
      const writeTex = nextTex++
      passes.push({
        fs: u.split[i].source,
        read: readTex,
        write: writeTex,
        resScale: 1,
        flipY: 1,
      })
      uSplitTex.push(writeTex)
      readTex = writeTex
    }
  }

  // 16ch Split ReLU passes (双纹理输入, L) — 拆成成对单输出 pass (非 MRT)
  // 每两个连续 shader 是一对 (tf + tf1)，权重不同。
  const uSplit16Tex: number[] = []
  const uSplit16Pairs: Array<[number, number]> = []

  if (u.split16) {
    let readA = uLinearTex[0]
    let readB = uLinearTex[1]
    for (let i = 0; i < u.split16.length; i += 2) {
      const fsA = u.split16[i].source
      const fsB = (i + 1 < u.split16.length) ? u.split16[i + 1].source : fsA
      const outA = nextTex++, outB = nextTex++
      passes.push({ fs: fsA, read: readA, read2: readB, write: outA, resScale: 1, flipY: 1 })
      passes.push({ fs: fsB, read: readA, read2: readB, write: outB, resScale: 1, flipY: 1 })
      uSplit16Tex.push(outA, outB)
      uSplit16Pairs.push([outA, outB])
      readA = outA; readB = outB
    }
  }

  // Pointwise passes (M: 1×1 卷积，读取所有中间层输出)
  // M 的 FS_U7 是 Conv-4x1x1x56 (需要 1 linear + 6 split = 7 个纹理)
  const pointwiseOutTex: number[] = []
  if (u.pointwise) {
    const allUpscaleTex = u.pwIncludeLinear
      ? [...uLinearTex, ...uSplitTex, ...uSplit16Tex]
      : [...uSplit16Tex]
    let pwRead = allUpscaleTex.length > 0
      ? allUpscaleTex[allUpscaleTex.length - 1]
      : restoreWrite
    for (let i = 0; i < u.pointwise.length; i++) {
      const writeTex = nextTex++
      passes.push({
        fs: u.pointwise[i].source,
        read: pwRead,
        write: writeTex,
        resScale: 1,
        flipY: 1,
        pwSources: [...allUpscaleTex],
      })
      pointwiseOutTex.push(writeTex)
      pwRead = writeTex
    }
  }

  // 最终空间卷积 pass (L: FS_U8, 在 Depth2Space 前)
  // The three RGB residuals are siblings reading the same preceding feature pair.
  let finalSpatialWrite = -1
  if (u.finalSpatial) {
    const [read, read2] = uSplit16Pairs[uSplit16Pairs.length - 2]
    finalSpatialWrite = nextTex++
    passes.push({ fs: u.finalSpatial.source, read, read2, write: finalSpatialWrite, resScale: 1, flipY: 1 })
  }

  // Depth2Space: 最终 upscale 输出 → canvas (2x 分辨率)
  // L 模型: read conv2d_last_tf(tf1) = 最后一对 split16 + conv2d_last_tf2 = finalSpatial 输出
  // S/M 模型: read 最后一个 pass 输出 (单纹理)
  //
  // flipY 策略: 所有中间 pass 用 flipY=1, Depth2Space 用 flipY=0.
  // 原因: WebGL 的 FBO 渲染纹理 v=0 在底部, 而上传纹理 (视频) v=0 在顶部,
  // 每经过一次 FBO 渲染垂直方向就翻转一次。如果中间 pass 不统一翻转,
  // pointwise pass (R7/U7) 会同时读到正/反两种朝向的特征纹理, 权重相加
  // 后产生 180 度旋转的鬼影。统一 flipY=1 让所有中间 FBO 的 v=0 都对齐
  // 视频顶部; 最后 Depth2Space 不翻转, 让画布保持正常朝向。
  const lastUpscaleWrite = passes.length > 0
    ? (passes[passes.length - 1].write as number)
    : restoreWrite
  const d2sPass: EnginePassDef = {
    fs: structure.depth2Space.source,
    read: lastUpscaleWrite,
    mainTex: restoreWrite,
    useVideo: true,
    write: 'canvas',
    resScale: 2,
    flipY: 0,
  }

  if (finalSpatialWrite >= 0 && uSplit16Pairs.length > 0) {
    // L 模型: Depth2Space 三输入 = 最后一对 split16 输出(R/G) + finalSpatial 输出(B)
    const lastPair = uSplit16Pairs[uSplit16Pairs.length - 1]
    d2sPass.read = lastPair[0]
    d2sPass.read2 = lastPair[1]
    d2sPass.read3 = finalSpatialWrite
  }
  passes.push(d2sPass)

  // 自检: 反馈环 + 纹理索引越界 (权重/管线配置错误时尽早暴露)
  // 纹理索引 0 = TEX_VIDEO, 中间纹理从 1 开始, 总纹理数 = nextTex
  // Translation to physical slots happens once. A texture stays live through every
  // residual, pointwise and depth-to-space read, including the current draw.
  const lastRead = new Map<number, number>()
  for (const [i, pass] of passes.entries()) {
    for (const input of [pass.read, pass.read2, pass.read3, pass.mainTex, ...(pass.pwSources ?? [])]) {
      if (input !== undefined) lastRead.set(input, i)
    }
  }
  const slots = [TEX_VIDEO]
  const occupiedUntil = [Infinity]
  for (const [i, pass] of passes.entries()) {
    pass.read = slots[pass.read]
    if (pass.read2 !== undefined) pass.read2 = slots[pass.read2]
    if (pass.read3 !== undefined) pass.read3 = slots[pass.read3]
    if (pass.mainTex !== undefined) pass.mainTex = slots[pass.mainTex]
    if (pass.pwSources) pass.pwSources = pass.pwSources.map(input => slots[input])
    if (pass.write === 'canvas') continue
    const free = occupiedUntil.findIndex(end => end < i)
    const slot = free < 0 ? occupiedUntil.length : free
    occupiedUntil[slot] = lastRead.get(pass.write) ?? i
    slots[pass.write] = slot
    pass.write = slot
  }
  assertPipelineValid(passes, occupiedUntil.length)

  return passes
}

/** 管线自检: 确保 pass 不读写同一纹理 (反馈环), 且所有纹理索引在合法范围内。 */
function assertPipelineValid(passes: EnginePassDef[], numTextures: number): void {
  for (let i = 0; i < passes.length; i++) {
    const p = passes[i]
    const w = typeof p.write === 'number' ? p.write : -1
    const inputs = [p.read, p.read2 ?? -1, p.read3 ?? -1, p.mainTex ?? -1, ...(p.pwSources ?? [])]
    const label = `(pass ${i}: ${p.fs.substring(0, 40).replace(/\n/g, ' ')}...)`
    // 中间纹理写入不能落到 TEX_VIDEO 上 — 视频纹理每帧由 texImage2D 重新上传,
    // 一旦被中间 pass 覆盖, 后续读 TEX_VIDEO 的 pass (残差/MAIN) 会拿到错误数据,
    // 且 RGBA8 截断会把 CNN 浮点特征变成 0 导致画面近黑。
    if (w >= 0 && w === TEX_VIDEO) {
      console.error(`[Anime4K] 纹理索引冲突 ${label}: write(${w}) 落在 TEX_VIDEO 上, 会覆盖视频纹理`)
    }
    for (const inp of inputs) {
      if (inp >= 0 && inp === w) {
        console.error(`[Anime4K] 反馈环告警 ${label}: write(${w}) === 读纹理(${inp})`)
      }
      if (inp >= 0 && inp >= numTextures) {
        console.error(`[Anime4K] 纹理越界 ${label}: 读纹理 ${inp} >= 总纹理数 ${numTextures}`)
      }
    }
    if (w >= 0 && w >= numTextures) {
      console.error(`[Anime4K] 纹理越界 ${label}: write ${w} >= 总纹理数 ${numTextures}`)
    }
  }
}
