import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { pages } from '../router'
import { primePlugins } from '../test/prime'
import { job, ok, snapshot } from '../test/fixtures'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({ GetAppSnapshot: vi.fn(), UpdateSettings: vi.fn() }),
)
import { Bridge, state } from '../shared/app'
import { useLayout } from './composables/layout'
import AppLayout from './AppLayout.vue'

let unmount: (() => void) | undefined
beforeEach(() => {
  state.snapshot = snapshot()
  state.loading = false
  state.busy = 0
  state.alerts = []
  state.startingTest = ''
  state.pendingTestId = ''
  state.stoppingTestId = ''
  const { layoutConfig, layoutState } = useLayout()
  layoutConfig.menuMode = 'static'
  layoutState.staticMenuInactive = false
  layoutState.overlayMenuActive = false
  layoutState.mobileMenuActive = false
  layoutState.activePath = null
  layoutState.menuHoverActive = false
  layoutState.sidebarExpanded = false
  layoutState.anchored = false
  layoutState.desktop = true
  vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
  vi.mocked(Bridge.UpdateSettings).mockResolvedValue(ok(true))
})
afterEach(() => {
  unmount?.()
  unmount = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})
async function setup(width = 800) {
  vi.stubGlobal('innerWidth', width)
  const removeListener = vi.spyOn(window, 'removeEventListener')
  const removeDocumentListener = vi.spyOn(document, 'removeEventListener')
  const router = createRouter({
    history: createMemoryHistory(),
    routes: pages.map((page) => ({
      path: page.path,
      component: { template: '<h1>測試內容</h1>' },
    })),
  })
  await router.push('/')
  const w = mount(AppLayout, {
    attachTo: document.body,
    global: {
      plugins: [router, ...primePlugins],
      stubs: { transition: false },
    },
  })
  unmount = () => w.unmount()
  await flushPromises()
  return {
    w,
    router,
    removeListener,
    removeDocumentListener,
    resize: async (nextWidth: number) => {
      vi.stubGlobal('innerWidth', nextWidth)
      window.dispatchEvent(new Event('resize'))
      await flushPromises()
    },
    trigger: w.find<HTMLButtonElement>('#navigation-toggle'),
  }
}

it('直接使用 Sakai 頂欄、側欄、遞迴選單、頁尾及八頁路由，桌面沿用官方收合狀態', async () => {
  const { w, router, trigger } = await setup(1280)
  expect(w.find('.layout-topbar-logo').text()).toContain('Fastly 路線最佳化')
  expect(w.find('.layout-sidebar .layout-menu').exists()).toBe(true)
  expect(w.find('.layout-footer').text()).toContain('SAKAI by PrimeVue')
  expect(w.findAll('.layout-menu a[href]')).toHaveLength(8)
  expect(
    w.find('.layout-menu .active-route[aria-current="page"]').text(),
  ).toContain('總覽')
  expect(w.find('.p-toolbar, .p-menu, .p-drawer').exists()).toBe(false)
  const link = w.find<HTMLAnchorElement>('.layout-menu a[href="/domains"]')
  expect(link.attributes('tabindex')).toBe('0')
  link.element.focus()
  expect(document.activeElement).toBe(link.element)
  await link.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/domains')
  expect(
    w.find('.layout-menu .active-route[aria-current="page"]').text(),
  ).toContain('網域管理')
  await trigger.trigger('click')
  expect(w.find('.layout-wrapper').classes()).toContain(
    'layout-static-inactive',
  )
  expect(w.find('.layout-sidebar').attributes('inert')).toBeDefined()
  expect(trigger.attributes('aria-expanded')).toBe('false')
  await trigger.trigger('click')
  expect(w.find('.layout-sidebar').attributes('inert')).toBeUndefined()
})

