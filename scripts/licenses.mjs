import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, URL } from "node:url";
import { isDeepStrictEqual, TextDecoder } from "node:util";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { buildVersion } from "./version.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const generated = path.join(root, "frontend", "generated", "about.json");
const generator = "go-fastly-ctrld/open-source-v1";
const legal = /^(LICENSE|LICENCE|COPYING|NOTICE|PATENTS)(\.|$)/i;
const call = (command, ...args) =>
  execFileSync(command, args, {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, GOTOOLCHAIN: "local" },
  }).trim();
const hash = (text) => createHash("sha256").update(text, "utf8").digest("hex");

export function safeSource(value) {
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" || url.username || url.password) return "";
    return url.href;
  } catch {
    return "";
  }
}

export function uniqueComponents(components) {
  const unique = new Map();
  for (const component of components) {
    const previous = unique.get(component.id);
    if (previous && !isDeepStrictEqual(previous, component))
      throw new Error(`同名同版元件資料或法律原文不一致：${component.name}`);
    if (!previous) unique.set(component.id, component);
  }
  return [...unique.values()];
}

function legalFile(name, filename) {
  const text = new TextDecoder("utf-8", {
    fatal: true,
    ignoreBOM: true,
  }).decode(fs.readFileSync(filename));
  return { name, text, sha256: hash(text) };
}

function licenseType(files, declared) {
  if (typeof declared === "string") return declared;
  const text = files.map((file) => file.text).join("\n");
  const kinds = [];
  if (/Permission is hereby granted, free of charge/i.test(text))
    kinds.push("MIT");
  if (/Apache License[\s\S]{0,80}Version 2\.0/i.test(text))
    kinds.push("Apache-2.0");
  if (/SIL OPEN FONT LICENSE[\s\S]{0,80}1\.1/i.test(text))
    kinds.push("OFL-1.1");
  if (/Redistribution and use in source and binary forms/i.test(text))
    kinds.push("BSD 類授權");
  return kinds.join(" / ") || "見原始法律文字";
}

export function combineNotices(data) {
  return [
    "go-fastly-ctrld 開源授權與第三方通知",
    "法律原文完整保留；應用程式的「關於與開源授權」頁使用同一份資料。",
    ...data.components.map((entry) =>
      [
        "\n============================================================",
        `${entry.name} — ${entry.version}`,
        `授權：${entry.license}`,
        `來源：${entry.source}`,
        ...entry.files.map((file) => `\n--- ${file.name} ---\n${file.text}`),
      ].join("\n"),
    ),
    "",
  ].join("\n");
}

export function referenceLicense(notices, name) {
  const section = notices
    .replace(/\r\n/g, "\n")
    .split(`## ${name}\n`)[1]
    ?.split("\n## ")[0];
  const text = section?.match(/```text\n([\s\S]*?)\n```/)?.[1];
  if (!text) throw new Error(`參考元件原始授權缺漏：${name}`);
  return text + "\n";
}

export function validateData(data) {
  if (
    data.generator !== generator ||
    !data.build ||
    !Array.isArray(data.components) ||
    !data.components.length
  )
    throw new Error("授權資料格式無效");
  const ids = new Set();
  for (const entry of data.components) {
    if (
      !entry.id ||
      ids.has(entry.id) ||
      !entry.name ||
      !entry.version ||
      !entry.license ||
      !safeSource(entry.source) ||
      !entry.files?.length
    )
      throw new Error("元件資料不完整或重複");
    ids.add(entry.id);
    for (const file of entry.files) {
      if (
        !file.name ||
        path.isAbsolute(file.name) ||
        /^[A-Za-z]:/.test(file.name) ||
        file.name.split(/[\\/]/).includes("..") ||
        !file.text ||
        file.sha256 !== hash(file.text)
      )
        throw new Error("原始法律文字缺漏、被修改或包含本機路徑");
    }
  }
  return data;
}

