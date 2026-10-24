import { createApp, h, reactive, ref } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/locales'
import App from '../../src/App.vue'
import DataBackupPanel from '../../src/components/DataBackupPanel.vue'
import SourceImportModal from '../../src/views/sources/SourceImportModal.vue'
import ThemeEditorModal from '../../src/views/settings/ThemeEditorModal.vue'
import { useThemeStore } from '../../src/stores/theme'
import { useConfirmStore } from '../../src/stores/confirm'
import { useErrorStore } from '../../src/stores/error'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'

void App // Include the production theme and shared frame CSS without startup jobs.
const state = reactive({ mounted: true, sourceOpen: true })
const source = new URLSearchParams(location.search).has('source')
const theme = new URLSearchParams(location.search).has('theme')
const editor = ref<InstanceType<typeof ThemeEditorModal> | null>(null)
createApp({ render: () => state.mounted ? theme ? h(ThemeEditorModal, { ref: editor }) : source
  ? h(SourceImportModal, { modelValue: state.sourceOpen, 'onUpdate:modelValue': (open: boolean) => { state.sourceOpen = open } })
  : h('div', { class: 'cczj-p-8' }, [h(DataBackupPanel)]) : null,
}).use(createPinia()).use(i18n).mount('#app')
;(window as any).fileTransferTest = { state, confirm: useConfirmStore(), errors: useErrorStore(), theme: useThemeStore(), openTheme: () => editor.value?.openCreate() }
