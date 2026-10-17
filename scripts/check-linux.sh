#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
[[ -f frontend/dist/index.html ]] || node scripts/frontend.mjs run build
go test ./internal/... ./cmd/... ./frontend
go vet ./internal/... ./cmd/... ./frontend
[[ -z "$(gofmt -l internal cmd frontend/assets.go)" ]] || { echo 'Go 程式碼尚未格式化' >&2; exit 1; }
for task in format:check format:bindings:check lint typecheck test build; do
  node scripts/frontend.mjs run "$task"
done
node scripts/frontend.mjs tool prettier --check scripts/*.mjs
node scripts/frontend.mjs tool eslint --config scripts/eslint.config.mjs scripts/*.mjs
node --test scripts/frontend.test.mjs scripts/licenses.test.mjs scripts/release.test.mjs
