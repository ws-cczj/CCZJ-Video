const qualityTiers = [144, 240, 360, 480, 720, 1080, 1440, 2160, 4320] as const

/** A display tier, not the encoded height. Wide crops use the 16:9-equivalent
 * long edge; the original declared dimensions remain available in the UI. */
export function declaredQualityTier(width: number, height: number): number {
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) return 0
  const equivalent = Math.max(Math.min(width, height), Math.max(width, height) * 9 / 16)
  return qualityTiers.reduce<number>((best, tier) =>
    Math.abs(tier - equivalent) <= Math.abs(best - equivalent) ? tier : best, qualityTiers[0])
}
