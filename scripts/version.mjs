import fs from "node:fs";
import { randomBytes } from "node:crypto";

export function taiwanDay(now = new Date()) {
  if (!Number.isFinite(now.getTime())) throw new Error("日期無效");
  const local = new Date(now.getTime() + 8 * 60 * 60 * 1000);
  const year = local.getUTCFullYear();
  if (year < 1000 || year > 9999) throw new Error("年份超出四碼範圍");
  const day =
    1 +
    Math.floor(
      (Date.UTC(year, local.getUTCMonth(), local.getUTCDate()) -
        Date.UTC(year, 0, 1)) /
        86400000,
    );
  return `${year}.${day}`;
}

export function validVersion(value) {
  if (
    typeof value !== "string" ||
    !/^[1-9]\d{3}\.[1-9]\d{0,2}\.[1-9]\d*$/.test(value)
  )
    return false;
  const [year, day, number] = value.split(".").map(Number);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  return day <= (leap ? 366 : 365) && Number.isSafeInteger(number);
}

export function buildVersion(value = process.env.FASTLY_BUILD_VERSION) {
  if (!value) return "development";
  if (!validVersion(value))
    throw new Error("正式版號須為四碼年.天序.當日升版序號");
  return value;
}

export function nextVersion(current, now = new Date()) {
  if (!validVersion(current)) throw new Error("目前版號無效，不升版");
  const day = taiwanDay(now);
  const [year, ordinal, number] = current.split(".").map(Number);
  const [todayYear, todayOrdinal] = day.split(".").map(Number);
  if (todayYear < year || (todayYear === year && todayOrdinal < ordinal))
    throw new Error("臺灣日期早於目前版本，不自動降版");
  const next = current.startsWith(day + ".") ? number + 1 : 1;
  const version = `${day}.${next}`;
  if (!validVersion(version)) throw new Error("升版序號超出安全範圍");
  return version;
}

export function readVersion(filename) {
  const stat = fs.lstatSync(filename);
  if (!stat.isFile() || stat.size > 128)
    throw new Error("版本檔必須是一般小型文字檔");
  const version = fs.readFileSync(filename, "utf8").trim();
  if (!validVersion(version)) throw new Error("版本檔無效，未自動配置版號");
  return version;
}

// 呼叫端持有發布鎖；僅明確升版命令使用，一般打包只讀版本檔。
export function advanceVersion(filename, now = new Date()) {
  const version = nextVersion(readVersion(filename), now);
  const temporary = filename + "." + randomBytes(16).toString("hex") + ".tmp";
  const fd = fs.openSync(temporary, "wx", 0o600);
  try {
    fs.writeFileSync(fd, version + "\n", "utf8");
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  fs.renameSync(temporary, filename);
  return version;
}
