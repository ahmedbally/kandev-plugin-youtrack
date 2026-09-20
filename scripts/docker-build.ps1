# scripts/docker-build.ps1
# Build kandev-plugin-youtrack entirely inside Docker — no local Go needed.
# Produces ./build-out/ with the unpacked package (manifest, README, ui bundle,
# platform binaries) and ./dist/kandev-plugin-youtrack-<version>.tar.gz — the
# same installable package the release workflow attaches to a GitHub release.
#
# The final Docker stage is FROM scratch, so artifacts are exported with
# BuildKit --output instead of docker create/cp (which requires a command).

[CmdletBinding()]
param(
    [string]$Tag = "kandev-plugin-youtrack:build",
    [string]$BuildDir = "",
    [string]$DistDir = "",
    [switch]$NoExport
)

$ErrorActionPreference = "Stop"

$pluginRoot = Split-Path -Parent $PSScriptRoot
if (-not $BuildDir) { $BuildDir = Join-Path $pluginRoot "build-out" }
if (-not $DistDir) { $DistDir = Join-Path $pluginRoot "dist" }

$contextRoot = Resolve-Path (Join-Path $pluginRoot "..")
$dockerfile = Join-Path $pluginRoot "Dockerfile"

# The versioned tarball name comes from the manifest.
$manifest = Get-Content (Join-Path $pluginRoot "manifest.yaml") -Raw
$version = [regex]::Match($manifest, '(?m)^version:\s*"([^"]+)"').Groups[1].Value
if (-not $version) { throw "Could not read version from manifest.yaml" }
$tarName = "kandev-plugin-youtrack-$version.tar.gz"

New-Item -ItemType Directory -Force -Path $BuildDir, $DistDir | Out-Null

Write-Host "==> Docker context: $contextRoot"
Write-Host "==> Dockerfile:     $dockerfile"
Write-Host "==> Image tag:      $Tag"
Write-Host "==> Build output:   $BuildDir"
Write-Host "==> Tarball:        $DistDir\$tarName"

# --output exports the scratch stage's filesystem to $BuildDir; -t also tags
# the image for inspection.
docker build -t $Tag --output $BuildDir -f $dockerfile $contextRoot
if ($LASTEXITCODE -ne 0) { throw "Docker build failed" }

if ($NoExport) {
    Write-Host "==> Build complete (-NoExport, skipping staging)"
    return
}

if (-not (Test-Path (Join-Path $BuildDir $tarName))) {
    throw "Build output did not contain $tarName"
}
Move-Item -Force (Join-Path $BuildDir $tarName) (Join-Path $DistDir $tarName)

Write-Host "==> Unpacked package staged in $BuildDir"
Get-ChildItem -Recurse $BuildDir | Select-Object FullName, Length | Format-Table -AutoSize
Write-Host "==> Installable package: $DistDir\$tarName"
Get-ChildItem $DistDir | Select-Object Name, Length | Format-Table -AutoSize
