import fs from "node:fs";
import { createHash } from "node:crypto";
import { gunzipSync, inflateRawSync } from "node:zlib";

const maximum = 256 * 1024 * 1024;
const digest = (bytes) => createHash("sha256").update(bytes).digest("hex");
const crcTable = Array.from({ length: 256 }, (_, value) => {
  for (let bit = 0; bit < 8; bit++)
    value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
  return value >>> 0;
});
export function crc32(bytes) {
  let result = 0xffffffff;
  for (const byte of bytes)
    result = crcTable[(result ^ byte) & 255] ^ (result >>> 8);
  return (result ^ 0xffffffff) >>> 0;
}

export function archiveFiles(filename, platform, folder) {
  if (!fs.lstatSync(filename).isFile() || fs.statSync(filename).size > maximum)
    throw new Error("壓縮檔路徑或大小無效");
  const archive = fs.readFileSync(filename);
  const files = {};
  const add = (name, bytes) => {
    if (
      !/^(go-fastly-ctrld(?:\.exe)?|README\.md|LICENSE|THIRD_PARTY_NOTICES\.txt)$/.test(
        name,
      ) ||
      Object.hasOwn(files, name)
    )
      throw new Error("壓縮檔包含額外、重複或不安全檔名");
    files[name] = digest(bytes);
  };
  if (platform.startsWith("windows-")) {
    let end = -1;
    for (
      let i = archive.length - 22;
      i >= Math.max(0, archive.length - 65557);
      i--
    ) {
      if (
        archive.readUInt32LE(i) === 0x06054b50 &&
        i + 22 + archive.readUInt16LE(i + 20) === archive.length
      ) {
        end = i;
        break;
      }
    }
    if (
      end < 0 ||
      archive.readUInt16LE(end + 4) ||
      archive.readUInt16LE(end + 6) ||
      archive.readUInt16LE(end + 8) !== 4 ||
      archive.readUInt16LE(end + 10) !== 4
    )
      throw new Error("ZIP 結構或檔案數無效");
    let cursor = archive.readUInt32LE(end + 16);
    if (cursor + archive.readUInt32LE(end + 12) !== end)
      throw new Error("ZIP 目錄不完整");
    for (let count = 0; count < 4; count++) {
      if (cursor + 46 > end || archive.readUInt32LE(cursor) !== 0x02014b50)
        throw new Error("ZIP 項目無效");
      const flags = archive.readUInt16LE(cursor + 8),
        method = archive.readUInt16LE(cursor + 10),
        checksum = archive.readUInt32LE(cursor + 16);
      const compressed = archive.readUInt32LE(cursor + 20),
        size = archive.readUInt32LE(cursor + 24),
        nameLength = archive.readUInt16LE(cursor + 28);
      const name = archive
          .subarray(cursor + 46, cursor + 46 + nameLength)
          .toString("utf8"),
        local = archive.readUInt32LE(cursor + 42);
      if (
        flags & 1 ||
        ![0, 8].includes(method) ||
        size > maximum ||
        local + 30 > cursor ||
        archive.readUInt32LE(local) !== 0x04034b50
      )
        throw new Error("ZIP 壓縮方法或內容無效");
      if (
        archive.readUInt16LE(local + 6) !== flags ||
        archive.readUInt16LE(local + 8) !== method
      )
        throw new Error("ZIP 本機標頭與目錄不一致");
      const start =
        local +
        30 +
        archive.readUInt16LE(local + 26) +
        archive.readUInt16LE(local + 28);
      if (
        start + compressed > cursor ||
        archive
          .subarray(local + 30, local + 30 + archive.readUInt16LE(local + 26))
          .toString("utf8") !== name
      )
        throw new Error("ZIP 檔名或範圍不符");
      const bytes =
        method === 0
          ? archive.subarray(start, start + compressed)
          : inflateRawSync(archive.subarray(start, start + compressed), {
              maxOutputLength: Math.max(1, size),
            });
      if (bytes.length !== size || crc32(bytes) !== checksum)
        throw new Error("ZIP 檔案校驗失敗");
      add(name, bytes);
      cursor +=
        46 +
        nameLength +
        archive.readUInt16LE(cursor + 30) +
        archive.readUInt16LE(cursor + 32);
    }
    if (cursor !== end) throw new Error("ZIP 目錄尾端無效");
  } else {
    const bytes = gunzipSync(archive, { maxOutputLength: maximum });
    let cursor = 0;
    while (
      cursor + 512 <= bytes.length &&
      bytes.subarray(cursor, cursor + 512).some((b) => b !== 0)
    ) {
      const header = bytes.subarray(cursor, cursor + 512);
      const field = (start, length) =>
        header
          .subarray(start, start + length)
          .toString("utf8")
          .split("\0")[0];
      const name = field(0, 100),
        prefix = field(345, 155),
        length = field(124, 12).trim(),
        type = field(156, 1);
      const checksum = field(148, 8).trim();
      const actual = [...header].reduce(
        (sum, value, index) => sum + (index >= 148 && index < 156 ? 32 : value),
        0,
      );
      if (!/^[0-7]+$/.test(checksum) || Number.parseInt(checksum, 8) !== actual)
        throw new Error("TAR 檔頭校驗失敗");
      if (prefix || !/^[0-7]+$/.test(length))
        throw new Error("TAR 格式不支援或無效");
      const size = Number.parseInt(length, 8),
        start = cursor + 512;
      if (!Number.isSafeInteger(size) || start + size > bytes.length)
        throw new Error("TAR 內容不完整");
      if (type === "5" && name === folder + "/" && size === 0) {
        /* 只允許套件根目錄。 */
      } else if ((type === "0" || type === "") && name.startsWith(folder + "/"))
        add(name.slice(folder.length + 1), bytes.subarray(start, start + size));
      else throw new Error("TAR 包含額外路徑或連結");
      cursor = start + Math.ceil(size / 512) * 512;
    }
    if (
      bytes.subarray(cursor).some((b) => b !== 0) ||
      bytes.length - cursor < 1024
    )
      throw new Error("TAR 結尾無效");
  }
  return files;
}
