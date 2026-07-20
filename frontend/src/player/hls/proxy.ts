/**
 * Builds a same-origin URL handled by Wails' HLS proxy middleware.
 * Keeping the encoded upstream URL deterministic also keeps TsCache keys stable.
 */
export function proxyHlsURL(remoteURL: string): string {
  if (!/^https?:\/\//i.test(remoteURL)) return remoteURL
  const bytes = new TextEncoder().encode(remoteURL)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  const encoded = btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
  return `/__cczj/hls?u=${encoded}`
}
