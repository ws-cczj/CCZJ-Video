/**
 * 「这行代码是哪个包跑的」。
 *
 * 包源码是从 blob URL 动态 import 进来的，模块一旦实例化，它的脚本名就固化在了
 * 每一次调用的堆栈里——即使那个 URL 随后被 revoke 也不影响。授权闸门要靠这一行
 * 才知道该向用户问哪个包的同意，所以这里只留一张极小的对照表。
 *
 * 判定方式刻意写得笨：拿着已知的几个 URL 在堆栈文本里找子串，而不是正则拆栈。
 * 各家浏览器的栈格式（有没有 `at`、带不带括号、行号怎么拼）不一样，正则拆开了
 * 也照样漏；子串命中要么准，要么就是没注入过任何包，两种结果都安全。
 */
const owners = new Map<string, string>()

/** 记下这次 import 用的 URL 属于哪个包；同一个包重注入会有新 URL，旧的先留着也无妨。 */
export function claimBlobUrl(url: string, packId: string): void {
  owners.set(url, packId)
}

/** 包被收回后它的 URL 就该从表里消失：迟到一步的回调不再算在这个包头上。 */
export function releaseBlobUrls(urls: Iterable<string>): void {
  for (const url of urls) owners.delete(url)
}

/** 当前调用栈里最靠上的那个扩展包 id；不是包发起的（应用自己的代码）返回空串。 */
export function callingPack(): string {
  // 一个包都没注入过时不必付出那次取栈的代价——播放器的分片请求就在这条路上。
  if (owners.size === 0) return ''
  const stack = new Error().stack ?? ''
  for (const [url, packId] of owners) {
    if (stack.includes(url)) return packId
  }
  return ''
}