it('跨頁保留宿主作業、階段、耗時與流量，自動功能啟用不冒充執行中', async () => {
  state.snapshot!.settings.automation.enabled = true
  state.snapshot!.activeTestId = 'job'
  state.snapshot!.jobs = [job()]
  const { w, router } = await setup(1280)
  await w.find('button[aria-label="展開作業狀態"]').trigger('click')
  await router.push('/tests')
  await flushPromises()
  expect(w.text()).toContain('pbs.twimg.com')
  expect(w.text()).toContain('跨網域連線與 TLS 資格探測')
  expect(w.text()).toContain('耗時')
  expect(w.text()).toContain('已讀')
  expect(w.text()).toContain('自動功能已啟用')
  expect(w.text()).not.toContain('自動執行中')
  expect(w.find('a[href="/"]').exists()).toBe(true)
  state.snapshot!.activeTestId = ''
  state.snapshot!.jobs = [job({ state: 'completed' })]
  state.startingTest = 'test'
  await flushPromises()
  expect(w.find('[role="status"]').text()).toContain('啟動中')
  expect(w.find('[role="status"]').text()).not.toContain('已完成')
})

it('作業面板預設收合、官方按鈕支援鍵盤，跨頁及作業開始結束不改手動選擇', async () => {
  state.snapshot!.jobs = [job({ state: 'partial' })]
  const { w, router } = await setup(1280)
  const panel = w.find('[role="status"]')
  expect(panel.find('.p-panel-header').text()).toContain('測速')
  expect(panel.find('.p-panel-header').text()).toContain('部分完成')
  expect(panel.find('[role="region"]').isVisible()).toBe(false)
  const expand = panel.find('button[aria-label="展開作業狀態"]')
  expect(expand.attributes('aria-expanded')).toBe('false')
  expect(panel.find('[role="region"]').attributes('id')).toBe(
    expand.attributes('aria-controls'),
  )
  await expand.trigger('keydown', { code: 'Enter' })
  expect(panel.find('[role="region"]').isVisible()).toBe(true)
  await router.push('/history')
  await flushPromises()
  expect(panel.find('button').attributes('aria-expanded')).toBe('true')
  state.snapshot!.activeTestId = 'next'
  state.snapshot!.jobs = [job({ id: 'next' })]
  await flushPromises()
  expect(panel.find('[role="region"]').isVisible()).toBe(true)
  await panel
    .find('button[aria-label="收合作業狀態"]')
    .trigger('keydown', { code: 'Space' })
  state.snapshot!.activeTestId = ''
  state.snapshot!.jobs = [job({ id: 'next', state: 'completed' })]
  await flushPromises()
  await vi.waitFor(() =>
    expect(panel.find('[role="region"]').isVisible()).toBe(false),
  )
  await panel.find('button').trigger('click')
  w.unmount()
  unmount = undefined
  const restarted = await setup(1280)
  expect(restarted.w.find('[role="status"] [role="region"]').isVisible()).toBe(
    false,
  )
})

it('刪除最後一筆歷史後全域面板消失，移除重複的 DNS 生效提示', async () => {
  state.snapshot!.activeTestId = 'job'
  state.snapshot!.jobs = [job()]
  const { w } = await setup(1280)
  await w.find('button[aria-label="展開作業狀態"]').trigger('click')
  expect(w.text()).not.toContain('DNS 已生效')
  expect(w.find('.layout-footer').text()).not.toContain('API 保存')
  state.snapshot!.activeTestId = ''
  state.snapshot!.jobs = []
  await flushPromises()
  expect(w.find('[role="status"]').exists()).toBe(false)
})

