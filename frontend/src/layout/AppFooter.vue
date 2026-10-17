<!-- Sakai Vue 5.0.0，Copyright 2018-2022 PrimeTek，MIT；固定來源見 sakai-source.json。 -->
<script setup lang="ts">
import { state } from '../shared/app'
</script>

<template>
  <footer class="layout-footer flex-wrap">
    <span>SAKAI by <strong class="text-primary">PrimeVue</strong></span>
    <span>Wails v3 · {{ state.snapshot?.version ?? 'development' }}</span>
  </footer>
</template>
