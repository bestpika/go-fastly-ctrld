// Sakai Vue 5.0.0，Copyright 2018-2022 PrimeTek，MIT；固定來源見 ../sakai-source.json。
import { computed, reactive } from 'vue'

const layoutConfig = reactive({
  preset: 'Aura',
  primary: 'emerald',
  surface: null,
  darkTheme: false,
  menuMode: 'static' as 'static' | 'overlay',
})
const layoutState = reactive({
  staticMenuInactive: false,
  overlayMenuActive: false,
  profileSidebarVisible: false,
  configSidebarVisible: false,
  mobileMenuActive: false,
  sidebarExpanded: false,
  anchored: false,
  menuHoverActive: false,
  activePath: null as string | null,
  desktop: window.innerWidth > 991,
})

export function useLayout() {
  const isDesktop = () => window.innerWidth > 991
  const toggleMenu = () => {
    if (isDesktop()) {
      if (layoutConfig.menuMode === 'static')
        layoutState.staticMenuInactive = !layoutState.staticMenuInactive
      if (layoutConfig.menuMode === 'overlay')
        layoutState.overlayMenuActive = !layoutState.overlayMenuActive
    } else layoutState.mobileMenuActive = !layoutState.mobileMenuActive
  }
  const hideMobileMenu = () => {
    layoutState.mobileMenuActive = false
  }
  const closeMenu = () => {
    layoutState.overlayMenuActive = false
    hideMobileMenu()
    layoutState.menuHoverActive = false
  }
  const changeMenuMode = (event: { value: 'static' | 'overlay' }) => {
    layoutConfig.menuMode = event.value
    layoutState.staticMenuInactive = false
    closeMenu()
    layoutState.sidebarExpanded = false
    layoutState.anchored = false
  }
  const hasOpenOverlay = computed(() => layoutState.overlayMenuActive)
  const isSidebarActive = computed(
    () => hasOpenOverlay.value || layoutState.mobileMenuActive,
  )
  const isSidebarVisible = computed(() =>
    layoutState.desktop
      ? layoutConfig.menuMode === 'overlay'
        ? layoutState.overlayMenuActive
        : !layoutState.staticMenuInactive
      : layoutState.mobileMenuActive,
  )
  const isDarkTheme = computed(() => layoutConfig.darkTheme)
  return {
    layoutConfig,
    layoutState,
    toggleMenu,
    hideMobileMenu,
    closeMenu,
    changeMenuMode,
    isDesktop,
    hasOpenOverlay,
    isSidebarActive,
    isSidebarVisible,
    isDarkTheme,
  }
}
