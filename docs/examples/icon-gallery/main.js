/**
 * 图标画廊：一个专门用来看「图标有没有显示出来」的扩展包。
 *
 * 三处图标，全部来自这个文件夹本身：
 *   1. 扩展包卡片上那枚 —— manifest 的 "icon": "icon.png"，由应用去包目录读；
 *   2. 侧边栏那一项 —— cczj.nav 的 icon 用的是应用内置图标名（不是包里的文件）；
 *   3. 这一页的 26 枚 —— 先读 icons.json 拿到清单，再逐张 readAsset 出 data URL。
 *
 * 和别的 script 包一样：不能写 import（源码从 blob URL 加载，没有模块解析器），
 * 组件写成 h() 渲染函数（应用打包的是 Vue 运行时版，没有模板编译器）。
 */

const PATH = '/icon-gallery'
/** 文案并进应用语言包时的命名空间；k() 负责拼出完整键。 */
const NS = 'pluginPacks.iconGallery'
/** 清单与图标都在包目录里，路径一律写成包内相对路径。 */
const LIST_FILE = 'icons.json'
const ICON_DIR = 'icons/'

export function setup(cczj) {
  const { computed, h, onMounted, ref } = cczj.vue
  const t = cczj.i18n.t
  const k = (name) => `${NS}.${name}`

  cczj.i18n.merge({
    'zh-CN': {
      pluginPacks: {
        iconGallery: {
          nav: '图标画廊',
          lead: '这一页的 26 枚图标都是包目录 icons/ 里的 PNG，运行时一张张读进来画成 <img>。看到图就说明「文件夹 → Go → 界面」这条链路是通的。',
          filter: '按名字或文件名筛选',
          count: '{n} / {total} 枚',
          loading: '正在从包目录读图标…',
          empty: '没有匹配的图标。',
          failed: '读不到 {file}，这一页没有内容可画。',
          missing: '这张没读出来',
          card: '卡片图标',
          cardHint: '扩展包面板上那枚就是它：manifest 里写 "icon": "icon.png"，应用会去包目录把这张图读出来显示。',
          navIcon: '侧边栏图标',
          navIconHint: '侧边栏这一项用的是应用内置的图标名（这里写的是 image），不是包里的文件——包不能替换应用的图标集。',
          hint: '把整个文件夹拖到「设置 → 扩展包」那张卡片上就能装；改完 icons/ 里的图再点「重新扫描」，这一页会跟着换。',
          loaded: '图标画廊已注入',
          disposed: '图标画廊已从界面收回',
        },
      },
    },
    en: {
      pluginPacks: {
        iconGallery: {
          nav: 'Icon Gallery',
          lead: 'All 26 tiles here are PNG files in this pack\'s icons/ folder, read one by one at runtime and drawn as <img>. If they show up, the folder → Go → UI path works.',
          filter: 'Filter by name or file',
          count: '{n} / {total} tiles',
          loading: 'Reading icons from the pack folder…',
          empty: 'No icon matches.',
          failed: 'Could not read {file}; nothing to draw here.',
          missing: 'not read',
          card: 'Card icon',
          cardHint: 'That is the one on the pack card: the manifest says "icon": "icon.png" and the app reads that file out of the pack folder.',
          navIcon: 'Sidebar icon',
          navIconHint: 'The sidebar entry uses a built-in app icon name (image here), not a file from the pack - packs cannot replace the app icon set.',
          hint: 'Drag this whole folder onto Settings > Extensions to install it. Replace files under icons/ and press Rescan - this page follows.',
          loaded: 'Icon Gallery injected',
          disposed: 'Icon Gallery tore down',
        },
      },
    },
  })

  /** 清单原文：[{ file, zh-CN, en }]。读不到就是空数组。 */
  const items = ref([])
  /** file -> data URL。某一张读挂了只少那一张，不牵连整页。 */
  const urls = ref({})
  const loading = ref(false)
  const failed = ref('')
  const query = ref('')
  const cardURL = ref('')

  function labelOf(item) {
    return item[cczj.i18n.locale] || item.en || item.file
  }

  const shown = computed(() => {
    const q = query.value.trim().toLowerCase()
    if (!q) return items.value
    return items.value.filter((item) =>
      item.file.toLowerCase().includes(q) || labelOf(item).toLowerCase().includes(q))
  })

  async function load() {
    if (loading.value) return
    loading.value = true
    const plugins = cczj.stores.plugins
    const packId = cczj.pack.id

    if (cczj.pack.icon) {
      cardURL.value = await plugins.readAsset(packId, cczj.pack.icon)
    }
    const text = await plugins.readText(packId, LIST_FILE)
    if (!text) {
      failed.value = t(k('failed'), { file: LIST_FILE })
      loading.value = false
      return
    }
    let list = []
    try {
      list = JSON.parse(text)
    } catch (e) {
      failed.value = `${t(k('failed'), { file: LIST_FILE })}: ${e && e.message}`
      loading.value = false
      return
    }
    failed.value = ''
    items.value = Array.isArray(list) ? list : []
    const next = {}
    await Promise.all(items.value.map(async (item) => {
      next[item.file] = await plugins.readAsset(packId, ICON_DIR + item.file)
    }))
    urls.value = next
    loading.value = false
  }

  function tile(item) {
    const url = urls.value[item.file]
    return h('figure', { class: 'pk-ig-cell', key: item.file }, [
      url
        ? h('img', { class: 'pk-ig-img', src: url, alt: item.file, loading: 'lazy' })
        : h('span', { class: 'pk-ig-missing' }, t(k('missing'))),
      h('figcaption', { class: 'pk-ig-caption' }, [
        h('span', { class: 'pk-ig-name' }, labelOf(item)),
        h('span', { class: 'pk-ig-file' }, ICON_DIR + item.file),
      ]),
    ])
  }

  function note(title, hint) {
    return h('div', { class: 'pk-ig-note' }, [
      h('span', { class: 'pk-ig-note-title' }, title),
      h('span', { class: 'pk-ig-note-hint' }, hint),
    ])
  }

  const GalleryPage = {
    name: 'IconGallery',
    setup() {
      onMounted(() => { void load() })
      return () => h('div', { class: 'pk-ig' }, [
        h('p', { class: 'pk-ig-lead' }, t(k('lead'))),

        h('div', { class: 'pk-ig-bar' }, [
          h('input', {
            class: 'pk-ig-input',
            type: 'search',
            value: query.value,
            placeholder: t(k('filter')),
            onInput: (e) => { query.value = e.target.value },
          }),
          h('span', { class: 'pk-ig-count' }, t(k('count'), { n: shown.value.length, total: items.value.length })),
        ]),

        failed.value ? h('p', { class: 'pk-ig-fail' }, failed.value) : null,

        shown.value.length
          ? h('div', { class: 'pk-ig-grid' }, shown.value.map(tile))
          : h('p', { class: 'pk-ig-empty' }, loading.value ? t(k('loading')) : t(k('empty'))),

        cczj.pack.icon
          ? h('div', { class: 'pk-ig-card' }, [
              cardURL.value
                ? h('img', { class: 'pk-ig-card-img', src: cardURL.value, alt: cczj.pack.icon })
                : h('span', { class: 'pk-ig-missing' }, t(k('missing'))),
              note(t(k('card')), t(k('cardHint'))),
            ])
          : null,

        note(t(k('navIcon')), t(k('navIconHint'))),
        h('p', { class: 'pk-ig-hint' }, t(k('hint'))),
      ])
    },
  }

  cczj.route(PATH, GalleryPage)
  cczj.nav({ path: PATH, label: () => t(k('nav')), icon: 'image' })
  cczj.onDispose(() => cczj.log.info(t(k('disposed'))))
  cczj.log.info(t(k('loaded')), cczj.pack.version)
}
