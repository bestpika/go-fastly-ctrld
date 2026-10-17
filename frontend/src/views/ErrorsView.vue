<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Card from 'primevue/card'
import IconButton from '../components/IconButton.vue'
import { useActionConfirm } from '../shared/confirmation'
import { useReadScope } from '../shared/reading'
const ask = useActionConfirm()
const reader = useReadScope()
import {
  Bridge as NativeBridge,
  state,
  run,
  date,
  notify,
  reportFailure,
} from '../shared/app'
import type { ErrorRecord } from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/domain/models'
const Bridge = { ...NativeBridge },
  route = useRoute(),
  router = useRouter(),
  first = ref(0),
  records = ref<ErrorRecord[]>([]),
  selected = ref<ErrorRecord | null>(null),
  selection = ref<ErrorRecord | null>(null),
  query = ref('')
async function load() {
  await reader.execute('讀取錯誤紀錄', async (t) => {
    const values = await t.wait(Bridge.ListErrors())
    records.value = values
    const id =
      typeof route.query.errorId === 'string' ? route.query.errorId : ''
    if (id) {
      query.value = ''
      const index = records.value.findIndex((r) => r.id === id)
      first.value = index >= 0 ? Math.floor(index / 20) * 20 : 0
      selected.value = records.value.find((r) => r.id === id) ?? null
      selection.value = selected.value
      if (!selected.value) notify('warn', '該筆錯誤已刪除或超過保留數量')
    }
  })
}
function close() {
  selected.value = null
  selection.value = null
  if (route.query.errorId) void router.replace({ path: '/errors', query: {} })
}
function open(r: ErrorRecord) {
  selected.value = r
  selection.value = r
  void router.replace({ path: '/errors', query: { errorId: r.id } })
}
async function remove(id: string) {
  if (
    await ask(
      id
        ? '刪除此錯誤紀錄？不影響復原與安全暫停。'
        : '清除全部錯誤紀錄？不會解除安全暫停或刪除復原資料。',
    )
  ) {
    await run(() => Bridge.DeleteError(id), '錯誤紀錄已清除')
    selected.value = null
    selection.value = null
    await load()
  }
}
async function copy() {
  if (!selected.value) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(selected.value, null, 2))
    notify('success', '已複製遮敏詳情')
  } catch {
    await reportFailure('COPY_FAILED')
  }
}
onMounted(load)
watch(() => route.query.errorId, load)
watch(
  () => state.alerts.length,
  () => {
    if (route.path === '/errors') void load()
  },
)
</script>
<template>
  <div class="flex flex-wrap items-center justify-between gap-4">
    <h1 class="text-2xl font-semibold">錯誤紀錄</h1>
    <div class="flex flex-wrap items-center gap-2">
      <InputText
        v-model="query"
        aria-label="搜尋錯誤"
        placeholder="搜尋"
      /><IconButton
        icon="pi pi-refresh"
        label="重新讀取錯誤"
        :loading="reader.loading.value"
        @click="load"
      /><IconButton
        icon="pi pi-trash"
        severity="danger"
        label="清除全部錯誤紀錄"
        :disabled="!records.length"
        @click="remove('')"
      /><IconButton
        icon="pi pi-info-circle"
        label="保留最近 200 筆，重複錯誤合併計次。"
      />
    </div>
  </div>
  <Card class="min-w-0"
    ><template #content>
      <DataTable
        v-model:first="first"
        v-model:selection="selection"
        selection-mode="single"
        :meta-key-selection="false"
        highlight-on-select
        :value="
          records.filter((r) =>
            `${r.code} ${r.message} ${r.operation} ${r.source}`
              .toLowerCase()
              .includes(query.toLowerCase()),
          )
        "
        data-key="id"
        paginator
        :rows="20"
        @row-select="open($event.data)"
        ><template #empty>沒有錯誤紀錄。</template
        ><Column header="時間"
          ><template #body="{ data }">{{
            date(data.occurredAt)
          }}</template></Column
        ><Column field="source" header="來源" /><Column
          field="operation"
          header="階段" /><Column field="code" header="代碼" /><Column
          field="message"
          header="摘要" /><Column field="count" header="次數" /><Column
          header="操作"
          ><template #body="{ data }"
            ><IconButton
              icon="pi pi-eye"
              label="錯誤詳情"
              @click.stop="open(data)" /><IconButton
              icon="pi pi-trash"
              label="刪除此錯誤"
              severity="danger"
              @click.stop="remove(data.id)" /></template></Column
      ></DataTable> </template
  ></Card>
  <Dialog
    :visible="!!selected"
    header="錯誤詳情"
    modal
    class="w-200 max-w-[calc(100vw-2rem)]"
    @update:visible="close"
    ><dl
      v-if="selected"
      class="grid grid-cols-1 gap-3 sm:grid-cols-[8rem_minmax(0,1fr)] [&>dt]:text-(--p-text-muted-color) [&>dd]:min-w-0 [&>dd]:wrap-anywhere"
    >
      <dt>時間</dt>
      <dd>{{ date(selected.occurredAt) }}</dd>
      <dt>來源／階段</dt>
      <dd>{{ selected.source }}／{{ selected.operation }}</dd>
      <dt>代碼</dt>
      <dd>{{ selected.code }}</dd>
      <dt>HTTP</dt>
      <dd>{{ selected.httpStatus || '—' }}</dd>
      <dt>供應商代碼</dt>
      <dd>{{ selected.vendorCode || '—' }}</dd>
      <dt>Request ID</dt>
      <dd>{{ selected.requestId || '—' }}</dd>
      <dt>作業</dt>
      <dd>{{ selected.jobId || '—' }}</dd>
      <dt>摘要</dt>
      <dd>{{ selected.message }}</dd>
      <dt>詳情</dt>
      <dd class="whitespace-pre-wrap wrap-anywhere">
        {{ selected.detail || '無其他遮敏詳情' }}
      </dd>
      <dt>錯誤 ID</dt>
      <dd>{{ selected.id }}</dd>
    </dl>
    <template #footer
      ><IconButton
        icon="pi pi-copy"
        label="複製遮敏錯誤詳情"
        @click="copy" /><IconButton
        icon="pi pi-times"
        label="關閉錯誤詳情"
        @click="close" /></template
  ></Dialog>
</template>
