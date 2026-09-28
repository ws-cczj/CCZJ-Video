/**
 * 请求代次：每次发起请求 begin() 拿一个号，响应回来时用 isCurrent() 判断它是否
 * 仍代表界面想要的那一份。
 *
 * Wails 的绑定调用没法从 JS 侧中断，所以「取消」只能做成这样：旧请求继续跑完，
 * 但结果整份丢弃，连它的错误提示都不弹。KeepAlive 常驻的页面、快速切换筛选或
 * 换集都会让在途请求晚到，不丢弃就会盖掉当前内容。
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
