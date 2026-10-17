import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import ConfirmDialog from 'primevue/confirmdialog'
import { job, ok, snapshot } from '../test/fixtures'
import { primePlugins } from '../test/prime'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({
    GetAppSnapshot: vi.fn(),
    DeleteJobs: vi.fn(),
    BuildRestorePreview: vi.fn(),
    ApplyRestorePreview: vi.fn(),
    ReportClientError: vi.fn(),
  }),
)
import { Bridge, state } from '../shared/app'
import HistoryView from './HistoryView.vue'

let unmount: (() => void) | undefined
beforeEach(() => {
  vi.clearAllMocks()
  state.snapshot = snapshot()
  state.snapshot.jobs = [job({ state: 'completed' })]
  state.busy = 0
  state.alerts = []
  state.notice = ''
  state.error = ''
  state.startingTest = ''
  state.pendingTestId = ''
  state.stoppingTestId = ''
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
  vi.mocked(Bridge.DeleteJobs).mockResolvedValue(
    ok({ deleted: 1, retained: 0 }),
  )
})
afterEach(() => {
  unmount?.()
  unmount = undefined
  document.body.innerHTML = ''
})
function setup() {
  const w = mount(
    {
      components: { HistoryView, ConfirmDialog },
      template: '<ConfirmDialog group="app"/><HistoryView/>',
    },
    { attachTo: document.body, global: { plugins: primePlugins } },
  )
  unmount = () => w.unmount()
  return w
}
async function confirm(label = '確認') {
  await new DOMWrapper(document.body)
    .get('.p-confirmdialog')
    .findAll('button')
    .find((button) => button.text() === label)!
    .trigger('click')
  await flushPromises()
}

it('單筆刪除使用官方確認視窗，取消不呼叫宿主', async () => {
  const w = setup()
  await w.get('button[aria-label="刪除此筆執行紀錄"]').trigger('click')
  expect(document.querySelector('.p-confirmdialog')?.textContent).toContain(
    '規則、測速結果與復原底帳會保留',
  )
  expect(Bridge.DeleteJobs).not.toHaveBeenCalled()
  await confirm('取消')
  expect(Bridge.DeleteJobs).not.toHaveBeenCalled()
  expect(state.snapshot!.jobs).toHaveLength(1)
})

it('確認單筆刪除以 ID 呼叫宿主、刷新列表並關閉已刪明細', async () => {
  const w = setup()
  await w.get('button[aria-label="明細"]').trigger('click')
  await flushPromises()
  expect(document.querySelector('.p-dialog-title')?.textContent).toBe(
    '作業逐項結果',
  )
  await w.get('button[aria-label="刪除此筆執行紀錄"]').trigger('click')
  await confirm()
  expect(Bridge.DeleteJobs).toHaveBeenCalledExactlyOnceWith('job')
  expect(Bridge.GetAppSnapshot).toHaveBeenCalledOnce()
  expect(w.text()).toContain('尚無紀錄')
  expect(state.notice).toContain('已刪除 1 筆')
  expect(
    [...document.querySelectorAll('.p-dialog-title')].some(
      (title) => title.textContent === '作業逐項結果',
    ),
  ).toBe(false)
})

it('清除全部可在作業與復原期間刪已結束紀錄，顯示保留筆數', async () => {
  const active = job({ id: 'active' })
  const recovery = job({ id: 'recovery', state: 'needs_recovery' })
  state.snapshot!.jobs.push(active, recovery)
  state.snapshot!.activeTestId = active.id
  state.snapshot!.recovery = true
  const next = snapshot()
  next.jobs = [active, recovery]
  next.activeTestId = active.id
  next.recovery = true
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(next))
  vi.mocked(Bridge.DeleteJobs).mockResolvedValue(
    ok({ deleted: 1, retained: 2 }),
  )
  const w = setup()
  expect(
    w.get('button[aria-label="清除全部執行紀錄"]').attributes('disabled'),
  ).toBeUndefined()
  await w.get('button[aria-label="清除全部執行紀錄"]').trigger('click')
  expect(document.querySelector('.p-confirmdialog')?.textContent).toContain(
    '進行中、待核對與復原底帳會保留',
  )
  await confirm()
  expect(Bridge.DeleteJobs).toHaveBeenCalledExactlyOnceWith('')
  expect(state.snapshot!.jobs.map((j) => j.id)).toEqual(['active', 'recovery'])
  expect(state.snapshot!.recovery).toBe(true)
  expect(state.alerts.at(-1)?.message).toContain('保留 2 筆')
  expect(state.alerts.at(-1)?.level).toBe('warn')
})

