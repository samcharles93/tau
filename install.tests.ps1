# Tests for install.ps1's prerequisite, checksum, archive, and
# temporary-binary validation (CAT-114, CAT-113).
#
# Run with: Invoke-Pester -Path ./install.tests.ps1
#
# Part 1 tests Test-TauChecksum/Get-TauSha256 and Test-TauArchiveBinary
# directly against fixture files. Part 2 tests Invoke-TauInstall end to end
# by mocking Invoke-WebRequest to serve local fixtures instead of hitting
# the network, proving that a failed verification throws (rather than
# exiting the whole process) and leaves any existing installed binary
# untouched.

BeforeAll {
    $env:TAU_INSTALL_TEST_MODE = "1"
    . (Join-Path $PSScriptRoot "install.ps1")
}

Describe "Test-TauChecksum" {
    BeforeEach {
        $script:TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
        New-Item -ItemType Directory -Path $script:TmpDir | Out-Null
    }

    AfterEach {
        Remove-Item -Recurse -Force $script:TmpDir -ErrorAction SilentlyContinue
    }

    It "accepts a valid checksum" {
        $archivePath = Join-Path $script:TmpDir "tau_1.2.3_windows_amd64.zip"
        Set-Content -LiteralPath $archivePath -Value "fake archive contents" -NoNewline
        $sum = Get-TauSha256 -Path $archivePath
        $checksumsPath = Join-Path $script:TmpDir "checksums.txt"
        Set-Content -LiteralPath $checksumsPath -Value "$sum  tau_1.2.3_windows_amd64.zip"

        Test-TauChecksum -ArchivePath $archivePath -ArchiveName "tau_1.2.3_windows_amd64.zip" -ChecksumsPath $checksumsPath |
            Should -BeTrue
    }

    It "rejects a missing checksum entry" {
        $archivePath = Join-Path $script:TmpDir "tau_1.2.3_windows_amd64.zip"
        Set-Content -LiteralPath $archivePath -Value "fake archive contents" -NoNewline
        $sum = Get-TauSha256 -Path $archivePath
        $checksumsPath = Join-Path $script:TmpDir "checksums.txt"
        Set-Content -LiteralPath $checksumsPath -Value "$sum  some_other_file.zip"

        Test-TauChecksum -ArchivePath $archivePath -ArchiveName "tau_1.2.3_windows_amd64.zip" -ChecksumsPath $checksumsPath -ErrorAction SilentlyContinue |
            Should -BeFalse
    }

    It "rejects a tampered archive" {
        $archivePath = Join-Path $script:TmpDir "tau_1.2.3_windows_amd64.zip"
        Set-Content -LiteralPath $archivePath -Value "fake archive contents" -NoNewline
        $sum = Get-TauSha256 -Path $archivePath
        $checksumsPath = Join-Path $script:TmpDir "checksums.txt"
        Set-Content -LiteralPath $checksumsPath -Value "$sum  tau_1.2.3_windows_amd64.zip"

        $tamperedPath = Join-Path $script:TmpDir "tampered.zip"
        Set-Content -LiteralPath $tamperedPath -Value "tampered contents" -NoNewline

        Test-TauChecksum -ArchivePath $tamperedPath -ArchiveName "tau_1.2.3_windows_amd64.zip" -ChecksumsPath $checksumsPath -ErrorAction SilentlyContinue |
            Should -BeFalse
    }

    It "rejects a missing checksums.txt file" {
        $archivePath = Join-Path $script:TmpDir "tau_1.2.3_windows_amd64.zip"
        Set-Content -LiteralPath $archivePath -Value "fake archive contents" -NoNewline
        $missingChecksums = Join-Path $script:TmpDir "does-not-exist.txt"

        Test-TauChecksum -ArchivePath $archivePath -ArchiveName "tau_1.2.3_windows_amd64.zip" -ChecksumsPath $missingChecksums -ErrorAction SilentlyContinue |
            Should -BeFalse
    }
}

Describe "Test-TauPrerequisites" {
    It "does not throw when required cmdlets are available" {
        { Test-TauPrerequisites } | Should -Not -Throw
    }
}

