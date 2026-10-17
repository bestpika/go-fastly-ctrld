# 第三方授權通知

本專案採 MIT；各第三方元件保有其原始著作權與授權。發佈套件以單一 `THIRD_PARTY_NOTICES.txt` 合併完整法律原文，應用的「關於與開源授權」頁內嵌同一份資料，可離線查閱版本、來源與全文。固定來源清單見 [third_party/README.md](../third_party/README.md)。

## Sakai Vue

來源：<https://github.com/primefaces/sakai-vue>，布局版本 5.0.0，固定提交 `ec6b6ef53ad4fa7f2861277f50f2d9e236c40372`；樣式使用下列固定 MIT 來源。

直接整合該版本的 AppLayout／AppTopbar／AppSidebar／AppMenu／AppMenuItem／AppFooter 與布局 composable，保留 PrimeTek 著作權；只適配 TypeScript、產品選單／路由／品牌、Wails 主題及必要無障礙行為，不帶入展示與假登入。不是另以 PrimeVue Toolbar／Menu／Drawer 組合替代。上游路徑、固定 Git blob 雜湊與適配說明見 `frontend/src/layout/sakai-source.json`。

十四個官方布局 SCSS 與 Tailwind 設定採主儲存庫版本 4.3.0、提交 `7c32ae1f6fc75e18a5e735f76e1ab25d1167d26c` 的 `src/assets/layout` 及 `src/assets/tailwind.css`；此固定提交中的檔案為 MIT 授權的一般檔案。十五份樣式的 Git blob 與 v5 固定的 <https://github.com/primefaces/sakai-assets> 提交 `eaa70ece4cfa9aeb8f60b36dce5a422b6bae8003` 全部相同，只記錄內容對照，不引入獨立子模組或推定其其他內容授權。原始 MIT 保存於 [third_party/sakai-vue/LICENSE.md](../third_party/sakai-vue/LICENSE.md)，與布局版本的法律原文相同；離線測試核對未修改檔案。關於頁與發佈合併法律檔由來源清單同源產生布局版本及樣式說明。以下為實際採用的 Sakai Vue 主儲存庫 MIT 原文。

```text
The MIT License (MIT)

Copyright (c) 2018-2022 PrimeTek

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

## fastly-hosts-adjust

來源：<https://github.com/AFAlimru/fastly-hosts-adjust>，參考版本 0.8.3、固定提交 `336e105f71b8fce123d5bfdc9f635b9b8d500d0b`。服務探測、資格後低延遲篩選、選檔、256 KiB 讀後兩秒及順暢排名以 Go 改寫；本案對 12 域各自探測、三域下載，X 使用指定 PNG 實測，不採上游 POP 借分。資格探測採跨域共用上限、管理與基準公平排程；下載只接受非空 2xx，與上游接受 3xx 本文的行為不同。只擷取原 rank／evaluate 做固定輸入 AST 重播，未執行主程式、配置或 hosts。

共享概念參考上游 X 借用 GitHub 路線成績；本案採 Twitch＋GitHub 兩來源共識、逐目標 TLS 驗證、有限批次快取／預算、IPv6 同位址與獨立自動授權。參考概念、適配與自行設計的分類見 [docs/DESIGN.md](DESIGN.md)，不把框架或樣板宣稱為本案原創。

```text
MIT License

Copyright (c) 2026 AFAlimru

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## 套件與字型

PrimeVue 4.5.5、Aura 主題 2.0.3、Vue、Vue Router、Wails、PrimeUI Tailwind 外掛及相關 Go／npm 依賴依各自原始授權；沿用 Sakai 的 Lato 字型，採 OFL-1.1。Sass 與 Node 型別為開發依賴。打包程序保留實際依賴授權與 NOTICE，不以本文件的概述取代原始法律文字。

應用頁列出名稱、版本、授權類型、安全來源與逐檔完整文字，包含 Go 標準函式庫及實際使用的巢狀授權。`@wailsio/runtime` npm 沒附法律檔，使用相同固定版本的 Wails Go 模組原文；`@vue/devtools-api` 6.6.4 使用已核對的上游版本，來源及提交見 [third_party/README.md](../third_party/README.md)。缺少其他依賴授權會停止建置，不用套件 metadata 的簡短標籤代替原文，也不因合併而刪減 NOTICE／PATENTS。
