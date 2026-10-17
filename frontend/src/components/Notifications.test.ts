import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { primePlugins } from '../test/prime'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({}),
)
import { state, notify } from '../shared/app'
import Notifications from './Notifications.vue'
afterEach(() => {
  vi.useRealTimers()
  state.alerts = []
})
it('官方 Toast 恢復初始訊息，重複計次替換不誤清除通知', async () => {
  state.alerts = []
  notify('error', '根目錄讀取失敗', 'id', 1)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div/>' } }],
  })
  await router.push('/')
  const w = mount(Notifications, {
    global: { plugins: [router, ...primePlugins] },
  })
  await nextTick()
  expect(document.querySelector('.p-toast-bottom-right')).not.toBeNull()
  expect(document.body.textContent).toContain('根目錄讀取失敗')
  expect(document.querySelector('.p-toast .pi-times-circle')).not.toBeNull()
  const action = document.querySelector<HTMLButtonElement>(
    'button[aria-label="開啟錯誤紀錄 根目錄讀取失敗"]',
  )
  expect(action?.classList.contains('p-button')).toBe(true)
  expect(action?.type).toBe('button')
  notify('error', '根目錄讀取失敗', 'id', 3)
  await nextTick()
  expect(state.alerts).toHaveLength(1)
  expect(state.alerts[0]?.count).toBe(3)
  expect(document.querySelectorAll('.p-toast-message')).toHaveLength(1)
  expect(document.body.textContent).toContain('×3')
  w.unmount()
})
it('成功由官方 life 自動消失，重要錯誤維持至手動關閉', async () => {
  vi.useFakeTimers()
  state.alerts = []
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div/>' } }],
  })
  await router.push('/')
  const w = mount(Notifications, {
    global: { plugins: [router, ...primePlugins] },
  })
  notify('error', '重要錯誤', 'id')
  notify('success', '已保存')
  await nextTick()
  await nextTick()
  expect(document.querySelector('.p-toast .pi-check-circle')).not.toBeNull()
  await vi.advanceTimersByTimeAsync(6500)
  await nextTick()
  expect(state.alerts).toHaveLength(1)
  expect(state.alerts[0]?.errorId).toBe('id')
  expect(document.body.textContent).not.toContain('已保存')
  w.unmount()
})
it('一般警告與無深連結錯誤保留嚴重程度圖示及可關閉通知', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div/>' } }],
  })
  await router.push('/')
  const w = mount(Notifications, {
    global: { plugins: [router, ...primePlugins] },
  })
  notify('warn', '請核對授權')
  notify('error', '無法取得本機狀態')
  await nextTick()
  expect(
    document.querySelector('.p-toast .pi-exclamation-triangle'),
  ).not.toBeNull()
  expect(document.querySelector('.p-toast .pi-times-circle')).not.toBeNull()
  expect(
    document.querySelectorAll('button[aria-label="關閉通知"]'),
  ).toHaveLength(2)
  w.unmount()
})