Describe "Test-TauArchiveBinary" {
    BeforeEach {
        $script:TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
        New-Item -ItemType Directory -Path $script:TmpDir | Out-Null
    }

    AfterEach {
        Remove-Item -Recurse -Force $script:TmpDir -ErrorAction SilentlyContinue
    }

    It "returns true when the archive contains exactly the expected binary" {
        $work = Join-Path $script:TmpDir "work"
        New-Item -ItemType Directory -Path $work | Out-Null
        Set-Content -LiteralPath (Join-Path $work "tau.exe") -Value "fixture" -NoNewline
        $archivePath = Join-Path $script:TmpDir "archive.zip"
        Compress-Archive -Path (Join-Path $work "tau.exe") -DestinationPath $archivePath

        Test-TauArchiveBinary -ArchivePath $archivePath -BinaryName "tau.exe" | Should -BeTrue
    }

    It "returns false when the archive lacks the expected binary" {
        $work = Join-Path $script:TmpDir "work"
        New-Item -ItemType Directory -Path $work | Out-Null
        Set-Content -LiteralPath (Join-Path $work "readme.txt") -Value "fixture" -NoNewline
        $archivePath = Join-Path $script:TmpDir "archive.zip"
        Compress-Archive -Path (Join-Path $work "readme.txt") -DestinationPath $archivePath

        Test-TauArchiveBinary -ArchivePath $archivePath -BinaryName "tau.exe" | Should -BeFalse
    }

    It "returns false when the binary is nested under a subdirectory instead of at the archive root" {
        $work = Join-Path $script:TmpDir "work"
        $nested = Join-Path $work "bin"
        New-Item -ItemType Directory -Path $nested | Out-Null
        Set-Content -LiteralPath (Join-Path $nested "tau.exe") -Value "fixture" -NoNewline
        $archivePath = Join-Path $script:TmpDir "archive.zip"
        Compress-Archive -Path $nested -DestinationPath $archivePath

        Test-TauArchiveBinary -ArchivePath $archivePath -BinaryName "tau.exe" | Should -BeFalse
    }
}

