import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import { Buffer } from "node:buffer";
import os from "node:os";
import path from "node:path";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath, URL } from "node:url";
import {
  buildVersion,
  advanceVersion,
  nextVersion,
  readVersion,
  taiwanDay,
  validVersion,
} from "./version.mjs";
import {
  acquireLock,
  runRelease,
  validateLedger,
  verifyArtifacts,
} from "./release.mjs";
import { crc32 } from "./archive.mjs";
import { writeRelease } from "./licenses.mjs";

const revision = "a".repeat(40);
const another = "b".repeat(40);
const date = new Date("2026-10-17T01:00:00Z");
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
const root = () =>
  fs.mkdtempSync(path.join(os.tmpdir(), "fastly-release-test-"));

function manifest(version, platform, source = revision) {
  const text = "完整原始授權\nCopyright\n";
  return {
    generator: "go-fastly-ctrld/open-source-v1",
    build: {
      version,
      revision: source,
      dirty: false,
      platform,
      builtAt: date.toISOString(),
      go: "go",
      node: "node",
      wails: "wails",
    },
    components: [
      {
        id: "project:tool",
        name: "tool",
        version,
        kind: "project",
        license: "MIT",
        source: "https://github.com/bestpika/go-fastly-ctrld",
        files: [{ name: "LICENSE", text, sha256: sha(text) }],
      },
    ],
  };
}

function zip(entries) {
  const local = [],
    central = [];
  let offset = 0;
  for (const [name, bytes] of entries) {
    const filename = Buffer.from(name),
      a = Buffer.alloc(30),
      b = Buffer.alloc(46);
    a.writeUInt32LE(0x04034b50);
    a.writeUInt16LE(20, 4);
    a.writeUInt32LE(crc32(bytes), 14);
    a.writeUInt32LE(bytes.length, 18);
    a.writeUInt32LE(bytes.length, 22);
    a.writeUInt16LE(filename.length, 26);
    b.writeUInt32LE(0x02014b50);
    b.writeUInt16LE(20, 4);
    b.writeUInt16LE(20, 6);
    b.writeUInt32LE(crc32(bytes), 16);
    b.writeUInt32LE(bytes.length, 20);
    b.writeUInt32LE(bytes.length, 24);
    b.writeUInt16LE(filename.length, 28);
    b.writeUInt32LE(offset, 42);
    local.push(a, filename, bytes);
    central.push(b, filename);
    offset += a.length + filename.length + bytes.length;
  }
  const directory = Buffer.concat(central),
    end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(directory.length, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, directory, end]);
}

function tar(folder, entries) {
  const output = [];
  for (const [name, bytes] of [["", Buffer.alloc(0)], ...entries]) {
    const header = Buffer.alloc(512);
    header.write(folder + "/" + name);
    header.write("0000755\0", 100);
    header.write("0000000\0", 108);
    header.write("0000000\0", 116);
    header.write(bytes.length.toString(8).padStart(11, "0") + "\0", 124);
    header.write("00000000000\0", 136);
    header.fill(32, 148, 156);
    header.write(name ? "0" : "5", 156);
    header.write("ustar\0", 257);
    const checksum = [...header].reduce((sum, value) => sum + value, 0);
    header.write(checksum.toString(8).padStart(6, "0") + "\0 ", 148);
    output.push(
      header,
      bytes,
      Buffer.alloc((512 - (bytes.length % 512)) % 512),
    );
  }
  return gzipSync(Buffer.concat([...output, Buffer.alloc(1024)]));
}

function artifacts(stage, version, platform, source = revision) {
  const data = manifest(version, platform, source),
    name = `go-fastly-ctrld-${version}-${platform}`,
    folder = path.join(stage, name);
  fs.mkdirSync(folder, { recursive: true });
  fs.writeFileSync(
    path.join(
      folder,
      platform.startsWith("windows")
        ? "go-fastly-ctrld.exe"
        : "go-fastly-ctrld",
    ),
    "假執行檔",
  );
  writeRelease(folder, data);
  const entries = fs
    .readdirSync(folder)
    .map((file) => [file, fs.readFileSync(path.join(folder, file))]);
  const filename = path.join(
    stage,
    name + (platform.startsWith("windows") ? ".zip" : ".tar.gz"),
  );
  fs.writeFileSync(
    filename,
    platform.startsWith("windows") ? zip(entries) : tar(name, entries),
  );
  fs.writeFileSync(
    filename + ".sha256",
    sha(fs.readFileSync(filename)) + "  " + path.basename(filename) + "\n",
  );
  return data;
}

