<script setup lang="ts">
import Card from 'primevue/card'
import { computed, ref } from 'vue'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import IconButton from '../components/IconButton.vue'
import { useActionConfirm } from '../shared/confirmation'
const ask = useActionConfirm()
import {
  Bridge as NativeBridge,
  state,
  busy,
  working,
  run,
  date,
} from '../shared/app'
import type {
  DomainEntry,
  ImportPreview,
  DetectionResult,
} from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/domain/models'
const Bridge = { ...NativeBridge },
  s = computed(() => state.snapshot!),
  content = ref(''),
  kind = ref('txt'),
  preview = ref<ImportPreview | null>(null),
  edit = ref<DomainEntry | null>(null),
  url = ref(''),
  evidence = ref<DetectionResult | null>(null)
async function inspect(file = false) {
  let id = ''
  if (file) id = (await run(() => Bridge.SelectImportFile())) ?? ''
  if (file && !id) return
  preview.value =
    (await run(() =>
      Bridge.PreviewDomainImport(content.value, kind.value, id),
    )) ?? null
}
async function resolve(line: number, replace: boolean) {
  if (preview.value)
    preview.value =
      (await run(() =>
        Bridge.ResolveDomainImport(preview.value!.id, line, replace),
      )) ?? preview.value
}
async function confirm() {
  if (preview.value) {
    const done = await run(
      () => Bridge.ConfirmDomainImport(preview.value!.id),
      '網域清單已更新',
    )
    if (done) {
      preview.value = null
      content.value = ''
    }
  }
}
async function remove(d: DomainEntry) {
  if (
    await ask(
      `移除 ${d.hostname} 的本機管理？不刪除 Control D 規則，相關自動授權會失效。`,
    )
  )
    await run(
      () => Bridge.RemoveDomain(s.value.settings.revision, d.id),
      '管理項已移除',
    )
}
async function save() {
  if (edit.value) {
    const done = await run(
      () =>
        Bridge.UpdateDomain(
          s.value.settings.revision,
          edit.value!.id,
          edit.value!.enabled,
          url.value,
        ),
      '網域已更新',
    )
    if (done) edit.value = null
  }
}
function startEdit(d: DomainEntry) {
  edit.value = { ...d }
  url.value = d.testUrl
}
</script>
<template>
  <div class="flex flex-wrap items-center justify-between gap-4">
    <h1 class="text-2xl font-semibold">網域管理</h1>
  </div>
  <Card class="min-w-0"
    ><template #content>
      <Textarea
        v-model="content"
        aria-label="貼上網域或 HTTPS 測速網址"
        placeholder="cdn.example.com 或 https://cdn.example.com/files/test.bin"
        :rows="4"
        fluid
      />
      <div class="mt-3 flex flex-wrap items-center gap-2">
        <Select
          v-model="kind"
          aria-label="匯入格式"
          :options="[
            { label: '逐行', value: 'txt' },
            { label: 'CSV', value: 'csv' },
          ]"
          option-label="label"
          option-value="value"
        />
        <IconButton
          icon="pi pi-file-import"
          label="預覽貼上清單"
          :disabled="busy || working || !content.trim()"
          @click="inspect()"
        /><IconButton
          icon="pi pi-folder-open"
          label="選擇 TXT 或 CSV"
          :disabled="busy || working"
          @click="inspect(true)"
        /><IconButton
          icon="pi pi-info-circle"
          label="保留完整網域與測速網址；匯入不下載或修改規則。"
        />
      </div> </template
  ></Card>
  <Card v-if="preview" class="min-w-0"
    ><template #content>
      <DataTable :value="preview.rows ?? []" data-key="line"
        ><Column field="line" header="列" /><Column
          field="entry.hostname"
          header="完整網域" /><Column
          field="entry.testUrl"
          header="測速網址" /><Column field="state" header="狀態" /><Column
          header="操作"
          ><template #body="{ data }"
            ><IconButton
              icon="pi pi-info-circle"
              :label="
                data.message +
                (data.previousUrl ? '；原網址：' + data.previousUrl : '')
              " /><template
              v-if="['conflict', 'replace', 'skip'].includes(data.state)"
              ><IconButton
                icon="pi pi-check"
                label="取代現有測速網址"
                :disabled="busy"
                @click="resolve(data.line, true)" /><IconButton
                icon="pi pi-times"
                label="保留現有測速網址"
                :disabled="busy"
                @click="
                  resolve(data.line, false)
                " /></template></template></Column
      ></DataTable>
      <div class="mt-3 flex flex-wrap items-center gap-2">
        <IconButton
          icon="pi pi-check"
          label="確認匯入有效或已決定更新項目"
          :disabled="
            busy ||
            preview.rows?.some((r) => r.state === 'conflict') ||
            !preview.rows?.some((r) =>
              ['valid', 'update', 'replace'].includes(r.state),
            )
          "
          @click="confirm"
        /><IconButton
          icon="pi pi-times"
          label="取消匯入"
          @click="preview = null"
        />
      </div> </template
  ></Card>
  <Card class="min-w-0"
    ><template #content>
      <DataTable :value="s.domains" data-key="id" paginator :rows="20"
        ><template #empty>尚未加入管理網域。</template
        ><Column header="啟用"
          ><template #body="{ data }"
            ><Checkbox
              binary
              :aria-label="'啟用 ' + data.hostname"
              :model-value="data.enabled"
              :disabled="busy || working"
              @update:model-value="
                run(() =>
                  Bridge.UpdateDomain(
                    s.settings.revision,
                    data.id,
                    !data.enabled,
                    data.testUrl,
                  ),
                )
              " /></template></Column
        ><Column field="hostname" header="完整網域" /><Column header="操作"
          ><template #body="{ data }"
            ><IconButton
              icon="pi pi-file-edit"
              label="編輯測速網址"
              :disabled="busy || working"
              @click="startEdit(data)" /><IconButton
              icon="pi pi-search"
              label="查看雙 DoH 證據"
              @click="
                evidence =
                  s.detections.find((d) => d.domainId === data.id) ?? null
              " /><IconButton
              icon="pi pi-trash"
              label="移除本機管理網域"
              severity="danger"
              :disabled="busy || working"
              @click="remove(data)" /><IconButton
              v-if="data.testUrl"
              icon="pi pi-info-circle"
              :label="data.testUrl" /></template></Column
      ></DataTable> </template
  ></Card>
  <Dialog
    :visible="!!edit"
    header="指定測試檔案"
    modal
    class="w-160 max-w-[calc(100vw-2rem)]"
    @update:visible="edit = null"
    ><strong class="mb-3 block wrap-anywhere">{{ edit?.hostname }}</strong
    ><InputText
      v-model="url"
      fluid
      aria-label="同完整網域的 HTTPS 測速網址"
      placeholder="https://cdn.example.com/files/test.bin" /><IconButton
      icon="pi pi-info-circle"
      label="限同網域 HTTPS、443 埠，不含帳密或片段；自訂測速檔優先。" /><template
      #footer
      ><IconButton
        icon="pi pi-times"
        label="取消編輯"
        @click="edit = null" /><IconButton
        icon="pi pi-save"
        label="保存測速網址"
        :disabled="busy"
        @click="save" /></template></Dialog
  ><Dialog
    :visible="!!evidence"
    header="雙 DoH 證據"
    modal
    class="w-200 max-w-[calc(100vw-2rem)]"
    @update:visible="evidence = null"
    ><p class="mb-3 leading-relaxed wrap-anywhere">
      {{ evidence?.hostname }} · {{ evidence?.classification }}
    </p>
    <section v-for="source in evidence?.sources ?? []" :key="source.source">
      <h3 class="my-3 text-lg font-semibold">{{ source.source }}</h3>
      <p class="mb-3 leading-relaxed">
        {{ date(source.observedAt) }} {{ source.error }}
      </p>
      <DataTable :value="source.records ?? []"
        ><Column field="type" header="類型" /><Column
          field="name"
          header="名稱" /><Column field="value" header="值" /><Column
          field="ttl"
          header="TTL" /><Column field="cidr" header="CIDR"
      /></DataTable></section
  ></Dialog>
</template>
