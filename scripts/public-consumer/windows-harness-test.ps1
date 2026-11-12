#!/usr/bin/env pwsh
# Static and synthetic checks for the Windows public-consumer harness.
#
# This test does not run Go or a native consumer. It parses the real driver with
# PowerShell's parser, checks the command boundary that must not fetch every
# platform, and exercises the real verify.py PE/cache failure paths with
# synthetic files.

[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (Get-Variable -Name PSNativeCommandUseErrorActionPreference -ErrorAction SilentlyContinue) {
    $PSNativeCommandUseErrorActionPreference = $false
}

function Fail([string]$Message) {
    throw "windows-harness-test: $Message"
}

function Find-Python {
    foreach ($name in @('python3', 'python')) {
        $command = Get-Command $name -ErrorAction SilentlyContinue
        if ($command) {
            return $command.Source
        }
    }
    Fail 'python3 or python is required'
}

function Invoke-Verify {
    param(
        [string]$Python,
        [string]$Verify,
        [string[]]$Arguments
    )
    $output = & $Python $Verify @Arguments 2>&1 | Out-String
    return [pscustomobject]@{ Code = $LASTEXITCODE; Output = $output }
}

function Read-Report([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        Fail "verify.py did not write a report: $Path"
    }
    return Get-Content -LiteralPath $Path -Raw -Encoding utf8 | ConvertFrom-Json
}

function New-SyntheticPe([string]$Path, [uint16]$Machine, [uint16]$Characteristics) {
    $data = New-Object byte[] 0x200
    $data[0] = 0x4D
    $data[1] = 0x5A
    [BitConverter]::GetBytes([uint32]0x80).CopyTo($data, 0x3C)
    $data[0x80] = 0x50
    $data[0x81] = 0x45
    $data[0x82] = 0
    $data[0x83] = 0
    [BitConverter]::GetBytes($Machine).CopyTo($data, 0x84)
    [BitConverter]::GetBytes($Characteristics).CopyTo($data, 0x96)
    [BitConverter]::GetBytes([uint16]0x20B).CopyTo($data, 0x98)
    [IO.File]::WriteAllBytes($Path, $data)
}

function Write-SyntheticCache([string]$Root, [byte[]]$Content) {
    $sha = [BitConverter]::ToString(
        [Security.Cryptography.SHA256]::HashData($Content)
    ).Replace('-', '').ToLowerInvariant()
    $entry = Join-Path (Join-Path $Root 'lance-go') $sha
    New-Item -ItemType Directory -Force -Path $entry | Out-Null
    [IO.File]::WriteAllBytes((Join-Path $entry '.lock'), [byte[]]@())
    [IO.File]::WriteAllBytes((Join-Path $entry 'lance_c.dll'), $Content)
    return [pscustomobject]@{ Sha = $sha; Size = $Content.Length }
}

$scriptDir = $PSScriptRoot
$driver = Join-Path $scriptDir 'run-windows.ps1'
$verify = Join-Path $scriptDir 'verify.py'
if (-not (Test-Path -LiteralPath $driver)) {
    Fail "driver is missing: $driver"
}
if (-not (Test-Path -LiteralPath $verify)) {
    Fail "verify.py is missing: $verify"
}
$python = Find-Python
$work = Join-Path ([IO.Path]::GetTempPath()) ('windows-harness-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $work | Out-Null
try {
    $tokens = $null
    $errors = $null
    [System.Management.Automation.Language.Parser]::ParseFile(
        $driver,
        [ref]$tokens,
        [ref]$errors
    ) | Out-Null
    if ($errors.Count -ne 0) {
        $messages = ($errors | ForEach-Object {
            '{0}:{1} {2}' -f $_.Extent.StartLineNumber, $_.Extent.StartColumnNumber, $_.Message
        }) -join '; '
        Fail "run-windows.ps1 does not parse: $messages"
    }
    $text = Get-Content -LiteralPath $driver -Raw -Encoding utf8
    foreach ($required in @(
        "GOTOOLCHAIN = 'local'",
        "CGO_ENABLED = '0'",
        "'--platform', 'windows'",
        'https://proxy.golang.org',
        'sum.golang.org',
        'LOCALAPPDATA'
    )) {
        if (-not $text.Contains($required)) {
            Fail "driver is missing required fragment: $required"
        }
    }
    $fixture = Join-Path $scriptDir 'windows-consumer/main.go'
    if (-not (Test-Path -LiteralPath $fixture)) {
        Fail "identity fixture is missing: $fixture"
    }
    $fixtureText = Get-Content -LiteralPath $fixture -Raw -Encoding utf8
    foreach ($required in @(
        'namespace.NewDirectory',
        'EnumProcessModulesEx',
        'GetModuleFileNameEx'
    )) {
        if (-not $fixtureText.Contains($required)) {
            Fail "identity fixture is missing required fragment: $required"
        }
    }
    if ($fixtureText -match 'lance-go/artifacts/') {
        Fail 'identity fixture must not link an artifact package'
    }
    foreach ($forbidden in @(
        "@('mod', 'tidy')",
        "@('mod', 'verify')",
        "@('run'",
        "@('test'"
    )) {
        if ($text.Contains($forbidden)) {
            Fail "driver contains forbidden command boundary: $forbidden"
        }
    }

    $pe = Join-Path $work 'lance_c.dll'
    $peReport = Join-Path $work 'pe.json'
    New-SyntheticPe $pe 0x8664 0x2000
    $ok = Invoke-Verify $python $verify @(
        'pe', '--binary', $pe, '--expect-machine', 'amd64', '--expect-dll', '--out', $peReport
    )
    if ($ok.Code -ne 0) {
        Fail "valid synthetic amd64 DLL was rejected: $($ok.Output)"
    }
    $peJson = Read-Report $peReport
    $armReport = Join-Path $work 'pe-arm.json'
    $bad = Invoke-Verify $python $verify @(
        'pe', '--binary', $pe, '--expect-machine', 'arm64', '--expect-dll', '--out', $armReport
    )
    if ($bad.Code -eq 0) {
        Fail 'synthetic amd64 DLL was accepted as arm64'
    }
    if (-not (@(Read-Report $armReport).errors -join ' ').Contains('PE machine')) {
        Fail 'wrong-machine failure did not report the PE machine boundary'
    }

    $cacheRoot = Join-Path $work 'LocalAppData'
    New-Item -ItemType Directory -Force -Path $cacheRoot | Out-Null
    $cache = Write-SyntheticCache $cacheRoot ([Text.Encoding]::UTF8.GetBytes('synthetic cached library'))
    $cacheReport = Join-Path $work 'cache.json'
    $cacheOk = Invoke-Verify $python $verify @(
        'cache', '--root', $cacheRoot, '--expect', 'present', '--platform', 'windows',
        '--sha', $cache.Sha, '--library', 'lance_c.dll', '--size', [string]$cache.Size,
        '--out', $cacheReport
    )
    if ($cacheOk.Code -ne 0) {
        Fail "Windows cache with inherited ACL modes was rejected: $($cacheOk.Output)"
    }
    $cacheJson = Read-Report $cacheReport
    if ($cacheJson.platform -ne 'windows' -or $cacheJson.library_mode_enforced -ne $false) {
        Fail 'Windows cache report does not record the non-POSIX contract'
    }
    $cachedLibrary = Join-Path (Join-Path (Join-Path $cacheRoot 'lance-go') $cache.Sha) 'lance_c.dll'
    [IO.File]::WriteAllBytes($cachedLibrary, [Text.Encoding]::UTF8.GetBytes('tampered synthetic library'))
    $badCacheReport = Join-Path $work 'cache-bad.json'
    $badCache = Invoke-Verify $python $verify @(
        'cache', '--root', $cacheRoot, '--expect', 'present', '--platform', 'windows',
        '--sha', $cache.Sha, '--library', 'lance_c.dll', '--size', [string]$cache.Size,
        '--out', $badCacheReport
    )
    if ($badCache.Code -eq 0) {
        Fail 'tampered Windows cache was accepted'
    }
    if (-not (@(Read-Report $badCacheReport).errors -join ' ').Contains('sha256')) {
        Fail 'tampered cache failure did not report the sha256 boundary'
    }

    Write-Host "WINDOWS_HARNESS_TEST_OK tokens=$($tokens.Count) pe=$($peJson.machine) cache=$($cacheJson.platform)"
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
}
