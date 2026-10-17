import { beforeEach, vi } from 'vitest'

// jsdom 沒有瀏覽器的媒體查詢；官方 Select 用它監聽螢幕方向。
beforeEach(() =>
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      matches: false,
      media,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  ),
)
