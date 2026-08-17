# scripts/docker-build.ps1
# Build kandev-plugin-youtrack entirely inside Docker — no local Go needed.
# Produces ./server/ with all 5 platform binaries + manifest + ui bundle,
# ready for `kandev plugin-pack .`

[CmdletBinding()]
param(
    [string]$Tag = "kandev-plugin-youtrack:build",
    [string]$OutputDir = (Join-Path $PSScriptRoot ".." "server" | Resolve-Path -ErrorAction SilentlyContinue),
    [switch]$NoExport
)

$ErrorActionPreference = "Stop"

$contextRoot = Resolve-Path (Join-Path $PSScriptRoot ".." "..")
$dockerfile = Join-Path $contextRoot "kandev-plugin-youtrack" "Dockerfile"

Write-Host "==> Docker context: $contextRoot"
Write-Host "==> Dockerfile:     $dockerfile"
Write-Host "==> Image tag:      $Tag"

docker build -t $Tag -f $dockerfile $contextRoot
if ($LASTEXITCODE -ne 0) { throw "Docker build failed" }

if ($NoExport) {
    Write-Host "==> Build complete (-NoExport, skipping extraction)"
    return
}

if (-not $OutputDir) {
    $OutputDir = Join-Path $PSScriptRoot ".." "server"
    New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
    $OutputDir = (Resolve-Path $OutputDir).Path
}

Write-Host "==> Extracting built artifacts to $OutputDir"

$container = "yt-export-" + [guid]::NewGuid().ToString("N").Substring(0,8)
docker create --name $container $Tag | Out-Null
try {
    foreach ($item in @("server", "manifest.yaml", "ui", "README.md")) {
        docker cp "${container}:/$item" $OutputDir 2>$null
    }
    Write-Host "==> Artifacts extracted to $OutputDir"
    Get-ChildItem -Recurse $OutputDir | Select-Object Name, Length | Format-Table -AutoSize
} finally {
    docker rm $container | Out-Null
}