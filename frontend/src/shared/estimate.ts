import type { Snapshot } from './app'

export function testWorkload(s: Snapshot) {
  const groups: Record<string, string[]> = {
    github: [
      'release-assets.githubusercontent.com',
      'raw.githubusercontent.com',
    ],
    twitch: [
      'assets.twitch.tv',
      'www.twitch.tv',
      'gql.twitch.tv',
      'k.twitchcdn.net',
      'static-cdn.jtvnw.net',
      'jtvnw.twitchcdn.net',
    ],
    x: [
      'pbs.twimg.com',
      'abs.twimg.com',
      'video.twimg.com',
      'video-s.twimg.com',
    ],
  }
  const download = new Set([
    'release-assets.githubusercontent.com',
    'assets.twitch.tv',
    'pbs.twimg.com',
  ])
  const managed = s.domains.filter((d) => d.enabled)
  const baselines = new Set(
    (s.settings.probe.services ?? ['github', 'twitch', 'x']).flatMap(
      (g) => groups[g] ?? [],
    ),
  )
  if (
    s.settings.probe.noResourceMode === 'shared' &&
    managed.some((d) => !d.testUrl && !download.has(d.hostname))
  ) {
    baselines.add('release-assets.githubusercontent.com')
    baselines.add('assets.twitch.tv')
  }
  // 尚未讀取帳號現用規則，不假設可去重；估計刻意保留每個獨立現用基準。
  const count =
    [...baselines].filter((host) => download.has(host)).length +
    managed.filter((d) => d.testUrl || download.has(d.hostname)).length
  const samples =
    ((s.settings.probe.candidates || 10) *
      (s.settings.probe.enableIpv6 ? 2 : 1) +
      16) *
    3
  return {
    bytes: count * (samples * 8 * 1024 * 1024 + 8 * 1024 * 1024),
    seconds: count * samples * 12,
  }
}
