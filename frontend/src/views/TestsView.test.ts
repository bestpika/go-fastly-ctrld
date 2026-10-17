import { mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { primePlugins } from '../test/prime'
import DataTable from 'primevue/datatable'

vi.mock('../shared/app', async () => {
  const { snapshot } = await import('../test/fixtures')
  return {
    state: { snapshot: snapshot() },
    busy: false,
    working: false,
    Bridge: {},
    run: vi.fn(),
    bytes: String,
    date: String,
  }
})
import { state } from '../shared/app'
import TestsView from './TestsView.vue'

it('區別順暢輪數、HTTP 階段與未量得 TCP，不顯示假零延遲', async () => {
  state.snapshot!.results = [
    {
      id: 'result',
      strategyVersion: 4,
      baselineService: false,
      complete: true,
      rejected: [],
      dnsClassification: 'confirmed',
      reference: null,
      referenceReused: false,
      reusedFrom: '',
      service: 'Twitch',
      mode: 'download',
      probeHost: 'static-cdn.jtvnw.net',
      speedHost: 'assets.twitch.tv',
      scanCount: 1188,
      domainId: 'domain',
      hostname: 'www.twitch.tv',
      networkFingerprint: 'network',
      settingsRevision: 'revision',
      observedAt: '',
      expiresAt: '',
      bestIpv4: '',
      bestIpv6: '',
      bytes: 0,
      reason: '樣本不足',
      emergency: false,
      automaticEligible: false,
      candidates: [
        {
          ip: '151.101.130.214',
          pop: '?',
          family: 4,
          baseline: false,
          successes: 0,
          smoothRounds: 0,
          smoothThresholdMbps: 3,
          medianMbps: 0,
          medianLatency: 0,
          samples: [
            {
              success: false,
              actualIp: '151.101.130.214',
              httpStatus: 405,
              stage: 'http',
              hostname: 'static-cdn.jtvnw.net',
              tcpMilliseconds: 0,
              tlsMilliseconds: 0,
              ttfbMilliseconds: 0,
              elapsedMilliseconds: 0,
              mbps: 0,
              bytes: 0,
              pop: '',
              error: 'HTTP 狀態不允許',
            },
          ],
          referenceScores: [],
        },
      ],
    },
  ]
  const wrapper = mount(TestsView, {
    global: {
      plugins: primePlugins,
      stubs: {
        Dialog: {
          props: ['visible'],
          template: '<section v-if="visible"><slot /></section>',
        },
      },
    },
  })
  await wrapper
    .findAll('button')
    .find((b) => b.attributes('aria-label') === '查看候選')!
    .trigger('click')
  expect(wrapper.text()).toContain('順暢輪數')
  expect(wrapper.text()).toContain('http HTTP 405')
  expect(wrapper.text()).toContain('未量得')
  expect(wrapper.text()).toContain('機房')
  expect(wrapper.text()).toContain('未知')
  expect(wrapper.text()).toContain('0.00 Mbps')
  const info = wrapper
    .findAll('button')
    .find((b) => b.attributes('aria-label')?.includes('assets.twitch.tv'))!
  expect(info.exists()).toBe(true)
  await info.trigger('focus')
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(document.querySelector('[role="tooltip"]')?.textContent).toContain(
    'assets.twitch.tv',
  )
  await info.trigger('blur')
  expect(wrapper.text()).not.toContain('0.00 ms')
  await wrapper
    .find('button[aria-label="查看 151.101.130.214 逐輪量測"]')
    .trigger('click')
  expect(wrapper.text()).toContain('本文位元組')
  expect(wrapper.text()).toContain('TTFB')
  expect(wrapper.text()).toContain('完整請求耗時')
  wrapper.unmount()
})

it('推薦路線排前、X 短檔限制及同作業重用清楚標示', async () => {
  state.snapshot!.results = [
    {
      id: 'x',
      domainId: 'x',
      hostname: 'pbs.twimg.com',
      strategyVersion: 10,
      baselineService: true,
      complete: true,
      rejected: [],
      dnsClassification: 'confirmed',
      reference: null,
      referenceReused: false,
      reusedFrom: 'same-job-source',
      service: 'X',
      mode: 'download',
      probeHost: 'pbs.twimg.com',
      speedHost: 'pbs.twimg.com',
      scanCount: 2,
      networkFingerprint: 'network',
      settingsRevision: 'revision',
      observedAt: '',
      expiresAt: '',
      bestIpv4: '151.101.1.159',
      bestIpv6: '',
      bytes: 0,
      reason: '',
      emergency: false,
      automaticEligible: false,
      candidates: [
        {
          ip: '151.101.1.159',
          family: 4,
          pop: '?',
          baseline: false,
          successes: 3,
          smoothRounds: 3,
          smoothThresholdMbps: 3,
          medianMbps: 12,
          medianLatency: 20,
          referenceScores: [],
          samples: [],
        },
      ],
    },
  ]
  const w = mount(TestsView, {
    global: {
      plugins: primePlugins,
      stubs: {
        Dialog: {
          props: ['visible'],
          template: '<section v-if="visible"><slot /></section>',
        },
      },
    },
  })
  await w.find('button[aria-label="查看候選"]').trigger('click')
  expect(w.text()).toContain('推薦')
  expect(w.text()).toContain('短檔')
  expect(w.text()).toContain('流量不重複計算')
  w.unmount()
})

it('共享來源分數與目標未下載成績分開呈現', async () => {
  state.snapshot!.results = [
    {
      id: 'shared',
      strategyVersion: 4,
      baselineService: false,
      complete: true,
      rejected: [],
      dnsClassification: 'confirmed',
      service: 'custom',
      mode: 'shared-reference',
      probeHost: 'one.example.net',
      speedHost: '',
      scanCount: 2,
      domainId: 'target',
      hostname: 'one.example.net',
      networkFingerprint: 'network',
      settingsRevision: 'revision',
      observedAt: '',
      expiresAt: '',
      bestIpv4: '151.101.1.49',
      bestIpv6: '',
      bytes: 0,
      reason: '共享路線參考',
      emergency: false,
      automaticEligible: false,
      referenceReused: true,
      reusedFrom: '',
      reference: {
        id: 'reference',
        digest: 'digest',
        strategyVersion: 4,
        networkFingerprint: 'network',
        rangesDigest: 'ranges',
        probeDigest: 'probe',
        observedAt: '',
        expiresAt: '',
        bytes: 1000000,
        sources: [
          {
            name: 'Twitch',
            probeHost: 'static-cdn.jtvnw.net',
            speedHost: 'assets.twitch.tv',
            candidates: [],
          },
          {
            name: 'GitHub',
            probeHost: 'raw.githubusercontent.com',
            speedHost: 'release-assets.githubusercontent.com',
            candidates: [],
          },
        ],
      },
      candidates: [
        {
          ip: '151.101.1.49',
          pop: '?',
          family: 4,
          baseline: false,
          samples: [],
          successes: 3,
          medianMbps: 0,
          medianLatency: 10,
          smoothRounds: 0,
          smoothThresholdMbps: 3,
          referenceScores: [
            {
              source: 'Twitch',
              route: '151.101.1.0/24',
              actualIp: '151.101.1.214',
              speedHost: 'assets.twitch.tv',
              smoothRounds: 3,
              thresholdMbps: 30,
              medianMbps: 100,
              successes: 3,
              rounds: 3,
            },
          ],
        },
      ],
    },
  ]
  const wrapper = mount(TestsView, {
    global: {
      plugins: primePlugins,
      stubs: {
        Dialog: {
          props: ['visible'],
          template: '<section v-if="visible"><slot /></section>',
        },
      },
    },
  })
  await wrapper
    .findAll('button')
    .find((b) => b.attributes('aria-label') === '查看候選')!
    .trigger('click')
  expect(wrapper.text()).toContain('非目標下載成績')
  expect(wrapper.text()).toContain('未量下載')
  expect(wrapper.text()).toContain('參考 100.00 Mbps')
  expect(wrapper.text()).toContain('151.101.1.214')
  expect(wrapper.text()).toContain('重用本作業實測基準')
  wrapper.unmount()
})

it('結果頁不重複提供測試啟動入口', () => {
  state.snapshot!.domains = []
  state.snapshot!.results = []
  const wrapper = mount(TestsView, {
    global: { plugins: primePlugins, stubs: { Dialog: true } },
  })
  expect(wrapper.find('button[aria-label="開始測速"]').exists()).toBe(false)
  wrapper.unmount()
})

it('十二個基準全列探測網域，僅三個另做下載測速', () => {
  const hosts = [
    'release-assets.githubusercontent.com',
    'assets.twitch.tv',
    'pbs.twimg.com',
    'raw.githubusercontent.com',
    'www.twitch.tv',
    'gql.twitch.tv',
    'k.twitchcdn.net',
    'static-cdn.jtvnw.net',
    'jtvnw.twitchcdn.net',
    'abs.twimg.com',
    'video.twimg.com',
    'video-s.twimg.com',
  ]
  state.snapshot!.domains = []
  state.snapshot!.results = hosts.map((hostname, i) => ({
    id: `baseline-${i}`,
    domainId: `baseline-${i}`,
    hostname,
    baselineService: true,
    strategyVersion: 10,
    complete: true,
    rejected: [],
    candidates: [],
    dnsClassification: 'confirmed',
    reference: null,
    referenceReused: false,
    reusedFrom: '',
    service: i === 0 || i === 3 ? 'GitHub' : i === 2 || i > 8 ? 'X' : 'Twitch',
    mode: i < 3 ? 'download' : 'connection',
    probeHost: hostname,
    speedHost: i < 3 ? hostname : '',
    scanCount: 2,
    networkFingerprint: 'network',
    settingsRevision: 'revision',
    observedAt: '',
    expiresAt: '',
    bestIpv4: '',
    bestIpv6: '',
    bytes: 0,
    reason: '基準不套用',
    emergency: false,
    automaticEligible: false,
  }))
  const wrapper = mount(TestsView, {
    global: { plugins: primePlugins, stubs: { Dialog: true } },
  })
  const baselines =
    wrapper.findAllComponents(DataTable)[0]!.props('value') ?? []
  expect(baselines).toHaveLength(12)
  expect(baselines.filter((r) => r.mode === 'download')).toHaveLength(3)
  expect(baselines.filter((r) => r.mode === 'connection')).toHaveLength(9)
  expect(wrapper.text()).toContain('探測網域')
  expect(wrapper.text()).toContain('下載測速')
  expect(wrapper.text()).toContain('僅連線延遲')
  expect(wrapper.text()).not.toContain('代表網域')
  wrapper.unmount()
})
