import { flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { primePlugins } from '../test/prime'
import { ok, snapshot } from '../test/fixtures'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({
    GetAppSnapshot: vi.fn(),
    PreviewDomainImport: vi.fn(),
    ResolveDomainImport: vi.fn(),
    ConfirmDomainImport: vi.fn(),
  }),
)
import { state, Bridge } from '../shared/app'
import DomainsView from './DomainsView.vue'
it('貼完整子域網址進私有預覽，衝突須選擇再確認，不啟動偵測', async () => {
  state.snapshot = snapshot()
  state.alerts = []
  state.busy = 0
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
  const p = {
    id: 'preview',
    revision: 'revision',
    rows: [
      {
        line: 1,
        entry: {
          id: 'd',
          hostname: 'cdn.example.com',
          displayName: 'cdn.example.com',
          enabled: true,
          testUrl: 'https://cdn.example.com/files/test.bin?q=1',
        },
        state: 'conflict',
        message: '已有網址',
        previousUrl: 'https://cdn.example.com/old.bin',
      },
    ],
  }
  vi.mocked(Bridge.PreviewDomainImport).mockResolvedValue(ok(p))
  vi.mocked(Bridge.ResolveDomainImport).mockResolvedValue(
    ok({ ...p, rows: [{ ...p.rows[0]!, state: 'replace' }] }),
  )
  vi.mocked(Bridge.ConfirmDomainImport).mockResolvedValue(ok(true))
  const w = mount(DomainsView, {
    global: { plugins: primePlugins, stubs: { Dialog: true } },
  })
  expect(w.find('button[aria-label="偵測 Fastly"]').exists()).toBe(false)
  await w.find('textarea').setValue(p.rows[0]!.entry.testUrl)
  await w.find('button[aria-label="預覽貼上清單"]').trigger('click')
  await flushPromises()
  expect(Bridge.PreviewDomainImport).toHaveBeenCalledWith(
    p.rows[0]!.entry.testUrl,
    'txt',
    '',
  )
  expect(w.text()).toContain('cdn.example.com')
  expect(
    w
      .find('button[aria-label="確認匯入有效或已決定更新項目"]')
      .attributes('disabled'),
  ).toBeDefined()
  await w.find('button[aria-label="取代現有測速網址"]').trigger('click')
  await flushPromises()
  expect(Bridge.ResolveDomainImport).toHaveBeenCalledWith('preview', 1, true)
  await w
    .find('button[aria-label="確認匯入有效或已決定更新項目"]')
    .trigger('click')
  await flushPromises()
  expect(Bridge.ConfirmDomainImport).toHaveBeenCalledWith('preview')
  w.unmount()
})
