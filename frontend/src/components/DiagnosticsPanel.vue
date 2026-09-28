<script setup lang="ts">
defineOptions({ name: 'DiagnosticsPanel' })
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import { Button } from './ui'
import { GetDiagnostics, ProbeSources, ListIdentityMergeCandidates, MergeGlobalVideoIdentities } from '../api/app'
import { useErrorStore } from '../stores/error'
import { useConfirmStore } from '../stores/confirm'
import { useVideoStore } from '../stores/video'

const { t } = useI18n()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const videoStore = useVideoStore()

const diag = ref<Record<string, any> | null>(null)
const probes = ref<Record<string, any>[]>([])
const loading = ref(false)
const probing = ref(false)
const updatedAt = ref(0)

const env = computed(() => diag.value?.env || {})
const storage = computed(() => diag.value?.storage || {})
const douban = computed(() => diag.value?.douban || {})
const health = computed(() => douban.value.health || {})
const collect = computed(() => diag.value?.collect || {})
const sources = computed<Record<string, any>[]>(() => diag.value?.sources || [])
const tables = computed<Record<string, any>[]>(() => diag.value?.tables || [])
const notes = computed<string[]>(() => diag.value?.notes || [])
const backgroundTasks = computed(() => {
  const tasks = env.value.background_tasks || {}
  return Object.keys(tasks).map(name => ({ name, count: tasks[name] }))
})

// 身份合并队列单独拉：诊断快照是只读统计，候选组要能被用户一条条消化掉。
const CANDIDATE_LIMIT = 60
const candidates = ref<Record<string, any>[]>([])
const candidatesLoading = ref(false)
const mergingKey = ref('')

function candidateKey(group: Record<string, any>): string {
  return `${group.reason}:${group.key}`
}

function reasonLabel(reason: string): string {
  return reason === 'douban_id' ? t('diagnostics.mergeReasonDoubanId') : t('diagnostics.mergeReasonSameName')
}

async function loadCandidates(): Promise<void> {
  candidatesLoading.value = true
  try {
    const resp = (await ListIdentityMergeCandidates(CANDIDATE_LIMIT)) as unknown as Record<string, any> | null
    candidates.value = resp && Array.isArray(resp.groups) ? resp.groups : []
  } catch (e: any) {
    candidates.value = []
    errorStore.fromError(t('diagnostics.mergeLoadFailed'), e, 'DiagnosticsPanel.loadCandidates')
  } finally {
    candidatesLoading.value = false
  }
}

/**
 * 一次只并一组，并且必须用户点头：候选是「疑似」，机器不敢替人决定。
 * 合并改的是 global_id 归属，收藏/历史/目录引用都会被搬走，所以成功后要广播刷新。
 */
async function mergeGroup(group: Record<string, any>): Promise<void> {
  const rows = Array.isArray(group.rows) ? group.rows : []
  const ids = rows.map((r: any) => Number(r.global_id) || 0).filter((n: number) => n > 0)
  if (ids.length < 2) return
  const key = candidateKey(group)
  if (mergingKey.value) return
  const ok = await confirmStore.confirm({
    title: t('diagnostics.mergeConfirmTitle'),
    message: t('diagnostics.mergeConfirm', { names: rows.map((r: any) => r.vod_name).join(' / ') }),
    level: 'warn',
  })
  if (!ok) return
  mergingKey.value = key
  try {
    await MergeGlobalVideoIdentities({ global_ids: ids } as any)
    errorStore.info(t('diagnostics.mergeDone'), '', '', 'DiagnosticsPanel.mergeGroup')
    videoStore.notifyRefresh()
    await load()
  } catch (e: any) {
    errorStore.fromError(t('diagnostics.mergeFailed'), e, 'DiagnosticsPanel.mergeGroup')
  } finally {
    mergingKey.value = ''
  }
}


// 探测结果按源键索引，方便和统计表并排显示。
const probeByKey = computed<Record<string, Record<string, any>>>(() => {
  const map: Record<string, Record<string, any>> = {}
  for (const probe of probes.value) map[probe.source_key] = probe
  return map
})