it('Sakai 行動側欄使用官方 FocusTrap，Escape 關閉後解除捲動並返回按鈕', async () => {
  const { w, trigger } = await setup()
  expect(trigger.attributes('aria-controls')).toBe('app-sidebar')
  expect(w.find('.layout-sidebar').attributes('inert')).toBeDefined()
  trigger.element.focus()
  await trigger.trigger('click')
  await flushPromises()
  expect(w.find('.layout-wrapper').classes()).toContain('layout-mobile-active')
  expect(document.body.classList.contains('blocked-scroll')).toBe(true)
  const sidebar = w.find('[role="dialog"]')
  expect(sidebar.attributes('aria-modal')).toBe('true')
  expect(sidebar.element.contains(document.activeElement)).toBe(true)
  const sentinels = sidebar.findAll<HTMLElement>(
    '[data-p-hidden-focusable="true"]',
  )
  expect(sentinels).toHaveLength(2)
  for (const sentinel of sentinels) {
    sentinel.element.focus()
    expect(sidebar.element.contains(document.activeElement)).toBe(true)
    expect(document.activeElement).not.toBe(sentinel.element)
  }
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  await flushPromises()
  expect(w.find('.layout-wrapper').classes()).not.toContain(
    'layout-mobile-active',
  )
  expect(document.body.classList.contains('blocked-scroll')).toBe(false)
  expect(w.find('.layout-sidebar').attributes('inert')).toBeDefined()
  expect(document.activeElement).toBe(trigger.element)
})

it('官方遮罩與選單導覽皆會關閉 Sakai 行動側欄，重開後焦點循環仍有效', async () => {
  const { w, trigger, router } = await setup()
  await trigger.trigger('click')
  await flushPromises()
  await w.find('.layout-mask').trigger('click')
  expect(trigger.attributes('aria-expanded')).toBe('false')
  await trigger.trigger('click')
  await flushPromises()
  expect(
    w.findAll('.layout-sidebar [data-p-hidden-focusable="true"]'),
  ).toHaveLength(2)
  await w.find('.layout-menu a[href="/settings"]').trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/settings')
  expect(trigger.attributes('aria-expanded')).toBe('false')
  expect(document.body.classList.contains('blocked-scroll')).toBe(false)
  expect(document.activeElement).toBe(trigger.element)
})

it('依官方 991／992px 分界切換側欄，卸載清除捲動及所有自有監聽', async () => {
  const { w, trigger, resize, removeListener, removeDocumentListener } =
    await setup(991)
  await trigger.trigger('click')
  await flushPromises()
  await resize(992)
  expect(trigger.attributes('aria-expanded')).toBe('true')
  expect(w.find('.layout-wrapper').classes()).not.toContain(
    'layout-mobile-active',
  )
  expect(w.find('.layout-sidebar').attributes('inert')).toBeUndefined()
  expect(document.body.classList.contains('blocked-scroll')).toBe(false)
  await resize(991)
  expect(trigger.attributes('aria-expanded')).toBe('false')
  await trigger.trigger('click')
  await flushPromises()
  expect(
    w.findAll('.layout-sidebar [data-p-hidden-focusable="true"]'),
  ).toHaveLength(2)
  w.unmount()
  unmount = undefined
  expect(removeListener).toHaveBeenCalledWith('resize', expect.any(Function))
  expect(removeDocumentListener).toHaveBeenCalledWith(
    'click',
    expect.any(Function),
  )
  expect(removeDocumentListener).toHaveBeenCalledWith(
    'keydown',
    expect.any(Function),
  )
  expect(document.body.classList.contains('blocked-scroll')).toBe(false)
})

it('桌面 overlay 沿用 Sakai 狀態與外點關閉，不替換成 Drawer', async () => {
  const { w, trigger } = await setup(1280)
  useLayout().layoutConfig.menuMode = 'overlay'
  await trigger.trigger('click')
  await flushPromises()
  expect(w.find('.layout-wrapper').classes()).toContain('layout-overlay-active')
  expect(useLayout().layoutState.staticMenuInactive).toBe(false)
  expect(useLayout().layoutState.mobileMenuActive).toBe(false)
  await w.find('.layout-sidebar').trigger('click')
  expect(useLayout().layoutState.overlayMenuActive).toBe(true)
  await w.find('.layout-main').trigger('click')
  expect(w.find('.layout-wrapper').classes()).not.toContain(
    'layout-overlay-active',
  )
})

