import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync, spawnSync } from "node:child_process";
import { randomBytes, createHash } from "node:crypto";
import {
  advanceVersion,
  buildVersion,
  readVersion,
  validVersion,
} from "./version.mjs";
import { combineNotices, usage, validateData } from "./licenses.mjs";
import { archiveFiles } from "./archive.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const platforms = ["windows-amd64", "linux-amd64"];
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
const call = (cwd, ...args) =>
  execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
const commonDirectory = (cwd) =>
  path.resolve(cwd, call(cwd, "rev-parse", "--git-common-dir"));
const source = (cwd) => {
  if (call(cwd, "status", "--porcelain"))
    throw new Error("正式打包須使用乾淨提交；一般建置不會改變 VERSION");
  return call(cwd, "rev-parse", "HEAD");
};
const exactKeys = (o, keys) =>
  o &&
  typeof o === "object" &&
  !Array.isArray(o) &&
  Object.keys(o).length === keys.length &&
  keys.every((key) => Object.hasOwn(o, key));
const hashOK = (v) => typeof v === "string" && /^[a-f0-9]{64}$/.test(v);
const revisionOK = (v) =>
  typeof v === "string" && /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(v);

function validateLegacyLedger(data) {
  if (
    !exactKeys(data, ["schema", "releases", "pending"]) ||
    data.schema !== 1 ||
    !Array.isArray(data.releases)
  )
    throw new Error("發布紀錄格式未知，保留原檔並停止");
  const versions = new Set();
  for (const r of data.releases) {
    if (
      !exactKeys(r, ["version", "revision", "platforms"]) ||
      !validVersion(r.version) ||
      !revisionOK(r.revision) ||
      versions.has(r.version) ||
      !r.platforms ||
      !Object.keys(r.platforms).length
    )
      throw new Error("發布紀錄無效或版號重複");
    versions.add(r.version);
    for (const [platform, a] of Object.entries(r.platforms)) {
      if (
        !platforms.includes(platform) ||
        !exactKeys(a, ["archive", "sha256", "files"]) ||
        a.archive !== archiveName(r.version, platform) ||
        !hashOK(a.sha256) ||
        !exactKeys(a.files, packageFiles(platform)) ||
        !Object.values(a.files).every(hashOK)
      )
        throw new Error("平台發布紀錄不完整");
    }
  }
  if (data.pending !== null) {
    const p = data.pending;
    if (
      !exactKeys(p, [
        "version",
        "revision",
        "platform",
        "token",
        "root",
        "artifact",
      ]) ||
      !validVersion(p.version) ||
      !revisionOK(p.revision) ||
      !platforms.includes(p.platform) ||
      !hashOK(p.token) ||
      typeof p.root !== "string" ||
      !path.isAbsolute(p.root)
    )
      throw new Error("未完成發布紀錄無效");
    if (p.artifact !== null)
      validateLegacyLedger({
        schema: 1,
        releases: [
          {
            version: p.version,
            revision: p.revision,
            platforms: { [p.platform]: p.artifact },
          },
        ],
        pending: null,
      });
    const existing = data.releases.find((r) => r.version === p.version);
    if (
      existing &&
      (existing.revision !== p.revision || existing.platforms[p.platform])
    )
      throw new Error("待發布紀錄與成功版本衝突");
  }
  return data;
}

export function validateLedger(data) {
  if (data?.schema === 1) {
    validateLegacyLedger(data);
    return {
      schema: 2,
      releases: data.releases,
      builds: [],
      pending: data.pending
        ? { ...data.pending, layout: "legacy", startedAt: null }
        : null,
    };
  }
  if (
    !exactKeys(data, ["schema", "releases", "builds", "pending"]) ||
    data.schema !== 2 ||
    !Array.isArray(data.builds)
  )
    throw new Error("發布紀錄格式未知，保留原檔並停止");
  validateLegacyLedger({ schema: 1, releases: data.releases, pending: null });
  const ids = new Set();
  const timeOK = (value) =>
    typeof value === "string" &&
    Number.isFinite(Date.parse(value)) &&
    new Date(value).toISOString() === value;
  for (const b of data.builds) {
    if (
      !exactKeys(b, [
        "id",
        "version",
        "revision",
        "platform",
        "startedAt",
        "artifact",
      ]) ||
      !hashOK(b.id) ||
      ids.has(b.id) ||
      !timeOK(b.startedAt) ||
      b.artifact === null
    )
      throw new Error("打包紀錄無效或識別碼重複");
    ids.add(b.id);
    validateLegacyLedger({
      schema: 1,
      releases: [
        {
          version: b.version,
          revision: b.revision,
          platforms: { [b.platform]: b.artifact },
        },
      ],
      pending: null,
    });
  }
  if (data.pending !== null) {
    const p = data.pending;
    if (
      !exactKeys(p, [
        "version",
        "revision",
        "platform",
        "token",
        "root",
        "artifact",
        "layout",
        "startedAt",
      ]) ||
      !["legacy", "build"].includes(p.layout) ||
      (p.layout === "legacy" ? p.startedAt !== null : !timeOK(p.startedAt)) ||
      ids.has(p.token)
    )
      throw new Error("未完成打包紀錄無效");
    const legacy = Object.fromEntries(
      Object.entries(p).filter(
        ([key]) => !["layout", "startedAt"].includes(key),
      ),
    );
    validateLegacyLedger({
      schema: 1,
      releases: p.layout === "legacy" ? data.releases : [],
      pending: legacy,
    });
  }
  return data;
}

