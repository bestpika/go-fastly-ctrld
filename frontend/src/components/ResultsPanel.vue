<script setup lang="ts">
import Card from 'primevue/card'
import { computed, ref } from 'vue'
import IconButton from './IconButton.vue'
import { useActionConfirm } from '../shared/confirmation'
const ask = useActionConfirm()
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Dialog from 'primevue/dialog'
import Fieldset from 'primevue/fieldset'
import Tag from 'primevue/tag'
import {
  Bridge as NativeBridge,
  state,
  busy,
  working,
  run,
  date,
  bytes,
} from '../shared/app'
const Bridge = { ...NativeBridge }
import type {
  CandidateResult,
  TestResult,
} from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/domain/models'
const s = computed(() => state.snapshot!)
const details = ref<TestResult | null>(null)
const rounds = ref<CandidateResult | null>(null)
function showDetails(result: TestResult) {
  details.value = result
  rounds.value = null
}
const candidates = computed(() =>
  [...(details.value?.candidates ?? [])].sort(
    (a, b) => Number(recommended(b.ip)) - Number(recommended(a.ip)),
  ),
)
function recommended(ip: string) {
  return (
    !!details.value?.complete &&
    [details.value.bestIpv4, details.value.bestIpv6].includes(ip)
  )
}
async function remove(id: string) {
  if (
    await ask(
      id
        ? '刪除此測速結果？相關預覽會失效；不刪除規則或復原紀錄。'
        : '清除所有測速結果？不刪網域、規則或復原紀錄。',
    )
  )
    await run(() => Bridge.DeleteTestResults(id), '測速結果已清除')
}
const groups = computed(() => [
  {
    title: '基準服務',
    results: s.value.results.filter((r) => r.baselineService),
  },
  {
    title: '管理網域',
    results: s.value.results.filter(
      (r) =>
        !r.baselineService &&
        !s.value.domains.find((d) => d.id === r.domainId)?.testUrl,
    ),
  },
  {
    title: '自訂檔案',
    results: s.value.results.filter(
      (r) =>
        !r.baselineService &&
        !!s.value.domains.find((d) => d.id === r.domainId)?.testUrl,
    ),
  },
])
</script>
<template>
  <div class="mb-4 flex flex-wrap items-center justify-between gap-4">
    <h2 class="text-xl font-semibold">測速結果</h2>
    <IconButton
      icon="pi pi-trash"
      label="清除全部測速結果"
      severity="danger"
      :disabled="busy || working || s.recovery || !s.results.length"
      @click="remove('')"
    />
  </div>
  <Card v-for="group in groups" :key="group.title" class="min-w-0">
    <template #content>
      <h3 class="mb-4 text-lg font-semibold">{{ group.title }}</h3>
      <DataTable
        :value="group.results"
        data-key="id"
        paginator
        :rows="10"
        striped-rows
        ><template #empty>尚無測速結果。</template
        ><Column
          v-if="group.title === '基準服務'"
          field="service"
          header="服務" /><Column
          field="hostname"
          :header="
            group.title === '基準服務' ? '探測網域' : '管理網域'
          " /><Column header="測試方式"
          ><template #body="{ data }">{{
            data.mode === 'download'
              ? '下載測速'
              : data.mode === 'connection'
                ? '僅連線延遲'
                : '路線參考'
          }}</template></Column
        ><Column field="bestIpv4" header="建議 IPv4" /><Column
          field="bestIpv6"
          header="建議 IPv6" /><Column header="自動資格"
          ><template #body="{ data }"
            ><Tag
              :value="data.automaticEligible ? '可評估' : '未授權／不符'"
              :severity="
                data.automaticEligible ? 'success' : 'warn'
              " /></template></Column
        ><Column header="完整性"
          ><template #body="{ data }">{{
            data.complete ? '已完成' : '未完成'
          }}</template></Column
        ><Column header="操作"
          ><template #body="{ data }"
            ><IconButton
              icon="pi pi-list"
              label="查看候選"
              @click="showDetails(data)" /><IconButton
              icon="pi pi-info-circle"
              :label="data.reason || '無其他說明'" /><IconButton
              icon="pi pi-trash"
              label="刪除此測速結果"
              severity="danger"
              :disabled="busy || working || s.recovery"
              @click="remove(data.id)" /></template></Column
      ></DataTable>
    </template>
  </Card>
  <Dialog
    :visible="!!details"
    header="候選測試明細"
    modal
    class="w-250 max-w-[calc(100vw-2rem)]"
    @update:visible="details = null"
    ><p class="mb-3 leading-relaxed wrap-anywhere">
      {{ details?.hostname }} · {{ date(details?.observedAt ?? '') }}
    </p>
    <p
      v-if="
        details?.hostname === 'pbs.twimg.com' && details.mode === 'download'
      "
      class="mb-3 leading-relaxed"
    >
      X 使用指定 PNG 圖片做三輪自身下載；短檔的本文 Mbps
      不代表持續頻寬，請一併比較每輪位元組、TTFB 與完整請求耗時。
    </p>
    <p v-if="details?.reusedFrom" class="mb-3 leading-relaxed">
      重用同作業、同網域的完整量測
      {{ details.reusedFrom }}；未省略獨立現用規則比較，流量不重複計算。
    </p>
    <p class="mb-3 text-(--p-text-muted-color)">
      {{ details?.service || '舊策略，請重新測試' }} ·
      {{
        details?.mode === 'download'
          ? '下載測速'
          : details?.mode === 'github-pop-reference'
            ? '舊策略 X／GitHub 路線參考，請重測'
            : details?.mode === 'shared-reference'
              ? '共享路線參考，非目標下載成績'
              : '僅連線延遲'
      }}
      · {{ details?.scanCount ?? 0 }} 路線
    </p>
    <IconButton
      icon="pi pi-info-circle"
      :label="`探測：${details?.probeHost || '未記錄'}；下載：${details?.speedHost || '未量下載'}`"
    />
    <IconButton
      icon="pi pi-info-circle"
      label="順暢門檻：同族最快樣本的 30%，至少 3 Mbps。"
    />
    <Card v-if="details?.reference" class="my-4"
      ><template #content>
        <h3 class="mb-4 text-lg font-semibold">共享 Fastly 路線參考</h3>
        <p class="mb-3 leading-relaxed wrap-anywhere">
          參考 {{ details.reference.id }} ·
          {{ date(details.reference.observedAt) }} ·
          {{
            details.referenceReused
              ? '重用本作業實測基準，不跨作業借成績'
              : '本批建立，下載只計一次'
          }}
        </p>
        <IconButton
          icon="pi pi-info-circle"
          label="IPv4 共用 /24 路線，IPv6 限相同 IP；參考分數非自身下載速度。"
        />
        <div
          v-for="source in details.reference.sources ?? []"
          :key="source.name"
        >
          <strong>{{ source.name }}</strong> · 探測 {{ source.probeHost }} ·
          下載
          {{ source.speedHost }}
        </div>
      </template></Card
    >
    <DataTable :value="candidates" paginator :rows="10"
      ><Column header="建議"
        ><template #body="{ data }"
          ><Tag
            v-if="recommended(data.ip)"
            value="推薦"
            severity="success"
          /><span v-else>—</span></template
        ></Column
      ><Column field="ip" header="IP" /><Column
        field="family"
        header="IP 族" /><Column header="機房"
        ><template #body="{ data }">{{
          data.pop && data.pop !== '?' ? data.pop : '未知'
        }}</template></Column
      ><Column header="路線"
        ><template #body="{ data }">{{
          data.baseline ? '現用基準' : '候選'
        }}</template></Column
      ><Column field="successes" header="成功輪數" /><Column header="順暢輪數"
        ><template #body="{ data }">{{
          details?.mode === 'download' ? data.smoothRounds : '未量下載'
        }}</template></Column
      ><Column header="下載中位數"
        ><template #body="{ data }">{{
          details?.mode === 'download' && data.samples?.length
            ? `${data.medianMbps.toFixed(2)} Mbps`
            : '未量下載'
        }}</template></Column
      ><Column header="TCP 中位數"
        ><template #body="{ data }">{{
          data.medianLatency > 0
            ? `${data.medianLatency.toFixed(2)} ms`
            : '未量得'
        }}</template></Column
      ><Column header="來源參考"
        ><template #body="{ data }">{{
          (data.referenceScores ?? [])
            .map(
              (score: {
                source: string
                actualIp: string
                smoothRounds: number
                rounds: number
                medianMbps: number
              }) =>
                `${score.source} ${score.actualIp}：順暢 ${score.smoothRounds}/${score.rounds}，參考 ${score.medianMbps.toFixed(2)} Mbps`,
            )
            .join('；') || '未使用共享成績'
        }}</template></Column
      ><Column header="失敗說明"
        ><template #body="{ data }">{{
          data.samples
            .filter((sample: { success: boolean }) => !sample.success)
            .map(
              (sample: {
                error: string
                stage: string
                httpStatus: number
                hostname: string
                actualIp: string
              }) =>
                `${sample.stage || '未記錄階段'}${sample.httpStatus ? ` HTTP ${sample.httpStatus}` : ''} ${sample.hostname || ''} ${sample.actualIp || ''}：${sample.error}`,
            )
            .join('；')
        }}</template></Column
      ><Column header="逐輪明細"
        ><template #body="{ data }"
          ><IconButton
            icon="pi pi-list"
            :label="`查看 ${data.ip} 逐輪量測`"
            @click="rounds = data" /></template></Column
    ></DataTable>
    <Fieldset v-if="rounds" :legend="`${rounds.ip} 逐輪量測`" class="my-4">
      <p class="mb-3">
        本文 Mbps 不含建立連線與等候首位元組；完整請求耗時包含 TCP、TLS、TTFB
        與本文讀取。舊策略未記錄者顯示未記錄。
      </p>
      <DataTable
        :value="
          (rounds.samples ?? []).map((sample, index) => ({
            ...sample,
            round: index + 1,
          }))
        "
      >
        <Column field="round" header="輪次" /><Column header="結果"
          ><template #body="{ data }"
            >{{ data.success ? '成功' : '失敗' }}
            {{ data.httpStatus ? `HTTP ${data.httpStatus}` : '' }}</template
          ></Column
        >
        <Column header="本文 Mbps"
          ><template #body="{ data }">{{
            details?.mode === 'download' ? data.mbps.toFixed(2) : '未量下載'
          }}</template></Column
        >
        <Column header="本文位元組"
          ><template #body="{ data }"
            >{{ data.bytes }} B · {{ bytes(data.bytes) }}</template
          ></Column
        >
        <Column header="TLS"
          ><template #body="{ data }">{{
            data.tlsMilliseconds > 0
              ? `${data.tlsMilliseconds.toFixed(2)} ms`
              : '未記錄'
          }}</template></Column
        >
        <Column header="TTFB"
          ><template #body="{ data }">{{
            data.ttfbMilliseconds > 0
              ? `${data.ttfbMilliseconds.toFixed(2)} ms`
              : '未記錄'
          }}</template></Column
        >
        <Column header="完整請求耗時"
          ><template #body="{ data }">{{
            data.elapsedMilliseconds > 0
              ? `${data.elapsedMilliseconds.toFixed(2)} ms`
              : '未記錄'
          }}</template></Column
        >
        <Column field="error" header="失敗說明" />
      </DataTable>
    </Fieldset>
    <Fieldset
      v-if="details?.rejected?.length"
      :legend="`探索淘汰診斷：${details.rejected.length} 條，不占下載候選名額`"
      toggleable
      collapsed
    >
      <DataTable :value="details.rejected" paginator :rows="10"
        ><Column field="ip" header="淘汰 IP" /><Column header="診斷"
          ><template #body="{ data }">{{
            (data.samples ?? [])
              .map(
                (sample: { stage: string; error: string }) =>
                  `${sample.stage}：${sample.error}`,
              )
              .join('；')
          }}</template></Column
        ></DataTable
      >
    </Fieldset>
  </Dialog>
</template>
