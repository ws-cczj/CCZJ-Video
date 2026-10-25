import { describe, expect, it } from 'vitest'
import { declaredQualityTier } from '../src/player/streamQuality'

describe('declared stream quality tiers', () => {
  it.each([
    [640, 360, 360], [854, 480, 480], [1280, 720, 720], [1920, 1080, 1080],
    [1080, 608, 720], [1280, 608, 720], [1920, 800, 1080], [3840, 1600, 2160],
    [720, 1280, 720], [1080, 1920, 1080], [2560, 1440, 1440],
  ])('classifies %i × %i as %iP while preserving the original dimensions', (width, height, tier) => {
    expect(declaredQualityTier(width, height)).toBe(tier)
  })

  it.each([[0, 0], [0, 1080], [1920, 0], [-1, 720], [NaN, 720], [1280, Infinity]])(
    'does not invent a tier for incomplete dimensions %s × %s', (width, height) => {
      expect(declaredQualityTier(width, height)).toBe(0)
    })
})
