export function formatPlaybackTime(sec: number): string {
  if (!isFinite(sec) || sec <= 0) return '00:00'
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = s % 60
  const mm = m.toString().padStart(2, '0')
  const sss = ss.toString().padStart(2, '0')
  if (h > 0) return `${h}:${mm}:${sss}`
  return `${mm}:${sss}`
}

/** Display metadata only: never change the source label or episode identity. */
export function splitEpisodeLabel(label: string): { title: string; date: string } {
  const raw = label.trim()
  const match = /^(?:第\s*)?((?:19|20)\d{2})([-/.]?)(\d{2})\2(\d{2})(?!\d)(?:期)?/.exec(raw)
  if (!match) return { title: raw, date: '' }
  const year = Number(match[1]), month = Number(match[3]), day = Number(match[4])
  const date = new Date(Date.UTC(year, month - 1, day))
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return { title: raw, date: '' }
  const title = raw.slice(match[0].length).replace(/^[\s·:：_\-－—]+/, '').trim()
  // A date-only entry still needs its original label; no episode number is guessed.
  return title ? { title, date: `${match[1]}-${match[3]}-${match[4]}` } : { title: raw, date: '' }
}

export function hasDetailedEpisodeLabels(episodes: readonly { ep_name?: string }[]): boolean {
  return episodes.some(episode => {
    const label = episode.ep_name?.trim() || ''
    return !!splitEpisodeLabel(label).date || Array.from(label).length > 12
  })
}
