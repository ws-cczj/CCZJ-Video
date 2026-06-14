import { nextTick } from 'vue'
import { createRouter, createWebHashHistory, type RouteLocationNormalized } from 'vue-router'

// ====== 滚动位置管理：记录每个路由路径在 .main-content 上的 scrollTop ======
// 背景：App.vue 中滚动容器是 .main-content（overflow-y: auto），不是 window。
// 路由懒加载导致离开时组件销毁，返回时组件重新挂载；而 .main-content 在详情页
// 可能被滚动到其它位置，因此返回时需要主动恢复之前的位置。
const scrollPositions: Record<string, number> = {}

function getScrollContainer(): HTMLElement | null {
  return document.querySelector('.main-content')
}

function saveScrollFor(path: string): void {
  const el = getScrollContainer()
  if (!el) return
  scrollPositions[path] = el.scrollTop
}

function restoreScrollFor(path: string): void {
  const el = getScrollContainer()
  if (!el) return
  const y = scrollPositions[path]
  if (typeof y === 'number' && y >= 0) {
    el.scrollTop = y
  }
}

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: '/',
      component: () => import('../views/Home.vue'),
    },
    {
      path: '/search',
      component: () => import('../views/Search.vue'),
    },
    {
      path: '/detail/:sourceKey/:vodId',
      component: () => import('../views/Detail.vue'),
      props: true,
    },
    {
      path: '/player/:sourceKey/:vodId/:epIndex',
      component: () => import('../views/Player.vue'),
      props: true,
    },
    {
      path: '/sources',
      component: () => import('../views/Sources.vue'),
    },
    {
      path: '/settings',
      component: () => import('../views/Settings.vue'),
    },
    {
      path: '/downloads',
      component: () => import('../views/Downloads.vue'),
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
      path: '/dev-admin',
      component: () => import('../views/DevAdmin.vue'),
    },
  ],
})

// 离开当前路由前先记录 .main-content 的 scrollTop
router.beforeEach((to: RouteLocationNormalized, from: RouteLocationNormalized) => {
  if (from.path) saveScrollFor(from.path)
  return true
})

// 进入新路由后：
//   · 如果是从其它页“返回”到一个之前访问过的路径（该路径有记录），恢复滚动位置
//   · 如果是“首次进入”该路径（没有 saved scrollTop），滚到顶部（与浏览器默认行为一致）
//   · 用 nextTick + setTimeout 确保异步组件挂载 + 列表数据渲染完成后再恢复
router.afterEach((to: RouteLocationNormalized, from: RouteLocationNormalized) => {
  const toPath = to.path
  const hasSaved = typeof scrollPositions[toPath] === 'number' && scrollPositions[toPath] > 0
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