function readJSON(filename) {
  if (
    fs.lstatSync(filename).isSymbolicLink() ||
    fs.statSync(filename).size > 32 * 1024 * 1024
  )
    throw new Error("發布紀錄路徑或大小無效");
  return JSON.parse(fs.readFileSync(filename, "utf8"));
}

function atomicJSON(filename, data) {
  const temporary = filename + "." + randomBytes(16).toString("hex") + ".tmp";
  const fd = fs.openSync(temporary, "wx", 0o600);
  try {
    fs.writeFileSync(fd, JSON.stringify(data, null, 2) + "\n", "utf8");
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  fs.renameSync(temporary, filename);
}

export function acquireLock(directory) {
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  if (fs.lstatSync(directory).isSymbolicLink())
    throw new Error("發布資料目錄不能是符號連結");
  const filename = path.join(directory, "release.lock");
  const token = randomBytes(32).toString("hex");
  let fd;
  try {
    fd = fs.openSync(filename, "wx", 0o600);
  } catch {
    throw new Error(
      "已有發布作業或未清理的發布鎖；確認沒有建置程序後再人工處理，不自動搶鎖",
    );
  }
  try {
    fs.writeFileSync(fd, JSON.stringify({ token, pid: process.pid }) + "\n");
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  return {
    token,
    release: () => {
      if (readJSON(filename).token !== token)
        throw new Error("發布鎖已變動，未刪除");
      fs.unlinkSync(filename);
    },
  };
}

const packageName = (version, platform) =>
  `go-fastly-ctrld-${version}-${platform}`;
const archiveName = (version, platform) =>
  packageName(version, platform) +
  (platform.startsWith("windows-") ? ".zip" : ".tar.gz");
const packageFiles = (platform) => [
  platform.startsWith("windows-") ? "go-fastly-ctrld.exe" : "go-fastly-ctrld",
  "README.md",
  "LICENSE",
  "THIRD_PARTY_NOTICES.txt",
];

export function verifyArtifacts(stage, pending, manifest) {
  validateData(manifest);
  if (
    manifest.build.version !== pending.version ||
    manifest.build.revision !== pending.revision ||
    manifest.build.dirty ||
    manifest.build.platform !== pending.platform
  )
    throw new Error("執行檔內嵌版本來源不符，不登記發布");
  const folder = path.join(
    stage,
    packageName(pending.version, pending.platform),
  );
  if (
    !fs.lstatSync(folder).isDirectory() ||
    fs.lstatSync(folder).isSymbolicLink()
  )
    throw new Error("套件目錄不是一般目錄");
  const files = packageFiles(pending.platform);
  if (fs.readdirSync(folder).sort().join() !== [...files].sort().join())
    throw new Error("正式套件不是四個檔案");
  const hashes = {};
  for (const file of files) {
    const filename = path.join(folder, file);
    if (!fs.lstatSync(filename).isFile()) throw new Error("套件檔案不是一般檔");
    hashes[file] = sha(fs.readFileSync(filename));
  }
  const project = manifest.components.find((c) => c.kind === "project");
  const license = project?.files.find((f) => f.name === "LICENSE")?.text;
  if (
    !license ||
    fs.readFileSync(path.join(folder, "LICENSE"), "utf8") !== license ||
    fs.readFileSync(path.join(folder, "README.md"), "utf8") !==
      usage(manifest) ||
    fs.readFileSync(path.join(folder, "THIRD_PARTY_NOTICES.txt"), "utf8") !==
      combineNotices(manifest)
  )
    throw new Error("內外版本或法律資料不同源");
  const archive = archiveName(pending.version, pending.platform);
  const digest = sha(fs.readFileSync(path.join(stage, archive)));
  if (
    fs.readFileSync(path.join(stage, archive + ".sha256"), "utf8").trim() !==
    `${digest}  ${archive}`
  )
    throw new Error("壓縮檔摘要不符");
  const archived = archiveFiles(
    path.join(stage, archive),
    pending.platform,
    packageName(pending.version, pending.platform),
  );
  if (
    !exactKeys(archived, files) ||
    files.some((file) => archived[file] !== hashes[file])
  )
    throw new Error("壓縮檔與被測套件不同");
  return { archive, sha256: digest, files: hashes };
}

function promote(directory, p) {
  const stage = path.join(directory, "work", p.token);
  let destination = p.root;
  for (const part of p.layout === "legacy"
    ? ["dist"]
    : ["dist", "builds", p.token]) {
    destination = path.join(destination, part);
    if (!fs.existsSync(destination)) fs.mkdirSync(destination);
    if (
      !fs.lstatSync(destination).isDirectory() ||
      fs.lstatSync(destination).isSymbolicLink()
    )
      throw new Error("拒絕發布至非一般目錄或符號連結");
  }
  const folder = packageName(p.version, p.platform);
  const target = path.join(destination, folder);
  if (!fs.existsSync(target)) fs.mkdirSync(target);
  if (fs.lstatSync(target).isSymbolicLink())
    throw new Error("拒絕發布至符號連結");
  for (const [file, digest] of Object.entries(p.artifact.files)) {
    const source = path.join(stage, folder, file);
    if (
      !fs.lstatSync(source).isFile() ||
      sha(fs.readFileSync(source)) !== digest
    )
      throw new Error("待恢復套件已變動，未發布");
  }
  if (
    !fs.lstatSync(path.join(stage, p.artifact.archive)).isFile() ||
    sha(fs.readFileSync(path.join(stage, p.artifact.archive))) !==
      p.artifact.sha256 ||
    fs
      .readFileSync(path.join(stage, p.artifact.archive + ".sha256"), "utf8")
      .trim() !== `${p.artifact.sha256}  ${p.artifact.archive}`
  )
    throw new Error("待恢復壓縮檔已變動");
  for (const [file, digest] of Object.entries(p.artifact.files)) {
    const dest = path.join(target, file);
    if (fs.existsSync(dest)) {
      if (!fs.lstatSync(dest).isFile() || sha(fs.readFileSync(dest)) !== digest)
        throw new Error("已有不同套件，不覆寫");
    } else
      fs.copyFileSync(
        path.join(stage, folder, file),
        dest,
        fs.constants.COPYFILE_EXCL,
      );
  }
  if (
    fs.readdirSync(target).sort().join() !==
    packageFiles(p.platform).sort().join()
  )
    throw new Error("目的套件包含未預期檔案");
  for (const name of [p.artifact.archive, p.artifact.archive + ".sha256"]) {
    const dest = path.join(destination, name),
      src = path.join(stage, name);
    const expected = name.endsWith(".sha256")
      ? sha(fs.readFileSync(src))
      : p.artifact.sha256;
    if (fs.existsSync(dest)) {
      if (
        !fs.lstatSync(dest).isFile() ||
        sha(fs.readFileSync(dest)) !== expected
      )
        throw new Error("已有不同壓縮檔，不覆寫");
    } else fs.copyFileSync(src, dest, fs.constants.COPYFILE_EXCL);
  }
}

// 版號只由 VERSION 決定；每次打包另記識別碼，ready 可在重開後完成複製。
export function runRelease({
  directory,
  version,
  revision,
  platform,
  outputRoot,
  now = new Date(),
  build,
  manifest,
  checkSource = () => {},
  checkRoot = () => {},
}) {
  if (
    !platforms.includes(platform) ||
    !revisionOK(revision) ||
    !validVersion(version)
  )
    throw new Error("打包版號、平台或來源無效");
  const lock = acquireLock(directory);
  const filename = path.join(directory, "releases.json");
  let ledger;
  try {
    ledger = fs.existsSync(filename)
      ? validateLedger(readJSON(filename))
      : { schema: 2, releases: [], builds: [], pending: null };
    const commitPending = () => {
      const p = ledger.pending;
      checkRoot(p.root);
      promote(directory, p);
      if (p.layout === "legacy") {
        let release = ledger.releases.find((r) => r.version === p.version);
        if (!release) {
          release = { version: p.version, revision: p.revision, platforms: {} };
          ledger.releases.push(release);
        }
        if (release.revision !== p.revision || release.platforms[p.platform])
          throw new Error("舊發布紀錄交易衝突");
        release.platforms[p.platform] = p.artifact;
      } else {
        ledger.builds.push({
          id: p.token,
          version: p.version,
          revision: p.revision,
          platform: p.platform,
          startedAt: p.startedAt,
          artifact: p.artifact,
        });
      }
      ledger.pending = null;
      atomicJSON(filename, validateLedger(ledger));
      return p;
    };
    if (ledger.pending?.artifact) {
      const restored = commitPending();
      if (
        restored.version === version &&
        restored.platform === platform &&
        restored.revision === revision &&
        path.resolve(restored.root) === path.resolve(outputRoot)
      )
        return restored.version;
    }
    ledger.pending = null;
    const pending = {
      version,
      revision,
      platform,
      root: path.resolve(outputRoot),
      token: lock.token,
      artifact: null,
      layout: "build",
      startedAt: now.toISOString(),
    };
    ledger.pending = pending;
    atomicJSON(filename, validateLedger(ledger));
    const stage = path.join(directory, "work", lock.token);
    fs.mkdirSync(stage, { recursive: true });
    build({
      version,
      stage,
      packageDirectory: path.join(stage, packageName(version, platform)),
      token: lock.token,
    });
    checkSource();
    pending.artifact = verifyArtifacts(stage, pending, manifest());
    atomicJSON(filename, validateLedger(ledger));
    commitPending();
    return version;
  } catch (error) {
    if (ledger?.pending && !ledger.pending.artifact) {
      ledger.pending = null;
      atomicJSON(filename, validateLedger(ledger));
    }
    throw error;
  } finally {
    lock.release();
  }
}

function cli() {
  const common = commonDirectory(root),
    directory = path.join(common, "fastly-releases");
  if (process.argv.length === 3 && process.argv[2] === "--bump") {
    const lock = acquireLock(directory);
    try {
      if (
        fs.existsSync(path.join(directory, "releases.json")) &&
        validateLedger(readJSON(path.join(directory, "releases.json"))).pending
      )
        throw new Error("仍有未完成打包，先核對恢復後才能升版");
      console.log(
        `已明確升版：${advanceVersion(path.join(root, "VERSION"))}；請提交 VERSION 後再打包`,
      );
    } finally {
      lock.release();
    }
    return;
  }
  if (process.argv[2] === "--check") {
    const platform = process.argv[3],
      lock = readJSON(path.join(directory, "release.lock"));
    const ledger = validateLedger(
      readJSON(path.join(directory, "releases.json")),
    );
    const p = ledger.pending;
    if (
      !p ||
      p.token !== lock.token ||
      p.token !== process.env.FASTLY_RELEASE_TOKEN ||
      p.platform !== platform ||
      p.version !== buildVersion() ||
      p.version !== readVersion(path.join(root, "VERSION")) ||
      path.resolve(p.root) !== root ||
      p.revision !== source(root) ||
      process.env.FASTLY_PACKAGE_DIR !==
        path.join(
          directory,
          "work",
          p.token,
          packageName(p.version, p.platform),
        )
    )
      throw new Error("內部打包必須由協調程序啟動且符合已提交版號");
    return;
  }
  if (process.argv.length !== 3 || !platforms.includes(process.argv[2]))
    throw new Error("請用原生平台建置腳本打包；只有明確要求升版才執行 --bump");
  const revision = source(root),
    platform = process.argv[2];
  const version = readVersion(path.join(root, "VERSION"));
  runRelease({
    directory,
    version,
    revision,
    platform,
    outputRoot: root,
    checkRoot: (value) => {
      if (commonDirectory(value) !== common)
        throw new Error("待發布路徑不是同一個 Git 專案");
    },
    checkSource: () => {
      if (source(root) !== revision)
        throw new Error("建置途中來源改變，未發布");
    },
    manifest: () => readJSON(path.join(root, "frontend/generated/about.json")),
    build: ({ version, packageDirectory, token }) => {
      const windows = platform === "windows-amd64";
      const command = windows ? "pwsh" : "bash";
      const args = windows
        ? [
            "-NoProfile",
            "-File",
            path.join(root, "scripts/build-windows.ps1"),
            "-PackageOnly",
          ]
        : [path.join(root, "scripts/build-linux.sh"), "--package-only"];
      const result = spawnSync(command, args, {
        cwd: root,
        stdio: "inherit",
        env: {
          ...process.env,
          GOTOOLCHAIN: "local",
          FASTLY_BUILD_VERSION: version,
          FASTLY_RELEASE_TOKEN: token,
          FASTLY_PACKAGE_DIR: packageDirectory,
        },
      });
      if (result.error || result.status !== 0)
        throw new Error("原生建置未成功；版號不變，失敗資料保留");
    },
  });
  console.log(
    `打包完成：${version}；各次產物保留於 dist/builds，來源與摘要記於發布紀錄`,
  );
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  cli();
