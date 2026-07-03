param(
    [Parameter(Position = 0)]
    [ValidateSet(
        "build", "build-web", "build-desktop", "build-all",
        "build-go", "build-go-linux", "build-go-mac",
        "package-desktop", "clean"
    )]
    [string]$Target = "build"
)

$ErrorActionPreference = "Stop"

function Build-Web {
    Write-Host "=== Building web portal ==="
    Push-Location portal
    pnpm approve-builds esbuild
    pnpm install
    pnpm build
    Pop-Location
    if (Test-Path internal/server/dist) { Remove-Item -Recurse -Force internal/server/dist }
    Copy-Item -Recurse portal/dist internal/server/dist
    Copy-Item -Recurse skills internal/server/dist/skills
}

function Build-Desktop {
    Write-Host "=== Building desktop UI ==="
    Push-Location desktop
    pnpm approve-builds
    pnpm install
    pnpm build
    Pop-Location
}

function Build-Go {
    Write-Host "=== Building Go binary ==="
    go build -o soloqueue.exe ./cmd/soloqueue
}

function Build-Go-Linux {
    Write-Host "=== Building Go binary for Linux ==="
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -o soloqueue ./cmd/soloqueue
}

function Build-Go-Mac {
    Write-Host "=== Building Go binary for macOS ==="
    $env:GOOS = "darwin"
    $env:GOARCH = "amd64"
    go build -o soloqueue ./cmd/soloqueue
}

function Package-Desktop {
    Write-Host "=== Packaging desktop client ==="
    Push-Location desktop
    pnpm run package -- --win
    Pop-Location
    Write-Host "Installer: desktop/dist-desktop/"
}

function Clean {
    Write-Host "=== Cleaning ==="
    if (Test-Path soloqueue) { Remove-Item soloqueue }
    if (Test-Path soloqueue.exe) { Remove-Item soloqueue.exe }
    if (Test-Path desktop/dist) { Remove-Item -Recurse -Force desktop/dist }
    if (Test-Path desktop/dist-desktop) { Remove-Item -Recurse -Force desktop/dist-desktop }
    if (Test-Path portal/dist) { Remove-Item -Recurse -Force portal/dist }
    if (Test-Path internal/server/dist) { Remove-Item -Recurse -Force internal/server/dist }
}

switch ($Target) {
    "build-web"       { Build-Web }
    "build-desktop"   { Build-Desktop }
    "build-go"        { Build-Go }
    "build-go-linux"  { Build-Go-Linux }
    "build-go-mac"    { Build-Go-Mac }
    "build"           { Build-Web; Build-Go }
    "build-all"       { Build-Web; Build-Go; Build-Desktop; Package-Desktop }
    "package-desktop" { Build-Web; Build-Go; Build-Desktop; Package-Desktop }
    "clean"           { Clean }
}
