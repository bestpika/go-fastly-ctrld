import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { expect, it } from 'vitest'
import source from './sakai-source.json'

function read(local: string) {
  return readFileSync(
    new URL('../../../' + local, import.meta.url),
    'utf8',
  ).replace(/\r\n/g, '\n')
}

it.each(source.unmodified)(
  '官方來源逐字保留：$upstream',
  ({ local, gitBlob }) => {
    const text = read(local)
    const hash = createHash('sha1')
      .update(`blob ${Buffer.byteLength(text)}\0${text}`)
      .digest('hex')
    expect(hash).toBe(gitBlob)
  },
)

it('入口直接載入官方 SCSS／Tailwind 設定與離線 Lato，不再以仿製版型替代', () => {
  expect(source.version).toBe('5.0.0')
  expect(source.commit).toBe('ec6b6ef53ad4fa7f2861277f50f2d9e236c40372')
  const main = read('frontend/src/main.ts')
  expect(main).toContain("import './assets/layout/layout.scss'")
  expect(main).toContain("import './assets/tailwind.css'")
  expect(main).toContain("import '@fontsource/lato/400.css'")
  expect(main).toContain("import '@fontsource/lato/700.css'")
  expect(source.adapted).toHaveLength(7)
  for (const file of source.adapted) {
    expect(read(file.local)).toContain('Sakai Vue 5.0.0')
    expect(read(file.local)).toContain('Copyright 2018-2022 PrimeTek')
  }
})

it('樣式保留歷史主儲存庫 MIT 來源，分別記錄 v5 布局與相同資產，不冒用子模組授權', () => {
  expect(source.styles.repository).toBe(source.repository)
  expect(source.styles.version).toBe('4.3.0')
  expect(source.styles.commit).toBe('7c32ae1f6fc75e18a5e735f76e1ab25d1167d26c')
  expect(source.styles.license).toBe('MIT')
  expect(source.styles.matches.repository).toBe(
    'https://github.com/primefaces/sakai-assets',
  )
  expect(source.styles.matches.commit).toBe(
    'eaa70ece4cfa9aeb8f60b36dce5a422b6bae8003',
  )
  expect(source.styles.matches).not.toHaveProperty('license')
  const styles = source.unmodified.filter((file) => file.source === 'styles')
  expect(styles).toHaveLength(15)
  for (const file of styles)
    expect(file.upstream.startsWith(source.styles.upstreamPrefix)).toBe(true)
  expect(source.unmodified.filter((file) => file.source === 'layout')).toEqual([
    expect.objectContaining({ upstream: 'LICENSE.md' }),
  ])
})
