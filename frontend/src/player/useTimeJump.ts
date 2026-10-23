import { ref, nextTick } from 'vue'
import { formatPlaybackTime as fmt } from './videoInfoFormat'

export function useTimeJump(deps:{video:()=>HTMLVideoElement|null;current:()=>number;duration:()=>number}) {
// B站风格时间显示 - 可点击跳转
const showTimeInput = ref(false)

const timeInputValue = ref('')
const timeInputRef = ref<HTMLInputElement | null>(null)

// 记录打开时间输入框时的原始值，用于判断用户是否实际修改了时间
const timeInputOriginal = ref('')

function toggleTimeInput(): void {
  showTimeInput.value = !showTimeInput.value
  if (showTimeInput.value) {
    timeInputValue.value = fmt(deps.current())
    timeInputOriginal.value = timeInputValue.value
    nextTick(() => { timeInputRef.value?.focus(); timeInputRef.value?.select() })
  }
}
function parseTimeInput(val: string): number {
  val = val.trim()
  // 支持格式: mm:ss, hh:mm:ss, 纯秒数
  const parts = val.split(':')
  if (parts.length === 3) {
    const h = parseInt(parts[0], 10) || 0
    const m = parseInt(parts[1], 10) || 0
    const s = parseInt(parts[2], 10) || 0
    return h * 3600 + m * 60 + s
  } else if (parts.length === 2) {
    const m = parseInt(parts[0], 10) || 0
    const s = parseInt(parts[1], 10) || 0
    return m * 60 + s
  } else {
    return parseInt(val, 10) || 0
  }
}
function jumpToTime(): void {
  // ⭐ 修复：只有用户实际修改了时间才执行跳转
  const trimmed = timeInputValue.value.trim()
  if (trimmed === timeInputOriginal.value.trim()) {
    showTimeInput.value = false
    return
  }
  const sec = parseTimeInput(timeInputValue.value)
  const v = deps.video()
  if (v && sec >= 0 && sec <= deps.duration()) {
    v.currentTime = sec
  }
  showTimeInput.value = false
}
function cancelTimeInput(): void {
  showTimeInput.value = false
}
return {showTimeInput,timeInputValue,timeInputRef,toggleTimeInput,jumpToTime,cancelTimeInput}
}
