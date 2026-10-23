export interface SubtitleCue {
  start: number
  end: number
  text: string
}

/**
 * 解析 SRT / WebVTT 字幕。
 *
 * 播放器不认 .ass/.ssa（Chromium 的 <track> 也渲染不了），所以只支持这两种最常见的
 * 文本字幕；解析结果由播放器自己叠加渲染，不走原生 <track> —— 画质增强开启时画面上
 * 盖的是 WebGL canvas，原生字幕轨会被它挡住。
 */
export function parseSubtitle(raw: string): SubtitleCue[] {
  const text = raw.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n')
  if (!text.trim()) return []
  const cues: SubtitleCue[] = []
  for (const block of text.split('\n\n')) {
    const lines = block.split('\n').filter((l) => l.trim() !== '')
    if (lines.length === 0) continue
    if (/^(WEBVTT|NOTE|STYLE|REGION|CUE)/i.test(lines[0]) && !lines[0].includes('-->')) continue
    const timeIdx = lines.findIndex((l) => l.includes('-->'))
    if (timeIdx < 0) continue
    const span = parseTimeRange(lines[timeIdx])
    if (!span) continue
    const body = lines.slice(timeIdx + 1).map(stripTags).filter((l) => l.trim() !== '')
    if (body.length === 0) continue
    cues.push({ start: span.start, end: span.end, text: body.join('\n') })
  }
  cues.sort((a, b) => a.start - b.start)
  return cues
}

/** 时间轴上取当前字幕：二分找最后一条 start<=time，再确认还没过 end。 */
export function findActiveCue(cues: SubtitleCue[], time: number): SubtitleCue | null {
  if (cues.length === 0) return null
  let lo = 0
  let hi = cues.length - 1
  let hit = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (cues[mid].start <= time) {
      hit = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  // A long cue can overlap any number of shorter cues. The prefix index lets
  // us stop as soon as no earlier cue can still be active, without scanning
  // the full subtitle file on every timeupdate. Parsed cue arrays are immutable.
  const ends = prefixEnds(cues)
  for (let i = hit; i >= 0 && ends[i] >= time; i--) {
    const cue = cues[i]
    if (time <= cue.end) return cue
  }
  return null
}

const endIndexes = new WeakMap<SubtitleCue[], number[]>()
function prefixEnds(cues: SubtitleCue[]): number[] {
  let ends = endIndexes.get(cues)
  if (ends && ends.length === cues.length) return ends
  ends = []
  let end = -Infinity
  for (const cue of cues) { end = Math.max(end, cue.end); ends.push(end) }
  endIndexes.set(cues, ends)
  return ends
}

function parseTimeRange(line: string): { start: number; end: number } | null {
  const parts = line.split('-->')
  if (parts.length < 2) return null
  const start = parseTime(parts[0])
  // 尾段可能带 VTT 的 position 设置（"00:01:02.000 line:90%"），只取第一个空白前的时刻。
  const end = parseTime(parts[1].trim().split(/\s+/)[0])
  if (start === null || end === null || end < start) return null
  return { start, end }
}

/** 认 HH:MM:SS,mmm / HH:MM:SS.mmm / MM:SS.mmm 三种写法。 */
function parseTime(value: string): number | null {
  const m = /^(?:(\d{1,3}):)?(\d{1,2}):(\d{1,2})[.,](\d{1,3})$/.exec(value.trim())
  if (!m) return null
  const hours = m[1] ? Number(m[1]) : 0
  return hours * 3600 + Number(m[2]) * 60 + Number(m[3]) + Number(m[4].padEnd(3, '0')) / 1000
}

/** 去掉 <i>/<b>/{\\an8} 这类标记，只留可读文本。 */
function stripTags(line: string): string {
  return line.replace(/<[^>]*>/g, '').replace(/\{[^}]*\}/g, '').trim()
}
