/**
 * AI 助手扩展包：把「自然语言 → 应用既有后端绑定」这条链路整个放在包里，应用侧零改动。
 *
 * 三条硬约束决定了它的形状：
 * 1. 包没有 import 能力，界面只能用 h() 渲染函数写（应用打包的是运行时版 Vue，没有模板编译器）。
 * 2. 出网只能靠包自己 fetch：后端没有通用 HTTP 绑定，所以服务商必须允许跨域（CORS），
 *    否则请求会被 WebView 挡掉——这是这条路线唯一的真门槛，界面上也如实提示了。
 * 3. 模型能做什么由下面那张 TOOLS 表白名单决定。删除、清空、覆盖备份、卸载包这类绑定
 *    一律不给，模型再怎么"想帮忙"也碰不到用户的库。会改状态的动作还要过一道确认弹窗。
 *
 * 工具描述（desc）故意不并进 i18n：那是给模型看的提示词，不是给人看的界面文案。
 */

const PATH = '/ai-assistant'
/** 文案命名空间：按规范挂在 pluginPacks.<包名> 下，不撞内置键。 */
const NS = 'pluginPacks.aiAssistant'
const CFG_KEY = 'config'
const CHAT_KEY = 'chat'
/** 一轮回答里最多让模型连续调几次工具，防止它自己跟自己绕圈。 */
const MAX_TOOL_ROUNDS = 6
/** 工具结果回灌给模型时截断到这里：一屏诊断数据就几千字，全塞会把上下文吃光。 */
const MAX_RESULT_CHARS = 6000
/** 界面上每条工具结果只展开这么多字符，剩下的省略。 */
const MAX_SHOWN_CHARS = 320
/** 会话持久化只留最近这么多条，localStorage 不是数据库。 */
const KEEP_MESSAGES = 30

const DEFAULT_CFG = {
  baseUrl: 'https://api.openai.com/v1',
  apiKey: '',
  model: 'gpt-4o-mini',
  temperature: 0.3,
  stream: true,
}

