import { computed, onScopeDispose, ref, shallowRef, type Ref } from 'vue'
import { findActiveCue, parseSubtitle, type SubtitleCue } from '../utils/subtitles'
import { tr } from '../locales'
import { createRequestScope } from '../api/requestScope'
import { readFileText } from '../api/fileTransfer'

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
  const subtitleCues = shallowRef<SubtitleCue[]>([])
  const subtitleName = ref('')
  const subtitleVisible = ref(true)
  const subtitleError = ref('')
  let generation = 0
  let disposed = false
  const reads = createRequestScope()
  const maxFileBytes = 8 * 1024 * 1024

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
    if (!file || disposed) return
    const token = ++generation
    reads.cancel()
    subtitleError.value = ''
    if (file.size > maxFileBytes) {
      subtitleError.value = tr('player.subtitleTooLarge')
      return
    }
    try {
      const text = await reads.run(() => readFileText(file, maxFileBytes))
      if (disposed || token !== generation) return
      const cues = parseSubtitle(text)
      if (cues.length === 0) {
        subtitleError.value = tr('player.subtitleEmptyFile')
        return
      }
      subtitleCues.value = cues
      subtitleName.value = file.name
      subtitleVisible.value = true
    } catch {
      if (disposed || token !== generation) return
      subtitleError.value = tr('player.subtitleReadFailed')
    }
  }

  function clearSubtitle(): void {
    generation++
    reads.cancel()
    subtitleCues.value = []
    subtitleName.value = ''
    subtitleError.value = ''
    subtitleVisible.value = true
  }

  onScopeDispose(() => { disposed = true; clearSubtitle() })

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