async function load(): Promise<void> {
  loading.value = true
  try {
    // 候选队列跟着一起刷新：合并完不重拉，队列里还会留着刚被并掉的那组。
    const [snapshot] = await Promise.all([GetDiagnostics(), loadCandidates()])
    diag.value = snapshot as unknown as Record<string, any> | null
    updatedAt.value = Date.now()
  } catch (e: any) {
    errorStore.fromError(t('diagnostics.loadFailed'), e, 'DiagnosticsPanel.load')
  } finally {
    loading.value = false
  }
}

async function probe(): Promise<void> {
  probing.value = true
  try {
    probes.value = (await ProbeSources()) as unknown as Record<string, any>[] || []
    // 探测现在会落一条巡检样本，不重新拉一次诊断数据，健康度列就还是旧的。
    await load()
  } catch (e: any) {
    probes.value = []
    errorStore.fromError(t('diagnostics.probeFailed'), e, 'DiagnosticsPanel.probe')
  } finally {
    probing.value = false
  }
}

function fmtBytes(bytes: number): string {
  const value = Number(bytes) || 0
  if (value >= 1 << 30) return `${(value / (1 << 30)).toFixed(2)} GB`
  if (value >= 1 << 20) return `${(value / (1 << 20)).toFixed(1)} MB`
  if (value >= 1 << 10) return `${(value / (1 << 10)).toFixed(1)} KB`
  return `${value} B`
}

function fmtSeconds(seconds: number): string {
  const total = Math.max(0, Number(seconds) || 0)
  if (total < 60) return t('diagnostics.seconds', { n: total })
  const minutes = Math.floor(total / 60)
  if (minutes < 60) return t('diagnostics.minutes', { n: minutes, rest: total % 60 })
  return t('diagnostics.hours', { h: Math.floor(minutes / 60), m: minutes % 60 })
}

function fmtTime(unix: number): string {
  if (!unix) return t('diagnostics.never')
  return new Date(unix * 1000).toLocaleString()
}

function fmtNumber(value: number): string {
  return (Number(value) || 0).toLocaleString()
}

interface LastCollectTag { cls: string; text: string; title: string }

// 缺整页的采集不能被显示成一次干净的完成，否则少数据只藏在日志里看不出来。
function lastCollectTag(source: Record<string, any>): LastCollectTag {
  const kind = String(source.last_error_kind || '')
  const secs = Math.round((Number(source.last_elapsed_ms) || 0) / 1000)
  const saved = fmtNumber(Number(source.last_saved) || 0)
  if (kind === 'partial') {
    return {
      cls: 'warn',
      text: t('sources.lastRunPartialShort'),
      title: t('sources.lastRunPartial', {
        saved,
        fetch: Number(source.last_fetch_failed_pages) || 0,
        save: Number(source.last_save_failed_pages) || 0,
        secs,
      }),
    }
  }
  if (kind) {
    return { cls: 'bad', text: t('sources.lastRunFailedShort'), title: String(source.last_error || '') }
  }
  if (!source.last_finished_at_unix) return { cls: 'muted', text: '—', title: '' }
  return {
    cls: 'ok',
    text: t('sources.lastRunOk', { saved, secs }),
    title: fmtTime(Number(source.last_finished_at_unix)),
  }
}

// 水位线代表"增量采集已经覆盖到哪一刻"。看不到它就无法判断停机这段时间会不会被补上，
// 所以它必须和上次采集结果并排显示。
function watermarkText(source: Record<string, any>): string {
  const covered = Number(source.covered_until_unix) || 0
  if (!covered) return t('diagnostics.watermarkNone')
  return t('diagnostics.watermarkCovered', { time: fmtTime(covered) })
}

function watermarkHint(source: Record<string, any>): string {
  const attempt = Number(source.last_attempt_unix) || 0
  if (!attempt) return ''
  return t('diagnostics.watermarkLastAttempt', { time: fmtTime(attempt) })
}

// 采集与巡检两种样本共用一条时间线，靠形状区分。
const patrolKind = 'patrol'

// 样本按"新→旧"下发，画成时间线必须反过来，
// 否则用户读到的最近一次永远在最左边。
function healthDots(source: Record<string, any>): Record<string, any>[] {
  const samples = (source.recent_samples || []) as Record<string, any>[]
  return [...samples].reverse()
}