export function setup(cczj) {
  const { h, ref, computed, nextTick, onMounted, onBeforeUnmount } = cczj.vue
  const t = cczj.i18n.t
  const k = (name) => `${NS}.${name}`
  const B = cczj.bindings

  // ===== 工具白名单：name 必须与后端绑定同名，参数按绑定原样透传 =====
  const s = (description) => ({ type: 'string', description })
  const i = (description) => ({ type: 'integer', description })
  const bo = (description) => ({ type: 'boolean', description })
  const obj = (properties, required) => ({
    type: 'object',
    properties,
    required: required || [],
  })

  const TOOLS = [
    { name: 'GetAppVersion', desc: 'App version string. No arguments.', props: {}, run: () => B.GetAppVersion() },
    { name: 'GetDiagnostics', desc: 'Runtime diagnostics: DB size, cache volumes, douban queue, goroutines, uptime.', props: {}, run: () => B.GetDiagnostics() },
    { name: 'GetAllSources', desc: 'All configured collect sources (key, name, api url, enabled, strategy).', props: {}, run: () => B.GetAllSources() },
    { name: 'GetSourceStats', desc: 'Per-source video counts and last-collect info.', props: {}, run: () => B.GetSourceStats() },
    { name: 'SourceProbeTimeline', desc: 'Recent health-probe timeline for every source.', props: {}, run: () => B.SourceProbeTimeline() },
    {
      name: 'GetCollectStatus',
      desc: 'Current collect status of ONE source. Empty source_key returns the global status.',
      props: { source_key: s('source key as in GetAllSources, may be empty') },
      run: (a) => B.GetCollectStatus(String(a.source_key || '')),
    },
    { name: 'ListDownloads', desc: 'Download queue with progress of each task.', props: {}, run: () => B.ListDownloads() },
    { name: 'ListPlugins', desc: 'Extension packs in the registry: id, kind, version, status, builtin flag.', props: {}, run: () => B.ListPlugins() },
    {
      name: 'GetRecentHistory',
      desc: 'Most recent watch-history entries.',
      props: { limit: i('how many entries, 1-50, default 10') },
      run: (a) => B.GetRecentHistory(clampInt(a.limit, 1, 50, 10)),
    },
    {
      name: 'GetFavorites',
      desc: 'Favorite list, paginated.',
      props: { page: i('1-based page'), page_size: i('page size, 1-50, default 20') },
      run: (a) => B.GetFavorites(Math.max(1, clampInt(a.page, 1, 1000, 1)), clampInt(a.page_size, 1, 50, 20)),
    },
    {
      name: 'SearchVideos',
      desc: 'Search the LOCAL library of one source by keyword. This is the right tool for "do I have X".',
      props: {
        source_key: s('which source to search'),
        keyword: s('title / actor / director keyword'),
        page: i('1-based page, default 1'),
        page_size: i('page size, 1-50, default 20'),
      },
      required: ['source_key', 'keyword'],
      run: (a) => B.SearchVideos({
        source_key: String(a.source_key || ''),
        keyword: String(a.keyword || ''),
        page: Math.max(1, clampInt(a.page, 1, 1000, 1)),
        page_size: clampInt(a.page_size, 1, 50, 20),
      }),
    },
    {
      name: 'GetVideoList',
      desc: 'Browse/filter the local library: by type, year, area, sort ("" default, "rating", "hot"), recent_days.',
      props: {
        source_key: s('source key, empty = all'),
        keyword: s('optional keyword'),
        type_id: s('optional type id as string, empty = all'),
        year: s('optional year, "all" or empty = no filter'),
        area: s('optional area, "all" or empty = no filter'),
        sort: s('"" default, "rating", or "hot"'),
        recent_days: i('only entries updated in the last N days, 0 = off'),
        page: i('1-based page'),
        page_size: i('page size, 1-50, default 20'),
      },
      run: (a) => B.GetVideoList({
        source_key: String(a.source_key || ''),
        keyword: String(a.keyword || ''),
        type_id: String(a.type_id || ''),
        year: String(a.year || ''),
        area: String(a.area || ''),
        sort: String(a.sort || ''),
        recent_days: clampInt(a.recent_days, 0, 3650, 0),
        page: Math.max(1, clampInt(a.page, 1, 1000, 1)),
        page_size: clampInt(a.page_size, 1, 50, 20),
      }),
    },
    { name: 'GetGlobalTypes', desc: 'Unified type/category tree across sources (id + names).', props: {}, run: () => B.GetGlobalTypes() },
    {
      name: 'GetYearsAndAreas',
      desc: 'Years and areas available in one source, for building filters.',
      props: { source_key: s('source key') },
      required: ['source_key'],
      run: (a) => B.GetYearsAndAreas(String(a.source_key || '')),
    },
    {
      name: 'GetSetting',
      desc: 'Read one app setting by key. Value comes back as a string.',
      props: { key: s('setting key') },
      required: ['key'],
      run: (a) => B.GetSetting(String(a.key || '')),
    },
    {
      name: 'SetSetting',
      desc: 'Write one app setting. Both key and value are strings. Confirm-gated because it changes app behaviour.',
      mutating: true,
      props: { key: s('setting key'), value: s('new value, serialized as string') },
      required: ['key', 'value'],
      run: (a) => B.SetSetting(String(a.key || ''), String(a.value == null ? '' : a.value)),
    },
    {
      name: 'GetLogRecords',
      desc: 'Recent application log lines (timeline). Empty since_seq reads the newest ones.',
      props: { since_seq: i('sequence watermark from the previous call, 0 = newest'), limit: i('how many, 1-200, default 40') },
      run: (a) => B.GetLogRecords(clampInt(a.since_seq, 0, Number.MAX_SAFE_INTEGER, 0), clampInt(a.limit, 1, 200, 40)),
    },
    {
      name: 'ProbeSource',
      desc: 'Probe ONE source once (reachability + latency). Confirm-gated: it hits the remote site.',
      mutating: true,
      props: { source_key: s('source key') },
      required: ['source_key'],
      run: (a) => B.ProbeSource(String(a.source_key || '')),
    },
    {
      name: 'SearchSource',
      desc: 'Search a REMOTE source for a title (this writes newly found rows into the library). Confirm-gated.',
      mutating: true,
      props: {
        source_key: s('source key'),
        keyword: s('title keyword'),
        page: i('1-based page'),
        page_size: i('page size, 1-50, default 20'),
      },
      required: ['source_key', 'keyword'],
      run: (a) => B.SearchSource(
        String(a.source_key || ''),
        String(a.keyword || ''),
        Math.max(1, clampInt(a.page, 1, 1000, 1)),
        clampInt(a.page_size, 1, 50, 20),
      ),
    },
    {
      name: 'StartCollect',
      desc: 'Start collecting a source. mode: "full" | "incremental" | "once". Confirm-gated.',
      mutating: true,
      props: {
        source_key: s('source key'),
        mode: s('full | incremental | once, empty = full'),
        hours: i('incremental look-back hours, 0 = source default'),
      },
      required: ['source_key'],
      run: (a) => B.StartCollect({
        source_key: String(a.source_key || ''),
        mode: String(a.mode || ''),
        hours: clampInt(a.hours, 0, 8760, 0),
      }),
    },
    {
      name: 'StopCollect',
      desc: 'Stop a running collection (graceful). Confirm-gated.',
      mutating: true,
      props: { source_key: s('source key, empty = the running task') },
      run: (a) => B.StopCollect({ source_key: String(a.source_key || '') }),
    },
    {
      name: 'PauseCollect',
      desc: 'Pause a running collection so it can be resumed. Confirm-gated.',
      mutating: true,
      props: { source_key: s('source key, empty = the running task') },
      run: (a) => B.PauseCollect({ source_key: String(a.source_key || '') }),
    },
    {
      name: 'ResumeCollect',
      desc: 'Resume a paused collection. Confirm-gated.',
      mutating: true,
      props: { source_key: s('source key, empty = the running task') },
      run: (a) => B.ResumeCollect({ source_key: String(a.source_key || '') }),
    },
    {
      name: 'TriggerCollectNow',
      desc: 'Ask the background scheduler to run its due sources right now. Confirm-gated.',
      mutating: true,
      props: {},
      run: (a) => B.TriggerCollectNow(a || {}),
    },
    {
      name: 'SetPluginEnabled',
      desc: 'Enable or disable an extension pack by id. Confirm-gated.',
      mutating: true,
      props: { id: s('pack id as in ListPlugins'), enabled: bo('true to enable') },
      required: ['id', 'enabled'],
      run: (a) => B.SetPluginEnabled(String(a.id || ''), !!a.enabled),
    },
    { name: 'RescanPlugins', desc: 'Rescan the plugins directory so newly dropped packs appear. Confirm-gated.', mutating: true, props: {}, run: () => B.RescanPlugins() },
  ]

  function clampInt(raw, min, max, fallback) {
    const n = Number(raw)
    if (!Number.isFinite(n)) return fallback
    return Math.min(max, Math.max(min, Math.round(n)))
  }

  // ===== 配置 =====
  const cfg = ref(Object.assign({}, DEFAULT_CFG, cczj.storage.get(CFG_KEY, {}) || {}))
  const showCfg = ref(!cfg.value.apiKey)
  const saveNote = ref('')

  const configured = computed(() => !!(cfg.value.apiKey && cfg.value.model))

  function saveCfg() {
    cczj.storage.set(CFG_KEY, {
      baseUrl: String(cfg.value.baseUrl || '').replace(/\/+$/, ''),
      apiKey: cfg.value.apiKey,
      model: cfg.value.model,
      temperature: clampInt(Number(cfg.value.temperature) * 10, 0, 20, 3) / 10,
      stream: !!cfg.value.stream,
    })
    saveNote.value = t(k('saved'))
    setTimeout(() => { saveNote.value = '' }, 2500)
  }

  // ===== 会话 =====
  /**
   * 一条数组同时当"界面渲染源"和"回灌给模型的上下文"。
   * local=true 的是包自己插进去的提示/报错，不参与发给模型的过滤集合。
   */
  const thread = ref(loadThread())
  const draft = ref('')
  const busy = ref(false)
  let controller = null

  function loadThread() {
    const stored = cczj.storage.get(CHAT_KEY, [])
    return Array.isArray(stored) ? stored.filter(validEntry) : []
  }
  function validEntry(m) {
    return !!m && typeof m.content === 'string' && ['user', 'assistant', 'tool', 'note'].indexOf(m.role) >= 0
  }
  function saveThread() {
    cczj.storage.set(CHAT_KEY, thread.value.slice(-KEEP_MESSAGES).map((m) => ({
      role: m.role,
      content: m.content,
      name: m.name || '',
      local: !!m.local,
      level: m.level || '',
    })))
  }
  function push(entry) {
    thread.value = thread.value.concat([entry])
    void scrollToEnd()
    return thread.value[thread.value.length - 1]
  }
  function clearThread() {
    thread.value = []
    saveThread()
    cczj.log.info(t(k('cleared')))
  }

  const listEl = ref(null)
  async function scrollToEnd() {
    await nextTick()
    const el = listEl.value
    if (el) el.scrollTop = el.scrollHeight
  }

  /**
   * 一次新会话的起点：系统提示 + 恢复出来的历史。
   * 这里只取 user/assistant 的纯文本。工具调用与结果不跨请求留档——它们必须和带
   * `tool_calls` 的那条 assistant 成对出现，孤零零一条 `role:'tool'` 会被接口判 400；
   * 本轮的配对由 send() 自己握着的那条 convo 负责。
   */
  function apiMessages() {
    const kept = thread.value
      .filter((m) => !m.local && (m.role === 'user' || m.role === 'assistant'))
      .slice(-20)
      .map((m) => ({ role: m.role, content: m.content }))
    return [{ role: 'system', content: systemPrompt() }].concat(kept)
  }

  function systemPrompt() {
    const names = TOOLS.map((x) => x.name).join(', ')
    return [
      'You are the built-in assistant of "CCZJ Video", a local desktop app that collects,',
      'organizes and plays video resources from user-configured Maccms-style sources.',
      `Locale for user-facing answers: ${cczj.i18n.locale}.`,
      '',
      'You can call these tools (they map to the app backend):',
      names,
      '',
      'Rules:',
      '- Answer in the user locale. Be concise; use short lists over prose when reporting data.',
      '- Never invent numbers: call a tool when the answer depends on app state.',
      '- Deleting videos, emptying history or recycle bin, uninstalling packs, restoring backups',
      '  and editing source credentials are NOT available to you. Say so plainly if asked.',
      '- If a tool returns an error, report the error instead of retrying it in a loop.',
      '- Prefer SearchVideos (local library) over SearchSource (remote) unless the user asks',
      '  to look outward.',
    ].join('\n')
  }

  function toolSpecs() {
    return TOOLS.map((tool) => ({
      type: 'function',
      function: {
        name: tool.name,
        description: tool.desc,
        parameters: obj(tool.props, tool.required),
      },
    }))
  }

  function endpoint() {
    const base = String(cfg.value.baseUrl || '').replace(/\/+$/, '')
    return `${base}/chat/completions`
  }

  /** 一次 chat 请求。流式时把增量写进 live 那条，界面就能看到打字机效果。 */
  async function request(signal, live, convo) {
    const body = {
      model: cfg.value.model,
      temperature: Number(cfg.value.temperature) || 0,
      messages: convo,
      tools: toolSpecs(),
      tool_choice: 'auto',
      stream: !!cfg.value.stream,
    }
    let res
    try {
      res = await fetch(endpoint(), {
        method: 'POST',
        signal,
        headers: {
          'content-type': 'application/json',
          authorization: `Bearer ${cfg.value.apiKey}`,
        },
        body: JSON.stringify(body),
      })
    } catch (e) {
      if (e && e.name === 'AbortError') throw e
      // fetch 在网络层被挡（CORS、DNS、断网）时只会给一个 "Failed to fetch"，
      // 而这恰恰是这条路线最容易撞上的失败，所以单独翻译一次。
      throw new Error(`${t(k('fetchBlocked'))} (${(e && e.message) || e})`)
    }
    if (!res.ok) {
      const detail = (await res.text().catch(() => '')).slice(0, 300)
      throw new Error(`HTTP ${res.status}${detail ? ` · ${detail}` : ''}`)
    }
    const type = res.headers.get('content-type') || ''
    if (!body.stream || type.indexOf('event-stream') < 0) {
      const json = await res.json()
      const msg = (json.choices && json.choices[0] && json.choices[0].message) || {}
      return { content: msg.content || '', toolCalls: msg.tool_calls || [] }
    }
    return await readStream(res, signal, live)
  }

  async function readStream(res, signal, live) {
    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let content = ''
    const calls = []
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) break
      buffer += decoder.decode(chunk.value, { stream: true })
      let nl = buffer.indexOf('\n')
      while (nl >= 0) {
        const line = buffer.slice(0, nl).trim()
        buffer = buffer.slice(nl + 1)
        nl = buffer.indexOf('\n')
        if (!line || line.indexOf('data:') !== 0) continue
        const payload = line.slice(5).trim()
        if (payload === '[DONE]') continue
        let json
        try { json = JSON.parse(payload) } catch (e) { continue }
        const delta = (json.choices && json.choices[0] && json.choices[0].delta) || null
        if (!delta) continue
        if (typeof delta.content === 'string' && delta.content) {
          content += delta.content
          if (live) live.content = content
        }
        if (Array.isArray(delta.tool_calls)) {
          for (const part of delta.tool_calls) {
            const at = part.index == null ? calls.length : part.index
            if (!calls[at]) calls[at] = { id: '', type: 'function', function: { name: '', arguments: '' } }
            if (part.id) calls[at].id = part.id
            if (part.function && part.function.name) calls[at].function.name += part.function.name
            if (part.function && part.function.arguments) calls[at].function.arguments += part.function.arguments
          }
        }
      }
      if (signal && signal.aborted) break
    }
    return { content, toolCalls: calls.filter(Boolean) }
  }

  async function runTool(call) {
    const name = (call.function && call.function.name) || ''
    const tool = TOOLS.filter((x) => x.name === name)[0]
    let args = {}
    try { args = JSON.parse((call.function && call.function.arguments) || '{}') } catch (e) { args = {} }

    let text
    if (!tool) {
      text = JSON.stringify({ error: `tool ${name} is not exposed by this pack` })
    } else if (tool.mutating) {
      const ok = await cczj.stores.confirm.confirm({
        title: t(k('confirmTitle')),
        message: t(k('confirmBody'), { name, args: JSON.stringify(args) }),
        level: 'warn',
      })
      text = ok ? await execTool(tool, args) : JSON.stringify({ cancelled: true })
      if (!ok) push({ role: 'note', level: 'warn', content: t(k('cancelled'), { name }), local: true })
    } else {
      text = await execTool(tool, args)
    }
    // 回灌给模型和留在界面上的必须是同一份截断结果：一屏片单能有好几十 KB，
    // 不截就是让一次工具调用把整个上下文吃掉。
    const capped = text.slice(0, MAX_RESULT_CHARS)
    push({ role: 'tool', name, content: capped, tool_call_id: call.id })
    return capped
  }

  async function execTool(tool, args) {
    try {
      const out = await tool.run(args)
      return typeof out === 'string' ? out : JSON.stringify(out == null ? {} : out)
    } catch (e) {
      const api = cczj.bindings.normalizeApiError ? cczj.bindings.normalizeApiError(e) : null
      return JSON.stringify({ error: (api && api.message) || (e && e.message) || String(e) })
    }
  }

  async function send() {
    const text = String(draft.value || '').trim()
    if (!text || busy.value) return
    if (!configured.value) {
      showCfg.value = true
      push({ role: 'note', level: 'error', content: t(k('needConfig')), local: true })
      return
    }
    push({ role: 'user', content: text })
    draft.value = ''
    busy.value = true
    controller = new AbortController()
    const signal = controller.signal
    /**
     * 这一轮真正发给模型的数组。界面那份 thread 只负责渲染和留档，两者不能互相代替：
     * 模型要求 `tool_calls` 与每条 `tool` 结果按 id 成对出现，而界面只需要一句人话。
     */
    const convo = apiMessages()
    try {
      for (let round = 0; round < MAX_TOOL_ROUNDS; round += 1) {
        const live = push({ role: 'assistant', content: '', local: true })
        let answer
        try {
          answer = await request(signal, live, convo)
        } catch (e) {
          if (e && e.name === 'AbortError') {
            live.level = 'warn'
            live.content = live.content || t(k('stopped'))
            break
          }
          throw e
        }
        const calls = answer.toolCalls || []
        if (!calls.length) {
          live.local = false
          if (!live.content) {
            live.role = 'note'
            live.level = 'warn'
            live.content = t(k('emptyAnswer'))
          }
          break
        }
        // 界面上只说「要调哪些工具」，发给模型的必须是原样那条 tool_calls：
        // 后面的每条 tool 结果都靠 tool_call_id 挂在它身上，缺一次配对接口就 400。
        live.role = 'note'
        live.name = ''
        live.content = t(k('callingTools'), { names: calls.map((c) => (c.function && c.function.name) || '?').join(', ') })
        convo.push({ role: 'assistant', content: answer.content || null, tool_calls: calls })
        for (const call of calls) {
          convo.push({ role: 'tool', tool_call_id: call.id, content: await runTool(call) })
        }
      }
    } catch (e) {
      push({ role: 'note', level: 'error', content: (e && e.message) || String(e), local: true })
      cczj.log.error(t(k('requestFailed')), (e && e.message) || String(e))
    } finally {
      busy.value = false
      controller = null
      saveThread()
      void scrollToEnd()
    }
  }

  function stop() {
    if (controller) controller.abort()
  }

  async function testConnection() {
    if (!cfg.value.apiKey) {
      saveNote.value = t(k('needKey'))
      return
    }
    saveNote.value = t(k('testing'))
    const started = Date.now()
    try {
      // 单独一条 convo：连通性测试不该把用户的真实对话发出去，更不能借道改 thread。
      const answer = await request(null, null, [
        { role: 'system', content: 'Reply with one short word.' },
        { role: 'user', content: 'ping' },
      ])
      saveNote.value = t(k('testOk'), { ms: Date.now() - started, text: (answer.content || '').slice(0, 40) || '-' })
    } catch (e) {
      saveNote.value = t(k('testFail'), { msg: (e && e.message) || String(e) })
    }
  }

  // ===== 界面 =====
  /** cfg 是 ref 包着的对象，深层可写，所以每项给一个可写 computed 就够了。 */
  function modelOf(field) {
    return computed({
      get: () => cfg.value[field],
      set: (v) => { cfg.value[field] = v },
    })
  }
  function field(labelText, node) {
    return h('label', { class: 'ai-field' }, [h('span', { class: 'ai-field-label' }, labelText), node])
  }
  function input(model, placeholder, type) {
    return h('input', {
      class: 'ai-input',
      type: type || 'text',
      value: model.value,
      placeholder,
      onInput: (ev) => { model.value = ev.target.value },
    })
  }
  function button(label, onClick, extraClass, disabled) {
    return h('button', {
      class: ['ai-btn'].concat(extraClass ? [extraClass] : []),
      type: 'button',
      disabled: !!disabled,
      onClick,
    }, label)
  }

  const roleLabel = (m) => {
    if (m.role === 'user') return t(k('roleYou'))
    if (m.role === 'assistant') return t(k('roleAI'))
    if (m.role === 'tool') return t(k('roleTool'))
    return t(k('roleNote'))
  }

  function entry(m) {
    const body = m.role === 'tool'
      ? `${m.name || ''} → ${m.content.length > MAX_SHOWN_CHARS ? `${m.content.slice(0, MAX_SHOWN_CHARS)}…` : m.content}`
      : m.content
    return h('div', { class: ['ai-entry', `ai-entry--${m.role}`, m.level ? `is-${m.level}` : ''] }, [
      h('span', { class: 'ai-entry-role' }, roleLabel(m)),
      h('span', { class: 'ai-entry-text' }, body || '…'),
    ])
  }

  const AssistantPage = {
    name: 'AiAssistant',
    setup() {
      onMounted(() => {
        if (!cczj.stores.plugins.packs.length) void cczj.stores.plugins.ensureLoaded()
        void scrollToEnd()
      })
      onBeforeUnmount(stop)

      return () => h('div', { class: 'ai' }, [
        h('div', { class: 'ai-hd' }, [
          h('div', { class: 'ai-hd-main' }, [
            h('h2', { class: 'ai-title' }, t(k('nav'))),
            h('span', { class: ['ai-state', configured.value ? 'is-ok' : 'is-warn'] },
              configured.value ? `${cfg.value.model} · ${cfg.value.baseUrl}` : t(k('notConfigured'))),
          ]),
          h('div', { class: 'ai-hd-acts' }, [
            button(t(k('settings')), () => { showCfg.value = !showCfg.value }, null, busy.value),
            button(t(k('clear')), clearThread, null, busy.value),
          ]),
        ]),
        h('p', { class: 'ai-lead' }, t(k('lead'))),

        showCfg.value ? h('div', { class: 'ai-cfg' }, [
          field(t(k('baseUrl')), input(modelOf('baseUrl'), t(k('baseUrlHint')))),
          field(t(k('apiKey')), input(modelOf('apiKey'), 'sk-…', 'password')),
          field(t(k('model')), input(modelOf('model'), 'gpt-4o-mini')),
          field(t(k('stream')), h('input', {
            class: 'ai-check',
            type: 'checkbox',
            checked: !!cfg.value.stream,
            onChange: (ev) => { cfg.value.stream = ev.target.checked },
          })),
          h('p', { class: 'ai-hint' }, t(k('corsHint'))),
          h('p', { class: 'ai-hint' }, t(k('keyHint'))),
          h('div', { class: 'ai-cfg-acts' }, [
            button(t(k('save')), saveCfg, 'is-primary'),
            button(t(k('test')), testConnection, null, busy.value),
          ]),
          saveNote.value ? h('p', { class: 'ai-note' }, saveNote.value) : null,
        ]) : null,

        h('div', { class: 'ai-list', ref: listEl }, thread.value.length
          ? thread.value.map(entry)
          : [h('p', { class: 'ai-empty' }, t(k('emptyThread')))]),

        h('div', { class: 'ai-compose' }, [
          h('textarea', {
            class: 'ai-input ai-textarea',
            value: draft.value,
            placeholder: t(k('placeholder')),
            rows: 2,
            disabled: busy.value,
            onKeydown: (ev) => {
              if (ev.key === 'Enter' && !ev.shiftKey) {
                ev.preventDefault()
                void send()
              }
            },
            onInput: (ev) => { draft.value = ev.target.value },
          }),
          h('div', { class: 'ai-compose-acts' }, busy.value
            ? [button(t(k('stop')), stop, 'is-danger')]
            : [button(t(k('send')), () => { void send() }, 'is-primary')]),
        ]),
      ])
    },
  }

  mergeLocale()

  cczj.css(`
.ai { display: flex; flex-direction: column; gap: 10px; height: 100%; padding: 16px 18px 18px; box-sizing: border-box; }
.ai-hd { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; }
.ai-hd-main { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
.ai-hd-acts { display: flex; gap: 8px; flex: none; }
.ai-title { margin: 0; font-size: 17px; font-weight: 600; color: var(--text-primary); }
.ai-state { font-size: 11px; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ai-state.is-ok { color: var(--accent); }
.ai-state.is-warn { color: #d97706; }
.ai-lead { margin: 0; font-size: 12px; line-height: 1.6; color: var(--text-secondary); }
.ai-cfg { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px 14px; padding: 12px; border: 1px solid var(--border); border-radius: 10px; background: var(--bg-card); }
.ai-field { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-secondary); }
.ai-field-label { width: 84px; flex: none; }
.ai-input { flex: 1; min-width: 0; padding: 6px 8px; font-size: 12px; color: var(--text-primary); background: var(--bg-input); border: 1px solid var(--border); border-radius: 6px; outline: none; }
.ai-input:focus { border-color: var(--accent); }
.ai-check { width: 16px; height: 16px; }
.ai-hint { grid-column: 1 / -1; margin: 0; font-size: 11px; line-height: 1.6; color: var(--text-muted); }
.ai-cfg-acts { grid-column: 1 / -1; display: flex; gap: 8px; align-items: center; }
.ai-note { grid-column: 1 / -1; margin: 0; font-size: 11px; color: var(--accent); word-break: break-all; }
.ai-list { flex: 1; min-height: 180px; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 10px; background: var(--bg-card); }
.ai-empty { margin: auto; font-size: 12px; color: var(--text-muted); }
.ai-entry { display: flex; gap: 8px; align-items: flex-start; font-size: 12px; line-height: 1.65; }
.ai-entry-role { flex: none; width: 46px; font-size: 11px; color: var(--text-muted); }
.ai-entry-text { flex: 1; min-width: 0; white-space: pre-wrap; word-break: break-word; color: var(--text-primary); }
.ai-entry--user .ai-entry-text { color: var(--accent-dim); }
.ai-entry--tool .ai-entry-text { color: var(--text-secondary); font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 11px; }
.ai-entry--note.is-error .ai-entry-text { color: #dc2626; }
.ai-entry--note.is-warn .ai-entry-text { color: #d97706; }
.ai-compose { display: flex; gap: 8px; align-items: flex-end; }
.ai-textarea { resize: vertical; font-family: inherit; line-height: 1.5; }
.ai-compose-acts { display: flex; gap: 8px; flex: none; }
.ai-btn { padding: 6px 12px; font-size: 12px; color: var(--text-primary); background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px; cursor: pointer; }
.ai-btn:hover:not(:disabled) { border-color: var(--border-strong); }
.ai-btn:disabled { opacity: .5; cursor: not-allowed; }
.ai-btn.is-primary { color: var(--accent-contrast); background: var(--accent); border-color: var(--accent); }
.ai-btn.is-danger { color: #dc2626; border-color: #fca5a5; }
`)

  cczj.route(PATH, AssistantPage)
  cczj.nav({ path: PATH, label: () => t(k('nav')), icon: 'sparkle' })
  cczj.onDispose(() => {
    stop()
    cczj.log.info(t(k('disposed')))
  })
  cczj.log.info(t(k('loaded')), cczj.pack.version)

  // ===== 文案：zh-CN 与 en 必须成对，包自己的键都挂在 pluginPacks.aiAssistant 下 =====
  function mergeLocale() {
    cczj.i18n.merge({
      'zh-CN': {
        pluginPacks: {
          aiAssistant: {
            nav: 'AI 助手',
            lead: '用自然语言查片库、看采集与下载状态、开关扩展包。模型地址、密钥和模型名在本页配置，请求由扩展包自己发出，应用不为此新增任何后端能力。',
            notConfigured: '未配置模型',
            settings: '配置',
            clear: '清空对话',
            cleared: '对话已清空。',
            fetchBlocked: '请求没能发出去：这个地址不允许跨域（CORS），或者网络不通。',
            baseUrl: '接口地址',
            baseUrlHint: 'OpenAI 兼容地址，例如 https://api.openai.com/v1',
            apiKey: '密钥',
            model: '模型',
            stream: '流式输出',
            corsHint: '必须允许跨域（CORS）：请求是从应用界面直接发出去的，服务商不放行的话会在网络层被挡掉。不支持的话请自建中转，或让应用侧开一个通用 HTTP 绑定。',
            keyHint: '密钥存在本机 localStorage 里，是明文。这是个人自用软件的取舍，别把这份配置同步到公共环境。',
            save: '保存',
            saved: '已保存到本机',
            test: '测试连接',
            testing: '正在请求…',
            testOk: '连通 {ms} ms · 回话：{text}',
            testFail: '失败：{msg}',
            needKey: '先填密钥',
            needConfig: '还没配好模型：先在本页填接口地址、密钥和模型名。',
            roleYou: '你',
            roleAI: '助手',
            roleTool: '工具',
            roleNote: '提示',
            placeholder: '问点什么，例如「现在哪个源在采集」「我收藏里有哪些是 2024 年的」',
            send: '发送',
            stop: '停止',
            stopped: '已停止本次请求。',
            emptyAnswer: '模型没有返回内容。',
            callingTools: '要调用工具：{names}',
            confirmTitle: 'AI 助手要改动应用',
            confirmBody: '模型请求执行 {name}：{args}\n这会改动应用状态，确认执行吗？',
            cancelled: '已取消 {name}，模型那边收到「cancelled」。',
            requestFailed: 'AI 请求失败',
            emptyThread: '还没有对话。',
            loaded: 'AI 助手已注入',
            disposed: 'AI 助手已从界面收回',
          },
        },
      },
      en: {
        pluginPacks: {
          aiAssistant: {
            nav: 'AI Assistant',
            lead: 'Ask in natural language: query the library, read collect and download status, toggle extension packs. Endpoint, key and model are configured here; requests go out from the pack itself, so the app gains no new backend capability.',
            notConfigured: 'Model not configured',
            settings: 'Settings',
            clear: 'Clear chat',
            cleared: 'Chat cleared.',
            fetchBlocked: 'The request never left: this endpoint does not allow cross-origin calls (CORS), or the network is unreachable.',
            baseUrl: 'Endpoint',
            baseUrlHint: 'OpenAI-compatible base, e.g. https://api.openai.com/v1',
            apiKey: 'API key',
            model: 'Model',
            stream: 'Stream',
            corsHint: 'CORS must be allowed: requests leave from the app UI directly, so a provider that withholds it fails at the network layer. Use your own proxy, or add a general HTTP binding on the app side.',
            keyHint: 'The key lives in localStorage in plain text. That is the trade-off of a personal app — do not sync this profile anywhere public.',
            save: 'Save',
            saved: 'Saved on this machine',
            test: 'Test',
            testing: 'Requesting…',
            testOk: 'Round trip {ms} ms · said: {text}',
            testFail: 'Failed: {msg}',
            needKey: 'Fill in the API key first',
            needConfig: 'No model configured yet: set endpoint, key and model name on this page.',
            roleYou: 'You',
            roleAI: 'AI',
            roleTool: 'Tool',
            roleNote: 'Note',
            placeholder: 'Ask something, e.g. "which source is collecting now" or "what did I favourite from 2024"',
            send: 'Send',
            stop: 'Stop',
            stopped: 'Request stopped.',
            emptyAnswer: 'The model returned nothing.',
            callingTools: 'Calling tools: {names}',
            confirmTitle: 'AI assistant wants to change the app',
            confirmBody: 'The model asked to run {name}: {args}\nThis changes app state. Run it?',
            cancelled: 'Cancelled {name}; the model received "cancelled".',
            requestFailed: 'AI request failed',
            emptyThread: 'No messages yet.',
            loaded: 'AI Assistant injected',
            disposed: 'AI Assistant tore down',
          },
        },
      },
    })
  }
}
