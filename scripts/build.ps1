param(
    [Parameter(Position = 0)]
    [ValidateSet("build", "build-web", "build-status", "build-assets", "build-go", "build-win", "start", "web", "clean")]
    [string]$Target = "build"
)

$ErrorActionPreference = "Stop"

function Build-Web {
    Push-Location web
    pnpm install --frozen-lockfile
    pnpm test
    pnpm build
    Pop-Location
    if (Test-Path internal/assets/dist/web) { Remove-Item -Recurse -Force internal/assets/dist/web }
    Copy-Item -Recurse web/dist internal/assets/dist/web
}

function Build-Status {
    Push-Location status-ui
    pnpm install --frozen-lockfile
    pnpm test
    pnpm build
    Pop-Location
    if (Test-Path internal/assets/dist/status) { Remove-Item -Recurse -Force internal/assets/dist/status }
    Copy-Item -Recurse status-ui/dist internal/assets/dist/status
}

function Build-Assets {
    Build-Web
    Build-Status
    if (Test-Path internal/assets/dist/skills) { Remove-Item -Recurse -Force internal/assets/dist/skills }
    Copy-Item -Recurse skills internal/assets/dist/skills
}

function Build-Go { go build -ldflags="-s -w" -o soloqueue.exe ./cmd/soloqueue }
function Build-Win { $env:GOOS = "windows"; $env:GOARCH = "amd64"; go build -ldflags="-s -w" -o soloqueue.exe ./cmd/soloqueue }
function Clean {
    foreach ($path in @("soloqueue", "soloqueue.exe", "web/dist", "status-ui/dist", "internal/assets/dist/web", "internal/assets/dist/status", "internal/assets/dist/skills")) {
        if (Test-Path $path) { Remove-Item -Recurse -Force $path }
    }
}

switch ($Target) {
    "build-web" { Build-Web }
    "build-status" { Build-Status }
    "build-assets" { Build-Assets }
    "build-go" { Build-Go }
    "build-win" { Build-Assets; Build-Win }
    "build" { Build-Assets; Build-Go }
    "start" { Build-Assets; Build-Go; .\soloqueue.exe start }
    "web" { Build-Web; Build-Go; .\soloqueue.exe web }
    "clean" { Clean }
}
