import { computed, ref, type Ref } from 'vue'
import { findActiveCue, parseSubtitle, type SubtitleCue } from '../utils/subtitles'
import { tr } from '../locales'

export interface SubtitlesDeps {
  /** 当前播放位置（秒）：字幕靠它挑出该显示哪一条 */
  current: Ref<number>
  /** 隐藏的文件选择框，由模板持有 ref */
  fileInput: Ref<HTMLInputElement | null>
}

/**
 * 外挂字幕：加载 .srt/.vtt 并算出当前该显示的一行。
 *
 * 源站几乎不附送字幕轨，所以这里只做「本地文件叠加」：自绘一层文字而不是用 <track>，
 * 因为画质增强会把画面盖在 WebGL canvas 上，原生字幕轨会被 canvas 挡住。
 */
export function useSubtitles(deps: SubtitlesDeps) {
  const showSubtitlePanel = ref(false)
  const subtitleCues = ref<SubtitleCue[]>([])
  const subtitleName = ref('')
  const subtitleVisible = ref(true)
  const subtitleError = ref('')

  const activeSubtitle = computed(() => {
    if (!subtitleVisible.value || subtitleCues.value.length === 0) return ''
    const cue = findActiveCue(subtitleCues.value, deps.current.value)
    return cue ? cue.text : ''
  })

  function openSubtitlePicker(): void {
    deps.fileInput.value?.click()
  }

  async function onSubtitleFileChosen(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file) return
    subtitleError.value = ''
    try {
      const text = await file.text()
      const cues = parseSubtitle(text)
      if (cues.length === 0) {
        subtitleError.value = tr('player.subtitleEmptyFile')
        return
      }
      subtitleCues.value = cues
      subtitleName.value = file.name
      subtitleVisible.value = true
    } catch {
      subtitleError.value = tr('player.subtitleReadFailed')
    }
  }

  function clearSubtitle(): void {
    subtitleCues.value = []
    subtitleName.value = ''
    subtitleError.value = ''
    subtitleVisible.value = true
  }

  return {
    showSubtitlePanel,
    subtitleCues,
    subtitleName,
    subtitleVisible,
    subtitleError,
    activeSubtitle,
    openSubtitlePicker,
    onSubtitleFileChosen,
    clearSubtitle,
  }
}
