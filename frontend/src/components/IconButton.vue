<script setup lang="ts">
import Button from 'primevue/button'
import Tooltip from 'primevue/tooltip'
defineOptions({ inheritAttrs: false })
defineProps<{
  icon: string
  label: string
  disabled?: boolean
  severity?: 'secondary' | 'success' | 'danger' | 'warn'
  loading?: boolean
}>()
defineEmits<{ click: [event: MouseEvent] }>()
const vTooltip = Tooltip
</script>
<template>
  <span v-tooltip.top="label" class="inline-flex"
    ><Button
      v-tooltip.focus.top="label"
      v-bind="$attrs"
      :icon="icon"
      :aria-label="label"
      :disabled="disabled"
      :severity="severity"
      :loading="loading"
      text
      rounded
      @click="$emit('click', $event)"
  /></span>
</template>
