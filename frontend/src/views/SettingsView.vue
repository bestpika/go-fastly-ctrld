<script setup lang="ts">
import Card from 'primevue/card'
import { computed, nextTick, reactive, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Checkbox from 'primevue/checkbox'
import Fieldset from 'primevue/fieldset'
import IconButton from '../components/IconButton.vue'
import { useActionConfirm } from '../shared/confirmation'
import { useReadScope } from '../shared/reading'
import { fonts, fontOptions, readLocalFonts, selectFont } from '../shared/fonts'
const ask = useActionConfirm()
import {
  Bridge as NativeBridge,
  state,
  busy,
  working,
  run,
  clone,
  notify,
} from '../shared/app'
const Bridge = { ...NativeBridge },
  s = computed(() => state.snapshot!)
const fontSelection = ref(fonts.family)
const connectionReader = useReadScope()
watch(
  () => s.value.settings.credentialRevision,
  () => connectionReader.cancel(),
)
async function checkConnection() {
  await connectionReader.execute('讀取帳號連線', async (t) => {
    await t.wait(Bridge.CheckControlDConnection())
    notify('success', '讀取連線成功')
  })
}
const token = ref(''),
  mode = ref('off'),
  minutes = ref<number | null>(1440),
  budgetMB = ref<number | null>(0),
  jobSeconds = ref<number | null>(0),
  services = ref<string[]>([]),
  noResourceMode = ref('latency'),
  enableIpv6 = ref(false),
  forceWrite = ref(false),
  grantDialog = ref(false),
  allowShared = ref(false)
const grants = reactive<
  Record<string, { selected: boolean; overwrite: boolean; move: boolean }>
>({})
function reset() {
  const a = s.value.settings.automation
  mode.value = a.enabled ? (a.testOnly ? 'test' : 'apply') : 'off'
  minutes.value = a.intervalSeconds / 60
  budgetMB.value = a.budgetBytes / 1e6
  jobSeconds.value = s.value.settings.probe.jobSeconds
  services.value = [...s.value.settings.probe.services]
  noResourceMode.value = s.value.settings.probe.noResourceMode
  enableIpv6.value = s.value.settings.probe.enableIpv6
  forceWrite.value = s.value.settings.forceWrite
  allowShared.value = a.allowSharedReference
  for (const key of Object.keys(grants)) delete grants[key]
  for (const d of s.value.domains) {
    const old = a.grants.find((g) => g.domainId === d.id)
    grants[d.id] = {
      selected: d.enabled,
      overwrite: old?.overwrite ?? false,
      move: old?.move ?? false,
    }
  }
}
watch(() => s.value.settings.revision, reset, { immediate: true })
async function saveToken() {
  const value = token.value
  token.value = ''
  await run(() => Bridge.SetControlDToken(value), '權杖已保存')
}
async function deleteToken() {
  if (await ask('刪除保存的權杖？自動授權會失效，不刪除帳號規則。'))
    await run(() => Bridge.DeleteControlDToken(), '權杖已刪除')
}
async function saveForceWrite() {
  if (
    !(await ask(
      forceWrite.value
        ? '開啟強制寫入？無改善時回填原始 DNS，同值仍寫入，自動套用略過冷卻。須重新測試及授權。'
        : '關閉強制寫入？恢復只寫改善候選，停用自動套用。須重新測試及授權。',
    ))
  )
    return
  await run(
    () =>
      Bridge.UpdateSettings(s.value.settings.revision, {
        theme: s.value.settings.theme,
        probe: s.value.settings.probe,
        forceWrite: forceWrite.value,
      }),
    '已保存，請重新測試及授權',
  )
}
async function saveAutomation() {
  const a = clone(s.value.settings.automation)
  a.enabled = mode.value !== 'off'
  a.testOnly = mode.value === 'test'
  a.intervalSeconds = Math.round((minutes.value ?? 0) * 60)
  a.profileId = s.value.settings.profileId
  a.credentialRevision = s.value.settings.credentialRevision
  a.destinationFolderId = s.value.settings.destinationFolderId
  a.allowSharedReference = allowShared.value
  a.grants = s.value.domains
    .filter((d) => d.enabled && grants[d.id]?.selected)
    .map((d) => ({
      domainId: d.id,
      overwrite: grants[d.id]!.overwrite,
      move: grants[d.id]!.move,
    }))
  await run(
    () => Bridge.ConfigureAutomation(s.value.settings.revision, a),
    '自動設定已保存',
  )
  grantDialog.value = false
}
async function configure() {
  if (mode.value === 'apply') grantDialog.value = true
  else if (
    mode.value === 'off' ||
    (await ask(
      `啟用自動測試？每 ${minutes.value} 分鐘執行，不寫入規則。流量 ${s.value.settings.automation.budgetBytes ? s.value.settings.automation.budgetBytes / 1e6 + ' MB' : '不限'}。`,
    ))
  )
    await saveAutomation()
}
async function advanced() {
  if (budgetMB.value == null || jobSeconds.value == null) return
  const budgetBytes = Math.round(budgetMB.value * 1e6)
  const p = {
    ...s.value.settings.probe,
    jobSeconds: jobSeconds.value,
    services: [...services.value],
    noResourceMode: noResourceMode.value,
    enableIpv6: enableIpv6.value,
  }
  const saved = await run(() =>
    Bridge.UpdateSettings(s.value.settings.revision, {
      theme: s.value.settings.theme,
      probe: p,
    }),
  )
  if (!saved) return
  const a = {
    ...s.value.settings.automation,
    budgetBytes,
    enabled: false,
    grants: [],
  }
  await run(
    () => Bridge.ConfigureAutomation(s.value.settings.revision, a),
    '已保存，請重新確認自動授權',
  )
}
async function quit() {
  if (await ask('真正結束程式並安全停止背景工作？已套用的規則不會刪除。'))
    await run(() => Bridge.QuitApplication())
}
async function loadFonts() {
  const error = await readLocalFonts()
  if (error) notify('warn', error)
}
async function changeFont(family: string) {
  const error = selectFont(family)
  if (error) {
    await nextTick()
    fontSelection.value = fonts.family
    notify('error', error)
  }
}
</script>
<template>
  <div class="flex flex-wrap items-center justify-between gap-4">
    <h1 class="text-2xl font-semibold">設定</h1>
  </div>
  <div class="grid gap-6">
    <Card
      ><template #content>
        <div class="mb-4 flex flex-wrap items-center justify-between gap-4">
          <h2 class="text-xl font-semibold">Control D</h2>
          <IconButton
            icon="pi pi-info-circle"
            label="權杖存於系統金鑰庫；讀取權限不代表可寫入。"
          />
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <InputText
            v-model="token"
            type="password"
            aria-label="Control D 權杖"
            autocomplete="new-password"
            placeholder="API 權杖"
          /><IconButton
            icon="pi pi-save"
            label="保存權杖"
            :disabled="busy || working || !token"
            @click="saveToken"
          /><IconButton
            icon="pi pi-trash"
            label="刪除權杖"
            severity="danger"
            :disabled="busy || working || !s.credentials.saved"
            @click="deleteToken"
          /><IconButton
            icon="pi pi-link"
            label="測試帳號讀取連線"
            :loading="connectionReader.loading.value"
            :disabled="busy || !s.credentials.saved"
            @click="checkConnection"
          />
          <IconButton
            v-if="connectionReader.loading.value"
            icon="pi pi-times"
            label="取消讀取帳號連線"
            @click="connectionReader.cancel"
          />
        </div>
        <small
          >{{ s.credentials.saved ? '已保存' : '未保存' }} ·
          {{ s.credentials.available ? '金鑰庫可用' : '金鑰庫不可用' }}</small
        >
      </template></Card
    >
    <Card
      ><template #content>
        <h2 class="mb-4 text-xl font-semibold">強制寫入 Control D</h2>
        <div class="flex items-center gap-2">
          <Checkbox
            v-model="forceWrite"
            binary
            input-id="force-write"
            :disabled="busy || working"
          />
          <label for="force-write">無改善時回填原始 DNS，同值仍寫入</label>
        </div>
        <p class="my-3 leading-relaxed">
          有改善用最佳 IP，否則回填原始 CNAME 或
          IP。驗證失敗不寫入；「只測試」不修改規則。
        </p>
        <IconButton
          icon="pi pi-save"
          label="保存強制寫入設定"
          :disabled="busy || working || forceWrite === s.settings.forceWrite"
          @click="saveForceWrite"
        /> </template
    ></Card>
    <Card
      ><template #content>
        <div class="mb-4 flex flex-wrap items-center justify-between gap-4">
          <h2 class="text-xl font-semibold">自動執行</h2>
          <IconButton
            icon="pi pi-info-circle"
            label="網路穩定等待 30 秒，切換冷卻 15 分鐘；強制寫入略過冷卻。"
          />
        </div>
        <div class="grid gap-5">
          <label class="flex flex-col gap-2"
            >模式<Select
              v-model="mode"
              aria-label="自動模式"
              :options="[
                { label: '關閉', value: 'off' },
                { label: '只測試', value: 'test' },
                { label: '測試後套用', value: 'apply' },
              ]"
              option-label="label"
              option-value="value"
              :disabled="busy || working" /></label
          ><label class="flex flex-col gap-2"
            >間隔，分鐘<InputNumber
              v-model="minutes"
              :min="1"
              :max="44640"
              :use-grouping="false"
              :disabled="busy || working"
              aria-label="自動間隔分鐘"
          /></label>
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <IconButton
            icon="pi pi-save"
            label="保存自動設定"
            :disabled="
              busy ||
              working ||
              minutes == null ||
              minutes < 1 ||
              minutes > 44640
            "
            @click="configure"
          /><IconButton
            icon="pi pi-pause"
            label="暫停自動執行"
            :disabled="busy || !s.settings.automation.enabled"
            @click="run(() => Bridge.PauseAutomation(), '自動已暫停')"
          />
        </div> </template
    ></Card>
    <Card
      ><template #content>
        <h2 class="mb-4 text-xl font-semibold">外觀與啟動</h2>
        <div class="mb-5 grid gap-3">
          <label for="app-font">應用程式字型</label>
          <Select
            v-model="fontSelection"
            input-id="app-font"
            aria-label="應用程式字型"
            placeholder="預設 Lato"
            :options="fontOptions"
            option-label="label"
            option-value="value"
            filter
            :loading="fonts.loading"
            @show="loadFonts"
            @update:model-value="changeFont"
          />
          <small>開啟清單時讀取系統字型，可能需要授權。</small>
          <p>字型預覽：繁體中文 Aa Bb 0123456789</p>
        </div>
        <div class="flex flex-wrap items-center gap-6">
          <div class="flex items-center gap-2">
            <Checkbox
              binary
              input-id="dark-mode"
              :model-value="s.settings.theme === 'dark'"
              :disabled="busy || working"
              aria-label="深色模式"
              @update:model-value="
                run(() =>
                  Bridge.UpdateSettings(s.settings.revision, {
                    theme: s.settings.theme === 'dark' ? 'light' : 'dark',
                    probe: s.settings.probe,
                  }),
                )
              "
            />
            <label for="dark-mode">深色模式</label>
          </div>
          <div class="flex items-center gap-2">
            <Checkbox
              binary
              input-id="launch-at-login"
              :model-value="s.settings.launchAtLogin"
              aria-label="登入時啟動"
              :disabled="busy || !s.desktop.launchAvailable"
              @update:model-value="run(() => Bridge.SetLaunchAtLogin($event))"
            />
            <label for="launch-at-login">登入時啟動</label>
          </div>
          <IconButton
            icon="pi pi-info-circle"
            :label="s.desktop.launchMessage + '；' + s.desktop.trayMessage"
          />
        </div> </template
    ></Card>
    <Card
      ><template #content>
        <Fieldset legend="測試策略與進階上限" toggleable collapsed>
          <p class="mb-3 leading-relaxed">
            基準不需權杖，不加入管理清單或修改規則。
          </p>
          <div class="my-3 flex flex-wrap items-center gap-6">
            <div
              v-for="service in [
                { value: 'github', label: 'GitHub' },
                { value: 'twitch', label: 'Twitch' },
                { value: 'x', label: 'X 圖片' },
              ]"
              :key="service.value"
              class="flex items-center gap-2"
            >
              <Checkbox
                v-model="services"
                :input-id="'baseline-' + service.value"
                :value="service.value"
                :aria-label="'基準 ' + service.label"
                :disabled="busy || working"
              />
              <label :for="'baseline-' + service.value">{{
                service.label
              }}</label>
            </div>
          </div>
          <small
            >共 12 域獨立探測，三個資源另測下載。共享參考會補測 GitHub 與
            Twitch。</small
          >
          <div class="mt-4 grid gap-5">
            <label class="flex flex-col gap-2"
              >一般網域沒有測速檔時<Select
                v-model="noResourceMode"
                :options="[
                  { label: '自身 TCP 延遲，原版預設', value: 'latency' },
                  {
                    label: '共享 GitHub＋Twitch 路線參考，擴充',
                    value: 'shared',
                  },
                ]"
                option-label="label"
                option-value="value"
                aria-label="無測速檔策略"
                :disabled="busy || working"
            /></label>
            <div class="flex items-center gap-2">
              <Checkbox
                v-model="enableIpv6"
                binary
                input-id="enable-ipv6"
                :disabled="busy || working"
              /><label for="enable-ipv6">啟用 IPv6</label>
            </div>
            <label class="flex flex-col gap-2"
              >整作業流量，MB<InputNumber
                v-model="budgetMB"
                :min="0"
                :max="100000"
                :use-grouping="false"
                aria-label="整作業流量 MB" /></label
            ><label class="flex flex-col gap-2"
              >整作業期限，秒<InputNumber
                v-model="jobSeconds"
                :min="0"
                :max="86400"
                :use-grouping="false"
                aria-label="整作業期限秒"
            /></label>
          </div>
          <div class="mt-3 flex flex-wrap items-center gap-2">
            <IconButton
              icon="pi pi-info-circle"
              label="0 為不限，流量不含協定開銷。修改後須重新測試及授權。"
            /><IconButton
              icon="pi pi-save"
              label="保存測試策略與上限"
              :disabled="
                busy ||
                working ||
                budgetMB == null ||
                jobSeconds == null ||
                budgetMB < 0 ||
                (budgetMB > 0 && budgetMB < 1) ||
                budgetMB > 100000 ||
                jobSeconds < 0 ||
                (jobSeconds > 0 && jobSeconds < 30) ||
                jobSeconds > 86400
              "
              @click="advanced"
            /><IconButton
              icon="pi pi-eraser"
              label="清除已過期快取"
              @click="run(() => Bridge.CleanExpiredCaches(), '快取已清理')"
            /><IconButton
              icon="pi pi-download"
              label="匯出遮敏診斷"
              @click="run(() => Bridge.ExportDiagnostics())"
            /><IconButton
              icon="pi pi-power-off"
              label="真正退出程式"
              severity="danger"
              @click="quit"
            />
          </div>
        </Fieldset> </template
    ></Card>
  </div>
  <Dialog
    v-model:visible="grantDialog"
    header="確認自動套用授權"
    modal
    class="w-192 max-w-[calc(100vw-2rem)]"
    ><p class="mb-3 leading-relaxed wrap-anywhere">
      Profile：{{ s.settings.profileId || '未選' }} · 目的資料夾：{{
        s.settings.destinationFolderId || '根目錄'
      }}
    </p>
    <p class="mb-3 leading-relaxed">
      僅限勾選網域，可能影響子網域。覆寫會取代原動作，移動會變更資料夾。每
      {{ minutes }} 分鐘及網路變動後執行，流量
      {{
        s.settings.automation.budgetBytes
          ? s.settings.automation.budgetBytes / 1e6 + ' MB'
          : '不限'
      }}。
    </p>
    <p v-if="s.settings.forceWrite" class="mb-3 leading-relaxed">
      強制寫入：無改善時回填原始 DNS，同值仍寫入並略過冷卻；仍須下列授權。
    </p>
    <div class="flex items-center gap-2">
      <Checkbox
        v-model="allowShared"
        binary
        input-id="allow-shared-reference"
        aria-label="允許共享參考自動套用"
      />
      <label for="allow-shared-reference">允許依本輪基準參考自動套用</label>
    </div>
    <div
      v-for="d in s.domains.filter((x) => x.enabled)"
      :key="d.id"
      class="my-3 flex flex-wrap items-center gap-6"
    >
      <div class="flex items-center gap-2">
        <Checkbox
          v-if="grants[d.id]"
          v-model="grants[d.id]!.selected"
          binary
          :input-id="'grant-' + d.id"
          :aria-label="'授權 ' + d.hostname"
        />
        <label :for="'grant-' + d.id" class="wrap-anywhere">{{
          d.hostname
        }}</label>
      </div>
      <div class="flex items-center gap-2">
        <Checkbox
          v-if="grants[d.id]"
          v-model="grants[d.id]!.overwrite"
          binary
          :input-id="'overwrite-' + d.id"
          :aria-label="'允許覆寫 ' + d.hostname"
        />
        <label :for="'overwrite-' + d.id">覆寫</label>
      </div>
      <div class="flex items-center gap-2">
        <Checkbox
          v-if="grants[d.id]"
          v-model="grants[d.id]!.move"
          binary
          :input-id="'move-' + d.id"
          :aria-label="'允許移動 ' + d.hostname"
        />
        <label :for="'move-' + d.id">移動</label>
      </div>
    </div>
    <template #footer
      ><IconButton
        icon="pi pi-times"
        label="取消授權"
        @click="grantDialog = false" /><IconButton
        icon="pi pi-check"
        label="確認自動套用授權"
        :disabled="busy || !s.settings.profileId || !s.credentials.saved"
        @click="saveAutomation" /></template
  ></Dialog>
</template>
