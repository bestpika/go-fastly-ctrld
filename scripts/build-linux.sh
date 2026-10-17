#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 CGO_ENABLED=1
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || { echo '需要 Linux x86_64 原生建置環境' >&2; exit 1; }
pkg-config --exists gtk4 webkitgtk-6.0 || { echo '缺少 GTK4／WebKitGTK 6.0 開發套件；不自動安裝' >&2; exit 1; }
if [[ $# == 0 ]]; then
  exec node scripts/release.mjs linux-amd64
fi
[[ $# == 1 && "$1" == --package-only ]] || { echo '版號沿用 VERSION；升版另執行明確命令，不接受打包參數' >&2; exit 1; }
node scripts/release.mjs --check linux-amd64
version="$FASTLY_BUILD_VERSION"
node scripts/frontend.mjs install
node scripts/frontend.mjs run build
name="go-fastly-ctrld-$version-linux-amd64"
package="$FASTLY_PACKAGE_DIR"
[[ ! -e "$package" && ! -e "$package.tar.gz" ]] || { echo '產物已存在，不自動覆寫或刪除' >&2; exit 1; }
mkdir -p "$package"
go build -tags production -trimpath -ldflags "-s -w -X github.com/bestpika/go-fastly-ctrld/internal/version.Version=$version" -o "$package/go-fastly-ctrld" ./cmd/go-fastly-ctrld
node scripts/licenses.mjs --release "$package"
tar -czf "$package.tar.gz" -C "$(dirname "$package")" "$name"
(cd "$(dirname "$package")" && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
echo "Linux 套件：$package.tar.gz"