it('進行中、待核對、未知或仍有未決項目的列不可刪除，其他終態可刪', () => {
  state.snapshot!.activeTestId = 'active-test'
  state.snapshot!.activeWriterId = 'active-writer'
  state.snapshot!.jobs = [
    job(),
    job({ id: 'queued', state: 'queued' }),
    job({ id: 'recovery', state: 'needs_recovery' }),
    job({ id: 'future', state: 'future' }),
    job({ id: 'active-test', state: 'completed' }),
    job({ id: 'active-writer', state: 'completed' }),
    job({
      id: 'unknown-item',
      state: 'partial',
      items: [{ state: 'unknown' } as ReturnType<typeof job>['items'][number]],
    }),
    job({
      id: 'writing-item',
      state: 'completed',
      items: [
        { state: 'write_started' } as ReturnType<typeof job>['items'][number],
      ],
    }),
    job({ id: 'cancelled', state: 'cancelled' }),
    job({ id: 'failed', state: 'failed' }),
  ]
  const w = setup()
  const buttons = w.findAll('button[aria-label="刪除此筆執行紀錄"]')
  expect(buttons).toHaveLength(10)
  expect(
    buttons
      .slice(0, 8)
      .every((button) => button.attributes('disabled') !== undefined),
  ).toBe(true)
  expect(
    buttons
      .slice(8)
      .every((button) => button.attributes('disabled') === undefined),
  ).toBe(true)
})

it('宿主拒絕刪除不移除列表、不顯示刪除成功', async () => {
  vi.mocked(Bridge.DeleteJobs).mockResolvedValue({
    ok: false,
    data: { deleted: 0, retained: 0 },
    error: {
      code: 'STORAGE_ERROR',
      message: '列表未能保存',
      errorId: 'error',
      retryable: false,
    },
  })
  const w = setup()
  await w.get('button[aria-label="刪除此筆執行紀錄"]').trigger('click')
  await confirm()
  expect(state.snapshot!.jobs).toHaveLength(1)
  expect(state.notice).toBe('')
  expect(state.alerts.at(-1)?.level).toBe('error')
  expect(Bridge.GetAppSnapshot).not.toHaveBeenCalled()
})

it('刪除後清除復原預覽，空列表停用清除全部，不再重複 DNS 生效提示', async () => {
  state.snapshot!.jobs = [
    job({
      kind: 'apply',
      state: 'completed',
      items: [{ state: 'verified' } as ReturnType<typeof job>['items'][number]],
    }),
  ]
  vi.mocked(Bridge.BuildRestorePreview).mockResolvedValue(
    ok({
      id: 'preview',
      kind: 'restore',
      profileId: 'profile',
      credentialRevision: 'credential',
      settingsRevision: 'revision',
      networkFingerprint: 'network',
      expiresAt: new Date(Date.now() + 60000).toISOString(),
      items: [],
    }),
  )
  const w = setup()
  await w.get('button[aria-label="復原預覽"]').trigger('click')
  await flushPromises()
  expect(w.find('button[aria-label="取消預覽"]').exists()).toBe(true)
  await w.get('button[aria-label="清除全部執行紀錄"]').trigger('click')
  await confirm()
  expect(w.find('button[aria-label="取消預覽"]').exists()).toBe(false)
  expect(
    w.get('button[aria-label="清除全部執行紀錄"]').attributes('disabled'),
  ).toBeDefined()
  expect(
    w
      .findAll('button')
      .some((button) => button.attributes('aria-label')?.includes('DNS 生效')),
  ).toBe(false)
})