function sampleTitle(sample: Record<string, any>): string {
  const kind = sample.kind === patrolKind
    ? t('diagnostics.healthKindPatrol')
    : t('diagnostics.healthKindCollect')
  const head = t('diagnostics.healthDot', { kind, time: fmtTime(Number(sample.ts_unix) || 0) })
  if (!sample.ok) return `${head} · ${String(sample.err || '')}`
  if (sample.kind === patrolKind) return `${head} · ${Number(sample.latency_ms) || 0}ms`
  return `${head} · ${t('diagnostics.healthDotSaved', { n: fmtNumber(Number(sample.saved) || 0) })}`
}

interface HealthTag { cls: string; text: string; title: string }

// 采集与巡检的成功率必须各算各的：一轮全量采集是几十秒、上百页，
// 一次巡检是几十毫秒的一个请求，混成一个数就什么都看不出来。
function gradeHealth(health: Record<string, any> | null | undefined, text: () => string): HealthTag {
  const sample = health || {}
  if (!(Number(sample.samples) || 0)) {
    return { cls: 'muted', text: t('diagnostics.healthNone'), title: t('diagnostics.healthHint') }
  }
  const streak = Number(sample.fail_streak) || 0
  const rate = Number(sample.success_rate) || 0
  return {
    cls: streak > 0 ? 'bad' : rate >= 100 ? 'ok' : rate >= 50 ? 'warn' : 'bad',
    text: streak > 0 ? `${text()} · ${t('diagnostics.healthStreak', { n: streak })}` : text(),
    title: String(sample.last_error || ''),
  }
}

function collectHealthTag(health: Record<string, any> | null | undefined): HealthTag {
  return gradeHealth(health, () => t('diagnostics.healthCollect', { rate: Number((health || {}).success_rate) || 0 }))
}

function patrolHealthTag(health: Record<string, any> | null | undefined): HealthTag {
  const sample = health || {}
  const rate = Number(sample.success_rate) || 0
  const ms = Number(sample.avg_latency_ms) || 0
  // 平均延迟只统计成功的那几次：一次都没成功时它是 0，写成"0ms"会被读成"接口飞快"。
  return gradeHealth(sample, () => (ms > 0
    ? t('diagnostics.healthPatrolMs', { rate, ms })
    : t('diagnostics.healthPatrol', { rate })))
}

onMounted(() => { void load() })
</script>

