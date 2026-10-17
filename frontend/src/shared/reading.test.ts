import { flushPromises } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({ GetAppSnapshot: vi.fn(), ReportClientError: vi.fn() }),
)
import { Bridge, read, state } from './app'
it('錯誤記錄等待期間切換範圍，不讓舊讀取遲到通知', async () => {
  state.alerts = []
  state.error = ''
  state.busy = 0
  let finish!: () => void
  const pending = new Promise<
    Awaited<ReturnType<typeof Bridge.ReportClientError>>
  >((resolve) => {
    finish = () =>
      resolve({
        ok: true,
        error: null,
        data: {
          id: 'old-error',
          occurredAt: '2026-10-17T00:00:00Z',
          source: 'frontend',
          operation: 'read',
          code: 'IPC_DISCONNECTED',
          message: '連線中斷',
          detail: '',
          httpStatus: 0,
          vendorCode: '',
          requestId: '',
          jobId: '',
          count: 1,
        },
      })
  })
  vi.mocked(Bridge.ReportClientError).mockReturnValue(
    pending as unknown as ReturnType<typeof Bridge.ReportClientError>,
  )
  let current = true
  const operation = read(
    async () => {
      throw new Error('不得直接顯示的原始錯誤')
    },
    () => current,
  )
  await flushPromises()
  expect(Bridge.ReportClientError).toHaveBeenCalledOnce()
  current = false
  finish()
  await operation
  expect(state.alerts).toHaveLength(0)
  expect(state.error).toBe('')
  expect(state.busy).toBe(0)
})
