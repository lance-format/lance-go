#!/usr/bin/env pwsh
# Static and synthetic checks for the Windows public-consumer harness.
#
# This test does not run Go or a native consumer. It parses the real driver with
# PowerShell's parser, checks the command boundary that must not fetch every
# platform, extracts the real cache-scope and version-identity helpers for
# synthetic boundary checks, and exercises the real verify.py payload/PE/cache
# failure paths with synthetic files.

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

function Assert-ThrowsLike([scriptblock]$Block, [string]$Pattern, [string]$Label) {
    $threw = $false
    $message = ''
    try {
        & $Block
    } catch {
        $threw = $true
        $message = $_.Exception.Message
    }
    if (-not $threw) {
        Fail "$Label did not fail"
    }
    if ($message -notmatch $Pattern) {
        Fail ("{0} failed with '{1}', want a match for '{2}'" -f $Label, $message, $Pattern)
    }
}

function Write-CacheInventory([string]$Path, [object[]]$Rows) {
    $lines = @("label`trelative`tmodule_path`tkind`tversion`tsize`tsha256`tpath")
    foreach ($row in $Rows) {
        $lines += (@(
            $row.Label,
            ($row.ModulePath + '/@v/' + $row.Version + '.' + $row.Kind),
            $row.ModulePath,
            $row.Kind,
            $row.Version,
            '1',
            '00',
            ('C:\cache\' + $row.ModulePath + '@' + $row.Version + '.' + $row.Kind)
        ) -join "`t")
    }
    Set-Content -LiteralPath $Path -Value $lines -Encoding utf8
}

function New-ArtifactRow([string]$Label, [string]$ModulePath, [string]$Version) {
    return [pscustomobject]@{ Label = $Label; ModulePath = $ModulePath; Kind = 'zip'; Version = $Version }
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
    $ast = [System.Management.Automation.Language.Parser]::ParseFile(
        $driver,
        [ref]$tokens,
        [ref]$errors
    )
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

    # Extract the real cache-scope helpers so the synthetic regressions exercise
    # the shipped code rather than a parallel reimplementation.
    $functions = @{}
    foreach ($definition in $ast.FindAll({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst]
    }, $true)) {
        $functions[$definition.Name] = [scriptblock]::Create($definition.Extent.Text)
    }
    foreach ($name in @(
        'Write-Evidence',
        'Write-Summary',
        'Get-FileSha256',
        'Assert-Equal',
        'Get-PseudoVersionCommitPrefix',
        'Assert-VersionIdentity',
        'Get-CacheRows',
        'Get-ArtifactZips',
        'Get-ArtifactMods',
        'Assert-SetupArtifactZips',
        'Assert-OnlySelectedArtifactZip',
        'Assert-EmptyCacheDirectory',
        'Get-CacheEntryCount',
        'Assert-PopulatedCacheDirectory',
        'Test-PathWithin',
        'Assert-CacheScopeBoundary'
    )) {
        if (-not $functions.ContainsKey($name)) {
            Fail "driver helper is missing: $name"
        }
        . $functions[$name]
    }

    $script:SyntheticTagCommit = ''
    $script:ResolvedTags = New-Object System.Collections.Generic.List[string]
    function Resolve-TagCommit([string]$Tag) {
        $script:ResolvedTags.Add($Tag)
        return $script:SyntheticTagCommit
    }

    $pseudoSource = '976c6ddc6358c5058b2496ff982047093eceb788'
    foreach ($version in @(
        'v0.0.0-20261113175157-976c6ddc6358',
        'v0.1.1-0.20261113175157-976c6ddc6358',
        'v2.0.1-0.20261113175157-976c6ddc6358+incompatible',
        'v0.1.0-preview.1.0.20261113175157-976c6ddc6358',
        'v2.0.0-preview.1.0.20261113175157-976c6ddc6358+incompatible'
    )) {
        $script:ResolvedTags.Clear()
        Assert-VersionIdentity $version $pseudoSource $pseudoSource
        if ($script:ResolvedTags.Count -ne 0) {
            Fail "pseudo-version $version was resolved as a tag"
        }
    }
    Assert-ThrowsLike {
        Assert-VersionIdentity 'v0.1.0-preview.1.0.20261113175157-000000000000' $pseudoSource $pseudoSource
    } 'proxy pseudo-version commit' 'wrong pseudo-version prefix'

    $script:SyntheticTagCommit = $pseudoSource
    foreach ($version in @('v1.2.3-deadbeefcafe', 'v1.2.3')) {
        $revision = if ($version -eq 'v1.2.3') { $version } else { $pseudoSource }
        $script:ResolvedTags.Clear()
        Assert-VersionIdentity $version $pseudoSource $revision
        if ($script:ResolvedTags.Count -ne 1 -or $script:ResolvedTags[0] -cne $version) {
            Fail "real tag $version did not use tag resolution"
        }
    }

    $script:RootModule = 'example.com/root'
    $script:RootVersion = 'v1.2.3'
    $script:SelectedPath = $script:RootModule + '/artifacts/windows-amd64'
    $script:SelectedVersion = 'v1.2.3'
    $script:SummaryFile = Join-Path $work 'synthetic-summary.txt'
    Set-Content -LiteralPath $script:SummaryFile -Value '' -Encoding utf8
    $script:ArtifactRequires = @(
        [pscustomobject]@{ Path = $script:RootModule + '/artifacts/darwin-amd64'; Version = 'v1.2.3' }
        [pscustomobject]@{ Path = $script:RootModule + '/artifacts/darwin-arm64'; Version = 'v1.2.3' }
        [pscustomobject]@{ Path = $script:RootModule + '/artifacts/linux-amd64'; Version = 'v1.2.3' }
        [pscustomobject]@{ Path = $script:RootModule + '/artifacts/linux-arm64'; Version = 'v1.2.3' }
    )
    $script:SelectedPath = $script:RootModule + '/artifacts/linux-amd64'
    $script:ModuleCacheTsv = Join-Path $work 'synthetic-inventory.tsv'
    $setupRows = @(
        (New-ArtifactRow 'default/setup/go-get' ($script:RootModule + '/artifacts/darwin-amd64') 'v1.2.3')
        (New-ArtifactRow 'default/setup/go-get' ($script:RootModule + '/artifacts/darwin-arm64') 'v1.2.3')
        (New-ArtifactRow 'default/setup/go-get' ($script:RootModule + '/artifacts/linux-amd64') 'v1.2.3')
        (New-ArtifactRow 'default/setup/go-get' ($script:RootModule + '/artifacts/linux-arm64') 'v1.2.3')
    )
    Write-CacheInventory $script:ModuleCacheTsv $setupRows
    Assert-SetupArtifactZips 'default' 'default/setup/'
    $expectedSetup = @($setupRows | ForEach-Object { '{0}@{1}' -f $_.ModulePath, $_.Version } | Sort-Object -Unique) -join ','
    $summaryText = Get-Content -LiteralPath $script:SummaryFile -Raw -Encoding utf8
    if (-not $summaryText.Contains("default.setup_artifact_zips=$expectedSetup")) {
        Fail "setup artifact archives were not recorded: $summaryText"
    }

    Write-CacheInventory $script:ModuleCacheTsv (@($setupRows) + @(
        (New-ArtifactRow 'default/setup/go-get' ($script:RootModule + '/artifacts/windows-amd64') 'v1.2.3')
    ))
    Assert-ThrowsLike { Assert-SetupArtifactZips 'default' 'default/setup/' } 'does not require' 'foreign setup artifact archive'

    $wrongVersionRows = @($setupRows | ForEach-Object { $_ })
    $wrongVersionRows[3] = New-ArtifactRow 'default/setup/go-get' ($script:RootModule + '/artifacts/linux-arm64') 'v9.9.9'
    Write-CacheInventory $script:ModuleCacheTsv $wrongVersionRows
    Assert-ThrowsLike { Assert-SetupArtifactZips 'default' 'default/setup/' } 'want pinned' 'wrong-version setup artifact archive'

    Write-CacheInventory $script:ModuleCacheTsv @($setupRows | Select-Object -First 3)
    Assert-ThrowsLike { Assert-SetupArtifactZips 'default' 'default/setup/' } 'missing pinned artifact archives' 'incomplete setup artifact set'

    Write-CacheInventory $script:ModuleCacheTsv @()
    Assert-ThrowsLike { Assert-SetupArtifactZips 'default' 'default/setup/' } 'downloaded no artifact archives' 'empty setup artifact set'

    $scope = Join-Path $work 'scope'
    $setupModule = Join-Path $scope 'setup-gomodcache'
    $setupBuild = Join-Path $scope 'setup-gocache'
    $coldModule = Join-Path $scope 'cold-gomodcache'
    $coldBuild = Join-Path $scope 'cold-gocache'
    $localAppData = Join-Path $scope 'LocalAppData'
    $temp = Join-Path $scope 'Temp'
    $consumer = Join-Path $scope 'consumer'
    New-Item -ItemType Directory -Force -Path $setupModule, $setupBuild, $coldModule, $coldBuild, $localAppData, $temp, $consumer | Out-Null
    Set-Content -LiteralPath (Join-Path $setupModule 'setup-download') -Value 'setup' -Encoding utf8
    $mode = [pscustomobject]@{
        Name = 'default'
        SetupModCache = $setupModule
        SetupBuildCache = $setupBuild
        ModCache = $coldModule
        BuildCache = $coldBuild
        LocalAppData = $localAppData
        Temp = $temp
        Consumer = $consumer
    }
    Assert-CacheScopeBoundary $mode
    Set-Content -LiteralPath (Join-Path $coldModule 'primed') -Value 'primed' -Encoding utf8
    Assert-ThrowsLike { Assert-CacheScopeBoundary $mode } 'cold target module cache is not empty' 'primed cold module cache'
    Remove-Item -LiteralPath (Join-Path $coldModule 'primed')
    Set-Content -LiteralPath (Join-Path $coldBuild 'primed') -Value 'primed' -Encoding utf8
    Assert-ThrowsLike { Assert-CacheScopeBoundary $mode } 'cold target build cache is not empty' 'primed cold build cache'
    Remove-Item -LiteralPath (Join-Path $coldBuild 'primed')
    $aliased = $mode.PSObject.Copy()
    $aliased.ModCache = $setupModule
    Assert-ThrowsLike { Assert-CacheScopeBoundary $aliased } 'cache scopes overlap' 'aliased setup and cold module cache'
    $emptySetup = Join-Path $scope 'empty-setup'
    New-Item -ItemType Directory -Force -Path $emptySetup | Out-Null
    $noSetup = $mode.PSObject.Copy()
    $noSetup.SetupModCache = $emptySetup
    Assert-ThrowsLike { Assert-CacheScopeBoundary $noSetup } 'setup module cache is empty' 'missing setup dependency resolution'

    Write-CacheInventory $script:ModuleCacheTsv @(
        (New-ArtifactRow 'default/cold/build' $script:SelectedPath $script:SelectedVersion)
    )
    Assert-OnlySelectedArtifactZip 'default/cold/build'
    Write-CacheInventory $script:ModuleCacheTsv @(
        (New-ArtifactRow 'default/setup/go-get' $script:SelectedPath $script:SelectedVersion)
    )
    Assert-ThrowsLike { Assert-OnlySelectedArtifactZip 'default/cold/build' } 'want exactly' 'setup archive standing in for a cold build observation'
    Write-CacheInventory $script:ModuleCacheTsv @(
        (New-ArtifactRow 'default/cold/build' $script:SelectedPath $script:SelectedVersion)
        (New-ArtifactRow 'default/cold/build' ($script:RootModule + '/artifacts/darwin-amd64') 'v1.2.3')
    )
    Assert-ThrowsLike { Assert-OnlySelectedArtifactZip 'default/cold/build' } 'want exactly' 'foreign cold artifact archive'

    foreach ($modeName in @('default', 'namespace', 'external')) {
        $helper = "Add-IdentityHelperSource '$modeName' " + '$' + $modeName
        $boundary = "Enter-ColdTargetPhase " + '$' + $modeName
        $helperIndex = $text.IndexOf($helper, [StringComparison]::Ordinal)
        $boundaryIndex = $text.IndexOf($boundary, [StringComparison]::Ordinal)
        if ($helperIndex -lt 0) {
            Fail "driver does not prepare the $modeName identity helper during setup"
        }
        if ($boundaryIndex -lt 0) {
            Fail "driver does not enter the $modeName cold target phase"
        }
        if ($helperIndex -gt $boundaryIndex) {
            Fail "$modeName identity helper dependency resolution runs after the cold cache boundary"
        }
        $nextMode = switch ($modeName) {
            'default' { "New-Mode 'namespace'" }
            'namespace' { "New-Mode 'external'" }
            default { $null }
        }
        $endIndex = if ($nextMode) { $text.IndexOf($nextMode, [StringComparison]::Ordinal) } else { $text.Length }
        if ($endIndex -lt 0) {
            Fail "driver is missing the mode after $modeName"
        }
        $tail = $text.Substring($boundaryIndex, $endIndex - $boundaryIndex)
        if ($tail.Contains("@('get'")) {
            Fail "$modeName runs go get after the cold cache boundary"
        }
        $moduleCheck = 'Assert-ConsumerModuleUnchanged ' + '$' + $modeName
        if (-not $tail.Contains($moduleCheck)) {
            Fail "$modeName does not verify the retained consumer module files"
        }
    }
    $coldPhaseText = $functions['Enter-ColdTargetPhase'].ToString()
    if (-not $coldPhaseText.Contains('Set-GoEnvironment -ModCache $Mode.ModCache -BuildCache $Mode.BuildCache')) {
        Fail 'cold target phase does not switch to the target caches'
    }
    $helperText = $functions['Add-IdentityHelperSource'].ToString()
    $buildText = $functions['Build-IdentityHelper'].ToString()
    if (-not $helperText.Contains("@('get'")) {
        Fail 'setup identity helper does not resolve golang.org/x/sys'
    }
    if ($buildText.Contains("@('get'")) {
        Fail 'cold identity helper build performs go get'
    }
    foreach ($coldFunction in @('Enter-ColdTargetPhase', 'Resolve-ColdRoot')) {
        if ($functions[$coldFunction].ToString().Contains("@('get'")) {
            Fail "$coldFunction performs go get during the cold target phase"
        }
    }
    if (-not $text.Contains('Save-ModuleCacheSnapshot([string]$Label, [string]$ModCache)')) {
        Fail 'module cache snapshots do not take an explicit cache directory'
    }
    if ($functions['Save-ModuleCacheSnapshot'].ToString().Contains('$env:GOMODCACHE')) {
        Fail 'module cache snapshots still read the process environment'
    }
    foreach ($line in @($text -split "`r?`n")) {
        if ($line -match 'Copy-Item' -and $line -match '(?i)gomodcache|gocache|SetupModCache|SetupBuildCache|\bModCache\b|\bBuildCache\b') {
            Fail "driver primes a cache from a file copy: $line"
        }
        if ($line -match '(?i)\brobocopy\b|\bxcopy\b') {
            Fail "driver primes a cache with a copy tool: $line"
        }
    }

    # Exercise the driver's real payload argv against the Python CLI. These
    # opaque bytes stand in for compressed data; DLL checks belong to the cache
    # and loaded-module gates after runtime extraction.
    $script:Verify = $verify
    $script:LibraryName = 'lance_c.dll'
    $path = 'example.com/root/artifacts/windows-amd64'
    $version = 'v1.2.3'
    $payloadPath = Join-Path $work 'lance_c.dll.zst'
    $payloadBytes = [Text.Encoding]::UTF8.GetBytes('synthetic compressed payload')
    [IO.File]::WriteAllBytes($payloadPath, $payloadBytes)
    $payload = Get-Item -LiteralPath $payloadPath
    $meta = [pscustomobject]@{
        CompressedSize = $payloadBytes.Length
        CompressedSHA256 = Get-FileSha256 $payloadPath
        UncompressedSize = 100
        UncompressedSHA256 = ('0' * 64)
    }
    $zip = Join-Path $work 'artifact.zip'
    $archive = [IO.Compression.ZipFile]::Open($zip, [IO.Compression.ZipArchiveMode]::Create)
    try {
        $entry = $archive.CreateEntry("$path@$version/lance_c.dll.zst")
        $stream = $entry.Open()
        try { $stream.Write($payloadBytes, 0, $payloadBytes.Length) } finally { $stream.Dispose() }
    } finally { $archive.Dispose() }
    $payloadOut = Join-Path $work 'payload-report.json'
    $payloadFunction = $ast.Find({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Read-ArtifactModule'
    }, $true)
    $argvAssignment = @($payloadFunction.FindAll({
        param($node)
        $node -is [System.Management.Automation.Language.AssignmentStatementAst] -and $node.Left.Extent.Text -eq '$arguments'
    }, $true))
    if ($argvAssignment.Count -ne 1) { Fail 'expected one payload CLI argument assignment' }
    $makePayloadArgs = [scriptblock]::Create($argvAssignment[0].Extent.Text)
    . $makePayloadArgs
    $payloadCheck = Invoke-Verify $python $verify $arguments[1..($arguments.Count - 1)]
    if ($payloadCheck.Code -ne 0) { Fail "driver payload CLI rejected matching bytes: $($payloadCheck.Output)" }

    $meta.CompressedSize++
    . $makePayloadArgs
    $badSize = Invoke-Verify $python $verify $arguments[1..($arguments.Count - 1)]
    if ($badSize.Code -ne 1 -or -not $badSize.Output.Contains('payload size is')) {
        Fail "driver payload CLI did not reject wrong metadata size: $($badSize.Output)"
    }
    $meta.CompressedSize--
    $payloadBytes[0] = $payloadBytes[0] -bxor 1
    [IO.File]::WriteAllBytes($payloadPath, $payloadBytes)
    . $makePayloadArgs
    $badPayload = Invoke-Verify $python $verify $arguments[1..($arguments.Count - 1)]
    if ($badPayload.Code -ne 1 -or -not $badPayload.Output.Contains('artifact file bytes differ from archive payload')) {
        Fail "driver payload CLI did not reject tampered module bytes: $($badPayload.Output)"
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
