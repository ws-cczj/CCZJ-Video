/** Persisted spellings are compatibility contracts; changing a key requires migration. */
export const Legacy = Object.freeze({
  novelShelf: 'cczj.novel.shelf.v2',
  novelProgress: 'cczj.novel.progress.v2',
  novelPreferences: 'cczj.novel.reader.v2',
  comicFollow: 'cczj.comic.follow.v2',
  comicProgress: 'cczj.comic.progress.v2',
  comicPreferences: 'cczj.comic.preferences.v2',
  playerQualityMode: 'vp_quality_mode',
  playerAnime4KTier: 'vp_anime4k_tier',
  playerAiWarningAccepted: 'vp_ai_warning_accepted',
  playerAutoResumeJump: 'vp_auto_resume_jump',
  playerVolume: 'vp_volume',
  playerMuted: 'vp_muted',
  playerSpeed: 'vp_speed',
  playerAutoNext: 'vp_auto_next',
} as const)
