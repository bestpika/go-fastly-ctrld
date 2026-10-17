import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { primePlugins } from './test/prime'
import { snapshot, ok } from './test/fixtures'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({
    GetAppSnapshot: vi.fn(),
    ReportUIReady: vi.fn(),
    UpdateSettings: vi.fn(),
  }),
)
import App from './App.vue'
import { Bridge, state } from './shared/app'

describe('桌面入口', () => {
  beforeEach(() => {
    state.snapshot = null
    state.loading = true
    state.error = ''
    state.alerts = []
    state.busy = 0
    vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
    vi.mocked(Bridge.ReportUIReady).mockResolvedValue(ok(true))
  })
  it('從 Go 快照恢復並顯示 Sakai 八頁導覽', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: '/:pathMatch(.*)*',
          component: { template: '<h1>測試頁</h1>' },
        },
      ],
    })
    await router.push('/')
    const wrapper = mount(App, {
      global: { plugins: [router, ...primePlugins] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Fastly 路線最佳化')
    expect(wrapper.findAll('.layout-menu a[href]')).toHaveLength(8)
    expect(Bridge.ReportUIReady).toHaveBeenCalled()
    wrapper.unmount()
  })
  it('宿主失敗顯示錯誤且不宣稱 IPC 已就緒', async () => {
    vi.mocked(Bridge.GetAppSnapshot).mockRejectedValue(new Error('宿主不可用'))
    vi.mocked(Bridge.ReportUIReady).mockClear()
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push('/')
    const wrapper = mount(App, {
      global: { plugins: [router, ...primePlugins] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('無法讀取本機狀態')
    expect(document.querySelector('.p-toast')?.textContent).toContain(
      '與桌面宿主的連線中斷',
    )
    expect(Bridge.ReportUIReady).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
