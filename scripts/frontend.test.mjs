import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, URL } from "node:url";
import { spawnSync } from "node:child_process";
import ts from "../frontend/node_modules/typescript/lib/typescript.js";
import { frontendCommand, validateToolchain } from "./frontend.mjs";

const frontend = fileURLToPath(new URL("../frontend/", import.meta.url));
const entry = fileURLToPath(new URL("./frontend.mjs", import.meta.url));
const manifest = JSON.parse(
  fs.readFileSync(path.join(frontend, "package.json"), "utf8"),
);
const lockPath = path.join(frontend, "bun.lock");

test("工具鏈固定 Bun 並保留 Node 下限，缺少或不符不自動升級", () => {
  for (const node of ["22.13.0", "v22.13.1", "24.21.0"])
    assert.doesNotThrow(() => validateToolchain(node, "1.4.2"));
  for (const [node, bun] of [
    ["22.12.0", "1.4.2"],
    ["20.99.0", "1.4.2"],
    ["24.21.0", "1.4.1"],
    ["24.21.0", "1.4.3"],
    ["24.21.0", ""],
    ["invalid", "1.4.2"],
  ])
    assert.throws(() => validateToolchain(node, bun));
});

test("安裝固定鎖檔、停用生命週期及 hoisted，腳本只接受已知名稱", () => {
  assert.deepEqual(frontendCommand("install"), [
    "bun",
    ["install", "--frozen-lockfile", "--ignore-scripts", "--linker", "hoisted"],
  ]);
  assert.deepEqual(frontendCommand("run", ["build"]), [
    "bun",
    ["run", "build"],
  ]);
  for (const [action, args] of [
    ["install", ["--force"]],
    ["run", ["unknown"]],
    ["run", ["build", "--bun"]],
    ["unknown", []],
  ])
    assert.throws(() => frontendCommand(action, args));
});

test("型別檢查固定目前 Node 執行檔，建置及獨立入口均保留檢查", () => {
  assert.deepEqual(frontendCommand("typecheck", ["--noEmit"]), [
    process.execPath,
    [path.join(frontend, "node_modules/vue-tsc/bin/vue-tsc.js"), "--noEmit"],
  ]);
  const command = "node ../scripts/frontend.mjs typecheck --noEmit";
  assert.equal(manifest.scripts.typecheck, command);
  assert.equal(manifest.scripts.build, `${command} && vite build`);
  for (const name of ["predev", "prebuild", "pretypecheck", "pretest"])
    assert.equal(manifest.scripts[name], "bun run prepare:licenses");
  assert.equal(
    manifest.scripts["prepare:licenses"],
    "node ../scripts/licenses.mjs --app",
  );
});

test("根目錄工具直接使用 Node CLI，不依賴 npm 的 Windows cmd 捷徑", () => {
  for (const [name, binary] of [
    ["prettier", "prettier/bin/prettier.cjs"],
    ["eslint", "eslint/bin/eslint.js"],
  ])
    assert.deepEqual(frontendCommand("tool", [name, "--version"]), [
      process.execPath,
      [path.join(frontend, "node_modules", binary), "--version"],
    ]);
  assert.throws(() => frontendCommand("tool", ["unknown"]));
  assert.throws(() => frontendCommand("tool"));
});

test("唯一 Bun 鎖檔與直接依賴相符並保留完整性摘要", () => {
  const parsed = ts.parseConfigFileTextToJson(
    lockPath,
    fs.readFileSync(lockPath, "utf8"),
  );
  assert.equal(parsed.error, undefined);
  const lock = parsed.config;
  assert.deepEqual(lock.workspaces[""].dependencies, manifest.dependencies);
  assert.deepEqual(
    lock.workspaces[""].devDependencies,
    manifest.devDependencies,
  );
  assert.ok(Object.keys(lock.packages).length > 0);
  for (const [name, version] of Object.entries({
    ...manifest.dependencies,
    ...manifest.devDependencies,
  }))
    assert.equal(lock.packages[name][0], `${name}@${version}`);
  for (const item of Object.values(lock.packages))
    assert.match(item.at(-1), /^sha512-[A-Za-z0-9+/]+=*$/);
  assert.ok(!fs.existsSync(path.join(frontend, "package-lock.json")));
});

