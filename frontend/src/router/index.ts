import { nextTick } from 'vue'
import { useApplicationMode } from '../modes/applicationMode'
import { modeHome, routeMode } from '../modes/definitions'
import {routeScreenKey} from '../mobile/routeMotion'
import { modeSessions } from '../modes/sessionRuntime'
import { restoreModeScroll } from '../modes/restoreScroll'
import { createRouter, createWebHashHistory, type RouteLocationNormalized } from 'vue-router'
import { WriteLog } from '../api/app'

// ====== 滚动位置管理：记录每个路由路径在 .main-content 上的 scrollTop ======
// 背景：App.vue 中滚动容器是 .main-content（overflow-y: auto），不是 window。
// 路由懒加载导致离开时组件销毁，返回时组件重新挂载；而 .main-content 在详情页
// 可能被滚动到其它位置，因此返回时需要主动恢复之前的位置。
const scrollPositions: Record<string, number> = {}
let stopScrollRestore:(()=>void)|undefined

function getScrollContainer(): HTMLElement | null {
  return document.querySelector('[data-route-active="true"] .main-content') || document.querySelector('.main-content')
}

function saveScrollFor(path: string): void {
  const el = getScrollContainer()
  if (!el) return
  scrollPositions[path] = el.scrollTop
  modeSessions.rememberScroll(routeMode(path),path,el.scrollTop)
}

function restoreScrollFor(path: string): void {
  const el = getScrollContainer()
  if (!el) return
  const y = scrollPositions[path] ?? modeSessions.scroll(routeMode(path),path)
  if (typeof y === 'number' && y >= 0) {
    stopScrollRestore?.()
    stopScrollRestore = restoreModeScroll(el,y)
  }
}

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/comic', component: () => import('../comic/views/ComicHome.vue'), meta: { title: 'comicRoom.browse' } },
    { path: '/comic/categories', component: () => import('../comic/views/ComicCatalog.vue'), meta: { title: 'comic.categories' } },
    { path: '/comic/search', component: () => import('../comic/views/ComicCatalog.vue'), meta: { title: 'comic.search' } },
    { path: '/comic/library', component: () => import('../comic/views/ComicLibrary.vue'), meta: { title: 'comic.following' } },
    { path: '/comic/me', redirect: '/comic/settings', meta: { title: 'sidebar.settings' } },
    { path: '/comic/settings', component: () => import('../comic/views/ComicSettings.vue'), meta: { title: 'sidebar.settings' } },
    { path:'/comic/diagnostics',component:()=>import('../views/RuntimeDiagnostics.vue'),meta:{title:'diagnostics.pageTitle'} },
    { path: '/comic/sources', component: () => import('../comic/views/ComicSources.vue'), meta: { title: 'librarySources.comicTitle' } },
    { path: '/comic/series/:seriesId', component: () => import('../comic/views/ComicSeries.vue'), meta: { title: 'comic.details' } },
    { path: '/comic/read/:seriesId/:episode', component: () => import('../comic/views/ComicReader.vue'), meta: { title: 'comic.reading' } },

    { path: '/novel', component: () => import('../novel/views/NovelHome.vue'), meta: { title: 'novelRoom.bookcity' } },
    { path: '/novel/categories', component: () => import('../novel/views/NovelCatalog.vue'), meta: { title: 'novel.categories' } },
    { path: '/novel/search', component: () => import('../novel/views/NovelSearch.vue'), meta: { title: 'novel.search' } },
    { path: '/novel/shelf', component: () => import('../novel/views/NovelShelf.vue'), meta: { title: 'novel.shelf' } },
    { path: '/novel/me', redirect: '/novel/settings', meta: { title: 'sidebar.settings' } },
    { path: '/novel/settings', component: () => import('../novel/views/NovelSettings.vue'), meta: { title: 'sidebar.settings' } },
    { path:'/novel/diagnostics',component:()=>import('../views/RuntimeDiagnostics.vue'),meta:{title:'diagnostics.pageTitle'} },
    { path: '/novel/sources', component: () => import('../novel/views/NovelSources.vue'), meta: { title: 'librarySources.novelTitle' } },
    { path: '/novel/book/:bookId', component: () => import('../novel/views/NovelBook.vue'), meta: { title: 'novel.details' } },
    { path: '/novel/read/:bookId/:chapter', component: () => import('../novel/views/NovelReader.vue'), meta: { title: 'novel.reading' } },

    {
      path: '/manage',
      redirect: '/settings',
    },
    {
      path: '/',
      component: () => import('../views/Home.vue'),
    },
    {
      path: '/search',
      component: () => import('../views/Search.vue'),
    },
    {
      path: '/recent',
      component: () => import('../views/Recent.vue'),
    },
    {
      path: '/detail/:sourceKey/:globalId',
      component: () => import('../views/Detail.vue'),
      props: true,
    },
    {
      name: 'player',
      path: '/player/:sourceKey/:globalId/:epIndex',
      component: () => import('../views/Player.vue'),
      props: true,
    },
    {
      path: '/sources',
      component: () => import('../views/Sources.vue'),
    },
    { path: '/danmaku-sources', component: () => import('../views/DanmakuSources.vue'), meta: { title: 'danmakuSources.title' } },
    {
      path: '/settings',
      component: () => import('../views/Settings.vue'),
    },
    {
      path: '/downloads',
      component: () => import('../views/Downloads.vue'),
    },
    {
      path: '/recycle',
      component: () => import('../views/RecycleBin.vue'),
    },
    {
      path: '/video-types',
      component: () => import('../views/VideoTypes.vue'),
    },
    {
      path: '/favorites',
      component: () => import('../views/Favorites.vue'),
    },
    {
      path: '/history',
      component: () => import('../views/History.vue'),
    },
    {
      path: '/recommendations',
      component: () => import('../views/Recommendations.vue'),
    },
  ],
})

