#Requires -Version 5.1
param(
    [string]$Version
)

$repo = "samcharles93/tau"
$baseUrl = "https://github.com/${repo}/releases"

# Supported ARCH values and the "tau_{version}_windows_{arch}.zip" archive
# name below must match internal/updater/targets.go's SupportedTargets()/
# ArchiveName() exactly -- that's the canonical source. This script can't
# import Go code (it runs before Go is even on the machine), so keep the
# two in sync by hand whenever the platform matrix changes.

# ---------- detect arch ----------
$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture) {
    "X64"    { "amd64" }
    "Arm64"  { "arm64" }
    default  {
        Write-Host "Error: unsupported architecture: $_" -ForegroundColor Red
        exit 1
    }
}

$platform = "windows_${arch}"

# ---------- resolve version ----------
if (-not $Version) {
    Write-Host "Fetching latest release..." -ForegroundColor Cyan
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/${repo}/releases/latest"
        $Version = $release.tag_name
    } catch {
        Write-Host "Error: could not fetch latest release" -ForegroundColor Red
        exit 1
    }
}

$archive = "tau_$($Version -replace '^v','')_$platform.zip"
$downloadUrl = "${baseUrl}/download/${Version}/${archive}"

# ---------- download & extract ----------
$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "tau-install-$(Get-Random)") -Force

try {
    Write-Host "Downloading tau ${Version} (${platform})..." -ForegroundColor Cyan
    $archivePath = Join-Path $tmp $archive
    Invoke-WebRequest -Uri $downloadUrl -OutFile $archivePath

    Expand-Archive -Path $archivePath -DestinationPath $tmp -Force
    $binary = Join-Path $tmp "tau.exe"

    # ---------- install ----------
    $installDir = Join-Path $env:LOCALAPPDATA "Programs\tau"
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
    Copy-Item -Path $binary -Destination (Join-Path $installDir "tau.exe") -Force

    Write-Host ""
    Write-Host "tau ${Version} installed to ${installDir}\tau.exe" -ForegroundColor Green

    # ---------- PATH ----------
    $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($userPath -notlike "*$installDir*") {
        [Environment]::SetEnvironmentVariable("PATH", "$userPath;$installDir", "User")
        $env:PATH = "$env:PATH;$installDir"
        Write-Host ""
        Write-Host "Note: ${installDir} added to your user PATH." -ForegroundColor Yellow
        Write-Host "  Restart your terminal for PATH changes to take effect." -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "  Run 'tau --help' to get started." -ForegroundColor White

} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
