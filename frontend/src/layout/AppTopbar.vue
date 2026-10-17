<!-- Sakai Vue 5.0.0，Copyright 2018-2022 PrimeTek，MIT；固定來源見 sakai-source.json。 -->
<script setup lang="ts">
import Tag from 'primevue/tag'
import Tooltip from 'primevue/tooltip'
import { Bridge, busy, run, state } from '../shared/app'
import { useLayout } from './composables/layout'

const vTooltip = Tooltip
const { toggleMenu, isSidebarVisible, isDarkTheme } = useLayout()
function toggleDarkMode() {
  const settings = state.snapshot?.settings
  if (settings)
    void run(() =>
      Bridge.UpdateSettings(settings.revision, {
        theme: settings.theme === 'dark' ? 'light' : 'dark',
        probe: settings.probe,
      }),
    )
}
</script>

<template>
  <header class="layout-topbar">
    <div class="layout-topbar-logo-container">
      <button
        id="navigation-toggle"
        v-tooltip.focus.bottom="'切換側邊選單'"
        type="button"
        class="layout-menu-button layout-topbar-action"
        aria-label="切換側邊選單"
        aria-controls="app-sidebar"
        :aria-expanded="isSidebarVisible"
        @click="toggleMenu"
      >
        <i class="pi pi-bars" aria-hidden="true" />
      </button>
      <RouterLink to="/" class="layout-topbar-logo min-w-0">
        <i class="pi pi-bolt" aria-hidden="true" />
        <span class="truncate">Fastly 路線最佳化</span>
      </RouterLink>
    </div>
    <div class="layout-topbar-actions">
      <div class="layout-config-menu">
        <Tag
          class="hidden sm:inline-flex"
          :value="
            state.snapshot?.settings.automation.enabled
              ? '自動功能已啟用'
              : '自動執行關閉'
          "
          :severity="
            state.snapshot?.settings.automation.enabled
              ? 'success'
              : 'secondary'
          "
        />
        <button
          v-tooltip.focus.bottom="'切換深淺色主題'"
          type="button"
          class="layout-topbar-action"
          aria-label="切換深淺色主題"
          :disabled="busy"
          @click="toggleDarkMode"
        >
          <i
            :class="['pi', isDarkTheme ? 'pi-sun' : 'pi-moon']"
            aria-hidden="true"
          />
        </button>
      </div>
    </div>
  </header>
</template>