it('v5 桌面 static／overlay 與行動選單分流，切換模式清除舊狀態', async () => {
  const { w, trigger, resize } = await setup(1280)
  const { layoutState, changeMenuMode, hasOpenOverlay } = useLayout()
  await trigger.trigger('click')
  expect(layoutState.staticMenuInactive).toBe(true)
  expect(hasOpenOverlay.value).toBe(false)
  changeMenuMode({ value: 'overlay' })
  await flushPromises()
  expect(layoutState.staticMenuInactive).toBe(false)
  await trigger.trigger('click')
  expect(hasOpenOverlay.value).toBe(true)
  expect(layoutState.mobileMenuActive).toBe(false)
  await resize(991)
  expect(hasOpenOverlay.value).toBe(false)
  await trigger.trigger('click')
  expect(layoutState.mobileMenuActive).toBe(true)
  expect(hasOpenOverlay.value).toBe(false)
  expect(layoutState.staticMenuInactive).toBe(false)
  expect(w.find('.layout-wrapper').classes()).not.toContain(
    'layout-static-inactive',
  )
  layoutState.sidebarExpanded = true
  layoutState.anchored = true
  layoutState.menuHoverActive = true
  changeMenuMode({ value: 'static' })
  await flushPromises()
  expect(layoutState.mobileMenuActive).toBe(false)
  expect(layoutState.sidebarExpanded).toBe(false)
  expect(layoutState.anchored).toBe(false)
  expect(layoutState.menuHoverActive).toBe(false)
  expect(document.body.classList.contains('blocked-scroll')).toBe(false)
  expect(document.activeElement).toBe(trigger.element)
})

it('v5 側欄路由同步 activePath，查詢參數導覽也會關閉並重設 hover', async () => {
  const { trigger, router, resize } = await setup(991)
  const { layoutState, changeMenuMode } = useLayout()
  expect(layoutState.activePath).toBe('/')
  await router.push('/domains')
  expect(layoutState.activePath).toBe('/domains')
  await trigger.trigger('click')
  layoutState.menuHoverActive = true
  await router.push('/domains?focus=example.com')
  await flushPromises()
  expect(layoutState.activePath).toBe('/domains')
  expect(layoutState.mobileMenuActive).toBe(false)
  expect(layoutState.menuHoverActive).toBe(false)
  await resize(1280)
  expect(layoutState.activePath).toBeNull()
  changeMenuMode({ value: 'overlay' })
  await trigger.trigger('click')
  layoutState.menuHoverActive = true
  await router.push('/settings')
  await flushPromises()
  expect(layoutState.activePath).toBeNull()
  expect(layoutState.overlayMenuActive).toBe(false)
  expect(layoutState.menuHoverActive).toBe(false)
})

it('桌面同側縮放不誤關 overlay，跨斷點後重新開啟不殘留外點監聽', async () => {
  const { trigger, resize, w } = await setup(1280)
  const { changeMenuMode, layoutState } = useLayout()
  const add = vi.spyOn(document, 'addEventListener')
  const remove = vi.spyOn(document, 'removeEventListener')
  changeMenuMode({ value: 'overlay' })
  await trigger.trigger('click')
  await flushPromises()
  await resize(1440)
  expect(layoutState.overlayMenuActive).toBe(true)
  await resize(991)
  expect(layoutState.overlayMenuActive).toBe(false)
  await trigger.trigger('click')
  await flushPromises()
  await w.find('.layout-main').trigger('click')
  expect(layoutState.mobileMenuActive).toBe(false)
  expect(add.mock.calls.filter(([event]) => event === 'click')).toHaveLength(2)
  expect(remove.mock.calls.filter(([event]) => event === 'click')).toHaveLength(
    2,
  )
})

it('官方頂欄主題動作仍將目前修訂及原探測設定交由 Go 保存', async () => {
  const { w } = await setup(1280)
  await w.find('button[aria-label="切換深淺色主題"]').trigger('click')
  await flushPromises()
  expect(Bridge.UpdateSettings).toHaveBeenCalledWith('revision', {
    theme: 'dark',
    probe: snapshot().settings.probe,
  })
})