function request(
  directory,
  platform = "windows-amd64",
  source = revision,
  now = date,
  version = "2026.290.1",
) {
  let data;
  return {
    directory: path.join(directory, "state"),
    version,
    outputRoot: directory,
    revision: source,
    platform,
    now,
    build: ({ stage, version }) => {
      data = artifacts(stage, version, platform, source);
    },
    manifest: () => data,
  };
}

test("臺灣日界、跨年及閏年使用未補零天序", () => {
  for (const [input, expected] of [
    ["2026-10-16T15:59:59Z", "2026.289"],
    ["2026-10-16T16:00:00Z", "2026.290"],
    ["2024-02-28T16:00:00Z", "2024.60"],
    ["2024-12-31T15:59:59Z", "2024.366"],
    ["2024-12-31T16:00:00Z", "2025.1"],
  ])
    assert.equal(taiwanDay(new Date(input)), expected);
  for (const value of [
    "2026.0.1",
    "2026.01.1",
    "2026.366.1",
    "2026.290.01",
    "0.1.0-dev",
    "2026.290.0",
  ])
    assert.equal(validVersion(value), false);
  assert.equal(validVersion("2024.366.1"), true);
  assert.equal(buildVersion(""), "development");
  assert.throws(() => buildVersion("0.1.0-dev"));
});

test("只有明確升版才改版本；臺灣新日從一開始且不倒退", () => {
  const filename = path.join(root(), "VERSION");
  fs.writeFileSync(filename, "2026.290.13\n");
  assert.equal(readVersion(filename), "2026.290.13");
  assert.equal(advanceVersion(filename, date), "2026.290.14");
  assert.equal(
    advanceVersion(filename, new Date("2026-10-17T16:00:00Z")),
    "2026.291.1",
  );
  assert.equal(
    advanceVersion(filename, new Date("2026-10-18T01:00:00Z")),
    "2026.291.2",
  );
  assert.throws(() => advanceVersion(filename, date), /早於/);
  assert.equal(readVersion(filename), "2026.291.2");
  assert.equal(
    nextVersion("2024.366.9", new Date("2024-12-31T16:00:00Z")),
    "2025.1.1",
  );
  assert.equal(
    nextVersion("2024.59.1", new Date("2024-02-28T16:00:00Z")),
    "2024.60.1",
  );
  assert.throws(() => nextVersion("2026.290.9007199254740991", date));
  assert.throws(() => nextVersion("development", date));
  assert.throws(() => nextVersion("2026.290.1", new Date("invalid")));
  fs.writeFileSync(filename, "unknown\n");
  assert.throws(() => advanceVersion(filename, date));
  assert.equal(fs.readFileSync(filename, "utf8"), "unknown\n");
});

test("明確升版 CLI 僅改隔離 VERSION，未決交易與殘留鎖仍阻擋", () => {
  const directory = root(),
    scripts = path.join(directory, "scripts");
  fs.mkdirSync(scripts);
  for (const name of ["release", "version", "licenses", "archive"])
    fs.copyFileSync(
      fileURLToPath(new URL(`./${name}.mjs`, import.meta.url)),
      path.join(scripts, `${name}.mjs`),
    );
  execFileSync("git", ["init", "--quiet", directory]);
  const filename = path.join(directory, "VERSION"),
    state = path.join(directory, ".git/fastly-releases");
  fs.writeFileSync(filename, "2025.1.1\n");
  const invoke = () =>
    spawnSync(process.execPath, [path.join(scripts, "release.mjs"), "--bump"], {
      cwd: directory,
      encoding: "utf8",
    });
  assert.equal(invoke().status, 0);
  assert.equal(readVersion(filename), `${taiwanDay()}.1`);
  assert.equal(invoke().status, 0);
  const current = readVersion(filename);
  assert.equal(current, `${taiwanDay()}.2`);
  assert.equal(fs.existsSync(path.join(state, "releases.json")), false);
  fs.writeFileSync(
    path.join(state, "releases.json"),
    JSON.stringify({
      schema: 1,
      releases: [],
      pending: {
        version: current,
        revision,
        platform: "windows-amd64",
        token: "c".repeat(64),
        root: directory,
        artifact: null,
      },
    }),
  );
  const blocked = invoke();
  assert.notEqual(blocked.status, 0);
  assert.match(blocked.stderr, /未完成打包/);
  assert.equal(readVersion(filename), current);
  assert.equal(fs.existsSync(path.join(state, "release.lock")), false);
  fs.unlinkSync(path.join(state, "releases.json"));
  const lock = acquireLock(state);
  try {
    assert.notEqual(invoke().status, 0);
    assert.equal(readVersion(filename), current);
    assert.equal(
      JSON.parse(fs.readFileSync(path.join(state, "release.lock"))).token,
      lock.token,
    );
  } finally {
    lock.release();
  }
});