Describe "Invoke-TauInstall end to end" {
    BeforeEach {
        $script:FixturesDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
        $script:InstallDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
        New-Item -ItemType Directory -Path $script:FixturesDir | Out-Null
        New-Item -ItemType Directory -Path $script:InstallDir | Out-Null

        # A real, working fixture executable so the --version check the
        # installer now runs against the extracted binary actually
        # succeeds -- a plain text file renamed to .exe is not a valid
        # Win32 application and would fail to launch at all.
        $work = Join-Path $script:FixturesDir "work"
        New-Item -ItemType Directory -Path $work | Out-Null
        Add-Type -OutputType ConsoleApplication -OutputAssembly (Join-Path $work "tau.exe") -TypeDefinition @"
public class TauFixtureGood {
    public static int Main(string[] args) {
        System.Console.WriteLine("tau version 9.9.9 (fixture)");
        return 0;
    }
}
"@
        $script:GoodArchivePath = Join-Path $script:FixturesDir "tau_9.9.9_windows_amd64.zip"
        Compress-Archive -Path (Join-Path $work "tau.exe") -DestinationPath $script:GoodArchivePath
        $script:GoodSum = Get-TauSha256 -Path $script:GoodArchivePath

        # A fixture executable that runs but always exits nonzero, to
        # exercise the --version-failure path distinctly from a corrupt or
        # missing binary.
        $badWork = Join-Path $script:FixturesDir "bad-work"
        New-Item -ItemType Directory -Path $badWork | Out-Null
        Add-Type -OutputType ConsoleApplication -OutputAssembly (Join-Path $badWork "tau.exe") -TypeDefinition @"
public class TauFixtureBad {
    public static int Main(string[] args) {
        System.Console.Error.WriteLine("boom");
        return 1;
    }
}
"@
        $script:BadVersionArchivePath = Join-Path $script:FixturesDir "tau_9.9.6_windows_amd64.zip"
        Compress-Archive -Path (Join-Path $badWork "tau.exe") -DestinationPath $script:BadVersionArchivePath
        $script:BadVersionSum = Get-TauSha256 -Path $script:BadVersionArchivePath

        # An archive with a matching, valid checksum but no tau.exe entry
        # at all.
        $noBinaryWork = Join-Path $script:FixturesDir "no-binary-work"
        New-Item -ItemType Directory -Path $noBinaryWork | Out-Null
        Set-Content -LiteralPath (Join-Path $noBinaryWork "readme.txt") -Value "not a binary" -NoNewline
        $script:NoBinaryArchivePath = Join-Path $script:FixturesDir "tau_9.9.5_windows_amd64.zip"
        Compress-Archive -Path (Join-Path $noBinaryWork "readme.txt") -DestinationPath $script:NoBinaryArchivePath
        $script:NoBinarySum = Get-TauSha256 -Path $script:NoBinaryArchivePath

        $env:TAU_INSTALL_DIR = $script:InstallDir
        $env:TAU_BASE_URL = "https://fixtures.invalid"
    }

    AfterEach {
        Remove-Item -Recurse -Force $script:FixturesDir -ErrorAction SilentlyContinue
        Remove-Item -Recurse -Force $script:InstallDir -ErrorAction SilentlyContinue
        Remove-Item Env:\TAU_INSTALL_DIR -ErrorAction SilentlyContinue
        Remove-Item Env:\TAU_BASE_URL -ErrorAction SilentlyContinue
    }

    It "installs cleanly with a valid archive and matching checksum" {
        Mock Invoke-WebRequest {
            param($Uri, $OutFile)
            if ($Uri -like "*checksums.txt") {
                Set-Content -LiteralPath $OutFile -Value "$($script:GoodSum)  tau_9.9.9_windows_amd64.zip"
            } else {
                Copy-Item -Path $script:GoodArchivePath -Destination $OutFile
            }
        }

        { Invoke-TauInstall -Version "v9.9.9" } | Should -Not -Throw
        Test-Path -LiteralPath (Join-Path $script:InstallDir "tau.exe") | Should -BeTrue
    }

    It "throws on a tampered archive and leaves an existing binary untouched" {
        $existingPath = Join-Path $script:InstallDir "tau.exe"
        Set-Content -LiteralPath $existingPath -Value "existing binary -- must not be touched" -NoNewline
        $beforeHash = Get-TauSha256 -Path $existingPath

        Mock Invoke-WebRequest {
            param($Uri, $OutFile)
            if ($Uri -like "*checksums.txt") {
                # checksums.txt lists the correct digest for the good
                # archive, but the "downloaded" archive below is tampered --
                # simulates corruption or tampering in transit.
                Set-Content -LiteralPath $OutFile -Value "$($script:GoodSum)  tau_9.9.9_windows_amd64.zip"
            } else {
                Set-Content -LiteralPath $OutFile -Value "tampered payload" -NoNewline
            }
        }

        { Invoke-TauInstall -Version "v9.9.9" -ErrorAction SilentlyContinue } | Should -Throw

        $afterHash = Get-TauSha256 -Path $existingPath
        $afterHash | Should -Be $beforeHash
    }

    It "throws on a missing checksum entry and leaves an existing binary untouched" {
        $existingPath = Join-Path $script:InstallDir "tau.exe"
        Set-Content -LiteralPath $existingPath -Value "existing binary -- must not be touched" -NoNewline
        $beforeHash = Get-TauSha256 -Path $existingPath

        Mock Invoke-WebRequest {
            param($Uri, $OutFile)
            if ($Uri -like "*checksums.txt") {
                Set-Content -LiteralPath $OutFile -Value "$($script:GoodSum)  some_other_file.zip"
            } else {
                Copy-Item -Path $script:GoodArchivePath -Destination $OutFile
            }
        }

        { Invoke-TauInstall -Version "v9.9.9" -ErrorAction SilentlyContinue } | Should -Throw

        $afterHash = Get-TauSha256 -Path $existingPath
        $afterHash | Should -Be $beforeHash
    }

    It "throws on an archive lacking the tau.exe binary and leaves an existing binary untouched" {
        $existingPath = Join-Path $script:InstallDir "tau.exe"
        Set-Content -LiteralPath $existingPath -Value "existing binary -- must not be touched" -NoNewline
        $beforeHash = Get-TauSha256 -Path $existingPath

        Mock Invoke-WebRequest {
            param($Uri, $OutFile)
            if ($Uri -like "*checksums.txt") {
                Set-Content -LiteralPath $OutFile -Value "$($script:NoBinarySum)  tau_9.9.5_windows_amd64.zip"
            } else {
                Copy-Item -Path $script:NoBinaryArchivePath -Destination $OutFile
            }
        }

        { Invoke-TauInstall -Version "v9.9.5" -ErrorAction SilentlyContinue } | Should -Throw

        $afterHash = Get-TauSha256 -Path $existingPath
        $afterHash | Should -Be $beforeHash
    }

    It "throws when the temporary binary fails --version and leaves an existing binary untouched" {
        $existingPath = Join-Path $script:InstallDir "tau.exe"
        Set-Content -LiteralPath $existingPath -Value "existing binary -- must not be touched" -NoNewline
        $beforeHash = Get-TauSha256 -Path $existingPath

        Mock Invoke-WebRequest {
            param($Uri, $OutFile)
            if ($Uri -like "*checksums.txt") {
                Set-Content -LiteralPath $OutFile -Value "$($script:BadVersionSum)  tau_9.9.6_windows_amd64.zip"
            } else {
                Copy-Item -Path $script:BadVersionArchivePath -Destination $OutFile
            }
        }

        { Invoke-TauInstall -Version "v9.9.6" -ErrorAction SilentlyContinue } | Should -Throw

        $afterHash = Get-TauSha256 -Path $existingPath
        $afterHash | Should -Be $beforeHash
    }
}
