$ErrorActionPreference = 'Stop'
$env:GOTOOLCHAIN = 'local'
$root = Split-Path $PSScriptRoot -Parent
$resource = Join-Path $root 'cmd/go-fastly-ctrld/appicon_windows_amd64.syso'
$temporary = Join-Path $root ('bin/icon-' + [guid]::NewGuid().ToString('N'))
$reserved = $false
$completed = $false
Push-Location $root
try {
    if (-not $IsWindows) { throw 'Windows 圖示資源須在 Windows 原生環境產生' }
    # 原子保留本次產物，拒絕覆寫使用者或其他建置留下的資源。
    $file = [IO.File]::Open($resource, [IO.FileMode]::CreateNew)
    $file.Dispose()
    $reserved = $true
    New-Item -ItemType Directory -Path $temporary | Out-Null
    $icon = Join-Path $temporary 'application.ico'
    go run ./cmd/appicon -out $icon
    if ($LASTEXITCODE -ne 0) { throw '應用程式圖示產生失敗' }
    go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.28 generate syso -icon $icon -manifest build/windows/application.manifest -out $resource -arch amd64 | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'Windows 圖示資源產生失敗' }
    $completed = $true
    Write-Output $resource
} finally {
    if ($reserved -and -not $completed -and (Test-Path $resource)) { Remove-Item -LiteralPath $resource }
    if (Test-Path "$temporary/application.ico") { Remove-Item -LiteralPath "$temporary/application.ico" }
    if (Test-Path $temporary) { Remove-Item -LiteralPath $temporary }
    Pop-Location
}
