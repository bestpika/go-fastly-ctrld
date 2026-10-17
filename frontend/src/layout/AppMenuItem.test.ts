import { flushPromises, mount } from '@vue/test-utils'
import type { MenuItem } from 'primevue/menuitem'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import AppMenuItem from './AppMenuItem.vue'
import { useLayout } from './composables/layout'

let unmount: (() => void) | undefined
beforeEach(() => {
  vi.stubGlobal('innerWidth', 1280)
  const { changeMenuMode, layoutState } = useLayout()
  changeMenuMode({ value: 'static' })
  layoutState.activePath = null
})
afterEach(() => {
  unmount?.()
  unmount = undefined
  vi.unstubAllGlobals()
})
async function setup(item: MenuItem, root = false) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  })
  await router.push('/')
  const w = mount(AppMenuItem, {
    props: { item, root },
    global: { plugins: [router] },
  })
  unmount = () => w.unmount()
  await flushPromises()
  return { w, router }
}

it('v5 遞迴選單以路徑及鍵盤展開，重複片段收合仍保留正確父路徑', async () => {
  const { w } = await setup({
    label: '父群組',
    path: '/group',
    items: [
      {
        label: '子群組',
        path: '/group',
        items: [{ label: '詳細', to: '/group/group/details' }],
      },
    ],
  })
  const { layoutState } = useLayout()
  const [parent, child] = w.findAll('a:not([href])')
  await parent!.trigger('keydown', { key: 'Enter' })
  expect(layoutState.activePath).toBe('/group')
  expect(parent!.attributes('aria-expanded')).toBe('true')
  await child!.trigger('keydown', { key: ' ' })
  expect(layoutState.activePath).toBe('/group/group')
  expect(child!.attributes('aria-expanded')).toBe('true')
  await child!.trigger('click')
  expect(layoutState.activePath).toBe('/group')
  expect(child!.attributes('aria-expanded')).toBe('false')
  await parent!.trigger('click')
  expect(layoutState.activePath).toBeNull()
})

it('v5 子項重新排序仍以穩定路徑維持目前展開群組', async () => {
  const first = {
    label: '第一組',
    path: '/first',
    items: [{ label: '第一頁', to: '/root/first/page' }],
  }
  const second = {
    label: '第二組',
    path: '/second',
    items: [{ label: '第二頁', to: '/root/second/page' }],
  }
  const item = { label: '根群組', path: '/root', items: [first, second] }
  const { w } = await setup(item)
  useLayout().layoutState.activePath = '/root/second'
  await flushPromises()
  await w.setProps({ item: { ...item, items: [second, first] } })
  const groups = w.findAll('a:not([href])')
  expect(groups[1]!.text()).toContain('第二組')
  expect(groups[1]!.attributes('aria-expanded')).toBe('true')
  expect(groups[2]!.attributes('aria-expanded')).toBe('false')
})

it.each([
  [1280, '/next'],
  [991, '/previous'],
])('v5 根群組 hover 僅於桌面啟用：%ipx', async (width, expected) => {
  vi.stubGlobal('innerWidth', width)
  const { w } = await setup(
    {
      label: '下一群組',
      path: '/next',
      items: [{ label: '頁面', to: '/next/page' }],
    },
    true,
  )
  const { layoutState } = useLayout()
  layoutState.activePath = '/previous'
  await w.find('a:not([href])').trigger('mouseenter')
  expect(layoutState.activePath).toBe('/previous')
  layoutState.menuHoverActive = true
  await w.find('a:not([href])').trigger('mouseenter')
  expect(layoutState.activePath).toBe(expected)
})

it.each([true, () => true])(
  '停用項目阻止導覽與 command，不變更目前布局狀態：%s',
  async (disabled) => {
    const command = vi.fn()
    const { w, router } = await setup({
      label: '停用頁面',
      to: '/disabled',
      disabled,
      command,
    })
    const { layoutState } = useLayout()
    layoutState.overlayMenuActive = true
    const link = w.find('a')
    expect(link.attributes('aria-disabled')).toBe('true')
    await link.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/')
    expect(command).not.toHaveBeenCalled()
    expect(layoutState.overlayMenuActive).toBe(true)
  },
)

it('葉節點執行 command 並關閉選單，路由高亮使用官方 exactActiveClass', async () => {
  const command = vi.fn()
  const { w, router } = await setup({
    label: '網域',
    to: '/domains',
    disabled: () => false,
    command,
  })
  const { layoutState } = useLayout()
  layoutState.mobileMenuActive = true
  layoutState.menuHoverActive = true
  await w.find('a').trigger('click')
  await flushPromises()
  expect(command).toHaveBeenCalledOnce()
  expect(layoutState.mobileMenuActive).toBe(false)
  expect(layoutState.menuHoverActive).toBe(false)
  expect(w.find('a').classes()).toContain('active-route')
  await router.push('/domains/details')
  expect(w.find('a').classes()).not.toContain('active-route')
})
