<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Panel from 'primevue/panel'
import Tag from 'primevue/tag'
import ProgressBar from 'primevue/progressbar'
import {
  activeTestId,
  bytes,
  label,
  severity,
  state,
  working,
} from '../shared/app'
const now = ref(Date.now())
const collapsed = ref(true)
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  timer = setInterval(() => {
    now.value = Date.now()
  }, 1000)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
const job = computed(() => {
  const s = state.snapshot
  if (!s || state.startingTest) return undefined
  const id = activeTestId.value || s.activeWriterId
  return id
    ? s.jobs.find((j) => j.id === id)
    : [...s.jobs].sort((a, b) => b.startedAt.localeCompare(a.startedAt))[0]
})
const elapsed = computed(() => {
  if (!job.value) return 0
  const end = working.value ? now.value : Date.parse(job.value.finishedAt)
  return Math.max(
    0,
    Math.floor(
      ((Number.isFinite(end) ? end : now.value) -
        Date.parse(job.value.startedAt)) /
        1000,
    ),
  )
})
</script>
<template>
  <Panel
    v-if="job || state.startingTest || activeTestId"
    v-model:collapsed="collapsed"
    header="作業狀態"
    toggleable
    :toggle-button-props="{
      ariaLabel: collapsed ? '展開作業狀態' : '收合作業狀態',
    }"
    role="status"
    aria-live="polite"
    :aria-busy="working"
  >
    <template #header>
      <div class="flex flex-wrap items-center gap-3">
        <i v-if="working" class="pi pi-spin pi-spinner" aria-hidden="true" />
        <strong>{{ job ? label(job.kind) : '測試' }}</strong>
        <Tag
          :value="
            state.stoppingTestId || (job?.cancelRequested && working)
              ? '停止中，等待宿主確認'
              : job
                ? label(job.state)
                : '啟動中'
          "
          :severity="job ? severity(job.state) : 'info'"
        />
      </div>
    </template>
    <div v-if="working" class="flex flex-wrap items-center gap-3">
      <span>{{ job?.phase ? label(job.phase) : '宿主正在處理' }}</span>
      <RouterLink v-if="activeTestId" to="/">前往總覽控制</RouterLink>
    </div>
    <p v-if="job" class="mt-3 leading-relaxed wrap-anywhere">
      {{ job.currentHostname || '尚未開始個別網域' }} · 網域
      {{ job.completed }}／{{ job.total }} · 耗時 {{ elapsed }} 秒 · 已讀
      {{ bytes(job.bytes)
      }}{{ job.budget ? `／${bytes(job.budget)}` : '／不限流量' }}
      <span v-if="working && job.probeTotal">
        · 目前階段 {{ job.probeCompleted }}／{{ job.probeTotal }}</span
      >
    </p>
    <p v-if="job?.message" class="mt-2 leading-relaxed">{{ job.message }}</p>
    <p v-if="working" class="mt-2 text-(--p-text-muted-color)">
      {{
        job?.kind === 'apply' || job?.kind === 'restore'
          ? '正在寫入並回讀確認。'
          : '測速不修改規則。'
      }}
    </p>
    <ProgressBar
      v-if="working"
      mode="indeterminate"
      :show-value="false"
      class="mt-3 h-1"
      aria-label="作業執行中，無精準百分比"
    />
  </Panel>
</template>
