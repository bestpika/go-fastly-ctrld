import { computed, reactive } from 'vue'
import { Events } from '@wailsio/runtime'
import * as NativeBridge from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge'
export const Bridge = { ...NativeBridge }
export type Dense<T> =
  NonNullable<T> extends (infer U)[]
    ? Dense<U>[]
    : T extends object
      ? { [K in keyof T]: Dense<T[K]> }
      : T
export type Snapshot = Dense<
  Awaited<ReturnType<typeof Bridge.GetAppSnapshot>>['data']
>
const arrayKeys = new Set([
  'domains',
  'profiles',
  'detections',
  'results',
  'jobs',
  'sources',
  'records',
  'candidates',
  'samples',
  'grants',
  'rows',
  'items',
  'resultIds',
  'interfaces',
  'routes',
  'resolvers',
  'warnings',
  'referenceScores',
  'rejected',
  'folders',
])
function normalize(value: unknown, key = ''): unknown {
  if (value === null && arrayKeys.has(key)) return []
  if (Array.isArray(value)) return value.map((item) => normalize(item))
  if (value && typeof value === 'object')
    return Object.fromEntries(
      Object.entries(value).map(([k, v]) => [k, normalize(v, k)]),
    )
  return value
}
export const state = reactive({
  alerts: [] as {
    key: string
    level: 'error' | 'success' | 'warn'
    message: string
    errorId: string
    count: number
  }[],
  snapshot: null as Snapshot | null,
  loading: true,
  busy: 0,
  error: '',
  notice: '',
  startingTest: '' as '' | 'test' | 'detect',
  pendingTestId: '',
  stoppingTestId: '',
})
export const activeTestId = computed(
  () => state.pendingTestId || state.snapshot?.activeTestId || '',
)
export const busy = computed(() => state.busy > 0)
export const working = computed(
  () =>
    !!activeTestId.value ||
    !!state.startingTest ||
    !!state.snapshot?.activeWriterId,
)
type WireResult<T> = {
  ok: boolean
  data: T
  error: { code: string; message: string; errorId?: string } | null
}
class OperationError extends Error {
  constructor(
    public code: string,
    message: string,
    public errorId = '',
  ) {
    super(message)
  }
}
export function dismiss(key: string) {
  state.alerts = state.alerts.filter((a) => a.key !== key)
}
export function notify(
  level: 'error' | 'success' | 'warn',
  message: string,
  errorId = '',
  count = 1,
) {
  const key = errorId || `${level}:${message}`
  const existing = state.alerts.find((a) => a.key === key)
  if (existing) {
    existing.message = message
    existing.count = Math.max(existing.count, count)
    return
  }
  state.alerts.push({ key, level, message, errorId, count })
  if (state.alerts.length > 6) state.alerts.shift()
}
async function showFailure(
  error: unknown,
  current: () => boolean = () => true,
) {
  const e =
    error instanceof OperationError
      ? error
      : new OperationError('IPC_DISCONNECTED', '與桌面宿主的連線中斷')
  if (e.code === 'CANCELLED' || !current()) return
  let id = e.errorId
  if (!id)
    try {
      const r = await Bridge.ReportClientError(e.code)
      if (r.ok) id = r.data.id
    } catch {
      /* 宿主恢復後可重讀；不在 localStorage 保存錯誤或權杖。 */
    }
  if (!current()) return
  state.error = `${e.code}：${e.message}`
  notify('error', state.error, id)
}
export async function reportFailure(
  code: 'BROWSER_OPEN_FAILED' | 'COPY_FAILED',
) {
  const message =
    code === 'COPY_FAILED'
      ? '無法複製遮敏詳情'
      : '無法開啟系統瀏覽器，仍可離線閱讀授權'
  await showFailure(new OperationError(code, message))
}
export async function unwrap<T>(
  promise: Promise<WireResult<T>> | WireResult<T>,
): Promise<Dense<T>> {
  const result = await promise
  if (!result.ok)
    throw new OperationError(
      result.error?.code ?? 'INTERNAL_ERROR',
      result.error?.message ?? '操作未完成',
      result.error?.errorId,
    )
  return normalize(result.data) as Dense<T>
}
let refreshFlight: Promise<void> | null = null
export function refresh(afterCurrent = false): Promise<void> {
  if (refreshFlight)
    return afterCurrent
      ? refreshFlight.catch(() => undefined).then(() => refresh())
      : refreshFlight
  const flight = refreshSnapshot().finally(() => {
    if (refreshFlight === flight) refreshFlight = null
  })
  refreshFlight = flight
  return flight
}
async function refreshSnapshot() {
  const old = state.snapshot
  const next = await unwrap(Bridge.GetAppSnapshot())
  if (!state.snapshot || next.sequence >= state.snapshot.sequence)
    state.snapshot = next
  document.documentElement.classList.toggle(
    'app-dark',
    state.snapshot.settings.theme === 'dark',
  )
  if (state.snapshot.recovery && !old?.recovery)
    notify('warn', '有未決作業，請至執行紀錄核對')
  if (
    state.snapshot.automationError &&
    state.snapshot.automationError !== old?.automationError
  )
    notify('warn', state.snapshot.automationError)
  const terminal = [
    'completed',
    'partial',
    'failed',
    'cancelled',
    'needs_recovery',
  ]
  for (const job of state.snapshot.jobs) {
    const previous = old?.jobs.find((j) => j.id === job.id)
    if (
      terminal.includes(job.state) &&
      ((previous && !terminal.includes(previous.state)) ||
        state.pendingTestId === job.id)
    ) {
      const message = `${label(job.kind)}：${label(job.state)}。${job.message || '請至執行紀錄查看明細'}`
      state.notice = message
      notify(
        job.state === 'completed'
          ? 'success'
          : job.state === 'failed' || job.state === 'needs_recovery'
            ? 'error'
            : 'warn',
        message,
      )
    }
  }
  const pending = state.snapshot.jobs.find((j) => j.id === state.pendingTestId)
  if (
    state.snapshot.activeTestId === state.pendingTestId ||
    (pending && terminal.includes(pending.state))
  )
    state.pendingTestId = ''
  if (
    state.stoppingTestId &&
    state.snapshot.activeTestId !== state.stoppingTestId &&
    !state.pendingTestId
  )
    state.stoppingTestId = ''
}

