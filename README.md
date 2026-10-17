# go-fastly-ctrld

使用 Go 與 Wails 開發的本機桌面工具：檢查網域是否使用 Fastly、量測目前網路的候選 IP，並將合適的 IP 寫入指定的 Control D Profile。

## 首版狀態

首版功能已完成實作，包含 Go 核心、Wails 桌面宿主與 Sakai 八頁管理介面。本專案目前以既有實作作為維護基準；**實作完成不等於完整交付驗收完成**。

| 範圍           | 狀態與依據                                                                                                                          |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| 功能與安全條件 | 首版實作基準見 [SPEC.md](SPEC.md)，架構與介面契約見 [docs/DESIGN.md](docs/DESIGN.md)／[docs/API_SPEC.md](docs/API_SPEC.md)。        |
| 已有驗證       | 自動檢查、專項回歸與 Windows 隔離原生驗證的既有紀錄見 [docs/TESTING.md](docs/TESTING.md)，各自有不同適用範圍。                      |
| 完整交付驗收   | 尚未全部完成；真實 Control D／系統金鑰庫、Linux 原生、桌面人工操作、最低工具鏈與現行套件等項目保留於 [docs/PLAN.md](docs/PLAN.md)。 |

「首版」表示本次功能基準，正式版號仍以根目錄 [VERSION](VERSION) 為唯一來源。文件整理不代表重新測試、正式打包或通過未完成的驗收。

## 功能範圍

- 以 Wails v3 Beta 提供桌面視窗與系統匣，管理介面直接使用 Sakai 官方版型、布局元件與樣式，接入八個業務頁面及 PrimeVue 官方控制項。
- 匯入網域或 HTTPS 測速網址，保留完整子網域、DNS 證據；資源衝突需確認。
- Google 與 Cloudflare DoH 同輪並行查詢 CNAME／A／AAAA，分別保存證據；不以系統 DNS 代替 CDN 判定來源。
- 從 `GET https://api.fastly.com/public-ip-list` 取得 IPv4／IPv6 範圍。
- 逐網域驗證自己的 HTTPS 與連線品質；沒有下載資源時可參考同批 Twitch＋GitHub 路線分數，不把來源 Mbps 當成目標的下載成績。
- IPv6 最佳化預設關閉，明確選用且有可用對外連線才執行。
- 支援手動預覽／套用，以及定期、網路變動觸發的自動偵測／套用。
- 依 Profile → 資料夾 → 規則讀取，選根目錄／一般資料夾或明確新增；預設只寫最佳 IP，可選無改善時回填原始 DNS，保留逐項備份／復原。
- 權杖保存在系統金鑰庫；自動套用與登入時啟動都預設關閉。
- 關閉視窗縮至系統匣；不可用時退工作列最小化，不等待作業。真正退出才安全收尾。
- 可單筆刪除／清空測速結果，錯誤通知集中右下角；點擊可定位持久錯誤紀錄與詳細內容。
- 跨頁作業狀態預設收合；執行紀錄可單筆刪除／清除全部已結束紀錄，保留進行中、待核對作業及必要安全底帳。
- 設定可選本機字型；開啟下拉選單才由前端讀取系統字型，選擇保存在本機，不影響測速或授權。

本工具不是 VPN，也不修改系統 hosts、系統 DNS 或裝置與 Profile 的綁定。不必安裝 Control D 的 `ctrld` 常駐程式；本案串接的是 Control D 帳號 API。

## 最低環境需求

### 開發與建置需求

| 項目    | 最低需求／支援基準                                                |
| ------- | ----------------------------------------------------------------- |
| Go      | 1.25.0；由開發者自行安裝或升級                                    |
| Node.js | 本專案支援基準為 22.13.0 以上，建議使用仍受維護的 LTS 版本        |
| Bun     | 固定 1.4.2，使用提交的 `frontend/bun.lock` 重現安裝               |
| Git     | 支援一般版本控制操作的版本                                        |
| Wails   | 固定為 `v3.0.0-beta.28`；CLI 與 Go 模組使用同一版本               |
| Windows | Windows 10 22H2／Windows 11，x86_64，具有 WebView2 Runtime        |
| Linux   | Ubuntu 24.04 以上，x86_64，GTK4、WebKitGTK 6.0 API 與原生建置工具 |

