<#
.SYNOPSIS
  Rebase the WB Stream owner-mode patch onto the latest upstream olcrtc.

.DESCRIPTION
  The owner-mode change lives as a single commit on the `owner-mode` branch
  (see OlcRTC.WBStream.OwnerMode.patch.md). This script pulls the newest
  upstream release and replays that commit on top of it, then builds and
  tests so you immediately know whether the rebase is clean.

  Workflow it automates:
    git fetch upstream
    git checkout owner-mode
    git rebase upstream/master
    go build ./... ; go test ./...

  If the rebase hits a conflict it stops and tells you which files to fix.
  Resolve them, then run:  git add <files>; git rebase --continue
  Or bail out entirely with: git rebase --abort

.EXAMPLE
  ./script/update-owner-mode.ps1
#>
[CmdletBinding()]
param(
    [string]$Upstream = "upstream",
    [string]$UpstreamBranch = "master",
    [string]$Branch = "owner-mode"
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

function Fail($msg) { Write-Host "ERROR: $msg" -ForegroundColor Red; exit 1 }

# Refuse to run on a dirty tree - a rebase would clobber uncommitted work.
if (git status --porcelain) {
    Fail "Working tree is not clean. Commit or stash your changes first."
}

Write-Host "Fetching $Upstream ..." -ForegroundColor Cyan
git fetch $Upstream --prune
if ($LASTEXITCODE -ne 0) { Fail "git fetch failed." }

git checkout $Branch
if ($LASTEXITCODE -ne 0) { Fail "could not checkout '$Branch'." }

$before = git rev-parse "$Upstream/$UpstreamBranch"
Write-Host "Rebasing '$Branch' onto $Upstream/$UpstreamBranch ($before) ..." -ForegroundColor Cyan
git rebase "$Upstream/$UpstreamBranch"
if ($LASTEXITCODE -ne 0) {
    Write-Host ""
    Write-Host "Rebase stopped on a conflict. Conflicting files:" -ForegroundColor Yellow
    git diff --name-only --diff-filter=U
    Write-Host ""
    Write-Host "Fix them, then:  git add <files>; git rebase --continue" -ForegroundColor Yellow
    Write-Host "Or abort with:   git rebase --abort" -ForegroundColor Yellow
    exit 1
}

Write-Host "Rebase clean. Building ..." -ForegroundColor Cyan
go build ./...
if ($LASTEXITCODE -ne 0) { Fail "go build failed - the upstream changes likely shifted the patch. Inspect the errors above." }

Write-Host "Running tests ..." -ForegroundColor Cyan
go test ./...
if ($LASTEXITCODE -ne 0) { Fail "go test failed - inspect the failures above." }

Write-Host ""
Write-Host "owner-mode is up to date with $Upstream/$UpstreamBranch and green." -ForegroundColor Green
Write-Host "Push it to your fork when ready:  git push --force-with-lease origin $Branch" -ForegroundColor Green