export async function startTest(kind: 'test' | 'detect') {
  if (state.startingTest || working.value) return
  state.startingTest = kind
  try {
    await run(async () => {
      const result = await (kind === 'test'
        ? Bridge.StartTest([], -1)
        : Bridge.StartDetection([]))
      if (result.ok) {
        state.pendingTestId = result.data
        state.startingTest = ''
      }
      return result
    })
  } finally {
    state.startingTest = ''
  }
}

export async function stopTest() {
  const id = activeTestId.value
  if (!id || state.stoppingTestId) return
  state.stoppingTestId = id
  let accepted = false
  await run(async () => {
    const result = await Bridge.CancelJob(id)
    accepted = result.ok && result.data
    return result
  })
  if (!accepted) state.stoppingTestId = ''
}
export async function run<T>(
  action: () => Promise<WireResult<T>>,
  notice = '',
): Promise<Dense<T> | undefined> {
  state.busy++
  state.error = ''
  try {
    const result = await unwrap(action())
    await refresh(true)
    if (notice) state.notice = notice
    if (notice) notify('success', notice)
    return result
  } catch (error) {
    await showFailure(error)
    return undefined
  } finally {
    state.busy--
  }
}
// 純讀取不鎖整個應用，也不連帶重刷測速與歷史快照。
export async function read<T>(
  action: () => Promise<WireResult<T>>,
  current: () => boolean = () => true,
): Promise<Dense<T> | undefined> {
  try {
    const result = await unwrap(action())
    return current() ? result : undefined
  } catch (error) {
    if (current() && !isReadCancelled(error)) await showFailure(error, current)
    return undefined
  }
}
export function isReadCancelled(error: unknown) {
  return (
    (error instanceof OperationError && error.code === 'CANCELLED') ||
    (error instanceof Error &&
      ['CancelError', 'CancelledRejectionError'].includes(error.name))
  )
}
export async function initialize(navigate: (path: string) => void) {
  let timer: ReturnType<typeof setTimeout> | undefined
  const schedule = () => {
    if (!timer)
      timer = setTimeout(() => {
        timer = undefined
        void refresh().catch(showFailure)
      }, 120)
  }
  const off = [
    'job:updated',
    'jobs:changed',
    'network:changed',
    'automation:changed',
    'settings:changed',
    'recovery:required',
  ].map((name) => Events.On(name, schedule))
  off.push(
    Events.On('error:recorded', (event) => {
      const payload = Array.isArray(event.data) ? event.data[0] : event.data
      const r = payload?.error
      if (r?.id) notify('error', `${r.code}：${r.message}`, r.id, r.count)
      schedule()
    }),
    Events.On('navigation:requested', (event) => {
      if (event.data === '/tests') navigate('/tests')
    }),
  )
  const poll = setInterval(schedule, 5000)
  const visibility = () => {
    if (!document.hidden) schedule()
  }
  document.addEventListener('visibilitychange', visibility)
  try {
    await refresh()
    await unwrap(Bridge.ReportUIReady())
  } catch (error) {
    await showFailure(error)
  } finally {
    state.loading = false
  }
  return () => {
    off.forEach((stop) => stop())
    clearInterval(poll)
    if (timer) clearTimeout(timer)
    document.removeEventListener('visibilitychange', visibility)
  }
}
export function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}
export function bytes(value: number) {
  return value >= 1e9
    ? `${(value / 1e9).toFixed(2)} GB`
    : `${(value / 1e6).toFixed(2)} MB`
}
export function date(value: string) {
  return !value || value.startsWith('0001-')
    ? '尚無紀錄'
    : new Date(value).toLocaleString('zh-TW')
}
const names: Record<string, string> = {
  confirmed: '雙來源確認 Fastly',
  mixed: '歸屬不一致／混合',
  suspected: '疑似 Fastly',
  not_detected: '未偵測 Fastly',
  indeterminate: '證據不足',
  queued: '排隊中',
  running: '執行中',
  completed: '作業完成',
  partial: '部分完成',
  failed: '失敗',
  cancelled: '已取消',
  needs_recovery: '待回讀復原',
  verified: '回讀確認',
  skipped: '略過',
  prepared: '已保存預備紀錄',
  write_started: '已開始寫入',
  unknown: '結果未知',
  unconfirmed: '無法確認曾寫入',
  conflict: '外部變更衝突',
  create: '新增',
  update: '修改',
  rewrite: '同值重寫',
  unchanged: '目標值相同，未寫入',
  delete: '刪除本工具新建規則',
  restore: '還原原值',
  detect: 'DNS 判定',
  test: '測速',
  apply: '規則套用',
  preparing: '準備作業與現用規則',
  qualification: '跨網域連線與 TLS 資格探測',
  download: '三輪下載測速',
  connection: '自身連線延遲比較',
  finished: '已結束',
}
export function label(value: string) {
  return names[value] ?? value
}
export function severity(
  value: string,
): 'success' | 'warn' | 'danger' | 'secondary' {
  if (['confirmed', 'verified', 'completed'].includes(value)) return 'success'
  if (
    ['failed', 'conflict', 'needs_recovery', 'unknown', 'unconfirmed'].includes(
      value,
    )
  )
    return 'danger'
  if (['mixed', 'partial', 'suspected', 'indeterminate'].includes(value))
    return 'warn'
  return 'secondary'
}
