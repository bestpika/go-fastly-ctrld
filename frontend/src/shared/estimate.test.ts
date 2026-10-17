import { expect, it } from 'vitest'
import { snapshot } from '../test/fixtures'
import { testWorkload } from './estimate'
it('依下載網域估算工作量，全關或僅連線不冒出下載額度', () => {
  const s = snapshot()
  const all = testWorkload(s)
  expect(all.bytes).toBeGreaterThan(0)
  expect(all.seconds).toBeGreaterThan(0)
  s.settings.probe.services = []
  expect(testWorkload(s).bytes).toBe(0)
  s.settings.probe.services = ['x']
  expect(testWorkload(s).bytes).toBe(all.bytes / 3)
})
