$ErrorActionPreference = 'Stop'
$env:GOTOOLCHAIN = 'local'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    if (-not (Test-Path './frontend/dist/index.html')) {
        Push-Location frontend
        try {
            node ../scripts/frontend.mjs run build
            if ($LASTEXITCODE -ne 0) { throw '前端初始建置失敗' }
        } finally { Pop-Location }
    }
    go test ./internal/... ./cmd/... ./frontend
    if ($LASTEXITCODE -ne 0) { throw 'Go 測試失敗' }
    go vet ./internal/... ./cmd/... ./frontend
    if ($LASTEXITCODE -ne 0) { throw 'Go 靜態檢查失敗' }
    $paths = @('./internal', './frontend/assets.go')
    if (Test-Path './cmd') { $paths += './cmd' }
    $unformatted = gofmt -l @paths
    if ($LASTEXITCODE -ne 0 -or $unformatted) { throw 'Go 格式檢查失敗' }
    Push-Location frontend
    try {
        foreach ($task in @('format:check', 'format:bindings:check', 'lint', 'typecheck', 'test', 'build')) {
            node ../scripts/frontend.mjs run $task
            if ($LASTEXITCODE -ne 0) { throw "前端 $task 失敗" }
        }
    } finally { Pop-Location }
    node scripts/frontend.mjs tool prettier --check scripts/*.mjs
    if ($LASTEXITCODE -ne 0) { throw '打包工具格式檢查失敗' }
    node scripts/frontend.mjs tool eslint --config scripts/eslint.config.mjs scripts/*.mjs
    if ($LASTEXITCODE -ne 0) { throw '打包工具靜態檢查失敗' }
    node --test scripts/frontend.test.mjs scripts/licenses.test.mjs scripts/release.test.mjs
    if ($LASTEXITCODE -ne 0) { throw '授權與精簡套件測試失敗' }
} finally { Pop-Location }