Linux 建置依賴包含 `build-essential`、`pkg-config`、`libgtk-4-dev`、`libwebkitgtk-6.0-dev`。不使用 GTK3 相容路徑，也不將 macOS／ARM64 列入驗收範圍。Windows 建議使用仍受安全維護的系統版本。

Node.js 的數值是本專案完整依賴圖的支援下限，不是 Sakai 的通用要求；Bun 則固定版本，兩者都需要。調整依賴時須核對最低版本，並同步維護本表與交接文件。

Go 工具鏈不得透過自動下載繞過環境要求；建置檢查應使用 `GOTOOLCHAIN=local`。缺少環境時回報問題，不自行執行安裝、升級或 Wails 互動式安裝精靈。

已安裝工具的版本可用下列命令檢查：

```powershell
go version
node --version
bun --version
git --version
```

## 開發與檢查

已具備上述環境後，在專案根目錄使用集中入口，驗證 Node.js／Bun 版本後，執行固定鎖檔安裝與 Bun 腳本；這不會安裝或升級 Runtime：

```powershell
node scripts/frontend.mjs install
node scripts/frontend.mjs run format
node scripts/frontend.mjs run lint
node scripts/frontend.mjs run typecheck
node scripts/frontend.mjs run test
node scripts/frontend.mjs run build
```

安裝等同 `bun install --frozen-lockfile --ignore-scripts --linker hoisted`；唯一前端鎖檔為 `bun.lock`。可在 `frontend` 執行 `bun run dev` 等腳本；`build`／`typecheck` 明確由 Node.js 執行 vue-tsc，法律生成與發布工具也使用 Node.js。不要用 `--bun` 強制換 Runtime，不加萬用 `.vue` 宣告來掩蓋型別錯誤。

完整檢查的根目錄 Prettier／ESLint 也透過集中入口，以 Node 直接執行本地套件 CLI，不依賴 npm 的 Windows `.cmd` 捷徑。

完整檢查在 PowerShell 7 執行 `./scripts/check.ps1`，Linux 執行 `bash scripts/check-linux.sh`。不要用 `go test ./...` 遍歷 node_modules。測試入口、文件專用檢查及證據限制見 [docs/TESTING.md](docs/TESTING.md)；Node.js 22.13.0 下限依 ESLint 10／Vite 8 的需求設定，最低版本的實際工具鏈驗收仍見 [docs/PLAN.md](docs/PLAN.md)。

### 原生建置與打包

Windows 使用 PowerShell 7，命令在專案根目錄執行：

```powershell
./scripts/generate-bindings.ps1
./scripts/build-windows.ps1
```

Linux 須在已具備上述 GTK4／WebKitGTK 的 x86_64 原生環境執行：

```bash
bash scripts/build-linux.sh
```

兩平台腳本共用上述 Bun／Node 入口、提交的 Bun 鎖檔與本機 Go 工具鏈；以 `production` 標籤編譯，不啟用除錯工具、開發伺服器或 Runtime 自動安裝。Linux 腳本已通過 Bash 語法檢查，Linux 原生建置與套件仍待驗收。

Windows 建置會以固定版本 Wails CLI 嵌入多尺寸 ICO 與使用者權限 manifest，執行檔、主視窗、工作列和系統匣沿用同源綠底白 F。暫存資源拒絕覆寫，完成或失敗後只清除此建置產生的資源；一般 `go build` 不會自動嵌入執行檔圖示。

每次產物獨立存於 `dist/builds/{打包識別碼}/`：Windows 為 ZIP，Linux 為 TAR.GZ。同版可重複打包，套件名稱沿用版號，以外層識別碼區分各次產物、不覆寫歷史。壓縮檔只含執行檔、使用者版 `README.md`、本案 `LICENSE` 與完整合併 `THIRD_PARTY_NOTICES.txt` 四檔；開發文件留在原始碼專案，不附 `licenses/` 目錄或 `build-info.json`。版本／提交／建置環境併入使用說明，SHA-256 留在壓縮檔外；同一交易內的異值檔案仍拒絕覆寫，失敗暫存保留。摘要用於完整性檢查，不等於數位簽章。建置入口已實作不表示現行來源的正式套件已完成驗收。

