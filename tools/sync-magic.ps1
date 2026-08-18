[CmdletBinding()]
param(
    [string]$UpstreamTag = 'latest',
    [string]$MagicBranch = 'magic',
    [switch]$Push
)

$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repoRoot

if (-not (Test-Path -LiteralPath (Join-Path $repoRoot '.git'))) {
    throw 'This command must run from a Git clone. See MAGIC_SYNC.md for the one-time setup.'
}

if ((git status --porcelain).Count -ne 0) {
    throw 'The working tree is not clean. Commit or stash local changes before syncing.'
}

$currentBranch = (git branch --show-current).Trim()
if ($currentBranch -ne $MagicBranch) {
    throw "Checkout the '$MagicBranch' branch before syncing. Current branch: '$currentBranch'."
}

if (-not (git remote get-url upstream 2>$null)) {
    git remote add upstream https://github.com/Wei-Shaw/sub2api.git
}

git fetch upstream --tags --prune
if ($LASTEXITCODE -ne 0) {
    throw 'Failed to fetch the upstream repository.'
}

if ($UpstreamTag -eq 'latest') {
    $UpstreamTag = git tag --list 'v*' --sort=-v:refname | Select-Object -First 1
}
$UpstreamTag = $UpstreamTag.Trim()
if (-not $UpstreamTag -or -not (git rev-parse --verify "$UpstreamTag^{commit}" 2>$null)) {
    throw "Upstream tag '$UpstreamTag' was not found."
}

git merge-base --is-ancestor "$UpstreamTag^{commit}" HEAD
if ($LASTEXITCODE -eq 0) {
    Write-Host "Already contains $UpstreamTag. Nothing to sync."
    exit 0
}

git config rerere.enabled true
git merge --no-ff --no-commit "$UpstreamTag^{commit}"
if ($LASTEXITCODE -ne 0) {
    $conflicts = git diff --name-only --diff-filter=U
    Write-Host 'Upstream merge stopped on conflicts:' -ForegroundColor Yellow
    $conflicts | ForEach-Object { Write-Host "  $_" -ForegroundColor Yellow }
    Write-Host 'Resolve the files, then rerun the checks documented in MAGIC_SYNC.md.'
    exit 1
}

$version = $UpstreamTag.TrimStart('v')
[System.IO.File]::WriteAllText(
    (Join-Path $repoRoot 'backend\cmd\server\VERSION'),
    "$version`n",
    [System.Text.UTF8Encoding]::new($false)
)

Push-Location (Join-Path $repoRoot 'frontend')
try {
    corepack pnpm install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw 'pnpm install failed.' }
    corepack pnpm test:run
    if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed.' }
    corepack pnpm lint:check
    if ($LASTEXITCODE -ne 0) { throw 'Frontend lint failed.' }
    corepack pnpm build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
}
finally {
    Pop-Location
}

Push-Location (Join-Path $repoRoot 'backend')
try {
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Backend tests failed.' }
}
finally {
    Pop-Location
}

git add --all
git commit -m "chore(magic): sync upstream $UpstreamTag"
if ($LASTEXITCODE -ne 0) {
    throw 'Failed to create the upstream sync commit.'
}

if ($Push) {
    git push origin "HEAD:$MagicBranch"
    if ($LASTEXITCODE -ne 0) { throw 'Failed to push the magic branch.' }
}

Write-Host "Magic branch synced to $UpstreamTag."
Write-Host "Commit: $(git rev-parse --short HEAD)"
if (-not $Push) {
    Write-Host "Review the result, then run: git push origin HEAD:$MagicBranch"
}
