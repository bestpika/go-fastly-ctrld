import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import PrimeVue from 'primevue/config'
import AccordionPanel from 'primevue/accordionpanel'
import AboutView from './AboutView.vue'

const runtime = vi.hoisted(() => ({ OpenURL: vi.fn() }))
vi.mock('@wailsio/runtime', () => ({ Browser: runtime }))
vi.mock('../../generated/about.json', () => ({
  default: {
    build: {
      version: 'test-version',
      revision: 'test-revision',
      dirty: false,
      platform: 'windows-amd64',
      builtAt: 'time',
      go: 'Go',
      node: 'Node',
      wails: 'Wails',
    },
    components: [
      {
        id: 'vue',
        name: 'Vue',
        version: '3.5',
        kind: 'npm',
        license: 'MIT',
        source: 'https://github.com/vuejs/core',
        files: [{ name: 'LICENSE', text: 'Vue 完整授權原文' }],
      },
      {
        id: 'font',
        name: 'Inter',
        version: '5.3',
        kind: 'npm',
        license: 'OFL-1.1',
        source: 'https://github.com/rsms/inter',
        files: [{ name: 'LICENSE', text: '字型完整授權原文' }],
      },
    ],
  },
}))

describe('離線開源授權頁', () => {
  beforeEach(() => runtime.OpenURL.mockReset())
  it('顯示版本、完整授權並支援搜尋', async () => {
    const wrapper = mount(AboutView, { global: { plugins: [PrimeVue] } })
    expect(wrapper.text()).toContain('test-version')
    expect(wrapper.text()).toContain('test-revision')
    expect(wrapper.findAllComponents(AccordionPanel)).toHaveLength(2)
    const header = wrapper.get('button[data-pc-name="accordionheader"]')
    expect(header.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('pre').isVisible()).toBe(false)
    await header.trigger('click')
    expect(header.attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('pre').isVisible()).toBe(true)
    expect(wrapper.find('pre').text()).toBe('Vue 完整授權原文')
    await wrapper.get('input').setValue('OFL')
    expect(wrapper.findAllComponents(AccordionPanel)).toHaveLength(1)
    await wrapper.get('button[data-pc-name="accordionheader"]').trigger('click')
    expect(wrapper.find('pre').isVisible()).toBe(true)
    expect(wrapper.find('pre').text()).toBe('字型完整授權原文')
    await wrapper.get('input').setValue('不存在')
    expect(wrapper.text()).toContain('沒有符合搜尋條件')
    wrapper.unmount()
  })
  it('來源只交給系統瀏覽器，不在 WebView 建立遠端連結', async () => {
    const wrapper = mount(AboutView, { global: { plugins: [PrimeVue] } })
    await wrapper.get('button[data-pc-name="accordionheader"]').trigger('click')
    await wrapper
      .find('button[aria-label="以系統瀏覽器開啟來源"]')
      .trigger('click')
    expect(runtime.OpenURL).toHaveBeenCalledWith(
      'https://github.com/vuejs/core',
    )
    expect(wrapper.find('a[href]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('保留官方展開按鈕、圖示、ARIA 與鍵盤導覽，可同時展開不同原文', async () => {
    const wrapper = mount(AboutView, {
      attachTo: document.body,
      global: { plugins: [PrimeVue] },
    })
    const headers = wrapper.findAll('button[data-pc-name="accordionheader"]')
    expect(headers).toHaveLength(2)
    const first = headers[0]!,
      second = headers[1]!
    expect(first.find('svg').exists()).toBe(true)
    expect(first.get('strong').text()).toBe('Vue')
    expect(first.get('span.font-mono').text()).toBe('3.5')
    const contentId = first.attributes('aria-controls')!
    const content = wrapper.get(`[id="${contentId}"]`)
    expect(content.attributes('aria-labelledby')).toBe(first.attributes('id'))
    await first.trigger('keydown', { code: 'Enter' })
    expect(first.attributes('aria-expanded')).toBe('true')
    expect(wrapper.findAll('pre')[0]!.isVisible()).toBe(true)
    await first.trigger('keydown', { code: 'ArrowDown' })
    expect(document.activeElement).toBe(second.element)
    await second.trigger('keydown', { code: 'Space' })
    expect(second.attributes('aria-expanded')).toBe('true')
    expect(first.attributes('aria-expanded')).toBe('true')
    await second.trigger('keydown', { code: 'Home' })
    expect(document.activeElement).toBe(first.element)
    await first.trigger('keydown', { code: 'Space' })
    expect(first.attributes('aria-expanded')).toBe('false')
    expect(wrapper.findAll('pre')[0]!.isVisible()).toBe(false)
    expect(wrapper.findAll('pre')[1]!.isVisible()).toBe(true)
    wrapper.unmount()
  })
})
