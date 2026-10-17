import { beforeEach, describe, expect, it, vi } from 'vitest'
import { job, ok, snapshot } from '../test/fixtures'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({ GetAppSnapshot: vi.fn() }),
)
import { Bridge, refresh, run, read, state, unwrap } from './app'

describe('桌面狀態與寫入防重送', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    state.snapshot = null
    state.busy = 0
    state.error = ''
    state.notice = ''
    state.startingTest = ''
    state.pendingTestId = ''
    state.stoppingTestId = ''
    state.alerts = []
  })
  it('Go 的 null 清單正規化，不把不存在的規則變成物件', async () => {
    const data = await unwrap(
      Promise.resolve(ok({ items: null as unknown[] | null, before: null })),
    )
    expect(data.items).toEqual([])
    expect(data.before).toBeNull()
  })
  it('拒絕舊快照倒退', async () => {
    const newer = snapshot()
    newer.sequence = 9
    state.snapshot = newer
    vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
    await refresh()
    expect(state.snapshot.sequence).toBe(9)
  })
  it('失敗不讀零值 data、不盲目重送', async () => {
    const action = vi.fn().mockResolvedValue({
      ok: false,
      data: '不能使用',
      error: { code: 'CONFLICT', message: '已變更' },
    })
    expect(await run(action)).toBeUndefined()
    expect(action).toHaveBeenCalledTimes(1)
    expect(state.error).toContain('CONFLICT')
    expect(state.busy).toBe(0)
  })
  it('唯讀不鎖全應用、不重刷完整快照', async () => {
    expect(await read(async () => ok(['folder7']))).toEqual(['folder7'])
    expect(state.busy).toBe(0)
    expect(Bridge.GetAppSnapshot).not.toHaveBeenCalled()
  })
  it('寫入後即使舊快照失敗仍重新取得最新版本', async () => {
    const next = snapshot()
    next.sequence = 20
    vi.mocked(Bridge.GetAppSnapshot)
      .mockRejectedValueOnce(new Error('測試中斷'))
      .mockResolvedValueOnce(ok(next))
    const old = refresh()
    const handled = old.catch(() => undefined)
    const after = refresh(true)
    await handled
    await after
    expect(Bridge.GetAppSnapshot).toHaveBeenCalledTimes(2)
    expect(state.snapshot?.sequence).toBe(20)
  })
  it('輪詢共用同一快照請求，寫入後會等舊請求再取新資料', async () => {
    let resolve!: (
      value: ReturnType<typeof ok<ReturnType<typeof snapshot>>>,
    ) => void
    const pending = new Promise<
      ReturnType<typeof ok<ReturnType<typeof snapshot>>>
    >((r) => {
      resolve = r
    }) as ReturnType<typeof Bridge.GetAppSnapshot>
    const next = snapshot()
    next.sequence = 10
    vi.mocked(Bridge.GetAppSnapshot)
      .mockReturnValueOnce(pending)
      .mockResolvedValueOnce(ok(next))
    const first = refresh(),
      same = refresh(),
      afterWrite = refresh(true)
    expect(Bridge.GetAppSnapshot).toHaveBeenCalledOnce()
    resolve(ok(snapshot()))
    await Promise.all([first, same, afterWrite])
    expect(Bridge.GetAppSnapshot).toHaveBeenCalledTimes(2)
    expect(state.snapshot?.sequence).toBe(10)
  })
  it.each(['completed', 'partial', 'failed', 'cancelled'])(
    '事件遺失時輪詢仍區分 %s，不把啟動當完成',
    async (status) => {
      state.snapshot = snapshot()
      state.snapshot.jobs = [job()]
      state.snapshot.activeTestId = 'job'
      const done = snapshot()
      done.jobs = [job({ state: status, finishedAt: new Date().toISOString() })]
      vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(done))
      await refresh()
      expect(state.alerts).toHaveLength(1)
      expect(state.alerts[0]!.level).toBe(
        status === 'completed'
          ? 'success'
          : status === 'failed'
            ? 'error'
            : 'warn',
      )
      await refresh()
      expect(state.alerts).toHaveLength(1)
    },
  )
})
