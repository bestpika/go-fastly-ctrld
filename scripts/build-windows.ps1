param([switch]$PackageOnly)
$ErrorActionPreference = 'Stop'
$env:GOTOOLCHAIN = 'local'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    if (-not $IsWindows) { throw 'Windows 套件須在 Windows 原生環境建置' }
    if (-not $PackageOnly) {
        node scripts/release.mjs windows-amd64
        if ($LASTEXITCODE -ne 0) { throw '打包未完成；VERSION 不自動變更' }
        return
    }
    node scripts/release.mjs --check windows-amd64
    if ($LASTEXITCODE -ne 0) { throw '發布程序驗證失敗' }
    $Version = $env:FASTLY_BUILD_VERSION
    Push-Location frontend
    try {
        node ../scripts/frontend.mjs install
        if ($LASTEXITCODE -ne 0) { throw '鎖檔安裝失敗' }
        node ../scripts/frontend.mjs run build
        if ($LASTEXITCODE -ne 0) { throw '前端建置失敗' }
    } finally { Pop-Location }
    $name = "go-fastly-ctrld-$Version-windows-amd64"
    $package = $env:FASTLY_PACKAGE_DIR
    $archive = "$package.zip"
    if ((Test-Path $package) -or (Test-Path $archive)) { throw '本次交易暫存產物已存在，請核對中斷作業；不自動刪除或升版' }
    New-Item -ItemType Directory -Path $package | Out-Null
    $resource = & "$PSScriptRoot/prepare-windows-icon.ps1"
    try {
        go build -tags production -trimpath -ldflags "-s -w -H windowsgui -X github.com/bestpika/go-fastly-ctrld/internal/version.Version=$Version" -o "$package/go-fastly-ctrld.exe" ./cmd/go-fastly-ctrld
        if ($LASTEXITCODE -ne 0) { throw 'Windows 原生建置失敗' }
    } finally { Remove-Item -LiteralPath $resource }
    node scripts/licenses.mjs --release "$package"
    if ($LASTEXITCODE -ne 0) { throw '原始授權保存失敗，不產生套件' }
    Compress-Archive -Path "$package/*" -DestinationPath $archive
    $hash = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText("$archive.sha256", "$hash  $name.zip`n", [Text.UTF8Encoding]::new($false))
    Write-Output "Windows 套件：$archive"
} finally { Pop-Location }