套件內的使用者 README 由 `scripts/licenses.mjs` 產生，包含啟動／系統需求、操作與安全說明，以及「GitHub」兩個、「Twitch」六個、「X」四個，共十二個範例網域。GitHub 範例為 `raw.githubusercontent.com` 與 `release-assets.githubusercontent.com`，Twitch 包含 `jtvnw.twitchcdn.net`。分組標題與可直接貼上的逐行網域區塊分開，附匯入預覽步驟，另說明全部 12 域各自探測、三域下載與 X 固定短圖限制；不複製本開發 README，也不自動匯入或套用範例。各網域仍須經當次 DNS 偵測與測速，不保證永久使用 Fastly。

`scripts/licenses.mjs --app` 收錄實際 Go 依賴、前端執行依賴閉包、字型、Tailwind、本案與改寫參考元件，保留原始 LICENSE／NOTICE／PATENTS；缺授權停止建置。產生的 `frontend/generated/about.json` 不提交，前端建置／開發／型別檢查／測試前自動生成；首次尚無 dist 只允許已知嵌入檔缺漏，不忽略其他 Go 依賴錯誤。

應用程式的「關於與開源授權」頁可離線搜尋名稱／版本／授權類型，以官方 Accordion 清單展開完整原文；保留展開圖示、滑鼠與鍵盤操作。來源連結只開系統瀏覽器。`--release` 使用同一份嵌入資料產生外部合併授權與使用說明，並核對來源提交／版本，避免頁面與套件內容不同；法律原文不刪減。npm 缺少的固定版本來源見 [third_party/README.md](third_party/README.md)。

## 執行環境與桌面行為

| 平台    | 執行需求                                                                                      |
| ------- | --------------------------------------------------------------------------------------------- |
| Windows | 上述支援系統、WebView2 Runtime、Windows Credential Manager                                    |
| Linux   | 上述支援系統、GTK4／WebKitGTK 6.0 API 執行函式庫、D-Bus 使用者工作階段、可用的 Secret Service |
| 共通    | 可連線至 DNS 來源、Fastly／Control D API 與受測網域；系統匣需要桌面環境支援                   |

執行已打包程式不需要 Go、Node.js 或 Bun，但仍需要作業系統的 WebView 與金鑰庫。Ubuntu 套件例子為 `libgtk-4-1`、`libwebkitgtk-6.0-4`，實際清單須隨打包產物驗證。

無可用系統匣時，關閉退工作列最小化，不能隱藏成無法操作的背景程式。若金鑰庫不可用，不得降級明文。缺少 WebView 只提示需求，不自動安裝。

Windows 核對本程式的圖示註冊，不以圖示座標判定可用性，位於「隱藏的圖示」區域仍可用。每五秒確認，單次失敗先重試；連續三次仍未確認才退工作列並記錄一次 `TRAY_UNCONFIRMED`，不冒稱已證實失效。確認恢復後可重新縮至系統匣。

## 首版使用流程

