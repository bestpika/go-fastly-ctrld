# 首版維護交接

本文件只保存跨工作階段必須遵守的決策與安全限制，不複製完整規格、測試數量或驗收 TODO。首版功能已完成實作，但完整交付驗收尚未全部完成；不可將文件中的「首版」解讀為全平台或真實帳號已驗收。

## 文件分工與工作規則

- 專案 `go-fastly-ctrld`，遠端 `git@github.com:bestpika/go-fastly-ctrld.git`，主要分支 `master`。
- 開發前閱讀根目錄 [README](../README.md)／[SPEC](../SPEC.md)／[CHANGELOG](../CHANGELOG.md) 與本文件、[DESIGN](DESIGN.md)／[API_SPEC](API_SPEC.md)／[PLAN](PLAN.md)；檢查前另讀 [TESTING](TESTING.md)。
- SPEC 維護功能、預設值與驗收條件；DESIGN 維護內部架構與介面；API_SPEC 維護傳輸與外部契約；TESTING 保存證據與限制；PLAN 是首版交付前未完成驗收的唯一 TODO 清單。
- 使用臺灣正體中文、UTF-8 無 BOM。一般文件／前端採 CRLF，Go／Shell／固定官方樣式與法律原文保 LF，依 `.gitattributes` 與 `.editorconfig` 處理。
- 修改、打包、推送、真實帳號讀寫、外部大量測速或 Runtime 安裝／升級，須有對應明確授權，不由文件維護推定取得。
- 修改時同步相關文件，同版本／日期分類整理 CHANGELOG，移除過時交接。完成驗收先在 TESTING 留下證據，再移除 PLAN 對應 TODO，不把未驗收項目改列為選做維護。
- 修改後執行適用格式／靜態檢查；文件專用檢查見 TESTING，不宣稱未執行的程式測試已重測。採約定式提交，訊息用臺灣正體中文且不含版號；程式碼先提交、文件分開提交，純文件修改只建立文件提交，不自動推送。
- `fastly-hosts-adjust/` 是 Git 忽略的使用者參考原碼，不修改、刪除或提交；固定參考版本與授權見 [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES.md)。

## 工具鏈、版本與產物

- Windows 使用 PowerShell 7；Go 最低 1.25.0，設定 `GOTOOLCHAIN=local`，不得自動下載工具鏈。支援平台與系統依賴見 README；缺少環境只回報，不自行補裝。
- Node.js 最低 22.13.0、Bun 固定 1.4.2，唯一鎖檔為 `frontend/bun.lock`。使用 `scripts/frontend.mjs` 集中入口與 frozen／ignore-scripts／hoisted 安裝，不另加 npm 鎖。
- Bun 負責安裝與啟動腳本，Node 執行 vue-tsc、法律與發布工具。不得以 `--bun` 強制改由 Bun 執行這些工具，不加萬用 `.vue` 宣告、不改第三方套件或省略檢查掩蓋錯誤。根工具以 Node 直接呼叫本地 CLI。
- Wails 固定 `v3.0.0-beta.28`，CLI／Go 模組同版；介面變更以 CLI 產生 TypeScript 綁定，不手寫傳輸型別。核心策略與 preset 版本以 SPEC／API_SPEC 為準；依賴固定於 manifest／鎖檔，不追蹤 latest。
- 正式版號只以已提交的 `VERSION` 為準，首版定位不改版本格式。僅明確要求才執行 `node scripts/release.mjs --bump`；按臺灣日同日加一、新日首次為一，日期倒退拒絕。
- 打包不升版；各次產物存 `dist/builds/{識別碼}/`，由來源提交與 SHA-256 辨識。版本與打包共用鎖，有未決交易先恢復，不刪紀錄或搶鎖。
- 備份 Git common 目錄下的 `fastly-releases`；跨 clone／主機同步 VERSION 與紀錄、避免併發。正式打包只接受乾淨提交，不自動 tag／push。
- 套件只含執行檔、使用者 README、LICENSE 與完整合併授權四檔，SHA-256 外置。使用者 README 由 `scripts/licenses.mjs` 產生，不複製開發 README；保留三組十二域、三下載域與 X 短圖限制等說明。
- 本案採 MIT，第三方法律原文不翻譯刪改。法律逐實體路徑收集，只有全部資料與法律內容相同的副本可合併；缺漏或差異停止建置。效能文件是歷史量測，不能推論現行完整打包加速比例。

## 不可放寬的產品與量測邊界

