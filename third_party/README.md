# 第三方固定來源與法律原文

本目錄保存實際採用的固定來源與法律原文，與 [第三方授權通知](../docs/THIRD_PARTY_NOTICES.md) 共同提供來源依據。版本及提交用於追溯授權；法律文字不翻譯、不刪減，也不以摘要取代原文。

## Sakai Vue 官方版型

布局直接使用官方 Sakai Vue 5.0.0，固定提交 `ec6b6ef53ad4fa7f2861277f50f2d9e236c40372`。樣式保留主儲存庫 4.3.0、提交 `7c32ae1f6fc75e18a5e735f76e1ab25d1167d26c` 的 MIT 來源，與 v5 固定資產逐檔相同，不引入獨立 assets 子模組。

- 布局上游：<https://github.com/primefaces/sakai-vue/tree/ec6b6ef53ad4fa7f2861277f50f2d9e236c40372>
- 樣式來源：<https://github.com/primefaces/sakai-vue/tree/7c32ae1f6fc75e18a5e735f76e1ab25d1167d26c/src/assets>
- 原始授權：<https://github.com/primefaces/sakai-vue/blob/ec6b6ef53ad4fa7f2861277f50f2d9e236c40372/LICENSE.md>；與上述樣式固定來源的法律原文相同。
- v5 固定資產對照：<https://github.com/primefaces/sakai-assets/tree/eaa70ece4cfa9aeb8f60b36dce5a422b6bae8003>；十五份樣式 Git blob 全部相同，只記錄內容對照，不推定該儲存庫其他內容的授權。
- 本機原文：[sakai-vue/LICENSE.md](sakai-vue/LICENSE.md)；保留 PrimeTek 著作權與完整 MIT。
- 未修改來源：十四個布局 SCSS、官方 Tailwind 設定及 MIT 原文，均由離線測試核對 Git blob 雜湊。
- 必要適配：官方布局元件轉為 TypeScript，接入產品路由／品牌、Wails 主題及無障礙行為；逐檔來源與差異見 `frontend/src/layout/sakai-source.json`。

這是隨官方來源保留的法律文字，不是缺漏授權的推測補件；不得自行重寫官方樣式或將其標為本案原創。應用程式與合併授權檔從同一來源清單產生布局／樣式版本說明，原始法律文字不改寫。

## Vue devtools API 授權來源

`@vue/devtools-api` 6.6.4 的 npm 套件沒有附原始授權；本目錄保留相同版本上游的 MIT 原文，不自行補造著作權聲明。

- 上游：<https://github.com/vuejs/devtools-v6>
- 標籤：`v6.6.4`
- 提交：`dd2ab5d4275187cbd81952a2a3d53c335643c1f4`
- 原始檔：<https://github.com/vuejs/devtools-v6/blob/dd2ab5d4275187cbd81952a2a3d53c335643c1f4/LICENSE>
- 本機原文：[vue-devtools-api-6.6.4/LICENSE](vue-devtools-api-6.6.4/LICENSE)。
- 上游 `packages/api/package.json` 的名稱與版本已核對。

打包工具只對名稱及版本完全符合的套件使用此來源。變更版本時須重新核對；其他缺少授權的依賴會停止打包，不自動猜測或下載。
