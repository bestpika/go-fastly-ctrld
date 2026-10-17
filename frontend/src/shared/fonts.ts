import { computed, reactive } from 'vue'

const storageKey = 'fastly-ctrld.fontFamily'
type FontWindow = Window & {
  queryLocalFonts?: () => Promise<{ family: string }[]>
}

export const fonts = reactive({
  family: '',
  families: [] as string[],
  loading: false,
})
let request = 0

function validFamily(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    value.length <= 256 &&
    ![...value].some((char) => {
      const code = char.charCodeAt(0)
      return code < 32 || code === 127
    })
  )
}

function applyFont() {
  if (fonts.family)
    document.body.style.fontFamily = `${JSON.stringify(fonts.family)}, Lato, sans-serif`
  else document.body.style.removeProperty('font-family')
}

export function initializeFont() {
  fonts.family = ''
  try {
    const saved = localStorage.getItem(storageKey)
    if (validFamily(saved)) fonts.family = saved
  } catch {
    // 無儲存權限時沿用官方預設，不影響宿主設定。
  }
  applyFont()
}

export const fontOptions = computed(() => {
  const options = [{ label: '預設 Lato', value: '' }]
  if (fonts.family && !fonts.families.includes(fonts.family)) {
    options.push({ label: `${fonts.family}（已保存）`, value: fonts.family })
  }
  return options.concat(
    fonts.families.map((family) => ({ label: family, value: family })),
  )
})

// 由 Select 的 show 事件直接呼叫，保留瀏覽器要求的使用者操作授權。
export async function readLocalFonts(): Promise<string> {
  const current = ++request
  fonts.loading = true
  try {
    const query = (window as FontWindow).queryLocalFonts
    if (!query) {
      fonts.families = []
      return '此瀏覽器不支援讀取系統字型'
    }
    const found = await query.call(window)
    if (current !== request) return ''
    fonts.families = [
      ...new Set(
        found
          .map((font) => font.family)
          .filter(validFamily)
          .map((family) => family.trim())
          .filter(Boolean),
      ),
    ].sort((a, b) => a.localeCompare(b))
    return fonts.families.length ? '' : '未取得系統字型'
  } catch (error) {
    if (current !== request) return ''
    fonts.families = []
    if (error instanceof DOMException) {
      if (error.name === 'NotAllowedError') return '未授權讀取系統字型'
      if (error.name === 'SecurityError') return '此環境不允許讀取系統字型'
      if (error.name === 'AbortError') return '已取消讀取系統字型'
    }
    return '無法讀取系統字型，請重試'
  } finally {
    if (current === request) fonts.loading = false
  }
}

export function selectFont(family: string): string {
  if (
    !validFamily(family) ||
    (family && family !== fonts.family && !fonts.families.includes(family))
  )
    return '請選擇清單中的字型'
  try {
    if (family) localStorage.setItem(storageKey, family)
    else localStorage.removeItem(storageKey)
  } catch {
    return '無法保存字型設定'
  }
  fonts.family = family
  applyFont()
  return ''
}