1. 在總覽選近端／完整，開始與停止共用一顆純圖示按鈕；啟動中與停止中顯示等待，停止須等宿主確認。空管理清單也可測基準，預設 GitHub／Twitch／X 三組共 12 個網域，不需權杖且不套用。
2. 管理網域貼主機名稱或 HTTPS 檔案網址。`https://cdn.example.com/files/test.bin` 保留 `cdn.example.com`，不縮為 `example.com`；相同主機不同資源需選擇取代。
3. 原版候選十組、三輪與逾時固定；設定的進階區可選基準服務、IPv6、共享參考與總流量／期限。擴充預設關閉，總流量與期限預設不限，已保存的非零上限保留。
4. 寫入前才保存權杖，到規則同步讀 Profile、選一般資料夾、只讀該夾規則；新增資料夾是獨立明確帳號寫入，不藏在預覽。
5. 預覽預設只寫最佳一組；開啟「強制寫入 Control D」後，完整測試無改善時使用已驗證原始 CNAME／IP。確認覆寫、原／新資料夾及子網域風險後套用。讀取失敗停止，不當成空規則。
6. 自動可關閉、只測試或測後套用；後者另確認 Profile／資料夾及各網域覆寫／移動授權。
7. 結果可單筆刪除或清空，作業進行中禁止；不刪雲端規則、管理網域或復原紀錄。右下通知可點開錯誤頁同筆詳細浮動視窗。
8. 執行紀錄可單筆刪除或清除全部，先確認後才移除；清除全部只刪已結束且無未決寫入的列表紀錄，提示保留的作業數。列表移除狀態持久保存，重開仍維持；原始安全與復原底帳保留，不影響雲端規則、測速結果或錯誤紀錄，也不是清理底帳檔案的功能。

讀取只顯示本區載入中，可取消或切換資料夾／頁面，不把整個介面按鈕鎖住；舊回應不覆蓋新選擇。帳號連線、復原預覽與錯誤紀錄同樣採局部讀取。差異預覽另查全 Profile 核對原規則與資料夾；已有同網域規則可更新，必要時依預覽移動，不先刪再建，也不以跨夾彙整讀到同網域為拒絕理由。必要寫入／復原互斥保持。

跨頁作業面板預設收合，只保留作業名稱與狀態；展開可看階段、目前網域、耗時、流量及詳細訊息。按鈕支援滑鼠與鍵盤，同次執行切頁保留選擇，作業開始／結束不強制改變，重新啟動回到收合；收合不會停止作業。完成／部分完成／失敗／取消依宿主實際狀態，啟動成功不等於作業完成。開始前提供保守下載工作量，並非精準完成時間或流量承諾；候選可查看每輪速度、位元組、TLS、TTFB 與完整請求耗時，已測的零 Mbps 與未量下載分開顯示。

### 強制寫入 Control D

設定頁的全域開關預設關閉並持久保存；關閉時維持無改善不寫入。開啟後，有改善仍用正常最佳 IP；完整測試無改善時，重新向 Google＋Cloudflare 取得原始 DNS，優先使用兩方一致的第一跳 CNAME，沒有 CNAME 才選原始 DNS 的合格 IP，不拿 Control D 現有覆寫當來源。原始鏈結、官方範圍與原網域 TLS／HTTPS 均須通過；來源不明、衝突、循環或測試未完成仍不寫，TTL 不作套用門檻。

目標與現有規則相同也會實際送出請求，成功必須同時有 API 明確接受與回讀一致；拒絕、逾時或崩潰不因同值回讀而誤報成功，也不盲目重送。「強制」不是保證 API 成功，更不會略過安全檢查或手動預覽確認。復原仍回到該次保存的原規則，不是重新查 DNS。

代理等非指定目標規則可省略不適用的 `via_v6`；指定目標規則仍須明確提供，null、未知欄位或缺少可還原註解仍阻擋覆寫。基準展示與未完成管理測試分別提示原因，強制不能繞過；略過項目顯示保留原值，不冒稱要刪除規則。

開關同時適用手動與自動「測試後套用」，不會自行啟用自動；「只測試」永遠不寫。強制模式略過十五分鐘切換冷卻，但仍遵守排程、單一作業、覆寫／移動與共享參考授權。變更開關會停用自動並清除舊授權，請重新測試及確認授權。

### 測速與安全限制

每次測速的下載資料上限由使用者設定；超過上限不繼續測試，樣本不足的網域不自動套用。候選先做 TCP 預掃，選低延遲路線，再以順暢輪數優先評分；相同順暢輪數不因速度中位數較高而切換。完整候選生成、評分、固定參數、下載失敗處理與安全條件以 [SPEC.md](SPEC.md) 第 3、6 節為準。

