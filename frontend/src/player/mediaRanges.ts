export type MediaRange = [number, number]

/** Preserve holes: downloaded or appended ranges need not be contiguous. */
export function mergeMediaRanges(ranges: readonly MediaRange[], duration = Infinity): MediaRange[] {
  const sorted = ranges.filter(([start, end]) => Number.isFinite(start) && Number.isFinite(end) && end > start)
    .map(([start, end]): MediaRange => [Math.max(0, start), Math.min(duration, end)])
    .filter(([start, end]) => end > start).sort((a, b) => a[0] - b[0])
  const result: MediaRange[] = []
  for (const range of sorted) {
    const previous = result[result.length - 1]
    if (previous && range[0] <= previous[1] + .05) previous[1] = Math.max(previous[1], range[1])
    else result.push([...range])
  }
  return result
}

export function readMediaRanges(ranges: TimeRanges): MediaRange[] {
  const result: MediaRange[] = []
  for (let i = 0; i < ranges.length; i++) result.push([ranges.start(i), ranges.end(i)])
  return result
}

export function contiguousMediaEnd(ranges: readonly MediaRange[], position: number): number {
  let end = position
  for (const [start, to] of mergeMediaRanges(ranges)) {
    if (start <= end + .05 && to > end) end = to
  }
  return end
}
