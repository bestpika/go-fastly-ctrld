import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { primePlugins } from '../test/prime'
import { ok, snapshot } from '../test/fixtures'
import Select from 'primevue/select'
vi.mock('../shared/app', async () => {
  const { reactive, ref } = await import('vue')
  return {
    state: reactive({ snapshot: null as ReturnType<typeof snapshot> | null }),
    busy: ref(false),
    working: ref(false),
    Bridge: {
      ListProfiles: vi.fn(),
      ListFolders: vi.fn(),
      ReadRuleList: vi.fn(),
      CreateFolder: vi.fn(),
      SelectFolder: vi.fn(),
      SelectProfile: vi.fn(),
      BuildSyncPreview: vi.fn(),
    },
    unwrap: async (
      p: Promise<{ ok: boolean; data: unknown; error?: unknown }>,
    ) => {
      const r = await p
      if (!r.ok) throw new Error('讀取失敗')
      return r.data
    },
    read: vi.fn(
      async (
        fn: () => Promise<{ ok: boolean; data: unknown }>,
        current = () => true,
      ) => {
        try {
          const r = await fn()
          return current() && r.ok ? r.data : undefined
        } catch {
          return undefined
        }
      },
    ),
    run: vi.fn(async (fn: () => Promise<{ ok: boolean; data: unknown }>) => {
      const r = await fn()
      return r.ok ? r.data : undefined
    }),
  }
})
import { Bridge, state, run } from '../shared/app'
import SyncView from './SyncView.vue'
beforeEach(() => {
  vi.clearAllMocks()
  state.snapshot = snapshot()
  state.snapshot.settings.profileId = 'profile'
  state.snapshot.credentials.saved = true
  vi.mocked(Bridge.ListProfiles).mockResolvedValue(
    ok([{ id: 'profile', name: '使用中' }]),
  )
  vi.mocked(Bridge.ListFolders).mockResolvedValue(
    ok([
      { id: 2, name: '繼承動作', action: true },
      { id: 7, name: '目的', action: false },
    ]),
  )
  vi.mocked(Bridge.ReadRuleList).mockResolvedValue(ok([]))
  vi.mocked(Bridge.SelectFolder).mockImplementation((_revision, id) => {
    state.snapshot!.settings.destinationFolderId = id
    state.snapshot!.settings.revision = 'folder-' + id
    return Promise.resolve(ok(true)) as ReturnType<typeof Bridge.SelectFolder>
  })
})
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
it('依序讀 Profile、folders、所選規則，讀取不走全域寫入忙碌', async () => {
  const w = mount(SyncView, options)
  await flushPromises()
  expect(
    vi.mocked(Bridge.ListProfiles).mock.invocationCallOrder[0],
  ).toBeLessThan(vi.mocked(Bridge.ListFolders).mock.invocationCallOrder[0]!)
  expect(
    vi.mocked(Bridge.ListFolders).mock.invocationCallOrder[0],
  ).toBeLessThan(vi.mocked(Bridge.ReadRuleList).mock.invocationCallOrder[0]!)
  expect(run).not.toHaveBeenCalled()
  const folderSelect = w.findAllComponents(Select)[1]!
  expect(folderSelect.props('optionDisabled')).toBe('action')
  expect(folderSelect.props('options')).toContainEqual(
    expect.objectContaining({ id: 2, action: true }),
  )
  await w.find('button[aria-label="新增一般資料夾"]').trigger('click')
  expect(w.text()).toContain('直接在所選 Profile 新增一般資料夾')
  await w.find('button[aria-label="取消新增資料夾"]').trigger('click')
  expect(Bridge.CreateFolder).not.toHaveBeenCalled()
  w.unmount()
})
it('資料夾讀取錯誤不能當空規則繼續', async () => {
  vi.mocked(Bridge.ListFolders).mockResolvedValue({
    ok: false,
    data: [],
    error: null,
  })
  const w = mount(SyncView, options)
  await flushPromises()
  expect(Bridge.ReadRuleList).not.toHaveBeenCalled()
  expect(
    w.find('button[aria-label="建立規則差異預覽"]').attributes('disabled'),
  ).toBeDefined()
  w.unmount()
})
it('讀取中能換資料夾並取消舊請求，舊回應不能覆蓋新的規則', async () => {
  let resolve!: (value: unknown) => void
  const cancel = vi.fn()
  const old = Object.assign(
    new Promise((r) => {
      resolve = r
    }),
    { cancel },
  ) as unknown as ReturnType<typeof Bridge.ReadRuleList>
  vi.mocked(Bridge.ReadRuleList)
    .mockReturnValueOnce(old)
    .mockResolvedValue(
      ok([
        {
          hostname: 'selected.example.com',
          group: 7,
          do: 2,
          status: 1,
          via: '151.101.0.49',
          viaV6: '',
          comment: '',
          safe: true,
          unsafeReason: '',
        },
      ]),
    )
  const w = mount(SyncView, options)
  await flushPromises()
  expect(w.findAllComponents(Select)[1]!.props('disabled')).toBe(false)
  expect(w.find('button[aria-label="取消讀取"]').exists()).toBe(true)
  // Profile 讀取串接至規則時仍只旋轉原發起按鈕，不讓全部操作一起忙碌。
  expect(
    w
      .find(
        'button[aria-label="重新讀取設定檔"] [data-pc-section="loadingicon"]',
      )
      .exists(),
  ).toBe(true)
  expect(
    w
      .find(
        'button[aria-label="重新讀取資料夾"] [data-pc-section="loadingicon"]',
      )
      .exists(),
  ).toBe(false)
  w.findAllComponents(Select)[1]!.vm.$emit('update:modelValue', 7)
  await flushPromises()
  expect(cancel).toHaveBeenCalled()
  expect(w.find('strong').text()).toBe('1')
  resolve(ok([]))
  await flushPromises()
  expect(w.find('strong').text()).toBe('1')
  expect(Bridge.SelectFolder).toHaveBeenCalledWith(expect.any(String), 7)
  w.unmount()
})
it('取消及切頁會把取消傳給未完成的 SDK 請求', async () => {
  const cancel = vi.fn()
  const pending = Object.assign(new Promise(() => {}), {
    cancel,
  }) as unknown as ReturnType<typeof Bridge.ReadRuleList>
  vi.mocked(Bridge.ReadRuleList).mockReturnValue(pending)
  const w = mount(SyncView, options)
  await flushPromises()
  await w.find('button[aria-label="取消讀取"]').trigger('click')
  expect(cancel).toHaveBeenCalledOnce()
  expect(w.find('button[aria-label="取消讀取"]').exists()).toBe(false)
  await w.find('button[aria-label="讀取所選資料夾規則"]').trigger('click')
  await flushPromises()
  w.unmount()
  expect(cancel).toHaveBeenCalledTimes(2)
})
