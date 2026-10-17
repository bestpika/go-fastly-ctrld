<script setup lang="ts">
import IconButton from './IconButton.vue'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Card from 'primevue/card'
import Message from 'primevue/message'
import type { SyncPreview } from '../../bindings/github.com/bestpika/go-fastly-ctrld/internal/domain/models'
import { label, date } from '../shared/app'
defineProps<{ preview: SyncPreview; busy: boolean; loading?: boolean }>()
defineEmits<{ apply: []; cancel: [] }>()
</script>
<template>
  <Card class="min-w-0"
    ><template #content>
      <h2 class="mb-4 text-xl font-semibold">
        {{ preview.kind === 'restore' ? '復原差異預覽' : '規則差異預覽' }}
      </h2>
      <p class="mb-3 leading-relaxed wrap-anywhere">
        Profile：{{ preview.profileId }}
      </p>
      <Message severity="warn" class="mb-4">
        可能影響子網域；請逐項確認結果。
      </Message>
      <Message
        v-if="preview.items?.some((item) => item.forceWrite)"
        severity="warn"
        class="mb-4"
        >強制寫入：無改善時回填原始 DNS，同值仍提交。</Message
      >
      <DataTable :value="preview.items ?? []" data-key="id" paginator :rows="10"
        ><Column field="hostname" header="網域" /><Column header="操作"
          ><template #body="{ data }">{{
            label(data.operation)
          }}</template></Column
        ><Column header="原值"
          ><template #body="{ data }"
            ><div>
              {{
                data.before
                  ? `do=${data.before.do}／${data.before.status ? '啟用' : '停用'}／資料夾 ${data.before.group}`
                  : '不存在'
              }}
            </div>
            <div class="font-mono wrap-anywhere">
              {{ data.before?.via }}<br />{{ data.before?.viaV6 }}
            </div>
            <div v-if="data.before?.comment">
              註解：{{ data.before.comment }}
            </div></template
          ></Column
        ><Column header="新值"
          ><template #body="{ data }"
            ><div>
              {{
                data.after
                  ? `do=${data.after.do}／${data.after.status ? '啟用' : '停用'}／資料夾 ${data.after.group}`
                  : preview.kind === 'restore' &&
                      data.operation === 'delete' &&
                      data.applicable
                    ? '刪除當時新增的規則'
                    : '不變更，保留原值'
              }}
            </div>
            <div class="font-mono wrap-anywhere">
              {{ data.after?.via }}<br />{{ data.after?.viaV6 }}
            </div>
            <div v-if="data.after?.comment">
              註解：{{ data.after.comment }}
            </div></template
          ></Column
        ><Column header="資格與原因"
          ><template #body="{ data }"
            ><Tag
              :value="data.applicable ? '可執行' : '略過'"
              :severity="data.applicable ? 'success' : 'warn'"
            />
            <p v-if="data.originalDns" class="mt-3 leading-relaxed">
              原始 DNS：Google＋Cloudflare ·
              {{ date(data.originalDns.observedAt) }}
            </p>
            <p class="mt-3 leading-relaxed">{{ data.reason }}</p></template
          ></Column
        ></DataTable
      >
      <div class="mt-3 flex flex-wrap items-center gap-2">
        <IconButton
          icon="pi pi-times"
          label="取消預覽"
          severity="secondary"
          :disabled="busy"
          @click="$emit('cancel')"
        /><IconButton
          icon="pi pi-check"
          :label="
            preview.kind === 'restore'
              ? '確認執行可復原項目'
              : '確認套用可執行項目'
          "
          :disabled="busy || !preview.items?.some((item) => item.applicable)"
          :loading="loading"
          @click="$emit('apply')"
        />
      </div> </template
  ></Card>
</template>
