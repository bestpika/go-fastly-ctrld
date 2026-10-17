import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { URL } from "node:url";
import {
  combineNotices,
  referenceLicense,
  safeSource,
  sakaiReference,
  uniqueComponents,
  validateData,
  writeRelease,
} from "./licenses.mjs";

const text = "完整法律原文\nCopyright\n";
const data = {
  generator: "go-fastly-ctrld/open-source-v1",
  build: {
    version: "test",
    revision: "revision",
    dirty: false,
    platform: "windows-amd64",
    builtAt: "time",
    go: "go",
    node: "node",
    wails: "wails",
  },
  components: [
    {
      id: "project:tool",
      name: "tool",
      version: "test",
      kind: "project",
      license: "MIT",
      source: "https://github.com/bestpika/go-fastly-ctrld",
      files: [
        {
          name: "LICENSE",
          text,
          sha256: createHash("sha256").update(text).digest("hex"),
        },
      ],
    },
  ],
};

test("合併授權保留原文，拒絕內容被修改或本機路徑", () => {
  assert.ok(combineNotices(validateData(data)).includes(text));
  const changed = JSON.parse(JSON.stringify(data));
  changed.components[0].files[0].text += "變更";
  assert.throws(() => validateData(changed));
  const privatePath = JSON.parse(JSON.stringify(data));
  privatePath.components[0].files[0].name = "C:\\Users\\private\\LICENSE";
  assert.throws(() => validateData(privatePath));
});

test("參考授權支援 CRLF 與 LF，保留相同法律原文", () => {
  const notices = `## Sakai Vue\n\n\`\`\`text\n${text}\`\`\`\n\n## 下一項\n`;
  assert.equal(referenceLicense(notices, "Sakai Vue"), text);
  assert.equal(
    referenceLicense(notices.replace(/\n/g, "\r\n"), "Sakai Vue"),
    text,
  );
  assert.throws(() => referenceLicense(notices, "缺漏元件"), /原始授權缺漏/);
  assert.throws(() => referenceLicense("## Sakai Vue\n", "Sakai Vue"));
});

test("相同實體副本只合併完整相同元件，法律或來源差異仍拒絕", () => {
  const original = data.components[0];
  const copy = () => JSON.parse(JSON.stringify(original));
  assert.deepEqual(uniqueComponents([original, copy()]), [original]);
  assert.throws(() =>
    validateData({ ...data, components: [original, copy()] }),
  );
  for (const mutate of [
    (item) => (item.license = "BSD-3-Clause"),
    (item) => (item.source = "https://example.com/other"),
    (item) => item.files.push({ ...item.files[0], name: "NOTICE" }),
    (item) => {
      item.files[0].text += "不同原文";
      item.files[0].sha256 = createHash("sha256")
        .update(item.files[0].text)
        .digest("hex");
    },
  ]) {
    const changed = copy();
    mutate(changed);
    assert.throws(() => uniqueComponents([original, changed]), /不一致/);
  }
  const other = { ...copy(), id: "project:other", name: "other" };
  assert.deepEqual(uniqueComponents([original, other]), [original, other]);
  assert.equal(validateData(data), data);
});

