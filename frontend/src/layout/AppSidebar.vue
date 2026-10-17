<!-- Sakai Vue 5.0.0，Copyright 2018-2022 PrimeTek，MIT；固定來源見 sakai-source.json。 -->
<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import FocusTrap from 'primevue/focustrap'
import AppMenu from './AppMenu.vue'
import { useLayout } from './composables/layout'

const vFocustrap = FocusTrap
const { layoutState, isDesktop, isSidebarActive, isSidebarVisible, closeMenu } =
  useLayout()
const route = useRoute()
const sidebarRef = ref<HTMLElement | null>(null)
let outsideClickListener: ((event: MouseEvent) => void) | null = null

watch(
  [() => route.fullPath, () => layoutState.desktop],
  () => {
    layoutState.activePath = isDesktop() ? null : route.path
    closeMenu()
  },
  { immediate: true },
)

watch(isSidebarActive, async (active) => {
  document.body.classList.toggle('blocked-scroll', layoutState.mobileMenuActive)
  if (active) {
    bindOutsideClickListener()
    if (layoutState.mobileMenuActive) {
      await nextTick()
      if (layoutState.mobileMenuActive && !layoutState.desktop)
        sidebarRef.value?.querySelector<HTMLAnchorElement>('a[href]')?.focus()
    }
  } else {
    unbindOutsideClickListener()
    document.getElementById('navigation-toggle')?.focus()
  }
})

function bindOutsideClickListener() {
  if (outsideClickListener) return
  outsideClickListener = (event) => {
    const target = event.target
    const toggle = document.querySelector('.layout-menu-button')
    if (
      target instanceof Node &&
      !sidebarRef.value?.contains(target) &&
      !toggle?.contains(target)
    )
      closeMenu()
  }
  document.addEventListener('click', outsideClickListener)
  document.addEventListener('keydown', onKeydown)
}
function unbindOutsideClickListener() {
  if (outsideClickListener) {
    document.removeEventListener('click', outsideClickListener)
    outsideClickListener = null
  }
  document.removeEventListener('keydown', onKeydown)
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') closeMenu()
}
onBeforeUnmount(() => {
  unbindOutsideClickListener()
  closeMenu()
  document.body.classList.remove('blocked-scroll')
})
</script>

<template>
  <aside
    id="app-sidebar"
    ref="sidebarRef"
    :key="layoutState.desktop ? 'desktop' : 'mobile'"
    v-focustrap="{
      disabled: layoutState.desktop,
      firstFocusableSelector: '[href]',
      lastFocusableSelector: '[href]',
    }"
    class="layout-sidebar"
    aria-label="主要導覽"
    :role="layoutState.mobileMenuActive ? 'dialog' : undefined"
    :aria-modal="layoutState.mobileMenuActive ? true : undefined"
    :inert="!isSidebarVisible ? true : undefined"
  >
    <AppMenu />
  </aside>
</template>
