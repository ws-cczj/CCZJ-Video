/**
 * 注入型扩展包样例：设置页「扩展包」里的自检台。
 *
 * 这个文件会被应用读出来、当成 ES 模块跑一遍，并把 `cczj` 作为唯一参数递进来。
 * 所以它不能写 import —— 源码是从 blob URL 加载的，没有模块解析器能接手。
 * Vue、路由、store、后端绑定、localStorage 全都从 cczj 上取，取法见 docs/plugins.md §6.2。
 *
 * 组件写成 h() 渲染函数而不是 <template>：应用打包的是 Vue 运行时版，没有模板编译器。
 */

const PATH = '/pack-inspector'
/** 文案并进应用语言包时用的命名空间；k() 负责拼出完整键。 */
const NS = 'pluginPacks.uiTweaks'

export function setup(cczj) {
  const { h, onMounted, ref } = cczj.vue
  const t = cczj.i18n.t
  const k = (name) => `${NS}.${name}`

  // 命名空间用普通标识符（uiTweaks）而不是包 id：包 id 带连字符时 vue-i18n 的点号路径
  // 得写成 pluginPacks['script-ui-tweaks'].x，样例想让它读起来跟内置键一样。
  cczj.i18n.merge({
    'zh-CN': {
      pluginPacks: {
        uiTweaks: {
          nav: '扩展包自检台',
          lead: '这一页完全由 script-ui-tweaks 这个包画出来：路由、文案、样式、按钮都是包自己的代码，应用只提供挂载点。',
          self: '本包与环境',
          selfHint: 'cczj.pack 是 manifest 摘要，应用版本来自后端绑定 GetAppVersion()，语言是 cczj.i18n.locale。',
          packs: '注册表里的扩展包',
          packsHint: '读的是 cczj.stores.plugins.packs，也就是 Go 侧校验后的结果。',
          intercept: '包住应用自己的方法',
          interceptHint: 'setup 里用 cczj.intercept 包住了 source store 的 loadSources：点下面的按钮，调用次数与耗时由本包记下。',
          events: '订阅后端事件',
          eventsHint: 'cczj.events.on("collect:done") 的最近 5 条；每条同时写进设置页的日志面板，来源标成本包 id。',
          storage: '自己的持久化',
          storageHint: 'cczj.storage 落在 localStorage 的 plugin:script-ui-tweaks: 前缀下，禁用或重扫都不会清掉它。',
          fieldId: '包 id',
          fieldKind: 'kind',
          fieldVersion: '包版本',
          fieldAppVersion: '应用版本',
          fieldLocale: '当前语言',
          fieldDir: '包目录',
          calls: 'loadSources 调用次数',
          lastMs: '最近一次耗时',
          visits: '本页访问次数',
          ms: '{n} ms',
          reloadSources: '立刻重载采集源',
          readVersion: '重新读版本号',
          openSettings: '打开设置页',
          writeLog: '写一条日志',
          resetVisits: '清零访问计数',
          noPacks: '注册表里还没有可用的包。',
          noEvents: '还没有收到 collect:done。',
          stateReady: '已启用',
          stateDisabled: '已停用',
          stateInvalid: '校验未过',
          scriptBroken: '脚本包已隔离：{n} 个',
          loaded: '扩展包自检台已注入',
          collectDone: '收到采集结束事件',
          disposed: '自检台已从界面收回',
        },
      },
    },
    en: {
      pluginPacks: {
        uiTweaks: {
          nav: 'Pack Inspector',
          lead: 'This page is drawn entirely by the script-ui-tweaks pack: the route, copy, styles and buttons are pack code; the app only provides the mount point.',
          self: 'This pack and its environment',
          selfHint: 'cczj.pack is the manifest summary, the app version comes from the GetAppVersion() binding, the locale from cczj.i18n.locale.',
          packs: 'Packs in the registry',
          packsHint: 'Read from cczj.stores.plugins.packs, i.e. what the Go validator accepted.',
          intercept: 'Wrapping an app method',
          interceptHint: 'setup wrapped loadSources on the source store with cczj.intercept: press the button below and this pack records the call count and duration.',
          events: 'Backend events',
          eventsHint: 'The last 5 payloads from cczj.events.on("collect:done"); each also lands in the log panel under this pack id.',
          storage: 'Pack-owned persistence',
          storageHint: 'cczj.storage lives in localStorage under plugin:script-ui-tweaks: and survives disabling or rescanning.',
          fieldId: 'Pack id',
          fieldKind: 'kind',
          fieldVersion: 'Pack version',
          fieldAppVersion: 'App version',
          fieldLocale: 'Locale',
          fieldDir: 'Pack dir',
          calls: 'loadSources calls',
          lastMs: 'Last duration',
          visits: 'Page visits',
          ms: '{n} ms',
          reloadSources: 'Reload sources now',
          readVersion: 'Re-read version',
          openSettings: 'Open settings',
          writeLog: 'Write a log line',
          resetVisits: 'Reset visit counter',
          noPacks: 'No usable pack in the registry yet.',
          noEvents: 'No collect:done received yet.',
          stateReady: 'Enabled',
          stateDisabled: 'Disabled',
          stateInvalid: 'Invalid',
          scriptBroken: 'Quarantined script packs: {n}',
          loaded: 'Pack Inspector injected',
          collectDone: 'collect:done received',
          disposed: 'Pack Inspector tore down',
        },
      },
    },
  })

  const stats = ref({ calls: 0, lastMs: 0 })
  const recent = ref([])
  const visits = ref(0)
  const appVersion = ref('')

  /** 包住 store 的 action：next 是原实现，调它才真正走应用自己的加载逻辑。 */
  cczj.intercept(cczj.stores.source, 'loadSources', async (next) => {
    const started = Date.now()
    try {
      return await next()
    } finally {
      stats.value = { calls: stats.value.calls + 1, lastMs: Date.now() - started }
    }
  })

  /** 事件订阅在包收回时自动退订，不必自己保存返回值。 */
  cczj.events.on('collect:done', (payload) => {
    const done = payload || {}
    recent.value = [{
      at: new Date().toLocaleTimeString(),
      sourceKey: done.source_key || '-',
      error: done.error || '',
    }].concat(recent.value).slice(0, 5)
    cczj.log.info(t(k('collectDone')), JSON.stringify(done))
  })

  async function readVersion() {
    try {
      appVersion.value = await cczj.bindings.GetAppVersion()
    } catch (e) {
      appVersion.value = (e && e.message) || String(e)
    }
  }

  function field(label, value) {
    return h('div', { class: 'pk-ins-field' }, [
      h('span', { class: 'pk-ins-label' }, label),
      h('span', { class: 'pk-ins-value' }, String(value)),
    ])
  }

  function button(label, onClick) {
    return h('button', { class: 'pk-ins-btn', type: 'button', onClick }, label)
  }

  function section(title, hint, children) {
    return h('section', { class: 'pk-ins-section' }, [
      h('h3', { class: 'pk-ins-section-title' }, title),
      h('p', { class: 'pk-ins-hint' }, hint),
    ].concat(children))
  }

  const stateKeys = { ready: 'stateReady', disabled: 'stateDisabled', invalid: 'stateInvalid' }

  const InspectorPage = {
    name: 'PackInspector',
    setup() {
      onMounted(() => {
        visits.value = cczj.storage.get('visits', 0) + 1
        cczj.storage.set('visits', visits.value)
        void cczj.stores.plugins.ensureLoaded()
        void readVersion()
      })

      return () => {
        const plugins = cczj.stores.plugins
        const packs = plugins.packs.map((p) => h('li', { class: 'pk-ins-pack' }, [
          h('span', { class: 'pk-ins-pack-name' }, plugins.localized(p.name) || p.id),
          h('span', { class: 'pk-ins-tag' }, p.kind),
          h('span', { class: `pk-ins-tag pk-ins-tag--${p.status}` }, t(k(stateKeys[p.status] || 'stateInvalid'))),
        ]))
        const broken = Object.keys(plugins.brokenScripts || {}).length

        return h('div', { class: 'pk-ins' }, [
          h('p', { class: 'pk-ins-lead' }, t(k('lead'))),

          section(t(k('self')), t(k('selfHint')), [
            h('div', { class: 'pk-ins-grid' }, [
              field(t(k('fieldId')), cczj.pack.id),
              field(t(k('fieldKind')), cczj.pack.kind),
              field(t(k('fieldVersion')), cczj.pack.version),
              field(t(k('fieldAppVersion')), appVersion.value || '-'),
              field(t(k('fieldLocale')), cczj.i18n.locale),
              field(t(k('fieldDir')), cczj.pack.dir || '-'),
            ]),
            h('div', { class: 'pk-ins-actions' }, [
              button(t(k('readVersion')), () => { void readVersion() }),
              button(t(k('openSettings')), () => { void cczj.router.push('/settings') }),
              button(t(k('writeLog')), () => cczj.log.info(t(k('loaded')), cczj.pack.id)),
            ]),
          ]),

          section(t(k('intercept')), t(k('interceptHint')), [
            h('div', { class: 'pk-ins-grid' }, [
              field(t(k('calls')), stats.value.calls),
              field(t(k('lastMs')), t(k('ms'), { n: stats.value.lastMs })),
            ]),
            h('div', { class: 'pk-ins-actions' }, [
              button(t(k('reloadSources')), () => { void cczj.stores.source.loadSources() }),
            ]),
          ]),

          section(t(k('events')), t(k('eventsHint')), [
            recent.value.length
              ? h('ul', { class: 'pk-ins-list' }, recent.value.map(e =>
                  h('li', { class: 'pk-ins-row' }, `${e.at} · ${e.sourceKey}${e.error ? ` · ${e.error}` : ''}`)))
              : h('p', { class: 'pk-ins-empty' }, t(k('noEvents'))),
          ]),

          section(t(k('packs')), t(k('packsHint')), [
            packs.length
              ? h('ul', { class: 'pk-ins-list' }, packs)
              : h('p', { class: 'pk-ins-empty' }, t(k('noPacks'))),
            broken ? h('p', { class: 'pk-ins-warn' }, t(k('scriptBroken'), { n: broken })) : null,
          ]),

          section(t(k('storage')), t(k('storageHint')), [
            field(t(k('visits')), visits.value),
            h('div', { class: 'pk-ins-actions' }, [
              button(t(k('resetVisits')), () => {
                cczj.storage.remove('visits')
                visits.value = 0
              }),
            ]),
          ]),
        ])
      }
    },
  }

  // 路由先登记、导航后出现：侧边栏一渲染，点进去就一定能落到页面上。
  cczj.route(PATH, InspectorPage)
  cczj.nav({ path: PATH, label: () => t(k('nav')), icon: 'code' })

  // 收回时留一行：禁用或重扫之后，日志面板能证明界面确实撤干净了。
  cczj.onDispose(() => cczj.log.info(t(k('disposed'))))
  cczj.log.info(t(k('loaded')), cczj.pack.version)
}