export function usage(data) {
  const b = data.build;
  const executable = b.platform.startsWith("windows-")
    ? "go-fastly-ctrld.exe"
    : "go-fastly-ctrld";
  const requirements = b.platform.startsWith("windows-")
    ? "Windows 10 22H2／Windows 11 x86_64、WebView2 Runtime、Windows Credential Manager。建議使用仍受安全維護的系統。"
    : "Ubuntu 24.04 以上 x86_64、GTK4／WebKitGTK 6.0 API 執行函式庫、D-Bus 使用者工作階段與 Secret Service；例如 libgtk-4-1、libwebkitgtk-6.0-4。";
  return `# Fastly 路線最佳化

## 開始使用

1. 解壓後執行 \`${executable}\`，不需要安裝 Go、Node.js 或 Bun。
2. 不需帳號或管理網域即可測基準，預設 GitHub／Twitch／X 三組共 12 個網域；進階設定可選服務，共享參考的必要依賴會補測。基準不套用、不自動加入管理清單。
3. 要最佳化自己的網域才匯入清單，確認 Google＋Cloudflare DoH；所有網域各自探測，同域自訂檔優先，一般域無檔案預設只比自身延遲。
4. 選近端或完整掃描；流量與整輪時間新預設不限，也可自訂，舊設定的非零上限不擅自放寬。每下載樣本最多 8 MiB，額度不足標未完成。
5. 要套用才保存 Control D Write 權杖並選裝置使用的 Profile，檢查差異與子網域影響後明確確認。
6. 自動可選僅測試、不套用且不需權杖；自動寫入仍須另確認完整授權。
7. 查看執行紀錄與復原預覽；可確認後單筆刪除或清除全部已結束紀錄，保留進行中與待核對作業並提示筆數。只持久移除列表項目，安全與復原底帳保留，不改規則、測速結果或錯誤紀錄。完整開源授權在「關於與開源授權」頁離線可讀。
8. 設定的字型選單開啟時才由前端讀取系統字型，需要 WebView 支援與授權；無法讀取時保留目前字型。選擇立即套用並獨立保存，重開沿用，可回復預設；不影響測試、預覽或自動授權，也不下載或匯入字型。

## 範例網域與匯入

到「網域管理」選擇「逐行」，貼上以下程式碼區塊內的網域，每行一個；按「預覽貼上清單」，先檢查匯入預覽，再確認加入清單。可分組匯入，也可合併各組；不要複製分組標題或 Markdown 標記。

### GitHub

\`\`\`text
raw.githubusercontent.com
release-assets.githubusercontent.com
\`\`\`

### Twitch

\`\`\`text
www.twitch.tv
assets.twitch.tv
gql.twitch.tv
k.twitchcdn.net
static-cdn.jtvnw.net
jtvnw.twitchcdn.net
\`\`\`

### X

\`\`\`text
abs.twimg.com
pbs.twimg.com
video.twimg.com
video-s.twimg.com
\`\`\`

以上僅為範例，不會自動匯入或套用規則，也不保證每個網域目前都使用 Fastly。加入後仍須到「總覽」執行 DNS 偵測與測速，符合安全條件並確認規則預覽後才可套用；Twitch 靜態資源測試不包含直播串流／聊天室。

## 探測與下載測速

以上 12 個網域全部各自驗證連線、Host 與 TLS，機房資訊只取自身回應，無可辨識資訊顯示未知。只有以下 3 個網域另做三輪下載測速，其餘 9 個預設僅測自身連線延遲，不借其他網域的 Mbps 冒充自身成績：

- GitHub：release-assets.githubusercontent.com，逐輪取得最新 Release 的動態簽章下載網址。
- Twitch：assets.twitch.tv，從 Twitch 首頁取得動態 JavaScript 檔案。
- X：pbs.twimg.com，使用 https://pbs.twimg.com/media/HU2En5YbAAAByoT?format=png&name=900x900 ，不再借用 GitHub 成績。這是短圖片，僅反映短檔下載表現，不代表持續大檔頻寬。

開始與停止共用一顆圖示按鈕；啟動中及停止中會顯示等待，停止須等宿主確認。跨頁作業面板預設收合，保留名稱與狀態；展開可看作業階段、耗時、流量與詳細訊息。切頁及作業開始／結束不改手動選擇，重新啟動回收合，收合不會停止作業。下載只接受 2xx 且非空本文，重新導向不算有效下載。必要共享來源優先，其餘管理網域與基準交錯；同作業安全重用不重計流量，有獨立現用規則仍完整比較。套用前會重新驗證選定 IP 的自身 Host／TLS，失敗不寫入。

## 最低執行需求

${requirements}

需能連線 DNS／Fastly 與受測服務，要套用才需 Control D 帳號。缺少 WebView 或金鑰庫不自動安裝、不改用明文權杖；系統匣不可用時保留視窗。

## 注意事項

- 長時間掃描或等待後，會重新向雙 DNS 來源查證，不硬延長舊證據；下載期間到期會再次查證。部分完成請查看各網域的實際原因，不一定是流量不足。強制寫入也不能套用未完成結果；略過項目保留原規則。
- 測速、DNS、網段、共享參考與套用／復原預覽不因時間經過失效；TTL 只供刷新，零 TTL 不阻擋，也不因此整輪重測。仍須通過設定、網路、原值、公開官方 IP 與自身 TLS 驗證，真實變更或失敗仍會阻擋。

- 遍歷官方 IPv4 各 /24，不截池取樣；近端共用 64 個 TCP 初掃／一秒，服務 16 個／三次三秒；完整 48 個初驗後 16 個複測。
- 每輪維持相同下載候選順序；任一輪資源取得失敗捨棄全部下載成績、退自身延遲，取消／期限／預算不足仍未完成。
- IPv6 與一般域共享參考預設關閉，須明確選用；共享自動套用另需授權，來源 Mbps 不是目標下載速度。
- 每次 socket I/O 預設十秒，不是整請求十秒；持續有資料可繼續，仍可取消。DoH／控制 API／寫入回讀的 HTTP 逾時與自訂作業期限維持。
- 自動套用與登入時啟動預設關閉；關閉視窗縮至系統匣，真正退出請用系統匣或設定頁。
- 不修改 hosts／系統 DNS、不安裝 ctrld；Profile 須由裝置實際使用，API 保存不等於 DNS 即刻生效。
- 未知 API 欄位／備份不足的既有規則不覆寫；復原先核後值，衝突保留現況。
- 策略更新後舊結果須重測，舊自動授權須重確認；既有規則與復原紀錄保留。基準及未完成結果不能套用。Twitch 不含直播／聊天室。
- 排程保存故障跨重開暫停，修復後重新確認授權，不抹除已套用紀錄。
- Wails v3 Beta；完整 Linux 原生與真實 API 契約仍待驗收。

## 版本與來源

- 版本：${b.version}
- 來源提交：${b.revision}
- 未提交內容：${b.dirty ? "有" : "無"}
- 平台：${b.platform}
- 建置時間：${b.builtAt}
- Go：${b.go}
- Node.js：${b.node}
- Wails：${b.wails}
- 原始碼：https://github.com/bestpika/go-fastly-ctrld

套件只附執行檔、本使用說明、LICENSE 與 THIRD_PARTY_NOTICES.txt。開發規格留在原始碼專案；ZIP／TAR.GZ 外附 SHA-256，摘要不是數位簽章。
`;
}