test("排他鎖不搶殘留鎖且僅解除自己的鎖", () => {
  const directory = root(),
    lock = acquireLock(directory);
  assert.throws(() => acquireLock(directory));
  lock.release();
  const next = acquireLock(directory);
  next.release();
});

test("建置失敗不改版本，重試成功後才新增打包紀錄", () => {
  const directory = root(),
    bad = request(directory);
  bad.build = () => {
    throw new Error("假建置失敗");
  };
  assert.throws(() => runRelease(bad));
  let ledger = JSON.parse(
    fs.readFileSync(path.join(directory, "state/releases.json")),
  );
  assert.equal(ledger.releases.length, 0);
  assert.equal(ledger.builds.length, 0);
  assert.equal(ledger.pending, null);
  assert.equal(runRelease(request(directory)), "2026.290.1");
  ledger = validateLedger(
    JSON.parse(fs.readFileSync(path.join(directory, "state/releases.json"))),
  );
  assert.equal(ledger.releases.length, 0);
  assert.equal(ledger.builds.length, 1);
  assert.equal(ledger.pending, null);
});

test("同版可跨日跨提交重打包，雙平台各次四檔與摘要獨立保留", () => {
  const directory = root();
  assert.equal(runRelease(request(directory)), "2026.290.1");
  const filename = path.join(directory, "state/releases.json");
  const first = JSON.parse(fs.readFileSync(filename)).builds[0];
  const firstArchive = path.join(
    directory,
    "dist/builds",
    first.id,
    first.artifact.archive,
  );
  const original = fs.readFileSync(firstArchive);
  assert.equal(
    runRelease(
      request(
        directory,
        "windows-amd64",
        another,
        new Date("2026-10-18T01:00:00Z"),
      ),
    ),
    "2026.290.1",
  );
  assert.equal(
    runRelease(
      request(
        directory,
        "linux-amd64",
        revision,
        new Date("2026-10-18T01:00:00Z"),
      ),
    ),
    "2026.290.1",
  );
  const ledger = validateLedger(
    JSON.parse(fs.readFileSync(path.join(directory, "state/releases.json"))),
  );
  assert.equal(ledger.releases.length, 0);
  assert.equal(ledger.builds.length, 3);
  assert.equal(new Set(ledger.builds.map((b) => b.id)).size, 3);
  assert.equal(ledger.builds[1].revision, another);
  assert.notEqual(
    ledger.builds[0].artifact.sha256,
    ledger.builds[1].artifact.sha256,
  );
  assert.deepEqual(fs.readFileSync(firstArchive), original);
  for (const b of ledger.builds) {
    const dest = path.join(directory, "dist/builds", b.id);
    assert.equal(
      sha(fs.readFileSync(path.join(dest, b.artifact.archive))),
      b.artifact.sha256,
    );
    assert.equal(
      fs.readdirSync(
        path.join(dest, `go-fastly-ctrld-${b.version}-${b.platform}`),
      ).length,
      4,
    );
  }
  assert.equal(
    runRelease(
      request(directory, "windows-amd64", another, date, "2026.290.2"),
    ),
    "2026.290.2",
  );
});

test("ready 複製失敗可恢復，不重建、不改版號或覆寫衝突檔", () => {
  const directory = root(),
    options = request(directory);
  let conflict;
  const build = options.build;
  options.build = (args) => {
    build(args);
    const destination = path.join(directory, "dist/builds", args.token);
    fs.mkdirSync(destination, { recursive: true });
    conflict = path.join(
      destination,
      `go-fastly-ctrld-${args.version}-windows-amd64.zip`,
    );
    fs.writeFileSync(conflict, "使用者原檔");
  };
  assert.throws(() => runRelease(options));
  assert.equal(fs.readFileSync(conflict, "utf8"), "使用者原檔");
  const before = JSON.parse(
    fs.readFileSync(path.join(directory, "state/releases.json")),
  );
  assert.ok(before.pending.artifact);
  assert.equal(before.builds.length, 0);
  fs.unlinkSync(conflict);
  options.build = () => {
    throw new Error("不應重建");
  };
  assert.equal(runRelease(options), "2026.290.1");
  const after = JSON.parse(
    fs.readFileSync(path.join(directory, "state/releases.json")),
  );
  assert.equal(after.pending, null);
  assert.equal(after.builds.length, 1);
});

