<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import AppLayout from './layout/AppLayout.vue'
import { initialize } from './shared/app'
import { router } from './router'
let dispose: (() => void) | undefined
let destroyed = false
onMounted(async () => {
  const stop = await initialize((path) => {
    void router.push(path)
  })
  if (destroyed) stop()
  else dispose = stop
})
onUnmounted(() => {
  destroyed = true
  dispose?.()
})
</script>
<template><AppLayout /></template>
