/**
 * 请求代次：每次发起请求 begin() 拿一个号，响应回来时用 isCurrent() 判断它是否
 * 仍代表界面想要的那一份。
 *
 * 绑定取消负责停止实际 I/O；代次同时拦截已在途的回调和不支持取消的兼容通道。
 * KeepAlive 页面、快速切换筛选或换集时，旧结果与旧错误都不能覆盖当前内容。
 */
export interface Generation {
  begin(): number
  isCurrent(seq: number): boolean
  /** 作废所有在途请求，例如界面状态被 reset() 清空时。 */
  invalidate(): void
}

export function useGeneration(): Generation {
  let seq = 0
  return {
    begin: () => ++seq,
    isCurrent: (value: number) => value === seq,
    invalidate: () => { seq++ },
  }
}