test("舊成功紀錄無損遷移，同版不同來源仍可打包且舊產物不動", () => {
  const directory = root(),
    options = request(directory, "windows-amd64", another);
  const data = artifacts(
    path.join(directory, "legacy"),
    options.version,
    options.platform,
  );
  const artifact = verifyArtifacts(
    path.join(directory, "legacy"),
    { version: options.version, platform: options.platform, revision },
    data,
  );
  const old = {
    schema: 1,
    releases: [
      {
        version: options.version,
        revision,
        platforms: { [options.platform]: artifact },
      },
    ],
    pending: null,
  };
  fs.mkdirSync(options.directory);
  fs.writeFileSync(
    path.join(options.directory, "releases.json"),
    JSON.stringify(old),
  );
  fs.mkdirSync(path.join(directory, "dist"));
  const oldFile = path.join(directory, "dist", artifact.archive);
  fs.writeFileSync(oldFile, "歷史產物");
  assert.equal(runRelease(options), options.version);
  const migrated = validateLedger(
    JSON.parse(fs.readFileSync(path.join(options.directory, "releases.json"))),
  );
  assert.equal(migrated.schema, 2);
  assert.deepEqual(migrated.releases, old.releases);
  assert.equal(migrated.builds.length, 1);
  assert.equal(fs.readFileSync(oldFile, "utf8"), "歷史產物");
  const bad = JSON.parse(JSON.stringify(migrated));
  bad.builds.push(bad.builds[0]);
  assert.throws(() => validateLedger(bad));
});

test("舊 ready 交易仍按舊路徑恢復，保留來源摘要且不重建", () => {
  const directory = root(),
    options = request(directory),
    token = "c".repeat(64);
  const stage = path.join(options.directory, "work", token);
  const data = artifacts(stage, options.version, options.platform);
  const pending = {
    version: options.version,
    revision,
    platform: options.platform,
    token,
    root: directory,
    artifact: null,
  };
  pending.artifact = verifyArtifacts(stage, pending, data);
  fs.writeFileSync(
    path.join(options.directory, "releases.json"),
    JSON.stringify({ schema: 1, releases: [], pending }),
  );
  options.build = () => {
    throw new Error("不應重建舊 ready");
  };
  assert.equal(runRelease(options), options.version);
  const ledger = validateLedger(
    JSON.parse(fs.readFileSync(path.join(options.directory, "releases.json"))),
  );
  assert.equal(ledger.pending, null);
  assert.equal(ledger.releases.length, 1);
  assert.equal(ledger.builds.length, 0);
  assert.equal(
    sha(
      fs.readFileSync(path.join(directory, "dist", pending.artifact.archive)),
    ),
    pending.artifact.sha256,
  );
});

test("來源變更或未指定合法 VERSION 時不產生成功紀錄", () => {
  const directory = root(),
    options = request(directory);
  options.version = undefined;
  assert.throws(() => runRelease(options), /版號/);
  options.version = "2026.290.1";
  options.checkSource = () => {
    throw new Error("來源已變更");
  };
  assert.throws(() => runRelease(options), /來源已變更/);
  const ledger = validateLedger(
    JSON.parse(fs.readFileSync(path.join(options.directory, "releases.json"))),
  );
  assert.equal(ledger.pending, null);
  assert.equal(ledger.builds.length, 0);
  assert.equal(fs.existsSync(path.join(directory, "dist")), false);
});

test("未知發布紀錄保持原檔並停止", () => {
  const directory = root(),
    options = request(directory);
  fs.mkdirSync(options.directory);
  const filename = path.join(options.directory, "releases.json"),
    original = '{"schema":999,"releases":[],"pending":null}';
  fs.writeFileSync(filename, original);
  options.build = () => {
    throw new Error("不應建置");
  };
  assert.throws(() => runRelease(options));
  assert.equal(fs.readFileSync(filename, "utf8"), original);
});

test("內嵌來源或壓縮檔內容不同不能發布", () => {
  const directory = root(),
    version = "2026.290.1",
    platform = "windows-amd64",
    data = artifacts(directory, version, platform),
    pending = { version, revision, platform };
  assert.ok(verifyArtifacts(directory, pending, data));
  const dirty = JSON.parse(JSON.stringify(data));
  dirty.build.dirty = true;
  assert.throws(() => verifyArtifacts(directory, pending, dirty));
  const folder = path.join(directory, `go-fastly-ctrld-${version}-${platform}`),
    archive = folder + ".zip";
  const entries = fs
    .readdirSync(folder)
    .map((name) => [
      name,
      name === "README.md"
        ? Buffer.from("被改過的說明")
        : fs.readFileSync(path.join(folder, name)),
    ]);
  fs.writeFileSync(archive, zip(entries));
  fs.writeFileSync(
    archive + ".sha256",
    sha(fs.readFileSync(archive)) + "  " + path.basename(archive) + "\n",
  );
  assert.throws(() => verifyArtifacts(directory, pending, data));
});
