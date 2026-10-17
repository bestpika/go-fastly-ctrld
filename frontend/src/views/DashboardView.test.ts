import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { primePlugins } from '../test/prime'
import { deferred, job, ok, snapshot } from '../test/fixtures'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({
    GetAppSnapshot: vi.fn(),
    StartTest: vi.fn(),
    StartDetection: vi.fn(),
    CancelJob: vi.fn(),
    ReportClientError: vi.fn(),
  }),
)
import { Bridge, refresh, state } from '../shared/app'
import DashboardView from './DashboardView.vue'
beforeEach(() => {
  vi.clearAllMocks()
  state.snapshot = snapshot()
  state.busy = 0
  state.startingTest = ''
  state.pendingTestId = ''
  state.stoppingTestId = ''
  state.alerts = []
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
  vi.mocked(Bridge.CancelJob).mockResolvedValue(ok(true))
})
const setup = () =>
  mount(DashboardView, {
    global: { plugins: primePlugins, stubs: { ResultsPanel: true } },
  })

it('空清單啟動三組基準，同顆純圖示按鈕由啟動中轉為可停止', async () => {
  const start = deferred<ReturnType<typeof ok<string>>>(),
    slowSnapshot =
      deferred<ReturnType<typeof ok<ReturnType<typeof snapshot>>>>()
  vi.mocked(Bridge.StartTest).mockReturnValue(
    start.promise as ReturnType<typeof Bridge.StartTest>,
  )
  vi.mocked(Bridge.GetAppSnapshot).mockReturnValueOnce(
    slowSnapshot.promise as ReturnType<typeof Bridge.GetAppSnapshot>,
  )
  const w = setup()
  expect(w.find('button[aria-label*="基準不需權杖"]').exists()).toBe(true)
  const button = w.find('button[aria-label="開始測速"]')
  expect(button.text()).toBe('')
  await button.trigger('click')
  expect(
    w.find('button[aria-label="啟動中"]').attributes('disabled'),
  ).toBeDefined()
  start.resolve(ok('job'))
  await flushPromises()
  const stop = w.find('button[aria-label="停止測試"]')
  expect(stop.attributes('disabled')).toBeUndefined()
  expect(Bridge.StartTest).toHaveBeenCalledExactlyOnceWith([], -1)
  const running = snapshot()
  running.activeTestId = 'job'
  running.jobs = [job()]
  slowSnapshot.resolve(ok(running))
  await flushPromises()
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(running))
  vi.mocked(Bridge.GetAppSnapshot).mockRejectedValueOnce(
    new Error('取消已接受後，快照暫時失聯'),
  )
  await stop.trigger('click')
  await flushPromises()
  expect(Bridge.CancelJob).toHaveBeenCalledExactlyOnceWith('job')
  expect(
    w.find('button[aria-label="停止中，等待宿主確認"]').attributes('disabled'),
  ).toBeDefined()
  const done = snapshot()
  done.jobs = [
    job({ state: 'cancelled', finishedAt: new Date().toISOString() }),
  ]
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(done))
  await refresh()
  await flushPromises()
  expect(
    w.find('button[aria-label="開始測速"]').attributes('disabled'),
  ).toBeUndefined()
  expect(
    state.alerts.some(
      (a) => a.level === 'warn' && a.message.includes('已取消'),
    ),
  ).toBe(true)
  w.unmount()
})

it('重新開啟或切頁後按 Go 快照還原停止控制，不顯示第二顆停止按鈕', () => {
  state.snapshot!.activeTestId = 'job'
  state.snapshot!.jobs = [job()]
  const w = setup()
  expect(w.findAll('button[aria-label="停止測試"]')).toHaveLength(1)
  expect(w.find('button[aria-label="開始測速"]').exists()).toBe(false)
  expect(w.find('button[aria-label="取消測試"]').exists()).toBe(false)
  w.unmount()
})

it('啟動失敗回到可重試，不留下停止狀態', async () => {
  vi.mocked(Bridge.StartTest).mockResolvedValue({
    ok: false,
    data: '',
    error: {
      code: 'INVALID_INPUT',
      message: '無可用網域',
      errorId: 'known',
      retryable: false,
    },
  })
  const w = setup()
  await w.find('button[aria-label="開始測速"]').trigger('click')
  await flushPromises()
  expect(
    w.find('button[aria-label="開始測速"]').attributes('disabled'),
  ).toBeUndefined()
  expect(state.pendingTestId).toBe('')
  w.unmount()
})