<template>
  <div class="diag cczj-flex cczj-flex-col cczj-gap-2">
    <section class="block">
      <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
        <h3>{{ t('diagnostics.title') }}</h3>
        <div class="cczj-flex cczj-items-center cczj-gap-4">
          <small class="hint">{{ updatedAt ? t('diagnostics.updatedAt', { time: new Date(updatedAt).toLocaleTimeString() }) : '' }}</small>
          <Button variant="secondary" size="sm" :loading="loading" @click="load">
            <Icon name="refresh" :size="12" /> {{ t('common.refresh') }}
          </Button>
        </div>
      </div>

      <p class="desc">{{ t('diagnostics.readonlyNote') }}</p>

      <div v-if="notes.length" class="diag-notes cczj-flex cczj-flex-col cczj-gap-2">
        <div v-for="(note, index) in notes" :key="`note-${index}`" class="diag-note">
          <Icon name="alert-triangle" :size="12" /> {{ note }}
        </div>
      </div>
    </section>

    <!-- ========== 运行环境 ========== -->
    <section class="block">
      <h3>{{ t('diagnostics.env') }}</h3>
      <div class="diag-grid cczj-grid">
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.appVersion') }}</div>
          <div class="diag-value">{{ env.app_version || '—' }}</div>
          <div class="diag-note-inline">{{ env.installed_marker ? t('diagnostics.installedFrom', { v: env.installed_marker }) : '' }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.uptime') }}</div>
          <div class="diag-value">{{ fmtSeconds(env.uptime_seconds) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.startedAt', { time: fmtTime(env.started_at_unix) }) }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.runtime') }}</div>
          <div class="diag-value">{{ env.go_version || '—' }}</div>
          <div class="diag-note-inline">Wails {{ env.wails_version || '—' }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.platform') }}</div>
          <div class="diag-value">{{ env.goos || '—' }} / {{ env.goarch || '—' }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.cpuCount', { n: env.num_cpu || 0 }) }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.goroutines') }}</div>
          <div class="diag-value">{{ env.goroutines ?? 0 }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.gcCycles', { n: env.num_gc ?? 0 }) }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.memory') }}</div>
          <div class="diag-value">{{ fmtBytes((env.heap_alloc_kb || 0) * 1024) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.memorySys', { size: fmtBytes((env.sys_mem_kb || 0) * 1024) }) }}</div>
        </div>
      </div>

      <div class="diag-kv cczj-flex cczj-flex-col">
        <div class="diag-kv-row">
          <span class="k">{{ t('diagnostics.dataDir') }}</span>
          <span class="v cczj-truncate" :title="env.data_dir">{{ env.data_dir || '—' }}</span>
        </div>
        <div class="diag-kv-row">
          <span class="k">{{ t('diagnostics.executable') }}</span>
          <span class="v cczj-truncate" :title="env.executable">{{ env.executable || '—' }}</span>
        </div>
        <div class="diag-kv-row">
          <span class="k">{{ t('diagnostics.backgroundTasks') }}</span>
          <span class="v">
            <template v-if="backgroundTasks.length">
              <span v-for="task in backgroundTasks" :key="task.name" class="diag-tag">{{ task.name }} · {{ task.count }}</span>
            </template>
            <template v-else>—</template>
          </span>
        </div>
      </div>
    </section>

    <!-- ========== 存储 ========== -->
    <section class="block">
      <h3>{{ t('diagnostics.storage') }}</h3>
      <div class="diag-grid cczj-grid">
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.database') }}</div>
          <div class="diag-value">{{ fmtBytes(storage.database_bytes) }}</div>
          <div class="diag-note-inline cczj-truncate" :title="storage.database_path">{{ t('diagnostics.schemaVersion', { v: env.schema_version ?? 0 }) }} · {{ storage.database_path || '—' }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.memoryCache') }}</div>
          <div class="diag-value">{{ fmtBytes(storage.detail_bytes ?? 0) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.memoryCacheNote', {
            entries: storage.detail_entries ?? 0,
            chart: storage.chart_matches ?? 0,
            comments: storage.comment_pages ?? 0,
          }) }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.logFiles') }}</div>
          <div class="diag-value">{{ storage.log_files ?? 0 }} · {{ fmtBytes(storage.log_bytes) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.keepDays', { days: storage.log_keep_days ?? 0 }) }}</div>
        </div>
      </div>

      <div class="diag-sub">{{ t('diagnostics.tables') }}</div>
      <div class="diag-table-wrap">
        <table class="diag-table">
          <tbody>
            <tr v-for="table in tables" :key="table.name">
              <td class="name">{{ table.name }}</td>
              <td class="num">{{ fmtNumber(table.rows) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- ========== 采集调度 ========== -->
    <section class="block">
      <h3>{{ t('diagnostics.collect') }}</h3>
      <div class="diag-grid cczj-grid">
        <div class="diag-card">
          <div class="diag-label">{{ t('common.status') }}</div>
          <div class="diag-value">{{ collect.running ? t('diagnostics.running') : t('diagnostics.stopped') }}</div>
          <div class="diag-note-inline">
            {{ collect.background ? t('diagnostics.globalTimer', { every: fmtSeconds(collect.every_seconds) }) : t('diagnostics.perSourceTimers') }}
          </div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.gaps') }}</div>
          <div class="diag-value">{{ collect.source_gap_seconds ?? 0 }}s · {{ collect.page_gap_seconds ?? 0 }}s</div>
          <div class="diag-note-inline">{{ t('diagnostics.gapsNote') }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.lastRun') }}</div>
          <div class="diag-value">{{ fmtTime(collect.last_run_unix) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.lastExit', { time: fmtTime(collect.last_exit_unix) }) }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.activity') }}</div>
          <div class="diag-value">{{ collect.active_collects ?? 0 }} / {{ collect.scheduled_sources ?? 0 }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.activeOfScheduled') }}</div>
        </div>
      </div>
    </section>

    <!-- ========== 豆瓣补全 ========== -->
    <section class="block">
      <h3>{{ t('diagnostics.douban') }}</h3>
      <div class="diag-grid cczj-grid">
        <div class="diag-card">
          <div class="diag-label">{{ t('common.status') }}</div>
          <div class="diag-value">{{ douban.running ? t('diagnostics.running') : t('diagnostics.stopped') }}</div>
          <div class="diag-note-inline">{{ douban.updating ? t('diagnostics.updatingNow') : t('diagnostics.idleNow') }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.nextTick') }}</div>
          <div class="diag-value">{{ fmtTime(douban.next_tick_unix) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.everyMinutes', { n: douban.interval_minutes ?? 0 }) }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.lastTick') }}</div>
          <div class="diag-value">{{ fmtTime(douban.last_tick_unix) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.lastTickNote') }}</div>
        </div>
        <div class="diag-card" :class="{ warn: (douban.silent_strikes ?? 0) > 0 }">
          <div class="diag-label">{{ t('diagnostics.anticrawl') }}</div>
          <div class="diag-value">
            {{ douban.silent_remaining_sec > 0 ? fmtSeconds(douban.silent_remaining_sec) : t('diagnostics.clear') }}
          </div>
          <div class="diag-note-inline">{{ t('diagnostics.strikes', { n: douban.silent_strikes ?? 0 }) }}</div>
        </div>
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.challenge') }}</div>
          <div class="diag-value">{{ douban.challenge_at_unix ? (douban.challenge_solved ? t('diagnostics.solved') : t('diagnostics.failed')) : t('diagnostics.none') }}</div>
          <div class="diag-note-inline">
            {{ douban.challenge_at_unix
              ? t('diagnostics.challengeDetail', { d: douban.challenge_difficulty, ms: douban.challenge_elapsed_ms, time: fmtTime(douban.challenge_at_unix) })
              : t('diagnostics.challengeNone') }}
          </div>
        </div>
      </div>

      <div class="diag-sub">{{ t('diagnostics.dataHealth') }}</div>
      <div class="diag-grid cczj-grid">
        <div class="diag-card">
          <div class="diag-label">{{ t('diagnostics.totalVideos') }}</div>
          <div class="diag-value">{{ fmtNumber(health.total_videos) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.withDoubanId', { n: fmtNumber(health.with_douban_id) }) }}</div>
        </div>
        <div class="diag-card" :class="{ warn: (health.missing_score ?? 0) > 0 }">
          <div class="diag-label">{{ t('diagnostics.missingScore') }}</div>
          <div class="diag-value">{{ fmtNumber(health.missing_score) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.inheritable', { n: fmtNumber(health.inherit_candidates) }) }}</div>
        </div>
        <div class="diag-card" :class="{ warn: (health.missing_subject_id ?? 0) > 0 }">
          <div class="diag-label">{{ t('diagnostics.missingSubjectId') }}</div>
          <div class="diag-value">{{ fmtNumber(health.missing_subject_id) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.missingSubjectIdNote') }}</div>
        </div>
        <div class="diag-card" :class="{ warn: (health.on_cooldown ?? 0) > 0 }">
          <div class="diag-label">{{ t('diagnostics.onCooldown') }}</div>
          <div class="diag-value">{{ fmtNumber(health.on_cooldown) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.onCooldownNote') }}</div>
        </div>
        <div class="diag-card" :class="{ warn: (health.duplicate_groups ?? 0) > 0 }">
          <div class="diag-label">{{ t('diagnostics.duplicates') }}</div>
          <div class="diag-value">{{ fmtNumber(health.duplicate_groups) }} · {{ fmtNumber(health.duplicate_rows) }}</div>
          <div class="diag-note-inline">{{ t('diagnostics.duplicatesNote') }}</div>
        </div>
      </div>

      <div class="diag-sub cczj-flex cczj-items-center cczj-justify-between">
        <span>{{ t('diagnostics.mergeQueue') }}</span>
        <Button variant="secondary" size="sm" :loading="candidatesLoading" @click="loadCandidates">
          <Icon name="refresh" :size="12" /> {{ t('common.refresh') }}
        </Button>
      </div>
      <p class="desc">{{ t('diagnostics.mergeQueueNote') }}</p>
      <div v-if="!candidates.length" class="diag-empty">{{ t('diagnostics.mergeQueueEmpty') }}</div>
      <div v-else class="merge-groups cczj-flex cczj-flex-col cczj-gap-3">
        <div v-for="group in candidates" :key="candidateKey(group)" class="merge-group cczj-rounded cczj-p-3 cczj-flex cczj-flex-col cczj-gap-2">
          <div class="merge-group-hd cczj-flex cczj-items-center cczj-justify-between cczj-gap-2">
            <span class="merge-reason cczj-truncate cczj-text-xs">{{ reasonLabel(String(group.reason)) }} · {{ group.key }}</span>
            <Button variant="primary" size="sm" :loading="mergingKey === candidateKey(group)" :disabled="!!mergingKey" @click="mergeGroup(group)">
              <Icon name="check" :size="12" /> {{ t('diagnostics.mergeAction') }}
            </Button>
          </div>
          <div class="diag-table-wrap">
            <table class="diag-table">
              <thead>
                <tr>
                  <th class="num">{{ t('diagnostics.mergeGlobalId') }}</th>
                  <th>{{ t('common.name') }}</th>
                  <th>{{ t('diagnostics.mergeType') }}</th>
                  <th>{{ t('diagnostics.doubanId') }}</th>
                  <th class="num" :title="t('diagnostics.mergeRefsNote')">{{ t('diagnostics.mergeRefs') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in group.rows" :key="row.global_id">
                  <td class="num mono">{{ row.global_id }}</td>
                  <td class="cczj-truncate" :title="row.vod_name">{{ row.vod_name }}</td>
                  <td class="cczj-truncate">{{ row.type_name || '—' }}</td>
                  <td class="mono">{{ row.douban_id || '—' }}</td>
                  <td class="num">{{ row.catalog_rows }} / {{ row.favorite_rows }} / {{ row.history_rows }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </section>

    <!-- ========== 采集源 ========== -->
    <section class="block">
      <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
        <h3>{{ t('diagnostics.sources') }}</h3>
        <Button variant="secondary" size="sm" :loading="probing" @click="probe">
          <Icon name="globe" :size="12" /> {{ t('diagnostics.probe') }}
        </Button>
      </div>
      <p class="desc">{{ t('diagnostics.probeNote') }}</p>
      <div v-if="!sources.length" class="diag-empty">{{ t('diagnostics.noSources') }}</div>
      <div v-else class="diag-table-wrap">
        <table class="diag-table">
          <thead>
            <tr>
              <th>{{ t('common.name') }}</th>
              <th class="num">{{ t('diagnostics.catalog') }}</th>
              <th>{{ t('diagnostics.lastCollect') }}</th>
              <th :title="t('diagnostics.healthHint')">{{ t('diagnostics.health') }}</th>
              <th>{{ t('diagnostics.probeResult') }}</th>
              <th>{{ t('diagnostics.api') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="source in sources" :key="source.source_key">
              <td>
                {{ source.name }}
                <span v-if="!source.enabled" class="diag-tag muted">{{ t('common.disabled') }}</span>
              </td>
              <td class="num">{{ fmtNumber(source.video_count) }}</td>
              <td>
                <span
                  class="diag-tag"
                  :class="lastCollectTag(source).cls"
                  :title="lastCollectTag(source).title"
                >{{ lastCollectTag(source).text }}</span>
                <div class="diag-watermark" :title="watermarkHint(source)">{{ watermarkText(source) }}</div>
              </td>
              <td>
                <div class="diag-dots">
                  <span v-for="sample in healthDots(source)" :key="sample.id"
                        class="diag-dot"
                        :class="[sample.kind === patrolKind ? 'round' : '', sample.ok ? 'ok' : 'bad']"
                        :title="sampleTitle(sample)"></span>
                  <span v-if="!healthDots(source).length" class="muted">—</span>
                </div>
                <div class="diag-health">
                  <span class="diag-tag" :class="collectHealthTag(source.collect_health).cls"
                        :title="collectHealthTag(source.collect_health).title">{{ collectHealthTag(source.collect_health).text }}</span>
                  <span class="diag-tag" :class="patrolHealthTag(source.patrol_health).cls"
                        :title="patrolHealthTag(source.patrol_health).title">{{ patrolHealthTag(source.patrol_health).text }}</span>
                </div>
              </td>
              <td>
                <span v-if="!probeByKey[source.source_key]" class="muted">—</span>
                <span v-else-if="probeByKey[source.source_key].ok" class="diag-tag ok">
                  {{ probeByKey[source.source_key].status_code }} · {{ probeByKey[source.source_key].latency_ms }}ms
                </span>
                <span v-else class="diag-tag bad" :title="probeByKey[source.source_key].error">
                  {{ probeByKey[source.source_key].status_code || t('common.failed') }} · {{ probeByKey[source.source_key].error }}
                </span>
              </td>
              <td class="cczj-truncate" :title="source.api_url">{{ source.api_url }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<style scoped>
.block {
  padding: 18px 20px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  margin-bottom: 12px;
}
.block h3 {
  font-size: 1rem;
  font-weight: 700;
  margin: 0 0 14px;
  letter-spacing: 0.3px;
}
.block-hd h3 { margin: 0; }
.desc {
  color: var(--text-muted);
  font-size: 0.9rem;
  margin: 10px 0 0;
  line-height: 1.5;
}
.hint {
  color: var(--text-muted);
  font-size: 0.86rem;
}
.diag-notes { margin-top: 12px; }
.diag-note {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--warning, var(--text-muted));
  font-size: 0.86rem;
}
.diag-grid {
  gap: 10px;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
}
.diag-card {
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-secondary);
  min-width: 0;
}
.diag-card.warn { border-color: var(--warning, var(--danger)); }
.diag-label {
  font-size: 0.79rem;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.4px;
}
.diag-value {
  margin-top: 4px;
  font-size: 1.07rem;
  font-weight: 700;
  color: var(--text-primary);
  font-variant-numeric: tabular-nums;
  word-break: break-all;
}
.diag-note-inline {
  margin-top: 4px;
  font-size: 0.79rem;
  color: var(--text-muted);
  line-height: 1.4;
}
.diag-sub {
  margin: 16px 0 8px;
  font-size: 0.86rem;
  font-weight: 700;
  color: var(--text-secondary);
}
.diag-kv { gap: 6px; margin-top: 14px; }
.diag-kv-row {
  display: flex;
  align-items: baseline;
  gap: 10px;
  font-size: 0.86rem;
}
.diag-kv-row .k {
  color: var(--text-muted);
  min-width: 110px;
  flex-shrink: 0;
}
.diag-kv-row .v {
  color: var(--text-secondary);
  min-width: 0;
  font-family: var(--font-mono, monospace);
}
.diag-table-wrap {
  margin-top: 10px;
  max-height: 320px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: 10px;
}
.diag-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.86rem;
}
.diag-table th,
.diag-table td {
  padding: 7px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border);
}
.diag-table th {
  position: sticky;
  top: 0;
  background: var(--bg-card);
  color: var(--text-muted);
  font-weight: 600;
}
.diag-table td.num,
.diag-table th.num { text-align: right; font-variant-numeric: tabular-nums; }
.diag-table .mono { font-family: var(--font-mono, monospace); }
.diag-empty {
  margin-top: 10px;
  color: var(--text-muted);
  font-size: 0.9rem;
}
.merge-group {
  border: 1px solid var(--border);
  background: var(--bg-secondary);
}
.merge-reason {
  color: var(--text-muted);
}
.diag-tag {
  display: inline-block;
  margin-left: 6px;
  padding: 1px 7px;
  border-radius: 999px;
  font-size: 0.75rem;
  background: var(--accent-alpha-15);
  color: var(--accent);
}
.diag-tag.ok { background: rgba(34, 197, 94, 0.16); color: var(--success, #22c55e); }
.diag-tag.bad { background: rgba(239, 68, 68, 0.16); color: var(--danger); }
.diag-tag.warn { background: rgba(245, 158, 11, 0.16); color: var(--warning); }
.diag-tag.muted { background: var(--bg-secondary); color: var(--text-muted); }
.muted { color: var(--text-muted); }
.diag-watermark {
  margin-top: 3px;
  font-size: 0.72rem;
  color: var(--text-muted);
}
.diag-dots {
  display: flex;
  align-items: center;
  gap: 3px;
  min-height: 10px;
}
.diag-dot {
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  border-radius: 2px;
  background: var(--text-muted);
}
/* 采集与巡检的样本混在同一条时间线上，靠形状区分。 */
.diag-dot.round { border-radius: 50%; }
.diag-dot.ok { background: var(--success, #22c55e); }
.diag-dot.bad { background: var(--danger); }
.diag-health { margin-top: 4px; }
.diag-health .diag-tag { margin-left: 0; margin-right: 4px; }
</style>
