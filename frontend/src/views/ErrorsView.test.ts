import { flushPromises, mount, DOMWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { primePlugins } from '../test/prime'
import { ok, snapshot } from '../test/fixtures'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({
    GetAppSnapshot: vi.fn(),
    ListErrors: vi.fn(),
    DeleteError: vi.fn(),
    ReportClientError: vi.fn(),
  }),
)
import { Bridge, state, notify } from '../shared/app'
import ErrorsView from './ErrorsView.vue'
import Notifications from '../components/Notifications.vue'
const record = {
  id: 'error-123',
  occurredAt: '2026-10-17T00:00:00Z',
  source: 'controld',
  operation: 'GET 資料夾清單',
  code: 'API_REJECTED',
  message: '讀取被拒',
  detail: '安全詳情',
  httpStatus: 400,
  vendorCode: '123',
  requestId: 'req-abc',
  jobId: 'job',
  count: 2,
}
beforeEach(() => {
  vi.clearAllMocks()
  state.snapshot = snapshot()
  state.alerts = []
  state.busy = 0
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
  vi.mocked(Bridge.ListErrors).mockResolvedValue(ok([record]))
  vi.mocked(Bridge.DeleteError).mockResolvedValue(ok(true))
})
async function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<p>首頁</p>' } },
      { path: '/errors', component: ErrorsView },
    ],
  })
  await router.push('/')
  await router.isReady()
  const w = mount(
    {
      components: { Notifications, RouterView },
      template: '<Notifications/><RouterView/>',
    },
    {
      global: {
        plugins: [router, ...primePlugins],
        stubs: {
          Dialog: {
            props: ['visible'],
            template:
              '<section v-if="visible" role="dialog"><slot/><slot name="footer"/></section>',
          },
        },
      },
    },
  )
  return { w, router }
}
it('右下錯誤通知定位同筆並開啟遮敏詳情，不在 URL 放內容', async () => {
  const { w, router } = await setup()
  notify('error', '讀取被拒', record.id, 2)
  notify('error', '讀取被拒', record.id, 2)
  expect(state.alerts).toHaveLength(1)
  await nextTick()
  await new DOMWrapper(document.body)
    .find('button[aria-label^="開啟錯誤紀錄"]')
    .trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.query).toEqual({ errorId: record.id })
  expect(w.find('[role=dialog]').text()).toContain('安全詳情')
  expect(w.find('[role=dialog]').text()).toContain('400')
  expect(w.find('tr[aria-selected="true"]').text()).toContain(record.code)
  await w.find('button[aria-label="關閉錯誤詳情"]').trigger('click')
  await flushPromises()
  expect(w.find('[role=dialog]').exists()).toBe(false)
  expect(w.find('tr[aria-selected="true"]').exists()).toBe(false)
  await w
    .find('tr[aria-selected="false"]')
    .trigger('keydown', { code: 'Space' })
  await flushPromises()
  expect(w.find('[role=dialog]').text()).toContain('安全詳情')
  w.unmount()
})
it('通知關閉不刪歷史、不解除安全狀態', async () => {
  const { w } = await setup()
  state.snapshot!.recovery = true
  notify('error', '失敗', record.id)
  await nextTick()
  await new DOMWrapper(document.body)
    .find('button[aria-label="關閉通知"]')
    .trigger('click')
  expect(state.alerts).toHaveLength(0)
  expect(Bridge.DeleteError).not.toHaveBeenCalled()
  expect(state.snapshot!.recovery).toBe(true)
  w.unmount()
})
it('已刪除的錯誤不開錯筆，顯示找不到的通知', async () => {
  vi.mocked(Bridge.ListErrors).mockResolvedValue(ok([]))
  const { w } = await setup()
  notify('error', '已刪除', 'missing')
  await nextTick()
  await new DOMWrapper(document.body)
    .find('button[aria-label^="開啟錯誤紀錄"]')
    .trigger('click')
  await flushPromises()
  expect(w.find('[role=dialog]').exists()).toBe(false)
  expect(document.querySelector('.p-toast')?.textContent).toContain(
    '該筆錯誤已刪除',
  )
  w.unmount()
})