const playerNavigations=new WeakMap<object,number>()
router.beforeEach(to=>{
  if(to.name==='player'){
    playerNavigations.set(to,performance.now())
    void WriteLog({level:'INFO',source:'player',message:'[player.startup] navigation_started',detail:''}).catch(()=>{})
  }
})
router.afterEach((to,_from,failure)=>{
  const began=playerNavigations.get(to)
  if(began===undefined)return
  playerNavigations.delete(to)
  void WriteLog({level:'INFO',source:'player',message:'[player.startup] navigation_settled',detail:JSON.stringify({elapsed_ms:Math.round(performance.now()-began),confirmed:!failure})}).catch(()=>{})
})

// Mode strategy is checked once at the router seam, including restored URLs.
router.beforeEach(async to => {
  const appMode = useApplicationMode()
  await appMode.load()
  if (routeMode(to.path) !== appMode.mode) return modeHome(appMode.mode)
  if (appMode.mode === 'novel') await (await import('../novel/readingState')).useNovelReading().init()
  if (appMode.mode === 'comic') await (await import('../comic/state')).useComicState().init()
  return true
})

// 离开当前路由前先记录 .main-content 的 scrollTop
router.beforeEach((to: RouteLocationNormalized, from: RouteLocationNormalized) => {
  stopScrollRestore?.();stopScrollRestore=undefined
  if (from.path) saveScrollFor(['/settings','/novel/settings','/comic/settings'].includes(from.path) ? routeScreenKey(from) : from.path)
  return true
})

// 进入新路由后：
//   · 如果是从其它页“返回”到一个之前访问过的路径（该路径有记录），恢复滚动位置
//   · 如果是“首次进入”该路径（没有 saved scrollTop），滚到顶部（与浏览器默认行为一致）
//   · 用 nextTick + setTimeout 确保异步组件挂载 + 列表数据渲染完成后再恢复
router.afterEach((to: RouteLocationNormalized, from: RouteLocationNormalized) => {
  modeSessions.rememberRoute(routeMode(to.path),to.fullPath)
  const toPath = ['/settings','/novel/settings','/comic/settings'].includes(to.path) ? routeScreenKey(to) : to.path
  const hasSaved = (scrollPositions[toPath] ?? modeSessions.scroll(routeMode(toPath),toPath)) > 0
  nextTick(() => {
    window.setTimeout(() => {
      if (hasSaved) {
        restoreScrollFor(toPath)
      } else {
        const el = getScrollContainer()
        if (el) el.scrollTop = 0
      }
    }, 0)
  })
})

export default router