三個內建下載域為 `release-assets.githubusercontent.com` 的動態 GitHub Release、`assets.twitch.tv` 的動態 Twitch JS，以及 `pbs.twimg.com` 的指定 X 圖片；明確同域自訂檔優先，各域仍各自驗證連線。

基準測試與管理清單獨立，可取消部分服務；每組展開全部探測網域：GitHub 為 `raw.githubusercontent.com`、`release-assets.githubusercontent.com`；Twitch 為 `www.twitch.tv`、`assets.twitch.tv`、`gql.twitch.tv`、`k.twitchcdn.net`、`static-cdn.jtvnw.net`、`jtvnw.twitchcdn.net`；X 為 `abs.twimg.com`、`pbs.twimg.com`、`video.twimg.com`、`video-s.twimg.com`。共 12 域各自驗證 Host／TLS，機房只取自身回應，無法辨識顯示未知；其餘 9 域預設僅比較自身連線，不借服務下載冒充自身 Mbps。不需先加入網域或提供權杖，基準不取得套用資格，也不自動加入管理清單或建立規則。只有明確選用共享參考的管理域才補測 GitHub／Twitch 兩個下載來源。

X 使用固定圖片 <https://pbs.twimg.com/media/HU2En5YbAAAByoT?format=png&name=900x900> 三輪實際下載，不借用 GitHub，也不依賴其基準。這是短圖片，僅反映短檔表現，不代表持續大檔頻寬；本文 Mbps 不含建立連線與等候首位元組，須一併看完整請求耗時。下載只接受 2xx 且非空本文，3xx 不算有效下載，也不跟隨轉址；不設最小檔案大小。

其他網域沒有測速檔時，預設驗自身 HTTPS 並比較 TCP 延遲；現用可達時須改善嚴格超過 10 ms 才建議。明確同域自訂檔優先。共享參考與 IPv6 均須明確選用；共享不足退自己的延遲，不跨作業代測。每樣本最多 8 MiB、固定三輪，流量與總期限預設不限，已保存的限制不自動放寬；取消、預算不足或期限到仍標未完成。

必要共享來源先完成，其餘管理網域與基準交錯；自動排程依實際管理處理數輪替，不固定扣除基準數。同作業同域且無自訂檔、獨立現用規則或共享目標時可重用量測，流量只計一次；有現用規則仍保精確 IP 完整比較。每批最多四域並行資格探測，共用全域 worker 上限，主要下載仍串行。有限額度可能只能部分完成，不省三輪，也不拿未開始網域的舊結果自動套用。

測速結果、DNS、官方網段、共享參考與套用／復原預覽不因時間經過而失去套用資格。TTL 只決定快取何時刷新；資格掃描後與下載末段需要時由程式重查雙 DoH，零 TTL 不阻擋，同作業重用不因資料年齡整輪重測。官方清單到期先刷新，暫時失敗可沿用內容與摘要仍合法的快取，不偽造取得時間；沒有合法清單仍停止。真實查詢失敗、歸屬改變、取消、作業期限、候選或額度不足則保留實際原因。

設定載入與格式正規化不會擴大共享或 IPv6 授權，也不會放寬已保存的流量／總期限；需要重新確認時會停用自動、要求重測及重新授權，不改既有規則或復原 journal。策略版本、設定預設版本及格式處理契約見 [docs/API_SPEC.md](docs/API_SPEC.md)。HTTP／I/O 逾時、取消與使用者設定的作業總期限均有效，IPv6 不造位址。

「允許共享參考自動套用」是獨立授權，預設關閉，不能由設定載入隱含取得。手動仍須確認不可變更預覽；所有新選定 IPv4／IPv6 套用前均重驗精確 IP、自身 Host／SNI 與正常 TLS／HTTPS，再核網路、設定、結果與證據內容，失敗不寫入。共享另驗私有來源與摘要，保留雙 DoH、原值備份與回讀。結果分開顯示自身成功輪數與來源分數，沒有量目標下載就顯示未量下載。

策略版本不符的結果須重新測試，不能套用。Twitch 靜態資源測速不包含直播串流／聊天室；資源 hostname 若未加入管理清單，不會替它建立 Control D 規則。結果詳情顯示真正 HTTP 狀態與階段，未量得延遲不顯示為 0 ms。