test("frozen 安裝拒絕套件與鎖檔不一致且不改寫鎖檔", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "fastly-bun-lock-"));
  const lock = fs.readFileSync(lockPath, "utf8");
  try {
    fs.writeFileSync(
      path.join(directory, "package.json"),
      JSON.stringify({
        ...manifest,
        dependencies: {},
        devDependencies: {},
      }),
    );
    fs.writeFileSync(path.join(directory, "bun.lock"), lock);
    const result = spawnSync(
      "bun",
      [...frontendCommand("install")[1], "--offline"],
      { cwd: directory, encoding: "utf8", timeout: 30_000 },
    );
    assert.ifError(result.error);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /frozen|lockfile/i);
    assert.equal(
      fs.readFileSync(path.join(directory, "bun.lock"), "utf8"),
      lock,
    );
    assert.ok(!fs.existsSync(path.join(directory, "node_modules")));
  } finally {
    for (const name of ["package.json", "bun.lock"])
      fs.unlinkSync(path.join(directory, name));
    fs.rmdirSync(directory);
  }
});

test("Bun 腳本經 Node 真正檢查 Vue，刻意型別錯誤必須失敗", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "fastly-vue-types-"));
  try {
    const config = path.join(directory, "tsconfig.json");
    fs.writeFileSync(
      config,
      JSON.stringify({
        compilerOptions: {
          target: "ES2022",
          module: "ESNext",
          moduleResolution: "Bundler",
          strict: true,
          noEmit: true,
          skipLibCheck: true,
          types: [],
          paths: { vue: [path.join(frontend, "node_modules/vue")] },
        },
        include: ["*.ts", "*.vue"],
      }),
    );
    fs.writeFileSync(
      path.join(directory, "main.ts"),
      'import Probe from "./Probe.vue";\nexport default Probe;\n',
    );
    const check = `node "${entry.replaceAll("\\", "/")}" typecheck --noEmit --project "${config.replaceAll("\\", "/")}"`;
    fs.writeFileSync(
      path.join(directory, "package.json"),
      JSON.stringify({ private: true, scripts: { typecheck: check } }),
    );
    const vue = path.join(directory, "Probe.vue");
    const source = (value) =>
      `<script setup lang="ts">\nconst count: number = ${value};\n</script>\n<template><span>{{ count }}</span></template>\n`;
    const run = () =>
      spawnSync("bun", ["run", "typecheck"], {
        cwd: directory,
        encoding: "utf8",
        timeout: 60_000,
      });
    fs.writeFileSync(vue, source("1"));
    const good = run();
    assert.ifError(good.error);
    assert.equal(good.status, 0, good.stdout + good.stderr);
    fs.writeFileSync(vue, source('"不是數字"'));
    const bad = run();
    assert.ifError(bad.error);
    assert.equal(bad.status, 2, bad.stdout + bad.stderr);
    assert.match(bad.stdout + bad.stderr, /Probe\.vue.*TS2322/);
    assert.doesNotMatch(bad.stdout + bad.stderr, /TS2307/);
    const forced = spawnSync("bun", [entry, "typecheck", "--noEmit"], {
      cwd: directory,
      encoding: "utf8",
      timeout: 30_000,
    });
    assert.ifError(forced.error);
    assert.equal(forced.status, 1);
    assert.match(forced.stderr, /Node\.js/);
  } finally {
    for (const name of [
      "package.json",
      "tsconfig.json",
      "main.ts",
      "Probe.vue",
    ])
      fs.unlinkSync(path.join(directory, name));
    fs.rmdirSync(directory);
  }
});
