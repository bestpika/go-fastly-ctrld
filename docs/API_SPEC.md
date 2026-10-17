# 首版介面與外部 API 契約

## 1. 適用範圍

本文件記錄首版已實作的 Wails 方法／事件、資料傳輸格式與 Fastly、Control D adapter 契約，供前後端與外部串接維護使用。原生 Bridge 僅公開白名單，TypeScript 綁定由 Go 型別產生，不將 Core 或金鑰庫直接公開。

本文件中的外部契約描述實作預期，不代表廠商實際行為已全數驗證。既有證據與限制見 [TESTING.md](TESTING.md)，原生與真實 API 的首版交付驗收待辦保留於 [PLAN.md](PLAN.md)。未知、缺少必要欄位或不可安全復原的既有規則，仍維持阻擋覆寫。

本工具不提供 HTTP WebAPI，因此不建立自身 OpenAPI 文件。Control D 契約以[使用者提供的 OpenAPI](https://bestpika.github.io/ctrld-openapi-doc/openapi.yaml)與官方文件核對；未經真實帳號驗證的行為不得視為已確認。

「關於與開源授權」使用靜態內嵌資料，來源只開系統瀏覽器。應用介面包含 41 個白名單方法、34 個模型；沒有權杖 getter，不綁整個 Core。

### 實作對照與維護方式

- `internal/desktop/bridge.go`：對外方法白名單與統一錯誤回應。
- `internal/domain/models.go`、`internal/settings/`：領域資料、設定與修訂的 Go 定義。
- `frontend/bindings/`：固定版本 Wails CLI 產生的 TypeScript 綁定；變更 Go 介面時重新產生，不手寫傳輸型別。只整理文件不重新生成綁定。

功能條件與預設值以 [SPEC.md](../SPEC.md) 為準，流程與內部保存方式見 [DESIGN.md](DESIGN.md)。Sakai Vue 5.0.0 只負責布局；版型與打包工具不取得額外的應用方法、模型或外部 API 權限。

## 2. Wails 共通契約

正式 `Snapshot.version` 與 About 靜態 `build.version` 都讀取同一已提交 `VERSION`，格式為 `YYYY.天序.當日升版序號`。僅明確要求升版時，按當下 UTC+8 日期同日加一、新日首次為一，天序不補零；重打包、換提交與跨日不改版。每次另記來源提交與 SHA-256，不能只用版號判斷內容相同。開發建置為 development；打包紀錄不新增 Bridge／遠端 API 或帳號權限。隔離啟動報告額外輸出 version，核對實際注入值。

Go 對外傳輸物件採 camelCase，時間採 UTC RFC 3339，ID／修訂／摘要為字串，下載量以 bytes 表示。Go 定義型別後產生 TypeScript 綁定，不以兩份手寫型別維護。

所有方法以共通 `Result<T>` 回應：

| 欄位    | 規則                                                                                                                            |
| ------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `ok`    | 是否完成該次方法呼叫，不能等同背景作業成功                                                                                      |
| `data`  | 成功時的資料；失敗是對應 Go 型別的零值，不應使用                                                                                |
| `error` | 成功為 null；失敗有 code／message／retryable／errorId，可另有 source／operation／HTTPStatus／vendorCode／requestId／遮敏 detail |

錯誤訊息不可含權杖、Authorization、未遮蔽的 URL 查詢參數或原始回應。背景方法成功只回傳 `jobId`；結果需讀作業快照。參數驗證、狀態／授權確認與安全限制全部由 Go 執行。

Profile／資料夾／規則、測試帳號讀取連線、差異及復原預覽的 Bridge 方法接收 Wails 注入的 context，不增加 JavaScript 參數。前端 CancellablePromise.cancel 傳到 Go／HTTP，讀取仍有三十秒上限；使用者取消回 `CANCELLED`，不重試也不寫錯誤紀錄。真正寫入／復原的安全項目邊界不變。列表只取所選資料夾，差異預覽與寫前仍核對完整 Profile 的原值與移動。

設定選擇可使用已成功讀取且同權杖修訂／Profile 的清單快取驗證識別碼，避免選夾又跑長讀取；這不是遠端寫入授權，預覽及寫入仍即時核對。唯讀操作不增加全域 busy，也不逐次重讀全應用快照；快照 singleflight 合併重疊請求，寫入後等待舊請求結束再取新版本。

### 對外方法

| 方法                      | 輸入                                                            | 輸出／語意                                                                                          |
| ------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `GetAppSnapshot`          | 無                                                              | 設定、網路、權杖保存狀態、Profile、自動狀態、活動作業與待復原摘要                                   |
| `UpdateSettings`          | `expectedRevision`、主題／Probe、選用 `forceWrite`              | 更新成功與否；新修訂由快照取得，拒絕過期修訂                                                        |
| `SetControlDToken`        | 使用者輸入的權杖                                                | 保存狀態；使舊預覽／自動授權失效，不回傳權杖                                                        |
| `DeleteControlDToken`     | 無，介面先確認                                                  | 刪除保存值、暫停自動寫入；保存狀態由快照取得                                                        |
| `GetCredentialStatus`     | 無                                                              | `saved`／`available`／`message`，不含明文；讀取連線另行測試                                         |
| `CheckControlDConnection` | 無                                                              | 讀取完成的成功與否；只以讀取 API 驗證，不假寫入                                                     |
| `ListProfiles`            | 無                                                              | 有界限讀取的 Profile 清單，不替裝置更換 Profile                                                     |
| `SelectProfile`           | `profileId`、`expectedRevision`                                 | 確認可讀取並保存選擇，使舊預覽／自動授權失效                                                        |
| `SelectImportFile`        | 無                                                              | 原生 TXT／CSV 選擇結果與短效 `fileSelectionId`，不接受任意檔案路徑讀取                              |
| `PreviewDomainImport`     | 文字或 `fileSelectionId`、格式                                  | `importId`、有效／重複／錯誤列，尚未變更清單                                                        |
| `ConfirmDomainImport`     | `importId`                                                      | 匯入預覽內有效列，成功後讀快照；預覽綁定原修訂                                                      |
| `UpdateDomain`            | 修訂、`domainId`、啟用與測速 URL                                | 成功與否；更新後從快照取得清單                                                                      |
| `RemoveDomain`            | `domainId`、修訂                                                | 只移除本機管理項目，不刪雲端規則                                                                    |
| `StartDetection`          | 已存在的 `domainIds`                                            | `jobId`；更新範圍、DNS 證據與判定                                                                   |
| `StartTest`               | `domainIds`、預算 bytes：0 不限、-1 用保存設定、其餘為明確上限  | `jobId`；位址族由 Go 的 IPv6 可用性決定，不可強迫掃描不可用 IPv6                                    |
| `GetJob`                  | `jobId`                                                         | 完整作業、逐項狀態、目前進度與錯誤                                                                  |
| `ListJobs`                | 無                                                              | 依時間排序的可見歷史；排除已移除列表項目、權杖與原始 API JSON                                       |
| `DeleteJobs`              | jobId；空值全部，介面先確認                                     | `JobHistoryDeletion`；只持久移除可刪終態的列表項目，保留安全底帳與受保護作業                        |
| `CancelJob`               | `jobId`                                                         | 已接受取消／不可立即停止；寫入只在安全項目邊界停止                                                  |
| `BuildSyncPreview`        | 選取 `domainIds`                                                | Go 關聯最新有效結果，產生不可變更差異，不寫入 Control D                                             |
| `ApplySyncPreview`        | `previewId`，介面先確認                                         | `jobId`；Go 重驗全部前置條件，UI 不能傳任意 IP 規則代替預覽                                         |
| `BuildRestorePreview`     | 歷史作業／快照 ID                                               | 可復原項目、前後值、限制與衝突                                                                      |
| `ApplyRestorePreview`     | 復原預覽 ID，介面先確認                                         | `jobId`；同樣先 journal、再寫入、再回讀                                                             |
| `ConfigureAutomation`     | 修訂、Profile、網域與覆寫／移動授權、排程／門檻／預算、啟用確認 | 保存授權快照；首次啟用須列出既有規則影響，不接受只送 `enabled=true` 的未授權請求                    |
| `PauseAutomation`         | 無                                                              | 停止新觸發、取消純測試，寫入在安全項目邊界停止，不刪規則                                            |
| `ResumeAutomation`        | 無，使用保存的完整授權                                          | 重驗 Profile、修訂與授權後恢復，不能沿用已失效授權                                                  |
| `SetLaunchAtLogin`        | `enabled`                                                       | 使用者層級 OS 設定結果；OS 失敗不能回報成功                                                         |
| `RecoverJob`              | `jobId`                                                         | 只回讀核對未決項目，不重送寫入；帳號必須符合原紀錄                                                  |
| `CleanExpiredCaches`      | 無，介面先確認                                                  | 僅清可辨識且待刷新的 Fastly／DNS 快取；保留結果、共享參考、預覽、快照與 journal                     |
| `ReportUIReady`           | 無                                                              | 前端確認快照 IPC 已完成，宿主再核對系統匣與背景啟動                                                 |
| `QuitApplication`         | 無，介面先確認                                                  | 接受安全退出要求，不代表立即停止正在回讀的作業                                                      |
| `ExportDiagnostics`       | 無，原生另選新檔案                                              | 匯出版本、網路摘要與作業狀態；不含權杖、規則原始 JSON、Profile 名稱或檔案路徑；取消返回 `CANCELLED` |
| `ListFolders`             | 無                                                              | 目前 Profile 的資料夾，action 只顯示不可選                                                          |
| `SelectFolder`            | `expectedRevision`、整數 folderId                               | 僅 root／一般資料夾；失效預覽／自動授權                                                             |
| `CreateFolder`            | 修訂、name                                                      | 明確帳號寫操作；只建一般資料夾、回讀確認，不偽造共用動作                                            |
| `ReadRuleList`            | 無                                                              | 僅讀目前所選資料夾，回傳去 Raw 的清單；切換 scope／取消則不採用，失敗不當空                         |
| `ResolveDomainImport`     | importId、line、replace                                         | 私有衝突逐筆取代或略過；未解衝突禁止 Confirm                                                        |
| `DeleteTestResults`       | resultId；空值全部                                              | busy／recovery 拒絕，typed 刪除及預覽失效，不碰復原／雲端                                           |
| `ListErrors`              | 無                                                              | 持久遮敏錯誤，最新優先；不含原 HTTP body                                                            |
| `DeleteError`             | errorId；空值全部                                               | 單筆／清空記錄，不解除 recovery 或安全暫停                                                          |
| `ReportClientError`       | 固定已知 code                                                   | 記錄前端 IPC／複製／瀏覽器等安全摘要，不接受任意 raw 文字                                           |

手動綁定不能用任意 `automatic` 旗標繞過啟用條件；自動作業由 Go 排程建立。所有清單操作對穩定 ID 執行，服務端確認 ID 與目前 Profile／作業關聯。

`UpdateSettings` 不接受權杖、Profile、自動管理授權或登入時啟動等需專用方法處理的欄位。`UpdateDomain` 不能單靠新增布林欄位取得自動覆寫授權；授權由 `ConfigureAutomation` 完整核對保存。

`Settings.forceWrite` 預設 false；`Patch.forceWrite` 為選用布林，省略或 null 保留，避免主題／進階設定誤清開關。值變更時旋轉修訂、停自動並清除授權，舊結果／預覽失效；不自動啟用套用。TypeScript 綁定由固定 Wails CLI 產生。

字型偏好不屬 Wails 設定：前端只在官方 Select 開啟時呼叫 `window.queryLocalFonts()`，依 family 去重；需要 WebView 支援與權限，不讀字型檔案、不新增 Go 列舉方法。選擇獨立保存在本機，不改 Settings.Revision 或自動授權；失敗保留現值並提示原因。

`DeleteJobs` 只接受現存作業 ID，不把輸入當路徑。可刪狀態限 completed／partial／failed／cancelled，且非活動 testID／writerID、無 write_started／unknown 項目；其他狀態含 needs_recovery 受保護。單刪已移除／不存在回 `NOT_FOUND`，保護中或安全停止回 `CONFLICT`。空 ID 可在其他作業與 recovery 期間清除可刪紀錄，回 deleted／retained，空列表成功回零；不因有受保護作業而整批拒絕。版本化索引保存失敗回 `STORAGE_ERROR` 且不改列表／發成功事件，未知索引不覆寫。Snapshot.jobs 與 ListJobs 同步過濾，GetJob、復原與自動冷卻仍可讀原始安全底帳；不改規則、測速結果、錯誤紀錄或安全標記。

更換／刪除權杖或選擇 Profile 時，若已有遠端寫入，先停止於安全項目邊界並核對未決操作。進行中的作業綁定原帳號與 Profile，不允許中途切換後繼續。

### 主要傳輸物件

| 物件                        | 必要內容                                                                                                                                                                                                                                                          |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Snapshot`                  | 設定、網域、網路、工作與 `automationError`；安全標記阻擋時 `settings.automation.enabled` 回報實際 false，不公開原始帳號紀錄                                                                                                                                       |
| `DetectionResult`           | `domainId`、`hostname`、`classification`、`observedAt`、`expiresAt`、`sources`、CNAME／A／AAAA、CIDR 證據、警告                                                                                                                                                   |
| `TestResult`                | `id`、`strategyVersion`、`dnsClassification`、`service`、`mode`、`probeHost`、`speedHost`、`scanCount`、`networkFingerprint`、`settingsRevision`、`expiresAt`、基準／候選、bytes、評分／略過原因、`reference` 可 null、`referenceReused`、同作業來源 `reusedFrom` |
| `CandidateResult`／`Sample` | 只表示自身樣本的 `smoothRounds`、`smoothThresholdMbps`、TCP 中位與成功數；樣本的 `httpStatus`、`stage`、`hostname`、`actualIp`、bytes／速度／錯誤、TLS／TTFB 與包含連線的 `elapsedMilliseconds`；共享分數另用 `referenceScores`                                   |
| `SharedReference`           | ID／摘要／策略、網路與範圍／probe 摘要、時間／期限、原來源 body 成本；完整 cache 保存來源樣本，目標結果只帶來源 hostname 摘要，路線分數另用 `referenceScores`                                                                                                     |
| `ReferenceScore`            | `source`、`route`、`actualIp`、`speedHost`、`smoothRounds`、`thresholdMbps`、`medianMbps`、`successes`、`rounds`，不是目標下載成績                                                                                                                                |
| `SyncPreview`               | `id`、`profileId`、`credentialRevision`、`settingsRevision`、`expiresAt`、`networkFingerprint`、逐項摘要與影響                                                                                                                                                    |
| `PreviewItem`               | `id`、hostname、操作、可顯示前後值、規則摘要、資料夾移動、子網域影響、是否可套用／原因、`forceWrite`、可 null 的 `originalDns`                                                                                                                                    |
| `Job`                       | `id`、種類、狀態、觸發來源、開始／結束時間、逐項狀態、已處理／總數、`managedCompleted`／`baselinesCompleted`、`phase`、候選輪次進度、bytes／上限、`estimatedBytes`／`estimatedSeconds`、取消狀態、待復原                                                          |
| `JobHistoryDeletion`        | `deleted`／`retained` 整數，分別為本次移除與受保護而保留的可見紀錄數                                                                                                                                                                                              |
| `CredentialStatus`          | 保存／可讀取／驗證狀態，沒有權杖、權杖片段或金鑰庫內容                                                                                                                                                                                                            |
| `ErrorRecord`               | id／occurredAt／source／operation／code／message／detail／httpStatus／vendorCode／requestId／jobId／count，最多 200 筆                                                                                                                                            |

`JournalItem.writeAccepted` 表示 API 明確成功的持久證據，缺值視為 false。同值操作 `rewrite` 必須接受證據與回讀一致才 verified；缺證據且同值為 `unconfirmed`，不是待重送或可復原成功。RecoverJob 只回讀、不重送，不能從相同原值推論曾寫成功。

### 事件

事件只傳可顯示的狀態摘要，不傳權杖或完整原始規則。共同欄位為 `eventId`、遞增 `sequence`、`occurredAt`、可為 null 的清理後 `job`；其他資料由 Go 快照讀回。

| 事件                   | 用途                                                                  |
| ---------------------- | --------------------------------------------------------------------- |
| `job:updated`          | 狀態、已完成／總數、下載 bytes 與逐項更新                             |
| `jobs:changed`         | 執行紀錄列表索引已保存，前端重讀快照以同步列表與最近作業面板          |
| `network:changed`      | 網路指紋、IPv6 與結果失效提示                                         |
| `automation:changed`   | 啟用／暫停、下次排程與原因                                            |
| `settings:changed`     | 新修訂，要求 UI 重讀設定                                              |
| `recovery:required`    | 未決遠端操作、關聯作業與復原入口                                      |
| `navigation:requested` | 系統匣要求前端前往測速結果，目前只接受固定 `/tests`                   |
| `error:recorded`       | error 指向同一 ErrorRecord，右下通知可用 ID 深連結定位／開詳細 Dialog |

重新開啟視窗、事件序號缺口或前端重新載入時，以 `GetAppSnapshot`／`GetJob` 重建；不以事件完整送達作為正確性的前提。實際 Wails 事件 API 依固定 v3 版本接入，不套用 v2 寫法。

`automationError` 非空時顯示持久安全暫停原因。`ConfigureAutomation`／`ResumeAutomation` 重建同帳號已驗證歷史、核對完整授權並成功保存後，才能解除；不以一般快照刷新或過時完成事件解除故障。

### 設定與量測契約

策略版本為 10，版本不符的結果不能套用。`PresetVersion=2` 固定候選十組／三輪／10 秒，不接受隱藏欄位調參；scanMode／jobSeconds／budget 可設定。動態 URL 簽章只留 Go 記憶體，白名單方法不接受任意規則寫入。

設定載入先驗證內容，再依以下條件處理，不改既有規則或復原 journal：

- `PresetVersion > 2`：回 `SCHEMA_UNSUPPORTED` 並保留原檔，不降版覆寫。
- `PresetVersion < 2`：將無資源模式設為 latency、關閉 IPv6、選取三組基準並關閉共享自動授權；保留 scanMode 及已保存的非零流量／總期限。
- preset、固定 Probe 參數或穩定等待／冷卻不符：正規化為 preset 2、固定參數及 30／900 秒，旋轉設定修訂、停用自動並清除 grants。保存失敗回 `STORAGE_ERROR`，不啟動自動作業。
- 符合上述格式的明確選項持久保存，設定讀取不能隱含擴大 IPv6 或共享寫入授權。

`ProbeSettings.noResourceMode` 為 latency／shared，預設 latency；shared 只適用無自訂檔且非內建下載的管理域，三個下載域與同域自訂檔優先。參考不足回自身延遲，allowSharedReference 仍另需明確授權；目的 folderId 亦綁入自動 scope。量測結果模式為 download／connection／shared-reference；github-pop-reference 僅供讀取展示，須重測才能套用。參考不冒填自身 Mbps，共享預覽只接受私有證據。

`ProbeSettings.services` 可選 github／twitch／x，不得重複或未知；缺值預設三項，空陣列代表全關。選服務即展開 GitHub 2／Twitch 6／X 4 個網域，共 12 域各自探自己的 Host／TLS。jtvnw.twitchcdn.net 預設僅自身連線，依自身 DNS 尾碼探索，不猜其他 Twitch 域的尾碼。X 不依賴 GitHub；只有 shared 管理域補 GitHub／Twitch 兩個下載來源，不強制補其餘連線域。`enableIpv6` 預設 false，且網路可用才測；最後預覽／寫入亦核選用狀態，不因關閉而清既有 via_v6。欄位透過 `UpdateSettings` 保存，綁定由固定 Wails CLI 生成。

`dnsClassification` 記錄目標測試當時的獨立 DNS 判定。手動混合 CDN 例外只適用測試當時已為 mixed、套用前仍有完整證據的情況，不把缺少共享自動授權誤當成原先 DNS 不一致。

`TestResult.baselineService`／`complete`／`rejected` 及 `CandidateResult.pop` 區分基準與淘汰。StartTest 先完成必要共享來源，其餘從管理域起交錯基準；每批最多四域並行資格探測，共用作業上限，下載串行。空管理清單仍可測所選基準，不需 token，作業總數依實際網域計算，預設 12 項。基準不能套用，不自動加入管理清單或規則；未完成不能產生有效預覽，Rejected 不占候選。只有 release-assets.githubusercontent.com／assets.twitch.tv／pbs.twimg.com 另做下載，其餘九域預設 connection。X 使用 https://pbs.twimg.com/media/HU2En5YbAAAByoT?format=png&name=900x900，不借 GitHub；`probeHost` 是各域自身，`speedHost` 只在下載時填實際資源域。POP 僅取自身回應，不可辨識為 `?`，UI 顯示未知。自訂檔優先。

`reusedFrom` 只指同作業、同域、無自訂檔／獨立現用規則／共享目標的量測來源，副本有獨立結果 ID、bytes 為零，不跨作業代測。來源或 DNS 不因時間經過完整重測，仍核設定／網路與完成狀態。`managedCompleted` 記實際管理數，不能扣固定基準數；自動套用只使用該作業完整管理結果。

一般 connection 結果使用資格階段一次 HTTPS 樣本與 TCP 中位數，不冒充三輪下載。有效下載只接受非空 2xx 本文，3xx 不跟轉址且不算成功，不設最低大小；`elapsedMilliseconds` 包含連線、TLS、首位元組與本文，Mbps 仍只計本文。下載任一輪資源取得失敗時丟棄全部下載成績、保留 bytes 記帳，退 connection；取消、期限或預算不足仍 complete=false 並清建議。每輪固定候選順序，官方 IPv4 完整遍歷各 /24，不限池取樣。

未完成原因分開記錄取消、作業時間上限、DNS 失敗／分類改變、網段資料無效、無合格候選、流量或樣本不足；資料年齡不是原因。partial 引導查看網域詳情，不一律歸因預算。基準與未完成測試用不同短句，細節留結果；forceWrite 不能繞過未完成，略過不附「僅可手動」，After 空值表示保留原值，只有復原新增規則的 delete 才顯示刪除。

所有新選定 IPv4／IPv6 在套用前再次以自身 Host／SNI 對精確 IP 驗 TLS／HTTPS，不只 shared-reference。驗完重核取消、網路、設定、結果與官方範圍內容，實際失敗零寫入；原始 DNS 回填仍走完整原鏈驗證，不把保留的 CNAME 當作 IP。

`Automation.testOnly` 無帳號可測且 forceWrite 開啟仍不寫；套用模式仍核 Profile／folder／權杖／grants。等待與冷卻固定 30／900 秒；forceWrite 僅略過冷卻，不略過排程或授權。SharedReference 不設年齡門檻，但仍綁本輪來源與摘要，不跨作業代測。

測速、DNS、網段、共享參考與 apply／restore 預覽的 `expiresAt` 等欄位保留格式相容，僅供刷新或歷史查閱，不作套用資格；不得以時間清理仍可套用的結果、參考或預覽。`PREVIEW_EXPIRED` 仍可表示預覽不存在、種類／修訂不符或原生選檔權限到期，不因套用資料的時間經過回此錯誤。取消、HTTP／I/O 逾時、作業期限、排程與冷卻維持。

`ProbeSettings.scanMode` 為 near／full，缺值正規化 near。`jobSeconds` 0 不限或 30 至 86400 秒；`Automation.budgetBytes` 0 不限或 1 MB 至 100 GB。兩者預設 0，已保存的非零限制及授權不自動變更。`sampleSeconds` 表示各次 socket I/O，不是整 request；DoH／Control D／寫入回讀的 HTTP 逾時與取消維持。

`estimatedBytes`／`estimatedSeconds` 是未假定同域去重的保守本文／逾時工作量提示，不含協定開銷、不保證完成時間。前端開始／停止共用純圖示；啟動 ID 回傳即可取消，取消等待 Go 終態。跨頁進度與完成／部分／失敗／取消通知以 Go 快照為準，讀取操作只轉自己的圖示，不用全域 loading 鎖住停止。

### 錯誤分類

至少區分 `INVALID_INPUT`、`NOT_FOUND`、`DEPENDENCY_MISSING`、`KEYRING_UNAVAILABLE`、`NO_VALID_RANGES`、`UNSAFE_TARGET`、`NETWORK_CHANGED`、`BUDGET_EXHAUSTED`、`AUTH_FAILED`、`PERMISSION_DENIED`、`RATE_LIMITED`、`PREVIEW_EXPIRED`、`CONFLICT`、`SNAPSHOT_UNSUPPORTED`、`SCHEMA_UNSUPPORTED`、`STORAGE_ERROR`、`CANCELLED`、`STATE_RECOVERY_REQUIRED`。

可重試不代表前端可直接重送寫入。是否重試由 Go 根據遠端回讀與 journal 決定。

原生系統匣監測另區分 `TRAY_UNCONFIRMED` 與 `TRAY_UNAVAILABLE`：連續三次未確認與連續三次確認未註冊，不把座標查詢失敗當不可用；同一中斷只記一次，恢復後才重新計次。`DesktopStatus.trayAvailable` 是關閉 hook 可採用的快取狀態，`trayMessage` 顯示重新確認或工作列退路；監測使用這些狀態欄位，不提供獨立 Bridge 方法。原生隔離報告另有 `trayChecks`／`trayMonitoringPassed`，不是 Bridge API。

## 3. Fastly API adapter

- 方法：`GET https://api.fastly.com/public-ip-list`，不需要 Control D 權杖。
- JSON 中 `addresses` 是 IPv4 CIDR 陣列，`ipv6_addresses` 是 IPv6 CIDR 陣列。
- 驗證 HTTP、JSON 型別、位址族、CIDR、回應大小與內容合理性；去重後保存取得時間／摘要。
- 使用有界限的 HTTPS client；不允許跳往任意非官方下載來源。
- 無效回應不覆蓋有效快取，沒有有效快取不得造假備援清單。
- 24 小時為刷新間隔；刷新失敗可沿用 CIDR／摘要合法快取，不偽造時間，取消與保存失敗仍回錯誤。無合法清單不得造備援，詳見 [SPEC.md](../SPEC.md)。

## 4. 獨立 DNS 介面

CDN 判定固定使用 Google `https://dns.google/dns-query` 與 Cloudflare `https://cloudflare-dns.com/dns-query`，由 Go 以 RFC 8484 `application/dns-message` 同輪並行查詢 CNAME／A／AAAA。不是可由前端任意替換的 URL，也不共用 Control D 權杖或允許環境代理的 client。

每方保留來源、紀錄、時間與 TTL，檢查 DNS 回應識別碼、Question 與錯誤碼，回應大小有上限。兩方均肯定 Fastly 歸屬才可自動評估，不要求 IP 相同；候選取聯集後逐項驗證。單方失敗或歸屬差異不自動套用，系統 DNS 只作現用基準。

長資格探測後，Finish 在取得下載槽後或整理連線結果前實查雙 DoH；量測後依 TTL 需要再刷新。重新確認目標、雙來源成功與分類未變，IP 可正常輪替，既有 mixed 手動例外不擴權。保存最後實取 DNS，不偽造時間；零 TTL 或經過時間不拒絕，實際查詢失敗仍保診斷／流量、清建議並保持未完成。

forceWrite 在完整結果無改善時才回填：綁定雙來源 confirmed 證據，優先管理 hostname 的一致第一跳 CNAME，沒有 CNAME 才選原始 IPv4；IPv6 仍需選用且可用。驗鏈尾／官方公開位址、八跳上限與循環／跨規則涵蓋，不以 TTL 阻擋；預覽與寫前正常驗證原 hostname 的 TLS／HTTPS。套用時重查並精確比對 After，不接受雲端覆寫或掃描 IP 冒充原始來源；有改善仍走最佳 IP。

## 5. Control D API adapter

### 基本協定

- Base URL：`https://api.controld.com`。
- 認證：`Authorization: Bearer <token>`，只從 Go 金鑰庫取得。
- 本案單帳號、單次選定 Profile；不實作組織標頭或多帳號管理。
- Read 權杖可讀，寫入需要 Write 權杖；讀取成功不能證明寫入權限。
- 同時判斷 HTTP 狀態與 JSON `success`，HTTP 200 的 `success:false` 也是失敗。
- 回應的 `body`／`error.message`／`error.code` 由 adapter 正規化；未知格式不得當成功。

### 必要端點

| 方法   | 路徑                                       | 用途                                           |
| ------ | ------------------------------------------ | ---------------------------------------------- |
| GET    | `/profiles`                                | 列 Profile，回應的 `PK` 作為 `profile_id`      |
| GET    | `/profiles/{profile_id}/groups`            | 列資料夾及其動作，辨識繼承語意                 |
| GET    | `/profiles/{profile_id}/rules`             | 列根目錄規則；省略資料夾 ID，不呼叫 `/rules/0` |
| GET    | `/profiles/{profile_id}/rules/{folder_id}` | 列非根目錄規則，使用實際資料夾 ID              |
| POST   | `/profiles/{profile_id}/rules`             | 新增指定網域規則                               |
| PUT    | `/profiles/{profile_id}/rules`             | 修改指定網域規則                               |
| DELETE | `/profiles/{profile_id}/rules/{hostname}`  | 復原時刪除仍符合本工具後值的新增規則           |

合併 OpenAPI 使用 `rule_target` 合併 GET 的 folder ID 與 DELETE 的 hostname。實作必須按 HTTP 方法區分，不能把 folder ID 當成刪除網域。ID／hostname 安全編碼，不直接拼接未驗證輸入。

不使用刪除資料夾端點，避免連帶刪除無關規則。不為不同候選 IP 建立共用資料夾動作。

### POST／PUT 表單

Content-Type 為 `application/x-www-form-urlencoded`。

| 欄位          | 規格                                                                         |
| ------------- | ---------------------------------------------------------------------------- |
| `hostnames[]` | 使用重複同名表單欄位，不是 JSON 陣列字串；本工具每次以單網域核對／寫入       |
| `do`          | 最佳化使用 `2`；復原使用可確認的原始值                                       |
| `status`      | 最佳化使用 `1`；復原保留原狀態                                               |
| `via`         | 最佳化使用已驗最佳 IPv4／原始 CNAME；復原可還原已確認原值，包括代理識別碼    |
| `via_v6`      | 僅 do=2 使用明確字串；保留已確認的既有值，復原非 do=2 不送不適用欄位         |
| `group`       | 明確目標資料夾，根目錄為 `0`；移動必須在預覽與授權中列出                     |
| `comment`     | 最多 64 字元；不得為了工具標記覆蓋原註解。PUT 空字串會清除，保留語意須經驗證 |

`do=0` 是 BLOCK、`1` 是 BYPASS、`2` 是 SPOOF、`3` 是代理 REDIRECT。本工具不將 Fastly POP 或 IP 當成 `do=3` 的代理識別碼。

本機 journal 是所有權／歷史來源，不能只靠 comment 判斷規則屬於本工具。同 hostname 的父域／通配符規則不修改，但新增更具體規則可能改變繼承結果，需在預覽說明。

### 規則讀取、還原能力與外部驗證邊界

全 Profile 彙整允許跨資料夾相同 hostname，不以去重或擇一限制拒絕。單夾回應須驗證歸屬、型別與安全可還原欄位；既有同網域規則按原值更新及必要移動，不先刪再建。

文件化回應包含網域 `PK`、`order`、`group`、`action.do`、`action.status`、`action.via`。官方 `via_v6` 僅適用 SPOOF，因此 do=0／1／3 可省略不適用欄位；這不代表 do=2 或 comment 缺值也可自行補空，亦不保證省略寫入就能保留。

首版實作保存可讀原始 JSON，另依目前 adapter 契約建立可寫回的正規化欄位。以下行為已有模擬測試範圍，但仍須另行授權的隔離 Profile 驗證；在取得真實證據前，不將它們視為已確認的廠商保證：

- 實際 GET 的 IPv6／comment 欄位位置與完整性，包含代理 do=3 省略 via_v6 的套用與復原。
- PUT 的省略、空值與明確指定是否保留／清除 `via_v6`、comment、group。
- IPv4-only 規則在不同 Profile IPv6 Compatibility Mode 下的 A／AAAA 行為。
- 原始 CNAME 回填與同值 PUT 的明確成功及回讀一致性，不把原值相同當寫入證據。
- 動作資料夾的繼承、單項移動及其回讀。
- 新增／更新／刪除重複、逾時後的回讀與排序可還原程度。
- 權限錯誤、`success:false` 與 429 的實際格式。

不能安全讀取／還原的類型不得覆寫；未驗證欄位不自行補空值，不承諾完全還原文件未提供的資料或順序。

adapter 要求頂層明確字串 `comment`；do=2 另需動作內明確字串 `via_v6`，do=0／1／3 可缺少或為空字串。null、非字串、非 do=2 卻含非空 IPv6、未知欄位或缺少其他必要欄位均阻擋覆寫。原始 JSON 與語意摘要保留供備份及衝突核對；可寫回欄位用明確值，非 do=2 復原不送 via_v6，不盲送原始 JSON。模擬測試不取代真實契約驗證。

### 期限、重試與衝突

- 每次控制 API 請求有 10 秒硬期限；重試亦受作業期限與取消控制。
- 讀取最多重試兩次，使用指數退避與抖動；429 尊重合法 `Retry-After`，過長則延後作業，不阻塞無期限。
- 401／403 停止自動寫入，要求使用者確認；不持續盲重試。
- 寫入逾時、連線中斷或不明格式先回讀目標項目，確認狀態後才決定是否重新產生操作。
- 沒有文件化 ETag／整批交易；回讀比較是最佳努力的衝突偵測，不是服務端條件式寫入。
- 寫入後值不一致、無法回讀或復原衝突保留 journal，狀態不得假裝完全成功。

## 6. 官方核對來源

官方公開儀表板 `getRules` 的根目錄 GET 省略 ID，其餘加入數字資料夾 ID。官方文件的「0 or omit」與描述有歧義，本案採官方前端路徑，並以 HTTP mock 驗證。寫入表單 `group=0` 仍代表根目錄，不能與讀取路徑混為一談。

- [新增規則 API](https://docs.controld.com/reference/post_profiles-profile-id-rules)
- [更新規則 API](https://docs.controld.com/reference/put_profiles-profile-id-rules)
- [讀取資料夾規則 API](https://docs.controld.com/reference/get_profiles-profile-id-rules-folder-id)
- [自訂規則及子網域行為](https://docs.controld.com/docs/custom-rules)
- [資料夾規則與繼承](https://docs.controld.com/docs/folder-rules)
- [IPv6 Compatibility Mode](https://docs.controld.com/docs/ipv6-compatibility-mode)

公開文件與 HTTP mock 只能確認 adapter 的預期契約。既有驗證紀錄尚未使用真實權杖呼叫 Control D；根目錄讀取、欄位保留、IPv6 與建立一般資料夾等行為仍須完成 [PLAN.md](PLAN.md) 的隔離帳號驗收，不能以首版實作完成取代。
