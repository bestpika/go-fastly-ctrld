<script setup lang="ts">
import { computed, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import InputText from 'primevue/inputtext'
import Card from 'primevue/card'
import Accordion from 'primevue/accordion'
import AccordionPanel from 'primevue/accordionpanel'
import AccordionHeader from 'primevue/accordionheader'
import AccordionContent from 'primevue/accordioncontent'
import IconButton from '../components/IconButton.vue'
import Tag from 'primevue/tag'
import { reportFailure } from '../shared/app'
import about from '../../generated/about.json'

const query = ref('')
const expanded = ref<string[]>([])
const build = about.build
const components = computed(() => {
  const value = query.value.trim().toLocaleLowerCase()
  return about.components.filter((entry) =>
    `${entry.name} ${entry.version} ${entry.license} ${entry.kind}`
      .toLocaleLowerCase()
      .includes(value),
  )
})

async function openSource(source: string) {
  try {
    const url = new URL(source)
    if (url.protocol !== 'https:' || url.username || url.password)
      throw new Error('來源連結不安全')
    await Browser.OpenURL(url.href)
  } catch {
    await reportFailure('BROWSER_OPEN_FAILED')
  }
}
</script>

<template>
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
      <h1 class="text-2xl font-semibold">關於與開源授權</h1>
      <IconButton
        icon="pi pi-info-circle"
        label="法律原文可離線閱讀；來源連結以系統瀏覽器開啟。"
      />
    </div>
    <Tag :value="`${about.components.length} 個元件`" severity="info" />
  </div>
  <Card
    ><template #content>
      <h2 class="mb-4 text-xl font-semibold">Fastly 路線最佳化</h2>
      <dl
        class="grid grid-cols-1 gap-3 sm:grid-cols-[8rem_minmax(0,1fr)] [&>dt]:text-(--p-text-muted-color) [&>dd]:min-w-0 [&>dd]:wrap-anywhere"
      >
        <dt>版本</dt>
        <dd>{{ build.version }}</dd>
        <dt>來源提交</dt>
        <dd class="font-mono wrap-anywhere">{{ build.revision }}</dd>
        <dt>未提交內容</dt>
        <dd>{{ build.dirty ? '有' : '無' }}</dd>
        <dt>平台</dt>
        <dd>{{ build.platform }}</dd>
        <dt>建置時間</dt>
        <dd>{{ build.builtAt }}</dd>
        <dt>Wails</dt>
        <dd>{{ build.wails }}</dd>
        <dt>Go</dt>
        <dd>{{ build.go }}</dd>
        <dt>Node.js</dt>
        <dd>{{ build.node }}</dd>
      </dl>
    </template></Card
  >
  <Card
    ><template #content>
      <div class="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h2 class="text-xl font-semibold">開源元件</h2>
        <InputText
          v-model="query"
          aria-label="搜尋開源元件"
          placeholder="搜尋名稱、版本或授權類型"
        />
      </div>
      <p v-if="!components.length" class="mb-3 text-(--p-text-muted-color)">
        沒有符合搜尋條件的元件。
      </p>
      <Accordion v-model:value="expanded" multiple>
        <AccordionPanel
          v-for="entry in components"
          :key="entry.id"
          :value="entry.id"
        >
          <AccordionHeader>
            <span class="flex min-w-0 flex-wrap items-center gap-3 text-left">
              <strong class="min-w-0 wrap-anywhere">{{ entry.name }}</strong>
              <span class="font-mono font-normal wrap-anywhere">{{
                entry.version
              }}</span>
              <Tag :value="entry.license" severity="secondary" />
            </span>
          </AccordionHeader>
          <AccordionContent>
            <div class="flex items-center gap-2">
              <span class="text-(--p-text-muted-color)">{{ entry.kind }}</span>
              <IconButton
                label="以系統瀏覽器開啟來源"
                icon="pi pi-external-link"
                severity="secondary"
                @click="openSource(entry.source)"
              />
            </div>
            <div v-for="file in entry.files" :key="file.name">
              <h3 class="my-3 font-mono font-semibold wrap-anywhere">
                {{ file.name }}
              </h3>
              <pre
                class="max-h-128 overflow-auto whitespace-pre-wrap wrap-anywhere text-sm leading-relaxed"
                >{{ file.text }}</pre>
            </div>
          </AccordionContent>
        </AccordionPanel>
      </Accordion>
    </template></Card
  >
</template>