- 不改 hosts／系統 DNS、不綁裝置 Profile、不要求安裝 ctrld；沒有資料庫或自建 HTTP WebAPI，不新增無用途的 DB_SPEC／OpenAPI。
- Fastly 範圍只取官方清單，Google＋Cloudflare 同輪並行作獨立 DNS 證據；系統解析與現用規則只供基準，不代替 CDN 判定。不造硬編碼備援或 IPv6 位址。
- 總覽是唯一偵測／測速入口。空管理清單且無權杖仍可測基準；基準不自動加入清單、不取得套用資格。十二域各自驗 Host／SNI／TLS／POP，三域另下載，其餘不以他域 Mbps 冒充自身成績；X 指定圖片不代表持續頻寬。
- IPv6 與共享參考預設關閉且須明確選用，共享自動套用另需授權；同域自訂資源優先。測速、DNS、範圍、共享參考與套用／復原預覽不以年齡判資格；TTL 只供刷新，實際身分、策略、設定、網路、範圍與 TLS 驗證仍有效。
- 完整遍歷官方 IPv4 各 /24，資格合格後才選候選，精確現用基準不得被去重省略。必要共享來源先完成，管理域與其餘基準交錯；跨域資格共用全作業上限，主要下載串行。
- 只重用同作業中符合條件的同域量測，來源明確且流量不重計；游標用實際管理處理數，自動只使用本作業完整管理結果，不借舊快取補齊。
- 下載只接受非空 2xx，不跟轉址、不設最小檔案大小。資源探索失敗清下載評分退自身延遲，取消／期限／額度不足仍為未完成；不減三輪或放寬已存非零上限。評分與固定參數以 SPEC 為準。
- 設定正規化不能隱含擴大 IPv6／共享授權或放寬已存上限；必要時旋轉修訂、停自動清授權，不改規則與復原 journal。策略不符的結果須重測；格式細節見 API_SPEC。

## 寫入、復原與資料安全

- 權杖只由 Go 存 OS 金鑰庫，無明文 getter、不退回明文；自動套用、登入啟動與強制寫入預設關閉。設定／Profile／憑證變更使相關預覽與授權失效，寫入／未決期間不得中途換帳號。
- 不先刪規則再測。前端套用只送不可變更預覽 ID；每項寫前核目的資料夾、全 Profile 原值與摘要，先持久保存 origin／journal，再寫入、回讀。讀取失敗不當成空清單，未知或不可安全還原的規則不覆寫。
- 所有新選定 IPv4／IPv6 均在寫前以精確 IP、自身 Host／SNI 重驗正常 TLS／HTTPS，再核取消、網路、設定、結果與範圍；失敗零寫入，保留 CNAME 不當成 IP。
- 強制模式有改善仍用最佳 IP，完整無改善才用雙來源已驗原始 CNAME／IP，不借雲端覆寫或掃描 IP。手動仍確認，自動仍需完整授權；只略過冷卻，`testOnly` 永不寫，不放行基準或未完成結果。
- 同值重寫須持久化 `writeAccepted` 與一致後像；缺接受證據為 `unconfirmed`，不盲重送或提供成功復原。逾時先回讀，不能假設沒有寫入；復原只用該次已確認快照，衝突保現況，不刪整夾或無關規則。
- Control D 沒有文件化的整批交易／條件式寫入，回讀無法排除跨客戶端競爭。`via_v6`／comment 的適用性、缺值與未知欄位依 API_SPEC 保守處理，不自行補空或意外清除 IPv6。
- 自動前置標記、游標或冷卻保存失敗即持久暫停，重開不解除；重新授權前以同帳號 verified journal 重建冷卻，過時完成事件不能解除新故障。
- 快取清理只動可辨識的 Fastly／DNS 刷新快取，不按年齡刪 results／references／previews。結果、錯誤或紀錄列表刪除不碰 origin／journal／checkpoint，未知格式保留，不以空資料覆寫損毀檔。
- 執行紀錄刪除只保存 `data/history-hidden.json` 列表索引，原始安全底帳保留；先保存再更新快照／事件，保存失敗不改列表。保護活動槽、待核對與未知狀態，索引不能隱藏受保護作業；詳細狀態契約見 API_SPEC。
- 錯誤最多 200 筆並遮敏，不存權杖、raw body、Profile ID 或簽章 query；通知與詳細視窗以 errorId 關聯。

## 介面、桌面與驗收交接

- 直接用 Sakai v5 官方布局及固定原文樣式，不以 Toolbar／Menu／Drawer 自組替代；業務頁用 PrimeVue 官方控制項，不重寫外觀。來源與必要適配見 `frontend/src/layout/sakai-source.json`，原始授權保留。
- 路由、響應式選單、焦點、Checkbox 標籤與 Accordion 的鍵盤／ARIA 契約見 DESIGN。局部讀取須真正取消，舊回應不覆新選擇；快照 singleflight 不放寬 Apply／Recover 互斥。
- 系統字型只由前端在官方 Select 開啟時呼叫 `queryLocalFonts()`，不加 Go 列舉方法、不讀字型檔或下載；偏好獨立保存，不改 Go 設定修訂或自動授權，權限／支援失敗保留現值。
- 開始／停止共用純圖示按鈕，取消須等 Go 終態；跨頁作業面板預設收合，同次執行保留選擇，重啟回收合。事件遺失靠快照恢復；自動啟用不是作業執行中，零 Mbps 不等於未量下載。
- × hook 只讀 atomic tray 狀態，不等 Core 鎖或移動視窗；有 tray 隱藏，否則工作列最小化。Windows 核自身註冊而非座標，每五秒確認、連續三次失敗退工作列；還原先 UnMinimise／Show／Focus，真正 Quit 才安全等待。
- Windows 圖示由 `internal/appicon` 同源生成，正式腳本嵌入 PE 圖示與使用者權限 manifest，只清當次暫存；一般 `go build` 不自帶 PE 圖示。
- 首版交付前的原生、真實 API、最低工具鏈、race 與現行套件驗收依 PLAN 執行；Windows 隔離證據不代替人工桌面驗收。Wails Beta `Chrome_WidgetWin_0 Error 1412` 清理警告仍未結案；不以退出碼 0 或模擬通過宣告解決。
