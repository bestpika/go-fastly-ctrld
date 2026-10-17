import type { Snapshot } from '../shared/app'

export function snapshot(): Snapshot {
  return {
    version: '0.1.0-test',
    sequence: 1,
    automationError: '',
    settings: {
      presetVersion: 2,
      destinationFolderId: 0,
      revision: 'revision',
      credentialRevision: 'credential',
      profileId: 'profile',
      theme: 'light',
      launchAtLogin: false,
      forceWrite: false,
      cursor: 0,
      automation: {
        destinationFolderId: 0,
        allowSharedReference: false,
        testOnly: false,
        enabled: false,
        profileId: '',
        credentialRevision: '',
        grants: [],
        intervalSeconds: 86400,
        stabilitySeconds: 30,
        cooldownSeconds: 900,
        budgetBytes: 0,
        lastRun: '',
        lastChanged: {},
      },
      probe: {
        noResourceMode: 'latency',
        services: ['github', 'twitch', 'x'],
        enableIpv6: false,
        scanMode: 'near',
        candidates: 10,
        rounds: 3,
        connectionSeconds: 10,
        sampleSeconds: 10,
        jobSeconds: 0,
        speedImprovement: 0.2,
        latencyImprovement: 0.2,
        latencyMilliseconds: 10,
      },
    },
    domains: [],
    profiles: [],
    folders: [],
    detections: [],
    results: [],
    jobs: [],
    network: {
      fingerprint: 'network',
      interfaces: [],
      routes: [],
      resolvers: [],
      ipv6: false,
      ipv6Reason: '沒有 IPv6',
      observedAt: '',
      error: '',
    },
    credentials: { saved: false, available: true, message: '' },
    recovery: false,
    activeTestId: '',
    activeWriterId: '',
    desktop: {
      trayAvailable: true,
      trayMessage: '',
      launchAvailable: true,
      launchMessage: '',
    },
  }
}

export function ok<T>(data: T) {
  return { ok: true, data, error: null }
}

export function job(
  overrides: Partial<Snapshot['jobs'][number]> = {},
): Snapshot['jobs'][number] {
  return {
    id: 'job',
    kind: 'test',
    trigger: 'manual',
    state: 'running',
    startedAt: new Date().toISOString(),
    finishedAt: '',
    completed: 0,
    managedCompleted: 0,
    baselinesCompleted: 0,
    phase: 'qualification',
    estimatedBytes: 0,
    estimatedSeconds: 0,
    total: 12,
    probeCompleted: 0,
    probeTotal: 0,
    currentHostname: 'pbs.twimg.com',
    bytes: 698569,
    budget: 0,
    cancelRequested: false,
    message: '',
    profileId: 'profile',
    credentialRevision: 'credential',
    items: [],
    resultIds: [],
    ...overrides,
  }
}

export function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
