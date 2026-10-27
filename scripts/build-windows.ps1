$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    pnpm --dir web install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw 'Web dependency installation failed' }
    pnpm --dir web test
    if ($LASTEXITCODE -ne 0) { throw 'Web tests failed' }
    pnpm --dir web build
    if ($LASTEXITCODE -ne 0) { throw 'Web build failed' }
    pnpm --dir status-ui install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw 'Status UI dependency installation failed' }
    pnpm --dir status-ui test
    if ($LASTEXITCODE -ne 0) { throw 'Status UI tests failed' }
    pnpm --dir status-ui build
    if ($LASTEXITCODE -ne 0) { throw 'Status UI build failed' }
    New-Item -ItemType Directory -Force internal/assets/dist | Out-Null
    Remove-Item -Recurse -Force internal/assets/dist/web, internal/assets/dist/status -ErrorAction SilentlyContinue
    Copy-Item -Recurse -Force web/dist internal/assets/dist/web
    Copy-Item -Recurse -Force status-ui/dist internal/assets/dist/status
    go build -o soloqueue.exe ./cmd/soloqueue
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
} finally {
    Pop-Location
}
