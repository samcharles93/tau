#Requires -Version 5.1
param(
    [string]$Version
)

$repo = "samcharles93/tau"
$baseUrl = if ($env:TAU_BASE_URL) { $env:TAU_BASE_URL } else { "https://github.com/${repo}/releases" }

# Supported ARCH values and the "tau_{version}_windows_{arch}.zip" archive
# name below must match internal/updater/targets.go's SupportedTargets()/
# ArchiveName() exactly -- that's the canonical source. This script can't
# import Go code (it runs before Go is even on the machine), so keep the
# two in sync by hand whenever the platform matrix changes.

function Get-TauSha256 {
    [CmdletBinding()]
    param([Parameter(Mandatory)][string]$Path)
    return (Get-FileHash -Path $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

# Test-TauChecksum checks that $ArchivePath's SHA-256 matches the entry for
# $ArchiveName in the checksums.txt at $ChecksumsPath. It never touches
# anything outside those two paths -- callers are responsible for only
# replacing an installed binary after this returns $true. Errors go to the
# error stream (not Write-Host) so they don't get lost when this is called
# non-interactively, and so they can't pollute the $true/$false return
# value the way Write-Output would.
function Test-TauChecksum {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)][string]$ArchivePath,
        [Parameter(Mandatory)][string]$ArchiveName,
        [Parameter(Mandatory)][string]$ChecksumsPath
    )

    if (-not (Test-Path -LiteralPath $ChecksumsPath)) {
        Write-Error "checksums.txt was not downloaded"
        return $false
    }

    $expected = $null
    foreach ($line in Get-Content -LiteralPath $ChecksumsPath) {
        $fields = $line -split '\s+'
        if ($fields.Length -ge 2 -and $fields[1] -eq $ArchiveName) {
            $expected = $fields[0].ToLowerInvariant()
            break
        }
    }
    if (-not $expected) {
        Write-Error "no checksum entry for $ArchiveName in checksums.txt"
        return $false
    }

    $actual = Get-TauSha256 -Path $ArchivePath
    if ($expected -ne $actual) {
        Write-Error "checksum mismatch for $ArchiveName (expected $expected, got $actual)"
        return $false
    }

    return $true
}

# Invoke-TauInstall performs arch detection, download, checksum
# verification, and installation. It throws on failure rather than calling
# exit directly, so tests can dot-source this file with
# $env:TAU_INSTALL_TEST_MODE = "1" and assert on `{ Invoke-TauInstall ... }
# | Should -Throw` without killing the test process -- exit inside a
# function called in-process would terminate the whole Pester run, not
# just the one test.
function Invoke-TauInstall {
    [CmdletBinding()]
    param([string]$Version)

    # ---------- detect arch ----------
    $arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture) {
        "X64"   { "amd64" }
        "Arm64" { "arm64" }
        default { throw "unsupported architecture: $_" }
    }

    $platform = "windows_${arch}"

    # ---------- resolve version ----------
    if (-not $Version) { $Version = $env:TAU_VERSION }
    if (-not $Version) {
        Write-Output "Fetching latest release..."
        try {
            $release = Invoke-RestMethod -Uri "https://api.github.com/repos/${repo}/releases/latest"
            $Version = $release.tag_name
        } catch {
            throw "could not fetch latest release: $_"
        }
    }

    $archive = "tau_$($Version -replace '^v','')_$platform.zip"
    $downloadUrl = "${baseUrl}/download/${Version}/${archive}"
    $checksumsUrl = "${baseUrl}/download/${Version}/checksums.txt"

    # ---------- install dir ----------
    $installDir = if ($env:TAU_INSTALL_DIR) { $env:TAU_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\tau" }

    # ---------- download & verify & extract & install ----------
    $tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "tau-install-$(Get-Random)") -Force

    try {
        Write-Output "Downloading tau ${Version} (${platform})..."
        $archivePath = Join-Path $tmp $archive
        Invoke-WebRequest -Uri $downloadUrl -OutFile $archivePath

        $checksumsPath = Join-Path $tmp "checksums.txt"
        Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath

        Write-Output "Verifying checksum..."
        if (-not (Test-TauChecksum -ArchivePath $archivePath -ArchiveName $archive -ChecksumsPath $checksumsPath)) {
            throw "checksum verification failed -- leaving any existing install untouched"
        }

        Expand-Archive -Path $archivePath -DestinationPath $tmp -Force
        $binary = Join-Path $tmp "tau.exe"

        # ---------- install ----------
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        Copy-Item -Path $binary -Destination (Join-Path $installDir "tau.exe") -Force

        Write-Output ""
        Write-Output "tau ${Version} installed to ${installDir}\tau.exe"

        # ---------- PATH ----------
        $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
        if ($userPath -notlike "*$installDir*") {
            [Environment]::SetEnvironmentVariable("PATH", "$userPath;$installDir", "User")
            $env:PATH = "$env:PATH;$installDir"
            Write-Warning "${installDir} added to your user PATH. Restart your terminal for PATH changes to take effect."
        }

        Write-Output ""
        Write-Output "  Run 'tau' to get started."

    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

if ($env:TAU_INSTALL_TEST_MODE -ne "1") {
    try {
        Invoke-TauInstall -Version $Version
    } catch {
        Write-Error $_
        exit 1
    }
}
