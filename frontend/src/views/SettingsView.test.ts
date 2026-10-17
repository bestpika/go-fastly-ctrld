import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import ConfirmDialog from 'primevue/confirmdialog'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { primePlugins } from '../test/prime'
import { ok, snapshot } from '../test/fixtures'
import Select from 'primevue/select'
import InputNumber from 'primevue/inputnumber'
import Checkbox from 'primevue/checkbox'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({
    GetAppSnapshot: vi.fn(),
    SetControlDToken: vi.fn(),
    ConfigureAutomation: vi.fn(),
    UpdateSettings: vi.fn(),
    CheckControlDConnection: vi.fn(),
    SetLaunchAtLogin: vi.fn(),
  }),
)
import SettingsView from './SettingsView.vue'
import { Bridge, state } from '../shared/app'
import { fonts, initializeFont } from '../shared/fonts'
const options = {
  global: {
    plugins: primePlugins,
    stubs: {
      Dialog: {
        props: ['visible'],
        template:
          '<section v-if="visible"><slot/><slot name="footer"/></section>',
      },
    },
  },
}
describe('簡化設定與完整授權', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    state.snapshot = snapshot()
    state.busy = 0
    state.error = ''
    state.alerts = []
    state.loading = false
    localStorage.clear()
    fonts.families = []
    initializeFont()
    vi.mocked(Bridge.GetAppSnapshot).mockResolvedValue(ok(snapshot()))
  })
  afterEach(() => vi.unstubAllGlobals())
  it('開啟字型清單才列舉，每次更新且不影響宿主修訂與授權', async () => {
    const query = vi.fn().mockResolvedValue([{ family: 'Microsoft JhengHei' }])
    vi.stubGlobal('queryLocalFonts', query)
    const revision = state.snapshot!.settings.revision
    const automation = state.snapshot!.settings.automation
    state.busy = 1
    const w = mount(SettingsView, options)
    const select = w
      .findAllComponents(Select)
      .find((c) => c.props('inputId') === 'app-font')!
    expect(query).not.toHaveBeenCalled()
    expect(select.props('disabled')).toBe(false)
    select.vm.$emit('show')
    await flushPromises()
    select.vm.$emit('update:modelValue', 'Microsoft JhengHei')
    await flushPromises()
    expect(document.body.style.fontFamily).toContain('Microsoft JhengHei')
    expect(state.snapshot!.settings.revision).toBe(revision)
    expect(state.snapshot!.settings.automation).toBe(automation)
    expect(Bridge.UpdateSettings).not.toHaveBeenCalled()
    expect(Bridge.ConfigureAutomation).not.toHaveBeenCalled()
    expect(Bridge.GetAppSnapshot).not.toHaveBeenCalled()
    select.vm.$emit('show')
    await flushPromises()
    expect(query).toHaveBeenCalledTimes(2)
    w.unmount()
  })
  it('字型保存失敗時，官方選單與預覽都保留目前字型', async () => {
    vi.stubGlobal(
      'queryLocalFonts',
      vi.fn().mockResolvedValue([{ family: 'Microsoft JhengHei' }]),
    )
    const w = mount(SettingsView, { ...options, attachTo: document.body })
    const select = w
      .findAllComponents(Select)
      .find((c) => c.props('inputId') === 'app-font')!
    expect(w.get('#app-font').text()).toBe('預設 Lato')
    select.vm.$emit('show')
    await flushPromises()
    const save = vi
      .spyOn(Storage.prototype, 'setItem')
      .mockImplementation(() => {
        throw new DOMException('blocked', 'QuotaExceededError')
      })
    try {
      await w.get('#app-font').trigger('click')
      await flushPromises()
      await new DOMWrapper(document.body)
        .get('[role="option"][aria-label="Microsoft JhengHei"]')
        .trigger('mousedown')
      await flushPromises()
      expect(save).toHaveBeenCalledOnce()
      expect(fonts.family).toBe('')
      expect(document.body.style.fontFamily).toBe('')
      expect(select.props('modelValue')).toBe('')
      expect(w.get('#app-font').text()).toBe('預設 Lato')
      expect(state.alerts.some((a) => a.message === '無法保存字型設定')).toBe(
        true,
      )
    } finally {
      save.mockRestore()
      w.unmount()
    }
  })
  it('帳號讀取僅本區載入，可取消且不重刷測速快照', async () => {
    // 僅模擬已有權杖的狀態，不操作真實金鑰庫。
    state.snapshot!.credentials.saved = true
    let finish!: (value: ReturnType<typeof ok<boolean>>) => void
    const cancel = vi.fn()
    const pending = Object.assign(
      new Promise<ReturnType<typeof ok<boolean>>>((resolve) => {
        finish = resolve
      }),
      { cancel },
    )
    vi.mocked(Bridge.CheckControlDConnection).mockReturnValue(
      pending as unknown as ReturnType<typeof Bridge.CheckControlDConnection>,
    )
    const w = mount(SettingsView, options)
    await w
      .find('[aria-label="Control D 權杖"]')
      .setValue('unsaved-local-test-token')
    await w.find('[aria-label="測試帳號讀取連線"]').trigger('click')
    expect(Bridge.CheckControlDConnection).toHaveBeenCalledOnce()
    expect(state.busy).toBe(0)
    expect(Bridge.GetAppSnapshot).not.toHaveBeenCalled()
    expect(
      w.find('[aria-label="保存權杖"]').attributes('disabled'),
    ).toBeUndefined()
    await w.find('[aria-label="取消讀取帳號連線"]').trigger('click')
    expect(cancel).toHaveBeenCalledOnce()
    finish(ok(true))
    await flushPromises()
    expect(state.alerts).toHaveLength(0)
    w.unmount()
  })
  it('立即清空權杖，不寫瀏覽器儲存', async () => {
    const local = vi.spyOn(Storage.prototype, 'setItem')
    vi.mocked(Bridge.SetControlDToken).mockResolvedValue(ok(true))
    const w = mount(SettingsView, options)
    await w.find('[aria-label="Control D 權杖"]').setValue('private-test-token')
    await w.find('button[aria-label="保存權杖"]').trigger('click')
    expect(
      (w.find('input[type=password]').element as HTMLInputElement).value,
    ).toBe('')
    expect(Bridge.SetControlDToken).toHaveBeenCalledWith('private-test-token')
    await flushPromises()
    expect(w.html()).not.toContain('private-test-token')
    expect(local).not.toHaveBeenCalled()
    local.mockRestore()
    w.unmount()
  })
  it('自動套用必須確認 Dialog，取消不提交', async () => {
    const w = mount(SettingsView, options)
    w.findComponent(Select).vm.$emit('update:modelValue', 'apply')
    await flushPromises()
    await w.find('button[aria-label="保存自動設定"]').trigger('click')
    expect(w.text()).toContain('僅限勾選網域')
    await w.find('button[aria-label="取消授權"]').trigger('click')
    expect(Bridge.ConfigureAutomation).not.toHaveBeenCalled()
    w.unmount()
  })
  it('外觀與啟動依 Sakai 結構分組，標籤可操作官方勾選控制', async () => {
    state.snapshot!.settings.forceWrite = true
    vi.mocked(Bridge.UpdateSettings).mockResolvedValue(ok(true))
    vi.mocked(Bridge.SetLaunchAtLogin).mockResolvedValue(ok(true))
    const w = mount(SettingsView, { ...options, attachTo: document.body })
    const dark = w.get('input#dark-mode')
    const launch = w.get('input#launch-at-login')
    expect(w.get('label[for="dark-mode"]').element.parentElement).toBe(
      dark.element.parentElement!.parentElement,
    )
    expect(w.get('label[for="launch-at-login"]').element.parentElement).toBe(
      launch.element.parentElement!.parentElement,
    )
    await w.get('label[for="dark-mode"]').trigger('click')
    await flushPromises()
    expect(Bridge.UpdateSettings).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ theme: 'dark' }),
    )
    expect(
      vi.mocked(Bridge.UpdateSettings).mock.calls[0]![1],
    ).not.toHaveProperty('forceWrite')
    await w.get('label[for="launch-at-login"]').trigger('click')
    await flushPromises()
    expect(Bridge.SetLaunchAtLogin).toHaveBeenCalledWith(true)
    w.unmount()
  })
  it('固定策略無可調欄位，進階保留舊上限且僅明確修改才解除', async () => {
    state.snapshot!.settings.automation.budgetBytes = 100_000_000
    state.snapshot!.settings.probe.jobSeconds = 600
    vi.mocked(Bridge.UpdateSettings).mockResolvedValue(ok(true))
    vi.mocked(Bridge.ConfigureAutomation).mockResolvedValue(ok(true))
    const w = mount(SettingsView, options)
    expect(w.find('select[aria-label="掃描模式"]').exists()).toBe(false)
    expect(w.find('input[aria-label="候選組數"]').exists()).toBe(false)
    expect(
      (w.find('[aria-label="整作業流量 MB"]').element as HTMLInputElement)
        .value,
    ).toBe('100')
    expect(
      (w.find('[aria-label="整作業期限秒"]').element as HTMLInputElement).value,
    ).toBe('600')
    const numbers = w.findAllComponents(InputNumber)
    numbers[1]!.vm.$emit('update:modelValue', 0)
    numbers[2]!.vm.$emit('update:modelValue', 0)
    await flushPromises()
    await w.find('button[aria-label="保存測試策略與上限"]').trigger('click')
    await flushPromises()
    expect(Bridge.UpdateSettings).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        probe: expect.objectContaining({
          jobSeconds: 0,
          candidates: 10,
          rounds: 3,
          services: ['github', 'twitch', 'x'],
          noResourceMode: 'latency',
          enableIpv6: false,
        }),
      }),
    )
    expect(Bridge.ConfigureAutomation).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ budgetBytes: 0, enabled: false, grants: [] }),
    )
    w.unmount()
  })
  it('僅明確選用擴充並保存服務清單，不修改原快照且停止舊授權', async () => {
    vi.mocked(Bridge.UpdateSettings).mockResolvedValue(ok(true))
    vi.mocked(Bridge.ConfigureAutomation).mockResolvedValue(ok(true))
    const w = mount(SettingsView, options)
    const checkboxes = w.findAllComponents(Checkbox)
    const service = checkboxes.find((c) => c.props('value') === 'github')!
    service.vm.$emit('update:modelValue', [])
    const ipv6 = checkboxes.find((c) => c.props('inputId') === 'enable-ipv6')!
    ipv6.vm.$emit('update:modelValue', true)
    const strategy = w
      .findAllComponents(Select)
      .find((c) => c.props('ariaLabel') === '無測速檔策略')!
    strategy.vm.$emit('update:modelValue', 'shared')
    await flushPromises()
    expect(state.snapshot!.settings.probe.services).toEqual([
      'github',
      'twitch',
      'x',
    ])
    expect(state.snapshot!.settings.probe.enableIpv6).toBe(false)
    await w.find('button[aria-label="保存測試策略與上限"]').trigger('click')
    await flushPromises()
    expect(Bridge.UpdateSettings).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        probe: expect.objectContaining({
          services: [],
          noResourceMode: 'shared',
          enableIpv6: true,
        }),
      }),
    )
    expect(Bridge.ConfigureAutomation).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ enabled: false, grants: [] }),
    )
    w.unmount()
  })
  it('強制寫入預設關閉，取消不保存，確認才送出開關且不啟用自動', async () => {
    vi.mocked(Bridge.UpdateSettings).mockResolvedValue(ok(true))
    const Host = defineComponent({
      components: { SettingsView, ConfirmDialog },
      template: '<ConfirmDialog group="app"/><SettingsView/>',
    })
    const w = mount(Host, { global: { plugins: primePlugins } })
    const checkbox = w
      .findAllComponents(Checkbox)
      .find((c) => c.props('inputId') === 'force-write')!
    expect(checkbox.props('modelValue')).toBe(false)
    checkbox.vm.$emit('update:modelValue', true)
    await flushPromises()
    await w.get('[aria-label="保存強制寫入設定"]').trigger('click')
    const body = new DOMWrapper(document.body)
    expect(body.get('.p-confirmdialog').text()).toContain('原始 DNS')
    expect(body.get('.p-confirmdialog').text()).toContain('略過冷卻')
    await body
      .findAll('button')
      .find((b) => b.text() === '取消')!
      .trigger('click')
    await flushPromises()
    expect(Bridge.UpdateSettings).not.toHaveBeenCalled()
    await w.get('[aria-label="保存強制寫入設定"]').trigger('click')
    await body
      .findAll('button')
      .find((b) => b.text() === '確認')!
      .trigger('click')
    await flushPromises()
    expect(Bridge.UpdateSettings).toHaveBeenCalledExactlyOnceWith(
      'revision',
      expect.objectContaining({ forceWrite: true }),
    )
    expect(Bridge.ConfigureAutomation).not.toHaveBeenCalled()
    w.unmount()
  })
  it('強制模式的自動授權顯示原始 DNS、同值與冷卻例外', async () => {
    state.snapshot!.settings.forceWrite = true
    const w = mount(SettingsView, options)
    w.findComponent(Select).vm.$emit('update:modelValue', 'apply')
    await flushPromises()
    await w.get('[aria-label="保存自動設定"]').trigger('click')
    expect(w.text()).toContain(
      '強制寫入：無改善時回填原始 DNS，同值仍寫入並略過冷卻',
    )
    expect(w.text()).toContain('仍須下列授權')
    await w.get('[aria-label="取消授權"]').trigger('click')
    expect(Bridge.ConfigureAutomation).not.toHaveBeenCalled()
    w.unmount()
  })
})