export function sakaiReference() {
  const layout = JSON.parse(
    fs.readFileSync(
      path.join(root, "frontend/src/layout/sakai-source.json"),
      "utf8",
    ),
  );
  const styles = layout.styles;
  const text = `Sakai Vue 布局：${layout.version} / ${layout.commit}
來源：${layout.repository}/tree/${layout.commit}
布局授權：${layout.license}；原始法律文字見 LICENSE。

官方樣式：${styles.version} / ${styles.commit}
來源：${styles.repository}/tree/${styles.commit}/${styles.upstreamPrefix}
樣式授權：${styles.license}；沿用主儲存庫歷史來源及相同 MIT 原文。
v5 固定資產對照：${styles.matches.repository}/tree/${styles.matches.commit}
${styles.note}
`;
  return {
    name: "Sakai Vue",
    version: `${layout.version} / ${layout.commit}`,
    source: `${layout.repository}/tree/${layout.commit}`,
    files: [{ name: "SOURCE_NOTES.txt", text, sha256: hash(text) }],
  };
}

function collect() {
  const modules = new Map();
  const lines = call(
    "go",
    "list",
    "-mod=readonly",
    "-e",
    "-tags",
    "production",
    "-deps",
    "-f",
    "{{if .Error}}ERROR|{{.ImportPath}}|{{.Error.Err}}{{else}}{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}|{{.Dir}}{{end}}",
    "./cmd/go-fastly-ctrld",
  ).split(/\r?\n/);
  for (const line of lines) {
    const [name, version, directory, packageDirectory] = line.split("|");
    if (name === "ERROR") {
      // 首次前端建置尚無 dist，但此套件僅匯入標準函式庫；依賴圖仍完整。
      if (
        version === "github.com/bestpika/go-fastly-ctrld/frontend" &&
        directory === "pattern all:dist: no matching files found"
      )
        continue;
      throw new Error(`Go 依賴無法完整列舉：${version}`);
    }
    if (!version || !directory) continue;
    if (!modules.has(name))
      modules.set(name, {
        name,
        version,
        directory,
        directories: new Set([directory]),
        kind: "go",
        source: `https://pkg.go.dev/${name}@${version}`,
      });
    const entry = modules.get(name);
    for (
      let current = packageDirectory;
      current.startsWith(directory + path.sep);
      current = path.dirname(current)
    )
      entry.directories.add(current);
  }
  const entries = [...modules.values()];
  const wails = modules.get("github.com/wailsapp/wails/v3");
  const visited = new Set();
  function npmDirectory(name, from) {
    for (
      let current = from;
      current.startsWith(root);
      current = path.dirname(current)
    ) {
      const directory = path.join(current, "node_modules", name);
      if (fs.existsSync(path.join(directory, "package.json"))) return directory;
      if (current === root) break;
    }
    throw new Error(`找不到 npm 依賴：${name}`);
  }
  function visit(directory) {
    if (visited.has(directory)) return;
    visited.add(directory);
    const pkg = JSON.parse(
      fs.readFileSync(path.join(directory, "package.json"), "utf8"),
    );
    let legalDirectory = directory;
    if (pkg.name === "@wailsio/runtime") {
      if (!wails || wails.version.replace(/^v/, "") !== pkg.version)
        throw new Error("Wails Runtime 版本不一致");
      legalDirectory = wails.directory;
    }
    if (pkg.name === "@vue/devtools-api" && pkg.version === "6.6.4")
      legalDirectory = path.join(root, "third_party", "vue-devtools-api-6.6.4");
    const repository =
      typeof pkg.repository === "string" ? pkg.repository : pkg.repository?.url;
    const source =
      safeSource(
        repository
          ?.replace(/^git\+/, "")
          .replace(/^git:\/\//, "https://")
          .replace(/\.git$/, ""),
      ) || `https://www.npmjs.com/package/${pkg.name}/v/${pkg.version}`;
    entries.push({
      name: pkg.name,
      version: pkg.version,
      license: pkg.license,
      directory: legalDirectory,
      directories: new Set([legalDirectory]),
      kind: "npm",
      source,
    });
    for (const name of Object.keys(pkg.dependencies ?? {}))
      visit(npmDirectory(name, directory));
    for (const name of Object.keys(pkg.optionalDependencies ?? {})) {
      let optional;
      try {
        optional = npmDirectory(name, directory);
      } catch {
        continue;
      }
      visit(optional);
    }
  }
  const front = path.join(root, "frontend");
  const pkg = JSON.parse(
    fs.readFileSync(path.join(front, "package.json"), "utf8"),
  );
  for (const name of [...Object.keys(pkg.dependencies), "tailwindcss"])
    visit(npmDirectory(name, front));
  const goRoot = call("go", "env", "GOROOT");
  entries.push({
    name: "Go 標準函式庫",
    version: call("go", "env", "GOVERSION"),
    directory: goRoot,
    directories: new Set([goRoot]),
    kind: "toolchain",
    source: "https://go.dev/",
  });
  const version = buildVersion();
  const revision = call("git", "rev-parse", "HEAD");
  const components = entries.map((entry) => {
    const files = [];
    for (const directory of entry.directories) {
      for (const file of fs.readdirSync(directory, { withFileTypes: true })) {
        if (!file.isFile() || !legal.test(file.name)) continue;
        const filename = path.join(directory, file.name);
        files.push(
          legalFile(
            path.relative(entry.directory, filename).split(path.sep).join("/"),
            filename,
          ),
        );
      }
    }
    if (!files.some((file) => /(?:LICENSE|LICENCE|COPYING)/i.test(file.name)))
      throw new Error(`缺少原始授權，停止建置：${entry.name}`);
    return {
      id: `${entry.kind}:${entry.name}@${entry.version}`,
      name: entry.name,
      version: entry.version,
      kind: entry.kind,
      license: licenseType(files, entry.license),
      source: entry.source,
      files: files.sort((a, b) => a.name.localeCompare(b.name)),
    };
  });
  components.push({
    id: "project:go-fastly-ctrld",
    name: "go-fastly-ctrld",
    version,
    kind: "project",
    license: "MIT",
    source: `https://github.com/bestpika/go-fastly-ctrld/tree/${revision}`,
    files: [legalFile("LICENSE", path.join(root, "LICENSE"))],
  });
  const notices = fs.readFileSync(
    path.join(root, "docs", "THIRD_PARTY_NOTICES.md"),
    "utf8",
  );
  for (const reference of [
    sakaiReference(),
    {
      name: "fastly-hosts-adjust",
      version: "0.8.3 / 336e105f71b8fce123d5bfdc9f635b9b8d500d0b",
      source:
        "https://github.com/AFAlimru/fastly-hosts-adjust/tree/336e105f71b8fce123d5bfdc9f635b9b8d500d0b",
    },
  ]) {
    const original = referenceLicense(notices, reference.name);
    components.push({
      ...reference,
      id: `reference:${reference.name}`,
      kind: "reference",
      license: "MIT",
      files: [
        { name: "LICENSE", text: original, sha256: hash(original) },
        ...(reference.files ?? []),
      ],
    });
  }
  return validateData({
    generator,
    build: {
      version,
      revision,
      dirty: !!call("git", "status", "--porcelain"),
      platform: `${process.env.GOOS || (process.platform === "win32" ? "windows" : process.platform)}-${process.env.GOARCH || "amd64"}`,
      builtAt: new Date().toISOString(),
      go: call("go", "version"),
      node: process.version,
      wails: wails.version,
    },
    components: uniqueComponents(components).sort((a, b) =>
      a.id.localeCompare(b.id),
    ),
  });
}

function generateApp() {
  const data = collect();
  if (fs.existsSync(generated))
    validateData(JSON.parse(fs.readFileSync(generated, "utf8")));
  fs.mkdirSync(path.dirname(generated), { recursive: true });
  fs.writeFileSync(generated, JSON.stringify(data, null, 2) + "\n", "utf8");
  console.log(`已內嵌 ${data.components.length} 項元件的完整開源授權`);
}

export function writeRelease(output, data) {
  validateData(data);
  const binary = data.build.platform.startsWith("windows-")
    ? "go-fastly-ctrld.exe"
    : "go-fastly-ctrld";
  if (
    !fs.statSync(output).isDirectory() ||
    fs.readdirSync(output).join() !== binary
  )
    throw new Error("套件目錄必須只含本次執行檔，不覆寫已有資料");
  fs.writeFileSync(path.join(output, "README.md"), usage(data), {
    encoding: "utf8",
    flag: "wx",
  });
  const own = data.components.find((entry) => entry.kind === "project");
  fs.writeFileSync(path.join(output, "LICENSE"), own.files[0].text, {
    encoding: "utf8",
    flag: "wx",
  });
  fs.writeFileSync(
    path.join(output, "THIRD_PARTY_NOTICES.txt"),
    combineNotices(data),
    { encoding: "utf8", flag: "wx" },
  );
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  if (process.argv[2] === "--app" && process.argv.length === 3) generateApp();
  else if (process.argv[2] === "--release" && process.argv.length === 4) {
    const data = validateData(JSON.parse(fs.readFileSync(generated, "utf8")));
    if (
      data.build.revision !== call("git", "rev-parse", "HEAD") ||
      data.build.dirty !== !!call("git", "status", "--porcelain") ||
      data.build.version !== buildVersion()
    )
      throw new Error("嵌入授權與目前來源版本不同，請重新建置前端");
    writeRelease(path.resolve(process.argv[3]), data);
    console.log("套件僅附執行檔、使用說明、本案與合併開源授權四檔");
  } else
    throw new Error(
      "請使用 --app 產生嵌入資料，或 --release 目錄產生精簡套件文件",
    );
}
