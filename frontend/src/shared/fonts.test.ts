import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  fontOptions,
  fonts,
  initializeFont,
  readLocalFonts,
  selectFont,
} from './fonts'

describe('前端系統字型', () => {
  beforeEach(() => {
    localStorage.clear()
    fonts.families = []
    fonts.loading = false
    initializeFont()
  })
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
    localStorage.clear()
    initializeFont()
  })
  it('啟動只還原偏好，不列舉字型；預設回到官方樣式', () => {
    const query = vi.fn()
    vi.stubGlobal('queryLocalFonts', query)
    localStorage.setItem('fastly-ctrld.fontFamily', 'Microsoft JhengHei')
    initializeFont()
    expect(fonts.family).toBe('Microsoft JhengHei')
    expect(document.body.style.fontFamily).toContain('Microsoft JhengHei')
    expect(query).not.toHaveBeenCalled()
    expect(fontOptions.value[1]!.label).toBe('Microsoft JhengHei（已保存）')
    expect(selectFont('')).toBe('')
    expect(document.body.style.fontFamily).toBe('')
    expect(localStorage.getItem('fastly-ctrld.fontFamily')).toBeNull()
  })
  it('每次開啟更新清單，依家族去重且不讀取字型檔', async () => {
    const blob = vi.fn()
    const query = vi
      .fn()
      .mockResolvedValue([
        { family: 'Lato', blob },
        { family: ' Lato ' },
        { family: '微軟正黑體' },
        { family: '' },
        { family: 'bad\nfont' },
      ])
    vi.stubGlobal('queryLocalFonts', query)
    const first = readLocalFonts()
    expect(query).toHaveBeenCalledOnce()
    expect(fonts.loading).toBe(true)
    expect(await first).toBe('')
    expect(fonts.families).toHaveLength(2)
    expect(blob).not.toHaveBeenCalled()
    expect(selectFont('微軟正黑體')).toBe('')
    initializeFont()
    expect(fonts.family).toBe('微軟正黑體')
    query.mockResolvedValue([{ family: 'Arial' }])
    await readLocalFonts()
    expect(fonts.families).toEqual(['Arial'])
    expect(fonts.family).toBe('微軟正黑體')
    expect(query).toHaveBeenCalledTimes(2)
  })
  it('不支援時保留已保存字型，不假造系統清單', async () => {
    fonts.family = 'Saved Font'
    fonts.families = ['Old Font']
    vi.stubGlobal('queryLocalFonts', undefined)
    expect(await readLocalFonts()).toContain('不支援')
    expect(fonts.family).toBe('Saved Font')
    expect(fonts.families).toEqual([])
    expect(fonts.loading).toBe(false)
  })
  it.each([
    ['NotAllowedError', '未授權'],
    ['SecurityError', '不允許'],
    ['AbortError', '已取消'],
  ])('處理 %s，不清除目前字型', async (name, message) => {
    fonts.family = 'Saved Font'
    vi.stubGlobal(
      'queryLocalFonts',
      vi.fn().mockRejectedValue(new DOMException('', name)),
    )
    expect(await readLocalFonts()).toContain(message)
    expect(fonts.family).toBe('Saved Font')
    expect(fonts.loading).toBe(false)
  })
  it('舊的非同步結果不能覆蓋新清單', async () => {
    let resolve!: (value: { family: string }[]) => void
    const query = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise((done) => {
            resolve = done
          }),
      )
      .mockResolvedValue([{ family: 'New Font' }])
    vi.stubGlobal('queryLocalFonts', query)
    const old = readLocalFonts()
    await readLocalFonts()
    resolve([{ family: 'Old Font' }])
    await old
    expect(fonts.families).toEqual(['New Font'])
    expect(fonts.loading).toBe(false)
  })
  it('保存失敗不改字型，拒絕清單外輸入', () => {
    fonts.families = ['Available Font']
    expect(selectFont('Arbitrary Font')).toContain('清單')
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('', 'QuotaExceededError')
    })
    expect(selectFont('Available Font')).toContain('無法保存')
    expect(fonts.family).toBe('')
    expect(document.body.style.fontFamily).toBe('')
  })
})
