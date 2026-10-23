import { createApp, defineComponent, h, computed } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHashHistory, useRoute } from 'vue-router'
import i18n from '../../src/locales'
import NovelShell from '../../src/novel/NovelShell.vue'
import ComicShell from '../../src/comic/ComicShell.vue'
import NovelShelf from '../../src/novel/views/NovelShelf.vue'
import NovelBook from '../../src/novel/views/NovelBook.vue'
import NovelReader from '../../src/novel/views/NovelReader.vue'
import NovelCatalog from '../../src/novel/views/NovelCatalog.vue'
import ComicSeries from '../../src/comic/views/ComicSeries.vue'
import ComicLibrary from '../../src/comic/views/ComicLibrary.vue'
import ComicCatalog from '../../src/comic/views/ComicCatalog.vue'
import ComicReader from '../../src/comic/views/ComicReader.vue'
import NovelSources from '../../src/novel/views/NovelSources.vue'
import ComicSources from '../../src/comic/views/ComicSources.vue'
import NovelSearch from '../../src/novel/views/NovelSearch.vue'
import NovelHome from '../../src/novel/views/NovelHome.vue'
import ComicHome from '../../src/comic/views/ComicHome.vue'
import NovelSettings from '../../src/novel/views/NovelSettings.vue'
import RuntimeDiagnostics from '../../src/views/RuntimeDiagnostics.vue'
import { useApplicationMode } from '../../src/modes/applicationMode'
import ComicSettings from '../../src/comic/views/ComicSettings.vue'
import { useNovelReading } from '../../src/novel/readingState'
import { useComicState } from '../../src/comic/state'
import ConfirmModal from '../../src/components/ConfirmModal.vue'
import '../../src/style.css'
import '../../src/styles/cczj-utilities.css'
import '../../src/styles/animations.css'

const pinia = createPinia()
const router = createRouter({ history: createWebHashHistory(), routes: [
  {path:'/novel',component:NovelHome},{path:'/comic',component:ComicHome},
  {path:'/novel/settings',component:NovelSettings},{path:'/comic/settings',component:ComicSettings},
  {path:'/novel/diagnostics',component:RuntimeDiagnostics,meta:{title:'diagnostics.pageTitle'}},{path:'/comic/diagnostics',component:RuntimeDiagnostics,meta:{title:'diagnostics.pageTitle'}},
  { path: '/novel/sources', component: NovelSources,meta:{title:'librarySources.novelTitle'} }, { path: '/comic/sources', component: ComicSources,meta:{title:'librarySources.comicTitle'} }, { path:'/novel/search',component:NovelSearch,meta:{title:'novel.search'} }, { path:'/comic/search',component:ComicCatalog,meta:{title:'comic.search'} },
  { path: '/novel/shelf', component: NovelShelf }, { path: '/novel/book/:bookId', component: NovelBook },
  { path: '/novel/read/:bookId/:chapter', component: NovelReader }, { path: '/novel/categories', component: NovelCatalog },
  { path: '/comic/series/:seriesId', component: ComicSeries }, { path: '/comic/library', component: ComicLibrary },
  { path: '/comic/categories', component: ComicCatalog }, { path: '/:pathMatch(.*)*', component: NovelCatalog },
  { path: '/comic/read/:seriesId/:episode', component: ComicReader },
] })
router.beforeEach(async to => {
  useApplicationMode(pinia).mode=to.path.startsWith('/comic')?'comic':'novel'
  if (to.path.startsWith('/comic')) await useComicState(pinia).init()
  else await useNovelReading(pinia).init()
})
const root = defineComponent({ setup() {
  const route = useRoute(), shell = computed(() => route.path.startsWith('/comic') ? ComicShell : NovelShell)
  return () => h('div', { style: { height: '100%' } }, [h(shell.value), h(ConfirmModal)])
} })
const app = createApp(root).use(pinia).use(router).use(i18n)
await router.isReady()
app.mount('#app')
