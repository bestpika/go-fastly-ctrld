<!-- Sakai Vue 5.0.0，Copyright 2018-2022 PrimeTek，MIT；固定來源見 sakai-source.json。 -->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import Card from 'primevue/card'
import ConfirmDialog from 'primevue/confirmdialog'
import IconButton from '../components/IconButton.vue'
import Notifications from '../components/Notifications.vue'
import JobStatus from '../components/JobStatus.vue'
import { Bridge, run, state } from '../shared/app'
import { useLayout } from './composables/layout'
import AppFooter from './AppFooter.vue'
import AppSidebar from './AppSidebar.vue'
import AppTopbar from './AppTopbar.vue'

const { layoutConfig, layoutState, isDesktop, hideMobileMenu, closeMenu } =
  useLayout()
const containerClass = computed(() => ({
  'layout-overlay': layoutConfig.menuMode === 'overlay',
  'layout-static': layoutConfig.menuMode === 'static',
  'layout-overlay-active': layoutState.overlayMenuActive,
  'layout-mobile-active': layoutState.mobileMenuActive,
  'layout-static-inactive': layoutState.staticMenuInactive,
}))

watch(
  () => state.snapshot?.settings.theme,
  (theme) => {
    layoutConfig.darkTheme = theme === 'dark'
  },
  { immediate: true },
)

function onResize() {
  const desktop = isDesktop()
  if (layoutState.desktop !== desktop) closeMenu()
  layoutState.desktop = desktop
}
function retryHost() {
  void run(() => Bridge.GetAppSnapshot())
}
onMounted(() => {
  onResize()
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
})
</script>

<template>
  <div class="layout-wrapper" :class="containerClass">
    <AppTopbar />
    <AppSidebar />
    <div class="layout-main-container">
      <main class="layout-main space-y-6">
        <Card v-if="state.loading" role="status">
          <template #content>正在讀取 Go 宿主快照…</template>
        </Card>
        <template v-else-if="state.snapshot"
          ><JobStatus /><RouterView
        /></template>
        <Card v-else>
          <template #content>
            <h1>無法讀取本機狀態</h1>
            <IconButton
              icon="pi pi-refresh"
              label="重新整理"
              @click="retryHost"
            />
          </template>
        </Card>
      </main>
      <AppFooter />
    </div>
    <div
      class="layout-mask animate-fadein"
      aria-hidden="true"
      @click="hideMobileMenu"
    />
  </div>
  <Notifications />
  <ConfirmDialog group="app" />
</template>
