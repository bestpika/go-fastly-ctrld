$ErrorActionPreference = 'Stop'
$env:GOTOOLCHAIN = 'local'
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.28 generate bindings -ts -i -d frontend/bindings ./cmd/go-fastly-ctrld
    if ($LASTEXITCODE -ne 0) { throw '固定版本綁定產生失敗' }
    Push-Location frontend
    try {
        node ../scripts/frontend.mjs run format:bindings
        if ($LASTEXITCODE -ne 0) { throw '綁定格式化失敗' }
    } finally { Pop-Location }
} finally { Pop-Location }