排程、游標或冷卻紀錄保存失敗時會暫停自動執行，重開仍保留安全暫停。請先修復儲存空間，再核對／重新啟用自動授權；工具會從同帳號已確認的紀錄重建冷卻，不把已套用的規則誤稱未寫入。

### 應用程式字型

設定的「外觀與啟動」提供官方可搜尋下拉選單；每次開啟才呼叫前端 `window.queryLocalFonts()`，需要 WebView 支援及本機字型權限。不支援或拒絕授權時提示原因並保留目前字型，不改由 Go 列舉，也不下載或匯入字型。

選擇立即套用並保存在此應用的本機儲存，重開沿用；選預設恢復 Lato／系統後備。偏好與 Go 設定修訂分開，不使測速、預覽或自動授權失效；圖示及程式碼字型維持。Windows WebView2 與 Linux WebKit 的實際能力及權限仍待原生驗收。

## 已知限制與安全事項

- 判定代表當次 DNS／網路觀察，不是永久證明網域只使用 Fastly。
- 測速反映執行程式的網路；VPN、路由與不同裝置所在地可能影響結果。
- Control D API 回讀成功不等於 DNS 已生效；DNS 快取及其他 DNS 設定可能繞過規則。
- Control D 對網域的規則可能涵蓋子網域，不與 hosts 的精確單域行為完全相同。
- API 沒有文件化的整批交易或條件式寫入；只能提供逐項核對與補償式復原，不能承諾整批原子操作。
- 真實帳號讀寫尚未驗收；實際 API 的 CNAME／同值重寫、IPv6、資料夾與復原契約須另經授權驗證。缺少完整快照欄位的既有規則不開放覆寫。
- Control D 根目錄 GET 使用 `/profiles/{id}/rules`，不加 `/0`；有 HTTP 回歸測試，尚未使用真實帳號驗證。失敗可查操作／HTTP／廠商代碼／request ID，不保存權杖、Profile 識別碼或原回應全文。
- Linux 原生建置／桌面、最低工具鏈、race 與現行來源套件尚未完成驗收；Windows 隔離驗證不涵蓋全部人工操作。Wails Beta 退出清理的 `Chrome_WidgetWin_0 Error 1412` 警告仍需追蹤。詳細證據見 [docs/TESTING.md](docs/TESTING.md)，交付前待辦見 [docs/PLAN.md](docs/PLAN.md)。

## 開發文件

| 文件                                                       | 用途                                            |
| ---------------------------------------------------------- | ----------------------------------------------- |
| [SPEC.md](SPEC.md)                                         | 首版功能、安全條件、預設值與完整交付驗收標準    |
| [docs/DESIGN.md](docs/DESIGN.md)                           | 已實作架構、資料模型、作業流程與 Sakai 介面設計 |
| [docs/API_SPEC.md](docs/API_SPEC.md)                       | Wails 綁定／事件與外部 adapter 的現行契約及限制 |
| [docs/PLAN.md](docs/PLAN.md)                               | 首版完整交付前必須完成的驗收 TODO               |
| [docs/MEMORY.md](docs/MEMORY.md)                           | 維護時必讀的決策、安全限制與交接                |
| [CHANGELOG.md](CHANGELOG.md)                               | 依版本與類別整理的變更紀錄                      |
| [docs/TESTING.md](docs/TESTING.md)                         | 測試入口、覆蓋範圍、既有證據與未驗證限制        |
| [docs/BUILD_PERFORMANCE.md](docs/BUILD_PERFORMANCE.md)     | 工具鏈決策的歷史量測與適用限制                  |
| [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md) | Sakai 與依賴授權說明                            |

規格與預設值集中於 SPEC，實作細節集中於 DESIGN／API_SPEC；測試證據集中於 TESTING，未完成驗收只在 PLAN 維護勾選狀態。MEMORY 不另複製完整規格或待辦，以免維護時出現不同版本的說法。

