import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, URL } from "node:url";
import { spawnSync } from "node:child_process";

const directory = fileURLToPath(new URL("../frontend/", import.meta.url));
const manifest = JSON.parse(
  fs.readFileSync(path.join(directory, "package.json"), "utf8"),
);

export function validateToolchain(nodeVersion, bunVersion) {
  const minimum = manifest.engines.node.match(/^>=(\d+)\.(\d+)\.(\d+)$/);
  const current = nodeVersion.match(/^v?(\d+)\.(\d+)\.(\d+)$/);
  const pinned = manifest.packageManager.match(/^bun@(\d+\.\d+\.\d+)$/)?.[1];
  if (!minimum || !current || !pinned || manifest.engines.bun !== pinned)
    throw new Error("工具鏈版本格式不符；請核對 package.json");
  const compare = (value) => value.slice(1).map(Number);
  const lower = compare(minimum);
  const actual = compare(current);
  const different = actual.findIndex((value, index) => value !== lower[index]);
  if (different >= 0 && actual[different] < lower[different])
    throw new Error(`需要 Node.js ${manifest.engines.node}，不自動安裝或升級`);
  if (bunVersion !== pinned)
    throw new Error(`需要 Bun ${pinned}，不自動安裝或升級`);
}

export function frontendCommand(action, args = []) {
  if (action === "install" && !args.length)
    return [
      "bun",
      [
        "install",
        "--frozen-lockfile",
        "--ignore-scripts",
        "--linker",
        "hoisted",
      ],
    ];
  if (
    action === "run" &&
    args.length === 1 &&
    Object.hasOwn(manifest.scripts, args[0])
  )
    return ["bun", ["run", args[0]]];
  if (action === "typecheck")
    return [
      process.execPath,
      [path.join(directory, "node_modules/vue-tsc/bin/vue-tsc.js"), ...args],
    ];
  const tools = {
    prettier: "prettier/bin/prettier.cjs",
    eslint: "eslint/bin/eslint.js",
  };
  if (action === "tool" && Object.hasOwn(tools, args[0]))
    return [
      process.execPath,
      [path.join(directory, "node_modules", tools[args[0]]), ...args.slice(1)],
    ];
  throw new Error(
    "請使用 install、run 腳本名稱、typecheck 參數，或 tool 工具名稱",
  );
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    if (process.versions.bun)
      throw new Error(
        "此入口必須由 Node.js 執行；不要使用 --bun 強制替換 Runtime",
      );
    const version = spawnSync("bun", ["--version"], { encoding: "utf8" });
    if (version.error || version.status !== 0)
      throw new Error("找不到可用的 Bun；不自動安裝或升級");
    validateToolchain(process.version, version.stdout.trim());
    const action = process.argv[2];
    const [command, args] = frontendCommand(action, process.argv.slice(3));
    const result = spawnSync(command, args, {
      cwd: action === "tool" ? path.resolve(directory, "..") : directory,
      stdio: "inherit",
    });
    if (result.error) throw result.error;
    process.exitCode = result.status ?? 1;
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
