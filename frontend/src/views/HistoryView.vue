<script setup lang="ts">
import Card from 'primevue/card'
import { computed, ref, watch } from 'vue'
import { useReadScope } from '../shared/reading'
import IconButton from '../components/IconButton.vue'
import { useActionConfirm } from '../shared/confirmation'
const ask = useActionConfirm()
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Dialog from 'primevue/dialog'
import Tag from 'primevue/tag'
import SyncPreviewPanel from '../components/SyncPreview.vue'
import {
  Bridge as NativeBridge,
  state,
  busy,
  working,
  run,
  date,
  label,
  severity,
  bytes,
  notify,
  type Snapshot,
} from '../shared/app'
import type { SyncPreview } from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/domain/models'
const Bridge = { ...NativeBridge }
const reader = useReadScope()
const s = computed(() => state.snapshot!),
  detailID = ref(''),
  preview = ref<SyncPreview | null>(null)
const details = computed(() =>
  s.value.jobs.find((j) => j.id === detailID.value),
)
function canDelete(job: Snapshot['jobs'][number]) {
  return (
    ['completed', 'partial', 'failed', 'cancelled'].includes(job.state) &&
    job.id !== s.value.activeTestId &&
    job.id !== s.value.activeWriterId &&
    !job.items.some((item) => ['write_started', 'unknown'].includes(item.state))
  )
}
async function remove(id: string) {
  if (
    !(await ask(
      id
        ? '刪除此筆紀錄？規則、測速結果與復原底帳會保留。'
        : '清除已結束紀錄？進行中、待核對與復原底帳會保留，不修改規則或結果。',
    ))
  )
    return
  const result = await run(() => Bridge.DeleteJobs(id))
  if (!result) return
  reader.cancel()
  preview.value = null
  if (!details.value) detailID.value = ''
  const message = `已刪除 ${result.deleted} 筆執行紀錄${result.retained ? `，保留 ${result.retained} 筆進行中或待核對作業` : ''}`
  state.notice = message
  notify(result.retained ? 'warn' : 'success', message)
}
async function restore(id: string) {
  preview.value = null
  await reader.execute('讀取跨資料夾復原預覽', async (t) => {
    preview.value = await t.wait(Bridge.BuildRestorePreview(id))
  })
}
watch(
  [() => s.value.settings.revision, () => s.value.settings.credentialRevision],
  () => {
    reader.cancel()
    preview.value = null
  },
)
async function apply() {
  if (
    !preview.value ||
    !(await ask('確定執行可復原項目？後續已被修改的規則不會強制還原。'))
  )
    return
  const id = preview.value.id
  preview.value = null
  await run(
    () => Bridge.ApplyRestorePreview(id),
    '復原作業已排入佇列，請查看逐項結果',
  )
}
</script>
<template>
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
      <h1 class="text-2xl font-semibold">執行紀錄</h1>
      <span v-if="reader.loading.value" role="status">{{
        reader.phase.value
      }}</span>
      <IconButton
        v-if="reader.loading.value"
        icon="pi pi-times"
        label="取消讀取復原預覽"
        @click="reader.cancel"
      />
      <IconButton icon="pi pi-info-circle" label="作業完成不等於整批成功。" />
    </div>
    <IconButton
      icon="pi pi-trash"
      label="清除全部執行紀錄"
      severity="danger"
      :disabled="busy || !s.jobs.length"
      @click="remove('')"
    />
  </div>
  <Card class="min-w-0"
    ><template #content>
      <DataTable :value="s.jobs" data-key="id" paginator :rows="10" striped-rows
        ><template #empty>尚無紀錄。</template
        ><Column header="時間"
          ><template #body="{ data }">{{
            date(data.startedAt)
          }}</template></Column
        ><Column header="作業"
          ><template #body="{ data }"
            >{{ label(data.kind) }} ·
            {{ data.trigger === 'manual' ? '手動' : '自動' }}</template
          ></Column
        ><Column header="狀態"
          ><template #body="{ data }"
            ><Tag
              :value="label(data.state)"
              :severity="severity(data.state)" /></template></Column
        ><Column field="message" header="摘要" /><Column header="操作"
          ><template #body="{ data }"
            ><div class="flex flex-wrap items-center gap-2">
              <IconButton
                icon="pi pi-list"
                label="明細"
                @click="detailID = data.id"
              /><IconButton
                v-if="
                  ['apply', 'restore'].includes(data.kind) &&
                  data.items.some(
                    (item: { state: string }) => item.state === 'verified',
                  )
                "
                icon="pi pi-undo"
                label="復原預覽"
                severity="secondary"
                :disabled="busy || working || s.recovery"
                @click="restore(data.id)"
              /><IconButton
                v-if="data.state === 'needs_recovery'"
                icon="pi pi-refresh"
                label="回讀確認"
                severity="warn"
                :disabled="busy || working"
                @click="
                  run(
                    () => Bridge.RecoverJob(data.id),
                    '已回讀，未重送任何寫入',
                  )
                "
              /><IconButton
                icon="pi pi-trash"
                label="刪除此筆執行紀錄"
                severity="danger"
                :disabled="busy || !canDelete(data)"
                @click="remove(data.id)"
              /></div></template></Column
      ></DataTable> </template
  ></Card>
  <SyncPreviewPanel
    v-if="preview"
    :preview="preview"
    :busy="busy || working"
    @apply="apply"
    @cancel="preview = null"
  /><Dialog
    :visible="!!details"
    header="作業逐項結果"
    modal
    class="w-250 max-w-[calc(100vw-2rem)]"
    @update:visible="detailID = ''"
    ><template v-if="details"
      ><p class="mb-3 leading-relaxed">
        {{ label(details.kind) }} · {{ label(details.state) }} ·
        {{ details.completed }}／{{ details.total }} ·
        {{ bytes(details.bytes) }}
      </p>
      <p class="mb-3 leading-relaxed">{{ details.message }}</p>
      <IconButton
        v-if="['queued', 'running'].includes(details.state)"
        icon="pi pi-stop"
        label="要求安全停止"
        severity="danger"
        :disabled="busy"
        @click="
          run(
            () => Bridge.CancelJob(details!.id),
            '已要求停止，開始寫入的項目先回讀或記錄待復原',
          )
        "
      /><DataTable :value="details.items ?? []"
        ><template #empty>此作業沒有帳號寫入項目。</template
        ><Column field="preview.hostname" header="網域" /><Column header="狀態"
          ><template #body="{ data }"
            ><Tag
              :value="label(data.state)"
              :severity="severity(data.state)" /></template></Column
        ><Column field="message" header="原因" /><Column header="回讀目標"
          ><template #body="{ data }"
            >{{ data.after?.via }}<br />{{ data.after?.viaV6 }}</template
          ></Column
        ></DataTable
      ></template
    ></Dialog
  >
</template>
