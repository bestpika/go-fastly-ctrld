<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch, type WatchStopHandle } from 'vue'
import { useRouter } from 'vue-router'
import Toast from 'primevue/toast'
import Button from 'primevue/button'
import { useToast } from 'primevue/usetoast'
import type { ToastMessageOptions, ToastEvent } from 'primevue/toast'
import { state, dismiss } from '../shared/app'
type AppMessage = ToastMessageOptions & {
  appKey: string
  errorId: string
  count: number
}
const router = useRouter(),
  toast = useToast(),
  shown = new Map<string, AppMessage>(),
  replacing = new Set<string>()
let stop: WatchStopHandle | undefined
const severityIcons: Record<string, string> = {
  success: 'pi pi-check-circle',
  warn: 'pi pi-exclamation-triangle',
  error: 'pi pi-times-circle',
}
onMounted(() => {
  stop = watch(
    () => state.alerts.map((a) => ({ ...a })),
    (alerts) => {
      for (const [key, message] of shown) {
        if (!alerts.some((a) => a.key === key)) {
          replacing.add(key)
          toast.remove(message)
          replacing.delete(key)
          shown.delete(key)
        }
      }
      for (const a of alerts) {
        const previous = shown.get(a.key)
        if (previous?.summary === a.message && previous.count === a.count)
          continue
        if (previous) {
          replacing.add(a.key)
          toast.remove(previous)
          replacing.delete(a.key)
        }
        const message: AppMessage = {
          appKey: a.key,
          errorId: a.errorId,
          count: a.count,
          severity: a.level,
          summary: a.message,
          group: 'app',
          closable: true,
          ...(a.level === 'success' ? { life: 6000 } : {}),
        }
        shown.set(a.key, message)
        toast.add(message)
      }
    },
    { immediate: true },
  )
})
function closed(event: ToastEvent) {
  const key = (event.message as AppMessage).appKey
  if (!replacing.has(key)) {
    shown.delete(key)
    dismiss(key)
  }
}
onBeforeUnmount(() => {
  stop?.()
  toast.removeGroup('app')
  shown.clear()
})
</script>
<template>
  <Toast
    group="app"
    position="bottom-right"
    class="max-w-[calc(100vw-2rem)]"
    :close-button-props="{ 'aria-label': '關閉通知' }"
    @close="closed"
    @life-end="closed"
    ><template #message="{ message }"
      ><i :class="severityIcons[message.severity]" aria-hidden="true" />
      <Button
        v-if="message.errorId"
        text
        :severity="message.severity === 'error' ? 'danger' : message.severity"
        class="min-w-0 flex-1 text-left wrap-anywhere"
        :label="
          message.summary + (message.count > 1 ? ' ×' + message.count : '')
        "
        :aria-label="'開啟錯誤紀錄 ' + message.summary"
        @click="
          router.push({ path: '/errors', query: { errorId: message.errorId } })
        "
      />
      <span v-else class="min-w-0 flex-1 wrap-anywhere"
        >{{ message.summary
        }}<small v-if="message.count > 1"> ×{{ message.count }}</small></span
      ></template
    ></Toast
  >
</template>
