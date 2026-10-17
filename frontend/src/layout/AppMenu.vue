<!-- Sakai Vue 5.0.0，Copyright 2018-2022 PrimeTek，MIT；固定來源見 sakai-source.json。 -->
<script setup lang="ts">
import { ref } from 'vue'
import type { MenuItem } from 'primevue/menuitem'
import { pages } from '../router'
import AppMenuItem from './AppMenuItem.vue'

const model = ref<MenuItem[]>([
  {
    label: '管理面板',
    items: pages.map((page) => ({
      label: page.title,
      icon: page.icon,
      to: page.path,
    })),
  },
])
</script>

<template>
  <ul class="layout-menu">
    <template v-for="item in model" :key="String(item.label)">
      <AppMenuItem v-if="!item.separator" :item="item" />
      <li v-else class="menu-separator" />
    </template>
  </ul>
</template>