for (const [platform, executable, requirement] of [
  ["windows-amd64", "go-fastly-ctrld.exe", "WebView2 Runtime"],
  ["linux-amd64", "go-fastly-ctrld", "GTK4／WebKitGTK 6.0 API"],
]) {
  test(`${platform} 發佈僅四檔，README 以英文服務名稱提供可貼上範例且不覆寫已有文件`, () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "fastly-legal-"));
    const manifest = { ...data, build: { ...data.build, platform } };
    try {
      fs.writeFileSync(path.join(directory, executable), "測試產物");
      writeRelease(directory, manifest);
      assert.deepEqual(
        fs.readdirSync(directory).sort(),
        ["LICENSE", "README.md", "THIRD_PARTY_NOTICES.txt", executable].sort(),
      );
      assert.equal(
        fs.readFileSync(path.join(directory, "LICENSE"), "utf8"),
        text,
      );
      const readme = fs.readFileSync(path.join(directory, "README.md"), "utf8");
      assert.ok(readme.includes(`解壓後執行 \`${executable}\``));
      assert.ok(readme.includes(requirement));
      assert.ok(!readme.startsWith("\ufeff"));
      assert.ok(readme.includes("不需帳號或管理網域"));
      assert.ok(readme.includes("要套用才保存 Control D Write 權杖"));
      assert.ok(readme.includes("僅測試、不套用"));
      assert.ok(readme.includes("新預設不限"));
      assert.ok(readme.includes("不是整請求十秒"));
      assert.ok(readme.includes("預設只比自身延遲"));
      assert.ok(readme.includes("IPv6 與一般域共享參考預設關閉"));
      assert.ok(readme.includes("必要依賴會補測"));
      assert.ok(!readme.includes("固定測 GitHub"));
      assert.ok(readme.includes("先檢查匯入預覽，再確認加入清單"));
      assert.ok(readme.includes("可分組匯入，也可合併各組"));
      assert.ok(readme.includes("不要複製分組標題或 Markdown 標記"));
      assert.ok(readme.includes("不會自動匯入或套用規則"));
      assert.ok(readme.includes("不保證每個網域目前都使用 Fastly"));
      assert.ok(readme.includes("12 個網域全部各自驗證連線、Host 與 TLS"));
      assert.ok(readme.includes("其餘 9 個預設僅測自身連線延遲"));
      assert.ok(readme.includes("不硬延長舊證據"));
      assert.ok(readme.includes("3 個網域另做三輪下載測速"));
      assert.ok(readme.includes("不再借用 GitHub 成績"));
      assert.ok(readme.includes("不代表持續大檔頻寬"));
      assert.ok(readme.includes("跨頁作業面板預設收合"));
      assert.ok(readme.includes("切頁及作業開始／結束不改手動選擇"));
      assert.ok(readme.includes("重新啟動回收合，收合不會停止作業"));
      assert.ok(readme.includes("單筆刪除或清除全部已結束紀錄"));
      assert.ok(readme.includes("保留進行中與待核對作業並提示筆數"));
      assert.ok(readme.includes("只持久移除列表項目，安全與復原底帳保留"));
      assert.ok(readme.includes("不因時間經過失效"));
      assert.ok(readme.includes("TTL 只供刷新，零 TTL 不阻擋"));
      assert.ok(readme.includes("字型選單開啟時才由前端讀取系統字型"));
      assert.ok(readme.includes("需要 WebView 支援與授權"));
      assert.ok(readme.includes("不影響測試、預覽或自動授權"));
      assert.ok(
        readme.includes(
          "https://pbs.twimg.com/media/HU2En5YbAAAByoT?format=png&name=900x900",
        ),
      );
      const groups = [
        ...readme.matchAll(/^### (.+)\n\n```text\n([^`]+)\n```$/gm),
      ];
      assert.deepEqual(
        groups.map((group) => [group[1], group[2].split("\n")]),
        [
          [
            "GitHub",
            [
              "raw.githubusercontent.com",
              "release-assets.githubusercontent.com",
            ],
          ],
          [
            "Twitch",
            [
              "www.twitch.tv",
              "assets.twitch.tv",
              "gql.twitch.tv",
              "k.twitchcdn.net",
              "static-cdn.jtvnw.net",
              "jtvnw.twitchcdn.net",
            ],
          ],
          [
            "X",
            [
              "abs.twimg.com",
              "pbs.twimg.com",
              "video.twimg.com",
              "video-s.twimg.com",
            ],
          ],
        ],
      );
      assert.throws(() => writeRelease(directory, manifest));
    } finally {
      // 僅刪除本測試建立且逐一命名的檔案，不對一般目錄遞迴刪除。
      for (const name of [
        "LICENSE",
        "README.md",
        "THIRD_PARTY_NOTICES.txt",
        executable,
      ])
        fs.unlinkSync(path.join(directory, name));
      fs.rmdirSync(directory);
    }
  });
}

test("來源僅 HTTPS，不接受內嵌帳密或其他協定", () => {
  assert.equal(safeSource("file:///secret"), "");
  assert.equal(safeSource("https://secret@example.com/"), "");
  assert.equal(
    safeSource("https://github.com/vuejs/core"),
    "https://github.com/vuejs/core",
  );
});

test("Sakai 授權版本與來源清單同源，離線保留舊 MIT 樣式及 v5 資產對照", () => {
  const source = JSON.parse(
    fs.readFileSync(
      new URL("../frontend/src/layout/sakai-source.json", import.meta.url),
      "utf8",
    ),
  );
  const entry = sakaiReference();
  assert.equal(entry.version, `${source.version} / ${source.commit}`);
  assert.ok(entry.version.startsWith("5.0.0 / "));
  assert.equal(entry.source, `${source.repository}/tree/${source.commit}`);
  const notes = entry.files[0];
  assert.equal(notes.name, "SOURCE_NOTES.txt");
  assert.ok(notes.text.includes(source.styles.commit));
  assert.ok(notes.text.includes(source.styles.matches.commit));
  assert.ok(notes.text.includes(source.styles.note));
  assert.equal(
    notes.sha256,
    createHash("sha256").update(notes.text).digest("hex"),
  );
  const combined = combineNotices({ components: [entry] });
  assert.ok(combined.includes(entry.version));
  assert.ok(combined.includes(notes.text));
});
