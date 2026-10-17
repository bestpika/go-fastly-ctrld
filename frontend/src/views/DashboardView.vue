<script setup lang="ts">
import Card from 'primevue/card'
import { computed } from 'vue'
import Tag from 'primevue/tag'
import Select from 'primevue/select'
import IconButton from '../components/IconButton.vue'
import ResultsPanel from '../components/ResultsPanel.vue'
import {
  state,
  busy,
  working,
  Bridge as NativeBridge,
  run,
  bytes,
  activeTestId,
  startTest,
  stopTest,
} from '../shared/app'
import { testWorkload } from '../shared/estimate'
const Bridge = { ...NativeBridge }
const s = computed(() => state.snapshot!)
const estimate = computed(() => testWorkload(s.value))
const stopping = computed(
  () =>
    !!state.stoppingTestId ||
    !!s.value.jobs.find((j) => j.id === activeTestId.value)?.cancelRequested,
)
async function mode(value: string) {
  await run(() =>
    Bridge.UpdateSettings(s.value.settings.revision, {
      theme: s.value.settings.theme,
      probe: { ...s.value.settings.probe, scanMode: value },
    }),
  )
}
</script>
<template>
  <div class="flex flex-wrap items-center justify-between gap-4">
    <h1 class="text-2xl font-semibold">總覽</h1>
    <div class="flex flex-wrap items-center gap-2">
      <Select
        aria-label="掃描模式"
        :model-value="s.settings.probe.scanMode"
        :options="[
          { label: '近端', value: 'near' },
          { label: '完整', value: 'full' },
        ]"
        option-label="label"
        option-value="value"
        :disabled="busy || working"
        @update:model-value="mode"
      />
      <IconButton
        icon="pi pi-search"
        label="偵測網域 CDN"
        :loading="state.startingTest === 'detect'"
        :disabled="
          busy || working || s.recovery || !s.domains.some((d) => d.enabled)
        "
        @click="startTest('detect')"
      /><IconButton
        :icon="activeTestId ? 'pi pi-stop' : 'pi pi-play'"
        :label="
          stopping
            ? '停止中，等待宿主確認'
            : state.startingTest === 'test'
              ? '啟動中'
              : activeTestId
                ? '停止測試'
                : '開始測速'
        "
        :severity="activeTestId ? 'danger' : undefined"
        :loading="state.startingTest === 'test' || stopping"
        :disabled="activeTestId ? stopping : busy || working || s.recovery"
        @click="activeTestId ? stopTest() : startTest('test')"
      /><IconButton
        icon="pi pi-info-circle"
        label="基準不需權杖；測試不修改規則。可至設定選擇基準服務。"
      />
    </div>
  </div>
  <Card
    ><template #content>
      <div class="flex flex-wrap items-center gap-2">
        <Tag
          :value="
            s.settings.probe.enableIpv6 && s.network.ipv6
              ? 'IPv4＋IPv6'
              : 'IPv4'
          "
        /><Tag
          :value="
            s.settings.automation.enabled
              ? s.settings.automation.testOnly
                ? '自動測試'
                : '自動套用'
              : '自動關閉'
          "
        /><Tag
          :value="s.recovery ? '待核對' : working ? '作業執行中' : '可操作'"
          :severity="s.recovery ? 'warn' : working ? 'info' : 'success'"
        /><span>{{ s.domains.filter((d) => d.enabled).length }} 網域</span
        ><span>{{
          s.settings.automation.budgetBytes
            ? bytes(s.settings.automation.budgetBytes)
            : '不限流量'
        }}</span>
      </div>
      <p
        v-if="!working"
        class="mt-3 leading-relaxed"
        data-testid="test-estimate"
      >
        預估下載上限 {{ bytes(estimate.bytes) }}，約
        {{ Math.ceil(estimate.seconds / 60) }}
        分鐘，另需網段掃描；實際耗時與流量可能不同。
        <span
          v-if="
            s.settings.automation.budgetBytes &&
            s.settings.automation.budgetBytes < estimate.bytes
          "
          >流量上限可能不足，部分測試可能無法完成。</span
        >
      </p>
    </template></Card
  >
  <ResultsPanel />
</template>
