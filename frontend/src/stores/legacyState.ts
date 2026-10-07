/**
 * 「旧数据找回」弹窗的开合。
 *
 * 放在 store 里只为一件事：启动时的全局 Modal 共用一个 z-index，同时开就是「先弹的那个
 * 神秘消失」。「库比这个程序新」那条提示（SchemaNewerPrompt）必须排在它后面。
 */
import { ref } from 'vue'

/** 弹窗是否打开 */
export const legacyPromptOpen = ref(false)
