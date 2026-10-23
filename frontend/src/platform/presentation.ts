import { isDesktopRuntime, isMobileRuntime } from '../api/runtime'

export interface PresentationProfile {
  readonly ui: 'mobile' | 'desktop'
  readonly host: 'native-mobile' | 'native-desktop' | 'browser'
}
let selected: PresentationProfile | undefined

/** Selected after runtime initialization, once per document. Window size can
 * change the layout, but must never switch the host or recreate media. */
export function presentationProfile(): PresentationProfile {
  if (!selected) {
    const mobile = isMobileRuntime()
    selected = Object.freeze({ ui: mobile ? 'mobile' : 'desktop',
      host: mobile ? 'native-mobile' : isDesktopRuntime() ? 'native-desktop' : 'browser' })
  }
  return selected
}

export async function loadPlatformPresentation(): Promise<void> {
  const profile = presentationProfile()
  document.documentElement.dataset.cczjPlatform = profile.ui
  if (profile.ui === 'mobile') await import('../styles/mobile.css')
}
