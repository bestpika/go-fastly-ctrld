<script setup lang="ts">
import Card from 'primevue/card'
import { computed, onMounted, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Tag from 'primevue/tag'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import IconButton from '../components/IconButton.vue'
import SyncPreview from '../components/SyncPreview.vue'
import {
  Bridge as NativeBridge,
  state,
  busy,
  working,
  run,
} from '../shared/app'
import { useReadScope, type ReadTicket } from '../shared/reading'
import { useActionConfirm } from '../shared/confirmation'
import type {
  Group,
  Profile,
  Rule,
  SyncPreview as Preview,
} from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/domain/models'
const Bridge = { ...NativeBridge },
  ask = useActionConfirm(),
  s = computed(() => state.snapshot)
const profiles = ref<Profile[]>([]),
  folders = ref<Group[]>([]),
  rules = ref<Rule[]>([]),
  loaded = ref(false),
  preview = ref<Preview | null>(null)
const folderOptions = computed(() => [
  { id: 0, name: '根目錄', action: false },
  ...folders.value.map((g) => ({
    ...g,
    name: g.name + (g.action ? ' — 繼承動作' : ''),
  })),
])
const showCreate = ref(false),
  folderName = ref(''),
  saving = ref(false),
  applying = ref(false)
const reader = useReadScope(),
  loading = reader.loading,
  phase = reader.phase
const locked = computed(
  () => busy.value || working.value || saving.value || !!s.value?.recovery,
)
function invalidate() {
  reader.cancel()
  loaded.value = false
  rules.value = []
  preview.value = null
}
async function readSelected(ticket: ReadTicket) {
  ticket.phase('讀取所選資料夾')
  const value = await ticket.wait(Bridge.ReadRuleList())
  rules.value = value
  loaded.value = true
}
async function readFolders(ticket: ReadTicket) {
  ticket.phase('讀取資料夾')
  folders.value = await ticket.wait(Bridge.ListFolders())
  await readSelected(ticket)
}
async function loadProfiles() {
  invalidate()
  await reader.execute('讀取設定檔', async (ticket) => {
    profiles.value = await ticket.wait(Bridge.ListProfiles())
    if (s.value?.settings.profileId) await readFolders(ticket)
  })
}
async function loadFolders() {
  invalidate()
  await reader.execute('讀取資料夾', readFolders)
}
async function readRules() {
  invalidate()
  await reader.execute('讀取所選資料夾', readSelected)
}
async function selectProfile(id: string) {
  if (!s.value || !id) return
  invalidate()
  folders.value = []
  saving.value = true
  try {
    const ok = await run(() =>
      Bridge.SelectProfile(s.value!.settings.revision, id),
    )
    saving.value = false
    if (ok) await loadFolders()
  } finally {
    saving.value = false
  }
}
async function selectFolder(id: number) {
  if (!s.value) return
  invalidate()
  saving.value = true
  try {
    const ok = await run(() =>
      Bridge.SelectFolder(s.value!.settings.revision, id),
    )
    saving.value = false
    if (ok) await readRules()
  } finally {
    saving.value = false
  }
}
async function build() {
  preview.value = null
  await reader.execute('跨資料夾核對差異', async (ticket) => {
    preview.value = await ticket.wait(Bridge.BuildSyncPreview([]))
  })
}
async function apply() {
  if (!preview.value) return
  const id = preview.value.id
  if (
    await ask('確認依預覽新增或覆寫規則，並移到所選資料夾？可能影響子網域。')
  ) {
    applying.value = true
    try {
      if (
        await run(
          () => Bridge.ApplySyncPreview(id),
          '已開始套用，請等待逐項回讀',
        )
      ) {
        preview.value = null
        loaded.value = false
      }
    } finally {
      applying.value = false
    }
  }
}
async function createFolder() {
  if (!s.value) return
  invalidate()
  const value = await run(
    () => Bridge.CreateFolder(s.value!.settings.revision, folderName.value),
    '一般資料夾已建立',
  )
  if (value) {
    showCreate.value = false
    folderName.value = ''
    await loadFolders()
  }
}
// 外部換帳號／修改設定不允許舊讀取覆蓋新 scope；自己的保存完成後才啟動新讀取。
watch(
  [
    () => s.value?.settings.revision,
    () => s.value?.settings.credentialRevision,
  ],
  () => {
    if (!saving.value) invalidate()
  },
)
onMounted(() => {
  profiles.value = s.value?.profiles ?? []
  folders.value = s.value?.folders ?? []
  if (s.value?.credentials.saved) void loadProfiles()
})
</script>
<template>
  <div v-if="s" class="space-y-6">
    <header class="flex flex-wrap items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold">Control D 同步</h1>
      <div class="flex flex-wrap items-center gap-2">
        <Tag v-if="loading" severity="info" :value="phase" /><IconButton
          v-if="loading"
          icon="pi pi-stop-circle"
          label="取消讀取"
          @click="reader.cancel"
        /><IconButton
          icon="pi pi-info-circle"
          label="預覽時核對完整 Profile；讀取不修改規則。"
        />
      </div>
    </header>
    <Card
      ><template #content>
        <div class="grid gap-5">
          <label class="flex flex-col gap-2"
            >Profile<Select
              aria-label="設定檔"
              :model-value="s.settings.profileId"
              :options="profiles"
              option-label="name"
              option-value="id"
              placeholder="選擇設定檔"
              :disabled="locked"
              @update:model-value="selectProfile"
          /></label>
          <div class="flex flex-wrap items-center gap-2">
            <IconButton
              icon="pi pi-refresh"
              label="重新讀取設定檔"
              :loading="reader.operation.value === '讀取設定檔'"
              :disabled="locked"
              @click="loadProfiles"
            />
          </div>
          <label class="flex flex-col gap-2"
            >目的資料夾<Select
              aria-label="目的資料夾"
              :model-value="s.settings.destinationFolderId"
              :options="folderOptions"
              option-label="name"
              option-value="id"
              option-disabled="action"
              :disabled="locked || !s.settings.profileId"
              @update:model-value="selectFolder"
          /></label>
          <div class="flex flex-wrap items-center gap-2">
            <IconButton
              icon="pi pi-refresh"
              label="重新讀取資料夾"
              :loading="reader.operation.value === '讀取資料夾'"
              :disabled="locked || !s.settings.profileId"
              @click="loadFolders"
            /><IconButton
              icon="pi pi-folder-plus"
              label="新增一般資料夾"
              :disabled="locked || !s.settings.profileId"
              @click="showCreate = true"
            />
          </div>
        </div>
        <div class="mt-4 flex flex-wrap items-center justify-between gap-4">
          <span
            >所選資料夾規則
            <strong>{{ loaded ? rules.length : '—' }}</strong></span
          >
          <div class="flex flex-wrap items-center gap-2">
            <IconButton
              icon="pi pi-list"
              label="讀取所選資料夾規則"
              :loading="reader.operation.value === '讀取所選資料夾'"
              :disabled="locked || !s.settings.profileId"
              @click="readRules"
            /><IconButton
              icon="pi pi-file-edit"
              label="建立規則差異預覽"
              :loading="reader.operation.value === '跨資料夾核對差異'"
              :disabled="locked || !loaded || !s.domains.some((d) => d.enabled)"
              @click="build"
            />
          </div>
        </div> </template
    ></Card>
    <SyncPreview
      v-if="preview"
      :preview="preview"
      :busy="locked"
      :loading="applying"
      @cancel="preview = null"
      @apply="apply"
    />
    <Dialog
      v-model:visible="showCreate"
      modal
      header="新增一般資料夾"
      class="w-120 max-w-[calc(100vw-2rem)]"
      ><p class="mb-3 leading-relaxed">直接在所選 Profile 新增一般資料夾。</p>
      <label class="flex flex-col gap-2"
        >名稱<InputText
          v-model="folderName"
          maxlength="64"
          aria-label="新資料夾名稱" /></label
      ><template #footer
        ><IconButton
          icon="pi pi-times"
          label="取消新增資料夾"
          @click="showCreate = false" /><IconButton
          icon="pi pi-check"
          label="確認新增資料夾"
          :disabled="locked || !folderName.trim()"
          @click="createFolder" /></template
    ></Dialog>
  </div>
</template>