本工具使用本機 JSON 而非資料庫，也不提供自建 HTTP WebAPI，因此不建立 `docs/DB_SPEC.md` 或本工具的 `openapi.yaml`／`openapi.json`。第三方 Control D OpenAPI 是串接參考，不是本工具的公開介面。

## 版本與交付管理

正式版號只以提交的根目錄 `VERSION` 為準，格式為 `{四碼年}.{臺灣日期的當年天序}.{當日升版序號}`。**打包不等於升版**：沒有明確要求升版，不論修改、提交、打包幾次或跨日，都沿用版本檔內容；不在文件另行維護一份版號。

只有使用者明確要求升版時，才執行下列命令並提交 `VERSION`，再進行打包：

```powershell
node scripts/release.mjs --bump
```

以升版當下 UTC+8 日期計算：同日第三碼加 1，新一天首次升版從 1 開始，天序不補零；日期倒退則拒絕。換日不會自行升版，打包成功或失敗也不改 `VERSION`。執行檔、授權頁、檔名與使用說明同源；開發建置顯示 `development`。

打包紀錄保存在 `git rev-parse --git-common-dir` 的 `fastly-releases/releases.json`，同 clone／worktree 共用，不提交或隨套件發佈。schema 2 保存成功紀錄，逐次記識別碼、版號、來源提交、平台、時間及四檔／壓縮檔 SHA-256。同版不同提交可分別追溯。升版與打包共用排他鎖；有未決交易先按其保存路徑恢復，ready 續傳不重建、不改版。未知紀錄不覆空、殘留鎖不自動搶。

請備份紀錄；不同主機／獨立 clone 須同步 `VERSION` 與打包紀錄，不具中央協調、不可自行併發操作。正式打包要求乾淨提交，內部 `PackageOnly` 僅供協調器使用；工具不自動建立 Git 標籤或推送。

## 技術與授權

前端採 Vue 3、TypeScript、Vite、Tailwind、PrimeVue 4.5.5 與 `@primeuix/themes` 2.0.3，直接整合 Sakai Vue 5.0.0 官方布局與原始樣式，沿用 Lato 字型及 PrimeUI Tailwind 外掛。布局／樣式雙來源、固定提交與必要適配見 [docs/DESIGN.md](docs/DESIGN.md)、[docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md) 及 `frontend/src/layout/sakai-source.json`；官方樣式完整性有離線測試。

本專案經使用者確認採 MIT，見 [LICENSE](LICENSE)。Sakai、上述 PrimeVue／主題版本也採 MIT；匯入樣板或沿用上游程式時保留原始授權與著作權聲明。Go／npm 依賴已固定並提交鎖定資訊，不追蹤浮動 `latest`。第三方字型等依賴另保留各自授權。

文件使用臺灣正體中文及 UTF-8 無 BOM，一般文件與前端採 Windows CRLF；Go、Shell、固定官方樣式與法律原文保留 LF。規格／README／變更紀錄置根目錄，設計／介面／交接／TODO 置 `docs/`。程式碼先提交，文件另外提交；不自動推送遠端。

## 參考來源

- [參考工具 fastly-hosts-adjust](https://github.com/AFAlimru/fastly-hosts-adjust)
- [Fastly 官方 IP 清單](https://api.fastly.com/public-ip-list)
- [Control D OpenAPI 參考文件](https://bestpika.github.io/ctrld-openapi-doc/openapi.yaml)
- [Control D 自訂規則](https://docs.controld.com/docs/custom-rules)
- [Control D IPv6 Compatibility Mode](https://docs.controld.com/docs/ipv6-compatibility-mode)
- [Wails v3 環境需求](https://v3.wails.io/quick-start/installation/)
- [Wails 固定版本的 Go 模組](https://github.com/wailsapp/wails/blob/v3.0.0-beta.28/v3/go.mod)
- [Sakai Vue v5.0.0 官方固定版本](https://github.com/primefaces/sakai-vue/tree/5.0.0)
- [Sakai 樣式的固定 MIT 來源](https://github.com/primefaces/sakai-vue/tree/7c32ae1f6fc75e18a5e735f76e1ab25d1167d26c/src/assets)
