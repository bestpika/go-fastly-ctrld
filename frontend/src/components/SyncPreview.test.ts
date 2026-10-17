import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PrimeVue from 'primevue/config'
vi.mock('@wailsio/runtime', () => ({ Events: { On: vi.fn(() => vi.fn()) } }))
vi.mock(
  '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/desktop/bridge',
  () => ({}),
)
import SyncPreview from './SyncPreview.vue'
import type { SyncPreview as Preview } from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/domain/models'

describe('不可變更預覽確認', () => {
  const preview: Preview = {
    id: 'preview',
    kind: 'apply',
    profileId: 'profile',
    credentialRevision: 'credential',
    settingsRevision: 'revision',
    networkFingerprint: 'network',
    expiresAt: '',
    items: [],
  }
  it('沒有可執行項目時不能套用', () => {
    const wrapper = mount(SyncPreview, {
      props: { preview: { ...preview, items: null }, busy: false },
      global: { plugins: [PrimeVue] },
    })
    expect(
      wrapper.findAll('button').at(-1)?.attributes('disabled'),
    ).toBeDefined()
    wrapper.unmount()
  })
  it('只在按確認時送出事件，取消不套用', async () => {
    const item = {
      id: 'item',
      domainId: 'domain',
      hostname: 'www.twitch.tv',
      operation: 'create',
      before: null,
      after: null,
      beforeDigest: '',
      applicable: true,
      reason: '',
      move: false,
      resultId: '',
      restoreOf: '',
      forceWrite: false,
      originalDns: null,
    }
    const wrapper = mount(SyncPreview, {
      props: { preview: { ...preview, items: [item] }, busy: false },
      global: { plugins: [PrimeVue] },
    })
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper.findAll('button').at(-2)!.trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper.findAll('button').at(-1)!.trigger('click')
    expect(wrapper.emitted('apply')).toHaveLength(1)
    wrapper.unmount()
  })
  it('強制同值重寫清楚標示原始 DNS 與成功條件，不自動送出', () => {
    const wrapper = mount(SyncPreview, {
      props: {
        preview: {
          ...preview,
          items: [
            {
              id: 'force',
              domainId: 'domain',
              hostname: 'cdn.example.com',
              operation: 'rewrite',
              before: null,
              after: null,
              beforeDigest: '',
              applicable: true,
              reason: '無改善，回填原始 CNAME edge.fastly.net',
              move: false,
              resultId: 'result',
              restoreOf: '',
              forceWrite: true,
              originalDns: {
                domainId: 'domain',
                hostname: 'cdn.example.com',
                classification: 'confirmed',
                observedAt: '',
                expiresAt: '',
                sources: [],
                candidates: [],
                warnings: [],
              },
            },
          ],
        },
        busy: false,
      },
      global: { plugins: [PrimeVue] },
    })
    expect(wrapper.text()).toContain('同值重寫')
    expect(wrapper.text()).toContain('無改善時回填原始 DNS，同值仍提交')
    expect(wrapper.text()).toContain('Google＋Cloudflare')
    expect(wrapper.text()).toContain('無改善，回填原始 CNAME')
    expect(wrapper.emitted('apply')).toBeUndefined()
    wrapper.unmount()
  })
  it.each(['apply', 'restore'])(
    '略過項目不冒稱刪除；只有復原刪除會明示刪除：%s',
    async (kind) => {
      const item = {
        id: 'skipped',
        domainId: 'domain',
        hostname: 'assets.twitch.tv',
        operation: 'skip',
        before: null,
        after: null,
        beforeDigest: '',
        applicable: false,
        reason: '測試未完成，保留原規則',
        move: false,
        resultId: '',
        restoreOf: '',
        forceWrite: false,
        originalDns: null,
      }
      const wrapper = mount(SyncPreview, {
        props: { preview: { ...preview, kind, items: [item] }, busy: false },
        global: { plugins: [PrimeVue] },
      })
      expect(wrapper.text()).toContain('不變更，保留原值')
      expect(wrapper.text()).not.toContain('刪除當時新增的規則')
      wrapper.unmount()
      if (kind === 'restore') {
        const deletion = mount(SyncPreview, {
          props: {
            preview: {
              ...preview,
              kind,
              items: [{ ...item, operation: 'delete', applicable: true }],
            },
            busy: false,
          },
          global: { plugins: [PrimeVue] },
        })
        expect(deletion.text()).toContain('刪除當時新增的規則')
        await deletion.setProps({
          preview: {
            ...preview,
            kind,
            items: [{ ...item, operation: 'delete', applicable: false }],
          },
        })
        expect(deletion.text()).toContain('不變更，保留原值')
        expect(deletion.text()).not.toContain('刪除當時新增的規則')
        deletion.unmount()
      }
    },
  )
})
