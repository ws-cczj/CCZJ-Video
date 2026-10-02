/**
 * 首启条款闸门的共享状态。
 *
 * 条款正文在仓库根的 `LICENSE`，这里只放两件事：本程序要求的是哪一版条款，
 * 以及弹窗的开合。设置页「关于」也从这里取开关，所以复看入口和首启闸门是同一个弹窗。
 */
import { ref } from 'vue'

/**
 * 本程序携带的许可条款版本。改动 `LICENSE` 的实质条款时必须把这里升一位，
 * 升位后所有机器会重新征求一次同意。
 */
export const LICENSE_TERMS_VERSION = '1.0'

/** 弹窗是否打开 */
export const licenseModalOpen = ref(false)

/**
 * 是否还没同意当前版本。决定页脚给「同意 / 不同意并退出」还是只给「关闭」，
 * 也决定弹窗能不能被点遮罩或 Esc 关掉——没同意时关不掉。
 */
export const licensePending = ref(false)
