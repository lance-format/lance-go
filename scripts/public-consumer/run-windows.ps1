#!/usr/bin/env pwsh
<#
.SYNOPSIS
Run the Windows public-consumer acceptance lane.

.DESCRIPTION
This driver is intentionally narrower than the Unix shell driver. It keeps
module, build, and runtime caches separate for the default, namespace-only, and
explicitly-supplied-DLL consumers, and reuses verify.py for archive, binary,
payload, cache, and PE inspection. It never runs go mod tidy or go mod verify,
so module downloads are observed before anything could fan out to every
platform artifact.

Each mode has two cache phases. The setup phase performs the ordinary go get
and prepares the identity helper with its pinned dependency. After every setup
dependency-resolution command has finished, the driver switches to a fresh,
empty target module cache and build cache, keeps the consumer go.mod and go.sum
written during setup, and measures the target build and its archive downloads
there. Setup archive observations are validated against the downloaded root
requirements but are never reused as target-build evidence.

The Windows cache contract is inherited from the per-user LocalAppData ACL.
POSIX modes are recorded as evidence only; this script does not claim them.
#>
[CmdletBinding()]
param(
    [string]$RootModule = $env:ROOT_MODULE,
    [string]$RootRevision = $env:ROOT_REVISION,
    [string]$LibraryName = $env:LIBRARY_NAME,
    [string]$TargetGoos = $env:TARGET_GOOS,
    [string]$TargetGoarch = $env:TARGET_GOARCH,
    [string]$TargetLabel = $env:TARGET_LABEL,
    [string]$SourceCommit = $env:SOURCE_COMMIT,
    [string]$EvidenceDir = $env:EVIDENCE_DIR,
    [string]$ExpectedProcessorArchitecture = $env:EXPECTED_PROCESSOR_ARCH,
    [string]$ExpectedOsName = $env:EXPECTED_OS_NAME
)

Set-StrictMode -Version Latest
if ([string]::IsNullOrWhiteSpace($RootModule)) {
    $RootModule = 'github.com/lance-format/lance-go'
}
$ErrorActionPreference = 'Stop'
if (Get-Variable -Name PSNativeCommandUseErrorActionPreference -ErrorAction SilentlyContinue) {
    $PSNativeCommandUseErrorActionPreference = $false
}

function Fail([string]$Message) {
    throw "public-consumer: $Message"
}

function Write-Evidence([string]$Path, [string]$Value) {
    Add-Content -LiteralPath $Path -Value $Value -Encoding utf8
}

function Write-Summary([string]$Value) {
    Write-Evidence $script:SummaryFile $Value
}

function Format-CommandArgument([string]$Value) {
    if ($Value -match '[\s"]') {
        return '"' + ($Value -replace '"', '\"') + '"'
    }
    return $Value
}

function Write-CommandLog([string]$WorkingDirectory, [string[]]$Arguments) {
    $line = '+ cwd=' + $WorkingDirectory + ' ' + (($Arguments | ForEach-Object { Format-CommandArgument $_ }) -join ' ')
    Write-Evidence $script:CommandsLog $line
}

function Read-JsonFile([string]$Path) {
    return (Get-Content -LiteralPath $Path -Raw -Encoding utf8 | ConvertFrom-Json)
}

function Get-FileSha256([string]$Path) {
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Assert-Equal([string]$Label, [string]$Actual, [string]$Expected) {
    if ($Actual -cne $Expected) {
        Fail "$Label is '$Actual', want '$Expected'"
    }
}

function Get-JsonField($Object, [string]$Name) {
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property) {
        return ''
    }
    return [string]$property.Value
}

function Assert-EqualPath([string]$Label, [string]$Actual, [string]$Expected) {
    $actualFull = [IO.Path]::GetFullPath($Actual)
    $expectedFull = [IO.Path]::GetFullPath($Expected)
    if (-not $actualFull.Equals($expectedFull, [StringComparison]::OrdinalIgnoreCase)) {
        Fail "$Label is '$actualFull', want '$expectedFull'"
    }
}

function Get-CacheEntryCount([string]$Path) {
    return @(Get-ChildItem -LiteralPath $Path -Force -ErrorAction Stop).Count
}

function Assert-EmptyCacheDirectory([string]$Path, [string]$Label) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        Fail "$Label cache directory is missing: $Path"
    }
    $count = Get-CacheEntryCount $Path
    if ($count -ne 0) {
        Fail ("{0} cache is not empty: {1} entries under {2}" -f $Label, $count, $Path)
    }
}

function Assert-PopulatedCacheDirectory([string]$Path, [string]$Label) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        Fail "$Label cache directory is missing: $Path"
    }
    if ((Get-CacheEntryCount $Path) -eq 0) {
        Fail "$Label cache is empty; setup dependency resolution did not run: $Path"
    }
}

function Test-PathWithin([string]$Inner, [string]$Outer) {
    $innerFull = [IO.Path]::GetFullPath($Inner).TrimEnd('\', '/')
    $outerFull = [IO.Path]::GetFullPath($Outer).TrimEnd('\', '/')
    if ($innerFull.Equals($outerFull, [StringComparison]::OrdinalIgnoreCase)) {
        return $true
    }
    $prefix = $outerFull + [IO.Path]::DirectorySeparatorChar
    return $innerFull.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)
}

function Assert-CacheScopeBoundary($Mode) {
    $named = @(
        [pscustomobject]@{ Name = 'setup module'; Path = $Mode.SetupModCache },
        [pscustomobject]@{ Name = 'setup build'; Path = $Mode.SetupBuildCache },
        [pscustomobject]@{ Name = 'cold target module'; Path = $Mode.ModCache },
        [pscustomobject]@{ Name = 'cold target build'; Path = $Mode.BuildCache },
        [pscustomobject]@{ Name = 'isolated LocalAppData'; Path = $Mode.LocalAppData },
        [pscustomobject]@{ Name = 'isolated TEMP'; Path = $Mode.Temp },
        [pscustomobject]@{ Name = 'consumer module'; Path = $Mode.Consumer }
    )
    for ($i = 0; $i -lt $named.Count; $i++) {
        for ($j = $i + 1; $j -lt $named.Count; $j++) {
            if ((Test-PathWithin $named[$i].Path $named[$j].Path) -or (Test-PathWithin $named[$j].Path $named[$i].Path)) {
                Fail ("{0} cache scopes overlap: {1} ({2}) and {3} ({4})" -f $Mode.Name, $named[$i].Name, $named[$i].Path, $named[$j].Name, $named[$j].Path)
            }
        }
    }
    Assert-PopulatedCacheDirectory $Mode.SetupModCache "$($Mode.Name) setup module"
    Assert-EmptyCacheDirectory $Mode.ModCache "$($Mode.Name) cold target module"
    Assert-EmptyCacheDirectory $Mode.BuildCache "$($Mode.Name) cold target build"
}

function Get-ConsumerModuleFingerprint($Mode) {
    $parts = @()
    foreach ($name in @('go.mod', 'go.sum')) {
        $path = Join-Path $Mode.Consumer $name
        if (-not (Test-Path -LiteralPath $path)) {
            Fail "consumer $name is missing: $path"
        }
        $parts += (Get-FileSha256 $path)
    }
    return ($parts -join ' ')
}

function Assert-ConsumerModuleUnchanged($Mode, [string]$Label) {
    $property = $Mode.PSObject.Properties['ModuleFingerprint']
    if ($null -eq $property) {
        Fail "$Label has no setup consumer module fingerprint"
    }
    $now = Get-ConsumerModuleFingerprint $Mode
    Assert-Equal "$Label consumer go.mod/go.sum" $now ([string]$property.Value)
    Write-Summary "$Label.consumer_module_files=unchanged"
}

function Set-GoEnvironment([string]$ModCache, [string]$BuildCache) {
    foreach ($name in @(
        'GOOS', 'GOARCH', 'GOFLAGS', 'GOPRIVATE', 'GONOPROXY', 'GONOSUMDB',
        'GONOSUMCHECK', 'GOINSECURE', 'GOPROXY_FALLBACK', 'GOBIN',
        'GOCACHEPROG', 'GO111MODULE'
    )) {
        Remove-Item -Path "Env:$name" -ErrorAction SilentlyContinue
    }
    $env:GOENV = 'off'
    $env:GOWORK = 'off'
    $env:GOTOOLCHAIN = 'local'
    $env:CGO_ENABLED = '0'
    $env:GOPROXY = 'https://proxy.golang.org'
    $env:GOSUMDB = 'sum.golang.org'
    $env:GOMODCACHE = $ModCache
    $env:GOCACHE = $BuildCache
}

function Invoke-Go {
    param(
        [string]$WorkingDirectory,
        [string[]]$GoArguments,
        [string]$LogFile,
        [switch]$AllowFailure
    )
    Write-CommandLog $WorkingDirectory (@('go') + $GoArguments)
    $code = 1
    Push-Location -LiteralPath $WorkingDirectory
    try {
        & go @GoArguments 2>&1 | Tee-Object -FilePath $LogFile -Append | Out-Host
        $code = $LASTEXITCODE
    } finally {
        Pop-Location
    }
    if ($code -ne 0 -and -not $AllowFailure) {
        Fail ("go {0} failed with exit code {1}" -f ($GoArguments -join ' '), $code)
    }
    return $code
}

function Invoke-GoCapture {
    param(
        [string]$WorkingDirectory,
        [string[]]$GoArguments,
        [string]$StdoutFile,
        [string]$LogFile
    )
    Write-CommandLog $WorkingDirectory (@('go') + $GoArguments)
    $code = 1
    Push-Location -LiteralPath $WorkingDirectory
    try {
        & go @GoArguments > $StdoutFile 2>> $LogFile
        $code = $LASTEXITCODE
    } finally {
        Pop-Location
    }
    if (Test-Path -LiteralPath $StdoutFile) {
        Get-Content -LiteralPath $StdoutFile -Encoding utf8 | Add-Content -LiteralPath $LogFile -Encoding utf8
    }
    if ($code -ne 0) {
        Fail ("go {0} failed with exit code {1}" -f ($GoArguments -join ' '), $code)
    }
    return $code
}

function Invoke-Python {
    param(
        [string[]]$PythonArguments,
        [string]$LogFile,
        [switch]$AllowFailure
    )
    Write-CommandLog $script:RepoDir (@($script:Python) + $PythonArguments)
    $code = 1
    & $script:Python @PythonArguments 2>&1 | Tee-Object -FilePath $LogFile -Append | Out-Host
    $code = $LASTEXITCODE
    if ($code -ne 0 -and -not $AllowFailure) {
        Fail ("verify.py {0} failed with exit code {1}" -f $PythonArguments[0], $code)
    }
    return $code
}

function Invoke-NativeProcess {
    param(
        [string]$WorkingDirectory,
        [string]$FilePath,
        [string[]]$Arguments,
        [string]$LogFile
    )
    Write-CommandLog $WorkingDirectory (@($FilePath) + $Arguments)
    $code = 1
    Push-Location -LiteralPath $WorkingDirectory
    try {
        $output = & $FilePath @Arguments 2>&1
        $code = $LASTEXITCODE
    } finally {
        Pop-Location
    }
    $text = (($output | ForEach-Object { $_.ToString() }) -join "`n").TrimEnd()
    Set-Content -LiteralPath $LogFile -Value $text -Encoding utf8
    Write-Evidence $script:CommandsLog $text
    if ($code -ne 0) {
        Fail ("{0} failed with exit code {1}" -f $FilePath, $code)
    }
    return $text
}

function Get-RegexValue([string]$Text, [string]$Pattern, [string]$Label) {
    if ($Text -match $Pattern) {
        return $Matches[1]
    }
    Fail "metadata is missing $Label"
}

function Get-ArtifactFieldInt([string]$Text, [string]$Name) {
    $pattern = '(?m)^\s*' + [regex]::Escape($Name) + '\s+int64\s*=\s*([0-9]+)'
    return [int64]::Parse(
        (Get-RegexValue $Text $pattern $Name),
        [Globalization.CultureInfo]::InvariantCulture
    )
}

function Get-ArtifactFieldString([string]$Text, [string]$Name) {
    $pattern = '(?m)^\s*' + [regex]::Escape($Name) + '\s*=\s*"([^"]*)"'
    return Get-RegexValue $Text $pattern $Name
}

function Assert-NativeHost {
    if ($env:OS -ne 'Windows_NT') {
        Fail 'this lane must run on a native Windows host'
    }
    if ($PSVersionTable.PSVersion.Major -lt 7) {
        Fail 'PowerShell 7 or newer is required'
    }
    if (-not [Environment]::Is64BitProcess) {
        Fail 'the lane must run in a 64-bit PowerShell process'
    }
    if ($env:PROCESSOR_ARCHITECTURE -ine $ExpectedProcessorArchitecture) {
        Fail ("PROCESSOR_ARCHITECTURE is '{0}', want '{1}'" -f $env:PROCESSOR_ARCHITECTURE, $ExpectedProcessorArchitecture)
    }
    $os = Get-CimInstance -ClassName Win32_OperatingSystem
    if ($os.Caption -notlike "*$ExpectedOsName*") {
        Fail ("operating system caption is '{0}', want it to contain '{1}'" -f $os.Caption, $ExpectedOsName)
    }
    Assert-Equal 'go env GOOS' ([string]$script:GoEnv.GOOS) 'windows'
    Assert-Equal 'go env GOARCH' ([string]$script:GoEnv.GOARCH) $TargetGoarch
    Assert-Equal 'go env GOHOSTOS' ([string]$script:GoEnv.GOHOSTOS) 'windows'
    Assert-Equal 'go env GOHOSTARCH' ([string]$script:GoEnv.GOHOSTARCH) $TargetGoarch
    Assert-Equal 'go env CGO_ENABLED' ([string]$script:GoEnv.CGO_ENABLED) '0'
    Assert-Equal 'go env GOTOOLCHAIN' ([string]$script:GoEnv.GOTOOLCHAIN) 'local'
    Assert-Equal 'go env GOWORK' ([string]$script:GoEnv.GOWORK) 'off'
    Assert-Equal 'go env GOPROXY' ([string]$script:GoEnv.GOPROXY) 'https://proxy.golang.org'
    Assert-Equal 'go env GOSUMDB' ([string]$script:GoEnv.GOSUMDB) 'sum.golang.org'
    foreach ($name in @('GOPRIVATE', 'GONOPROXY', 'GONOSUMDB', 'GONOSUMCHECK', 'GOINSECURE', 'GOFLAGS')) {
        $property = $script:GoEnv.PSObject.Properties[$name]
        if ($null -ne $property -and -not [string]::IsNullOrEmpty([string]$property.Value)) {
            Fail ("go env {0} is '{1}', want empty" -f $name, $property.Value)
        }
    }
    if ([string]$script:GoEnv.GOVERSION -notmatch '^go1\.23\.') {
        Fail ("go version is '{0}', want go1.23.x" -f $script:GoEnv.GOVERSION)
    }
    foreach ($line in @(
        "host=windows/$TargetGoarch",
        "processor_architecture=$($env:PROCESSOR_ARCHITECTURE)",
        "os_caption=$($os.Caption)",
        "os_version=$($os.Version)",
        "os_architecture=$($os.OSArchitecture)",
        "powershell=$($PSVersionTable.PSVersion)",
        "go_version=$($script:GoEnv.GOVERSION)",
        "goos=$($script:GoEnv.GOOS)",
        "goarch=$($script:GoEnv.GOARCH)",
        "gohostos=$($script:GoEnv.GOHOSTOS)",
        "gohostarch=$($script:GoEnv.GOHOSTARCH)",
        "cgo_enabled=$($script:GoEnv.CGO_ENABLED)",
        "gotoolchain=$($script:GoEnv.GOTOOLCHAIN)",
        "gowork=$($script:GoEnv.GOWORK)",
        "goproxy=$($script:GoEnv.GOPROXY)",
        "gosumdb=$($script:GoEnv.GOSUMDB)"
    )) {
        Write-Evidence $script:EnvironmentFile $line
    }
}

function Resolve-TagCommit([string]$Tag) {
    $out = Join-Path $script:WorkDir ('tag-' + [guid]::NewGuid().ToString('N') + '.txt')
    Write-CommandLog $script:RepoDir @('git', 'rev-parse', '--verify', '--quiet', "refs/tags/$Tag^{commit}")
    $code = 1
    & git -C $script:RepoDir rev-parse --verify --quiet "refs/tags/$Tag^{commit}" > $out 2>> $script:CommandsLog
    $code = $LASTEXITCODE
    if ($code -eq 0) {
        return (Get-Content -LiteralPath $out -Raw).Trim()
    }
    $remoteOut = Join-Path $script:WorkDir ('remote-tag-' + [guid]::NewGuid().ToString('N') + '.txt')
    Write-CommandLog $script:RepoDir @('git', 'ls-remote', 'origin', "refs/tags/$Tag", "refs/tags/$Tag^{}")
    & git -C $script:RepoDir ls-remote origin "refs/tags/$Tag" "refs/tags/$Tag^{}" > $remoteOut 2>> $script:CommandsLog
    if ($LASTEXITCODE -ne 0) {
        Fail "cannot resolve tag $Tag from the checkout or origin"
    }
    $records = @(Get-Content -LiteralPath $remoteOut | Where-Object { $_ -match '^[0-9a-f]{40}\s+refs/tags/' })
    $peeled = @($records | Where-Object { $_ -match '\^\{\}$' })
    $selected = if ($peeled.Count -gt 0) { $peeled[0] } elseif ($records.Count -gt 0) { $records[0] } else { $null }
    if (-not $selected) {
        Fail "tag $Tag does not resolve to a commit at origin"
    }
    return ($selected -split '\s+')[0]
}

function Get-PseudoVersionCommitPrefix([string]$Version) {
    if ($Version -match '^v[0-9]+\.(0\.0-|[0-9]+\.[0-9]+-([^+]*\.)?0\.)[0-9]{14}-(?<rev>[0-9a-f]{12})(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$') {
        return $Matches['rev']
    }
    return $null
}

function Assert-VersionIdentity([string]$ProxyVersion, [string]$Source, [string]$Revision) {
    if ($Revision -match '^[0-9a-f]{40}$') {
        $pseudoPrefix = Get-PseudoVersionCommitPrefix $ProxyVersion
        if ($pseudoPrefix) {
            Assert-Equal 'proxy pseudo-version commit' $pseudoPrefix $Source.Substring(0, 12)
            return
        }
        $tagCommit = Resolve-TagCommit $ProxyVersion
        Assert-Equal 'proxy version tag commit' $tagCommit $Source
        return
    }
    Assert-Equal 'proxy version for tag revision' $ProxyVersion $Revision
    $tagCommit = Resolve-TagCommit $Revision
    Assert-Equal 'source tag commit' $tagCommit $Source
}

function Parse-GoModRequires([string]$Text) {
    $requires = New-Object System.Collections.Generic.List[object]
    $inBlock = $false
    foreach ($raw in ($Text -split "`r?`n")) {
        $line = ($raw -replace '//.*$', '').Trim()
        if ($line -eq '') {
            continue
        }
        if ($line -match '^require\s*\($') {
            $inBlock = $true
            continue
        }
        if ($inBlock -and $line -eq ')') {
            $inBlock = $false
            continue
        }
        $candidate = $null
        if ($inBlock) {
            $candidate = $line
        } elseif ($line -match '^require\s+(.+)$') {
            $candidate = $Matches[1]
        }
        if ($candidate) {
            $fields = @($candidate -split '\s+' | Where-Object { $_ -ne '' })
            if ($fields.Count -lt 2) {
                Fail "cannot parse go.mod require line: $raw"
            }
            $requires.Add([pscustomobject]@{
                Path = $fields[0]
                Version = $fields[1]
            })
        }
    }
    return $requires.ToArray()
}

function Get-RequiredVersion([string]$Path) {
    foreach ($item in @($script:RootRequires)) {
        if ($item.Path -eq $Path) {
            return $item.Version
        }
    }
    return $null
}

function New-Mode([string]$Name) {
    $modeDir = Join-Path $script:WorkDir $Name
    $consumer = Join-Path $modeDir 'consumer'
    $setupModCache = Join-Path $modeDir 'setup/gomodcache'
    $setupBuildCache = Join-Path $modeDir 'setup/gocache'
    $modCache = Join-Path $modeDir 'target/gomodcache'
    $buildCache = Join-Path $modeDir 'target/gocache'
    $localAppData = Join-Path $modeDir 'runtime/LocalAppData'
    $temp = Join-Path $modeDir 'runtime/Temp'
    New-Item -ItemType Directory -Force -Path $consumer, $setupModCache, $setupBuildCache, $modCache, $buildCache, $localAppData, $temp | Out-Null
    foreach ($cache in @(
        @('setup module', $setupModCache),
        @('setup build', $setupBuildCache),
        @('cold target module', $modCache),
        @('cold target build', $buildCache)
    )) {
        Assert-EmptyCacheDirectory $cache[1] "$Name fresh $($cache[0])"
    }
    Set-GoEnvironment -ModCache $setupModCache -BuildCache $setupBuildCache
    $null = Invoke-Go $consumer @('mod', 'init', "example.com/public-consumer-$Name") $script:CommandsLog
    $env:LOCALAPPDATA = $localAppData
    $env:TEMP = $temp
    $env:TMP = $temp
    Remove-Item -Path Env:LANCE_LIB_PATH -ErrorAction SilentlyContinue
    if (Test-Path Env:LANCE_LIB_PATH) {
        Fail 'LANCE_LIB_PATH is still set in the isolated consumer environment'
    }
    $mode = [pscustomobject]@{
        Name = $Name
        Dir = $modeDir
        Consumer = $consumer
        SetupModCache = $setupModCache
        SetupBuildCache = $setupBuildCache
        ModCache = $modCache
        BuildCache = $buildCache
        LocalAppData = $localAppData
        Temp = $temp
    }
    Write-Evidence $script:EnvironmentFile "mode_$Name.localappdata=$localAppData"
    Write-Evidence $script:EnvironmentFile "mode_$Name.setup_gomodcache=$setupModCache"
    Write-Evidence $script:EnvironmentFile "mode_$Name.setup_gocache=$setupBuildCache"
    Write-Evidence $script:EnvironmentFile "mode_$Name.gomodcache=$modCache"
    Write-Evidence $script:EnvironmentFile "mode_$Name.gocache=$buildCache"
    Write-Evidence $script:EnvironmentFile "mode_$Name.lance_lib_path=unset"
    Assert-CacheAbsent $mode "$Name/initial"
    return $mode
}

function Assert-CacheAbsent($Mode, [string]$Label) {
    $out = Join-Path $script:EvidenceDir ("verify-$($Label -replace '/', '-')-absent.json")
    $arguments = @(
        $script:Verify, 'cache',
        '--root', $Mode.LocalAppData,
        '--expect', 'absent',
        '--platform', 'windows',
        '--out', $out
    )
    $null = Invoke-Python $arguments $script:CommandsLog
    Write-Summary "$Label.cache_absent=true"
}

function Save-ModuleCacheSnapshot([string]$Label, [string]$ModCache) {
    $root = Join-Path $ModCache ("cache/download/" + $script:RootModule)
    if (-not (Test-Path -LiteralPath $root)) {
        return
    }
    $files = @(Get-ChildItem -LiteralPath $root -Recurse -File -ErrorAction SilentlyContinue |
        Where-Object { $_.Extension -eq '.zip' -or $_.Extension -eq '.mod' } |
        Sort-Object FullName)
    foreach ($file in $files) {
        $relative = ([IO.Path]::GetRelativePath($root, $file.FullName) -replace '\\', '/')
        $modulePath = $script:RootModule
        $rest = $relative
        if ($rest.StartsWith('@v/')) {
            $rest = $rest.Substring(3)
        } else {
            $index = $rest.IndexOf('/@v/')
            if ($index -lt 0) {
                Fail "unexpected module cache path: $relative"
            }
            $modulePath = $script:RootModule + '/' + $rest.Substring(0, $index)
            $rest = $rest.Substring($index + 4)
        }
        $kind = $file.Extension.TrimStart('.')
        $version = [IO.Path]::GetFileNameWithoutExtension($rest)
        $line = @(
            $Label,
            $relative,
            $modulePath,
            $kind,
            $version,
            [string]$file.Length,
            (Get-FileSha256 $file.FullName),
            $file.FullName
        ) -join "`t"
        Write-Evidence $script:ModuleCacheTsv $line
    }
}

function Get-CacheRows([string]$LabelPrefix) {
    if (-not (Test-Path -LiteralPath $script:ModuleCacheTsv)) {
        return @()
    }
    return @(Import-Csv -LiteralPath $script:ModuleCacheTsv -Delimiter "`t" |
        Where-Object { $_.label.StartsWith($LabelPrefix) })
}

function Get-ArtifactZips([string]$LabelPrefix) {
    return @(Get-CacheRows $LabelPrefix |
        Where-Object { $_.module_path.StartsWith($script:RootModule + '/artifacts/') -and $_.kind -eq 'zip' } |
        ForEach-Object { '{0}@{1}' -f $_.module_path, $_.version } |
        Sort-Object -Unique)
}

function Get-ArtifactMods([string]$LabelPrefix) {
    return @(Get-CacheRows $LabelPrefix |
        Where-Object { $_.module_path.StartsWith($script:RootModule + '/artifacts/') -and $_.kind -eq 'mod' } |
        ForEach-Object { '{0}@{1}' -f $_.module_path, $_.version } |
        Sort-Object -Unique)
}

function Assert-SetupArtifactZips([string]$Name, [string]$LabelPrefix) {
    # Go 1.23 resolves imports with every build tag during go get, so setup
    # legitimately observes all pinned artifact archives. Validate that exact
    # set against the downloaded root go.mod instead of tolerating extras.
    $got = @(Get-ArtifactZips $LabelPrefix)
    if ($got.Count -eq 0) {
        Fail ("{0} setup dependency resolution downloaded no artifact archives; ordinary go get is expected to resolve every pinned artifact" -f $Name)
    }
    $expected = @($script:ArtifactRequires | ForEach-Object { '{0}@{1}' -f $_.Path, $_.Version } | Sort-Object -Unique)
    foreach ($entry in $got) {
        $at = $entry.LastIndexOf('@')
        if ($at -le 0) {
            Fail "cannot parse setup artifact archive identity: $entry"
        }
        $path = $entry.Substring(0, $at)
        $version = $entry.Substring($at + 1)
        $require = @($script:ArtifactRequires | Where-Object { $_.Path -eq $path })
        if ($require.Count -eq 0) {
            Fail ("{0} setup downloaded artifact archive {1} that the downloaded root go.mod does not require" -f $Name, $entry)
        }
        if ($require.Count -ne 1) {
            Fail "downloaded root go.mod has duplicate requirements for $path"
        }
        if ($require[0].Version -cne $version) {
            Fail ("{0} setup artifact archive {1} is at {2}, want pinned {3}" -f $Name, $path, $version, $require[0].Version)
        }
    }
    $missing = @($expected | Where-Object { $got -notcontains $_ })
    if ($missing.Count -ne 0) {
        Fail ("{0} setup dependency resolution is missing pinned artifact archives: {1}" -f $Name, ($missing -join ','))
    }
    Write-Summary "$Name.setup_artifact_zips=$($got -join ',')"
}

function Assert-RootZip([string]$LabelPrefix) {
    $versions = @(Get-CacheRows $LabelPrefix |
        Where-Object { $_.module_path -eq $script:RootModule -and $_.kind -eq 'zip' } |
        ForEach-Object { $_.version } |
        Sort-Object -Unique)
    if ($versions.Count -ne 1 -or $versions[0] -cne $script:RootVersion) {
        Fail ("root module zip for {0} is {1}, want [{2}]" -f $LabelPrefix, ($versions -join ','), $script:RootVersion)
    }
}

function Assert-OnlySelectedArtifactZip([string]$LabelPrefix) {
    $got = @(Get-ArtifactZips $LabelPrefix)
    $expected = '{0}@{1}' -f $script:SelectedPath, $script:SelectedVersion
    if ($got.Count -ne 1 -or $got[0] -cne $expected) {
        Fail ("artifact zips for {0} are {1}, want exactly [{2}]" -f $LabelPrefix, ($got -join ','), $expected)
    }
}

function Assert-RootArchive([string]$Zip, [string]$Label, [string[]]$NestedModules) {
    $out = Join-Path $script:EvidenceDir ("verify-root-archive-" + ($Label -replace '/', '-') + ".json")
    $arguments = @(
        $script:Verify, 'archive',
        '--zip', $Zip,
        '--repo', $script:RepoDir,
        '--module', $script:RootModule,
        '--version', $script:RootVersion,
        '--source-commit', $script:SourceHead,
        '--out', $out
    )
    foreach ($module in $NestedModules) {
        $arguments += @('--nested-module', $module)
    }
    $null = Invoke-Python $arguments $script:CommandsLog
    $report = Read-JsonFile $out
    Write-Summary "$Label.root_archive_files=$($report.expected_files)"
    Write-Summary "$Label.root_archive_nested_excluded=$($report.nested_modules_excluded -join ',')"
}

function Enter-ColdTargetPhase($Mode) {
    Assert-CacheScopeBoundary $Mode
    foreach ($entry in @(
        @('setup_gomodcache', $Mode.SetupModCache),
        @('setup_gocache', $Mode.SetupBuildCache),
        @('cold_gomodcache', $Mode.ModCache),
        @('cold_gocache', $Mode.BuildCache)
    )) {
        Write-Evidence $script:EnvironmentFile ("cold_boundary.{0}.{1}={2}" -f $Mode.Name, $entry[0], $entry[1])
    }
    Write-Evidence $script:CommandsLog ("# cold-target boundary for {0}; setup dependency resolution is complete" -f $Mode.Name)
    Set-GoEnvironment -ModCache $Mode.ModCache -BuildCache $Mode.BuildCache
    $out = Join-Path $Mode.Dir 'cold-go-env.txt'
    $null = Invoke-GoCapture $Mode.Consumer @('env', 'GOMODCACHE', 'GOCACHE') $out $script:CommandsLog
    $lines = @(Get-Content -LiteralPath $out -Encoding utf8 | Where-Object { $_ -ne '' })
    if ($lines.Count -ne 2) {
        Fail "$($Mode.Name) go env GOMODCACHE GOCACHE returned $($lines.Count) lines"
    }
    Assert-EqualPath "$($Mode.Name) go env GOMODCACHE" $lines[0].Trim() $Mode.ModCache
    Assert-EqualPath "$($Mode.Name) go env GOCACHE" $lines[1].Trim() $Mode.BuildCache
    Assert-EmptyCacheDirectory $Mode.ModCache "$($Mode.Name) cold target module"
    Assert-EmptyCacheDirectory $Mode.BuildCache "$($Mode.Name) cold target build"
    foreach ($moduleFile in @('go.mod', 'go.sum')) {
        $saved = Join-Path $script:EvidenceDir ("{0}-setup-{1}" -f $Mode.Name, $moduleFile)
        Copy-Item -LiteralPath (Join-Path $Mode.Consumer $moduleFile) -Destination $saved
        Write-Evidence $script:EnvironmentFile ("cold_boundary.{0}.setup_{1}={2}" -f $Mode.Name, $moduleFile, $saved)
    }
    $fingerprint = Get-ConsumerModuleFingerprint $Mode
    $Mode | Add-Member -NotePropertyName ModuleFingerprint -NotePropertyValue $fingerprint -Force
    Write-Evidence $script:EnvironmentFile ("cold_boundary.{0}.consumer_go_mod={1}" -f $Mode.Name, $fingerprint)
    Save-ModuleCacheSnapshot "$($Mode.Name)/cold/boundary" $Mode.ModCache
    Write-Summary "$($Mode.Name).cold_cache_scope=empty_after_setup"
}

function Resolve-ColdRoot($Mode, [string]$SetupZip, [string[]]$NestedModules) {
    $jsonPath = Join-Path $Mode.Dir 'cold-root-download.json'
    $null = Invoke-GoCapture $Mode.Consumer @('mod', 'download', '-json', ($script:RootModule + '@' + $script:RootVersion)) $jsonPath $script:CommandsLog
    $json = Read-JsonFile $jsonPath
    $errorText = Get-JsonField $json 'Error'
    if ($errorText) {
        Fail "$($Mode.Name) cold root download failed: $errorText"
    }
    Assert-Equal "$($Mode.Name) cold root version" (Get-JsonField $json 'Version') $script:RootVersion
    Assert-Equal "$($Mode.Name) cold root sum" (Get-JsonField $json 'Sum') $script:RootSum
    Assert-VersionIdentity $script:RootVersion $script:SourceHead $script:RootRevision
    $zip = Get-JsonField $json 'Zip'
    $goMod = Get-JsonField $json 'GoMod'
    if (-not $zip -or -not $goMod -or -not (Test-Path -LiteralPath $zip)) {
        Fail "$($Mode.Name) cold root download returned incomplete metadata"
    }
    Assert-Equal "$($Mode.Name) root zip bytes at setup and cold target caches" (Get-FileSha256 $SetupZip) (Get-FileSha256 $zip)
    Assert-Equal "$($Mode.Name) root go.mod bytes at setup and cold target caches" (Get-FileSha256 $script:SetupRootGoMod) (Get-FileSha256 $goMod)
    Save-ModuleCacheSnapshot "$($Mode.Name)/cold/root-resolve" $Mode.ModCache
    Assert-RootZip "$($Mode.Name)/cold/root-resolve"
    Assert-RootArchive $zip ("$($Mode.Name)-cold") $NestedModules
    return $zip
}

function Read-Pins {
    $path = Join-Path $script:RepoDir 'internal/ffi/upstream.go'
    if (-not (Test-Path -LiteralPath $path)) {
        Fail "missing source pin file: $path"
    }
    $text = Get-Content -LiteralPath $path -Raw -Encoding utf8
    $script:Pins = [pscustomobject]@{
        Repo = (Get-RegexValue $text '(?m)^\s*upstreamRepo\s*=\s*"([^"]+)"' 'upstreamRepo')
        Commit = (Get-RegexValue $text '(?m)^\s*upstreamCommit\s*=\s*"([^"]+)"' 'upstreamCommit')
        Header = (Get-RegexValue $text '(?m)^\s*upstreamHeaderSHA256\s*=\s*"([^"]+)"' 'upstreamHeaderSHA256')
    }
    if ($script:Pins.Commit -notmatch '^[0-9a-f]{40}$') {
        Fail "source pin upstreamCommit is not a full commit: $($script:Pins.Commit)"
    }
    if ($script:Pins.Header -notmatch '^[0-9a-f]{64}$') {
        Fail "source pin upstreamHeaderSHA256 is not a sha256: $($script:Pins.Header)"
    }
    Write-Summary "source_pin.repo=$($script:Pins.Repo)"
    Write-Summary "source_pin.commit=$($script:Pins.Commit)"
    Write-Summary "source_pin.header_sha256=$($script:Pins.Header)"
}

function Assert-GitTracked([string]$RelativePath) {
    Write-CommandLog $script:RepoDir @('git', 'ls-files', '--error-unmatch', '--', $RelativePath)
    $tracked = & git -C $script:RepoDir ls-files --error-unmatch -- $RelativePath 2>> $script:CommandsLog
    if ($LASTEXITCODE -ne 0 -or -not $tracked) {
        Fail "artifact file is not tracked at HEAD: $RelativePath"
    }
    Write-CommandLog $script:RepoDir @('git', 'diff', '--quiet', 'HEAD', '--', $RelativePath)
    & git -C $script:RepoDir diff --quiet HEAD -- $RelativePath 2>> $script:CommandsLog
    if ($LASTEXITCODE -ne 0) {
        Fail "artifact file differs from the checked-out revision: $RelativePath"
    }
}

function Assert-TrackedArtifact($Meta) {
    $leaf = Split-Path -Leaf $Meta.Path
    $trackedDir = Join-Path (Join-Path $script:RepoDir 'artifacts') $leaf
    if (-not (Test-Path -LiteralPath $trackedDir)) {
        Fail ("tracked Windows artifact is missing: {0}; publish and adopt the static-CRT Windows artifact module before this lane can pass" -f $trackedDir)
    }
    # Payload bytes live at the pinned publication commit. Matching artifact.go
    # binds Read-ArtifactModule's payload size/hash checks to this SDK revision.
    foreach ($name in @('artifact.go', 'go.mod', 'LICENSE')) {
        $tracked = Join-Path $trackedDir $name
        $published = Join-Path $Meta.Dir $name
        if (-not (Test-Path -LiteralPath $tracked)) {
            Fail "tracked artifact file is missing: $tracked"
        }
        if (-not (Test-Path -LiteralPath $published)) {
            Fail "published artifact module is missing: $published"
        }
        Assert-GitTracked ("artifacts/{0}/{1}" -f $leaf, $name)
        Assert-Equal "$name sha256" (Get-FileSha256 $published) (Get-FileSha256 $tracked)
    }
    Write-Summary "artifact.$($Meta.Label).tracked_dir=$trackedDir"
}

function Read-ArtifactModule($Json, [string]$Label) {
    $path = [string]$Json.Path
    $version = [string]$Json.Version
    $sum = [string]$Json.Sum
    $dir = [string]$Json.Dir
    $zip = [string]$Json.Zip
    if (-not $path -or -not $version -or -not $sum -or -not $dir -or -not $zip) {
        Fail "go mod download returned incomplete artifact metadata for $Label"
    }
    if (-not (Test-Path -LiteralPath $dir)) {
        Fail "artifact module directory is missing: $dir"
    }
    if (-not (Test-Path -LiteralPath $zip)) {
        Fail "artifact module zip is missing: $zip"
    }
    $artifactGo = Join-Path $dir 'artifact.go'
    $goMod = Join-Path $dir 'go.mod'
    if (-not (Test-Path -LiteralPath $artifactGo)) {
        Fail "artifact module is missing artifact.go: $dir"
    }
    if (-not (Test-Path -LiteralPath $goMod)) {
        Fail "artifact module is missing go.mod: $dir"
    }
    $text = Get-Content -LiteralPath $artifactGo -Raw -Encoding utf8
    $meta = [pscustomobject]@{
        Label = $Label
        Path = $path
        Version = $version
        Sum = $sum
        Dir = $dir
        Zip = $zip
        Target = Get-ArtifactFieldString $text 'Target'
        LibraryName = Get-ArtifactFieldString $text 'LibraryName'
        CompressedSize = Get-ArtifactFieldInt $text 'CompressedSize'
        CompressedSHA256 = Get-ArtifactFieldString $text 'CompressedSHA256'
        UncompressedSize = Get-ArtifactFieldInt $text 'UncompressedSize'
        UncompressedSHA256 = Get-ArtifactFieldString $text 'UncompressedSHA256'
        UpstreamRepo = Get-ArtifactFieldString $text 'UpstreamRepo'
        UpstreamCommit = Get-ArtifactFieldString $text 'UpstreamCommit'
        HeaderSHA256 = Get-ArtifactFieldString $text 'HeaderSHA256'
    }
    Assert-Equal "$Label artifact target" $meta.Target "windows/$script:TargetGoarch"
    Assert-Equal "$Label artifact library" $meta.LibraryName $script:LibraryName
    if ($meta.CompressedSize -le 0 -or $meta.UncompressedSize -le 0) {
        Fail "$Label artifact sizes must be positive"
    }
    if ($meta.CompressedSHA256 -notmatch '^[0-9a-f]{64}$') {
        Fail "$Label compressed sha256 is not a sha256: $($meta.CompressedSHA256)"
    }
    if ($meta.UncompressedSHA256 -notmatch '^[0-9a-f]{64}$') {
        Fail "$Label uncompressed sha256 is not a sha256: $($meta.UncompressedSHA256)"
    }
    Assert-Equal "$Label upstream repo" $meta.UpstreamRepo $script:Pins.Repo
    Assert-Equal "$Label upstream commit" $meta.UpstreamCommit $script:Pins.Commit
    Assert-Equal "$Label upstream header" $meta.HeaderSHA256 $script:Pins.Header

    $moduleText = Get-Content -LiteralPath $goMod -Raw -Encoding utf8
    Assert-Equal "$Label go.mod module" (Get-RegexValue $moduleText '(?m)^module\s+(\S+)' 'module') $path
    if ($moduleText -match '(?m)^\s*(replace|exclude)\s') {
        Fail "$Label artifact go.mod contains replace or exclude"
    }
    $goDirective = Get-RegexValue $moduleText '(?m)^go\s+(\S+)' 'go directive'
    if ($goDirective -notmatch '^1\.23\.') {
        Fail "$Label artifact go directive is $goDirective, want 1.23.x"
    }
    $payloads = @(Get-ChildItem -LiteralPath $dir -File | Where-Object { $_.Name -like '*.zst' })
    if ($payloads.Count -ne 1) {
        Fail "$Label artifact has $($payloads.Count) compressed payloads, want one"
    }
    $payload = $payloads[0]
    Assert-Equal "$Label payload filename" $payload.Name ($script:LibraryName + '.zst')
    $meta | Add-Member -NotePropertyName Payload -NotePropertyValue $payload.FullName
    Assert-TrackedArtifact $meta

    $payloadOut = Join-Path $script:EvidenceDir ("verify-$($Label -replace '/', '-')-payload.json")
    $arguments = @(
        $script:Verify, 'payload',
        '--zip', $zip,
        '--module', $path,
        '--version', $version,
        '--name', ($script:LibraryName + '.zst'),
        '--artifact-file', $payload.FullName,
        '--expect-size', [string]$meta.CompressedSize,
        '--expect-sha256', $meta.CompressedSHA256,
        '--out', $payloadOut
    )
    $null = Invoke-Python $arguments $script:CommandsLog
    $line = @(
        $Label,
        $path,
        $version,
        $sum,
        $meta.Target,
        $meta.LibraryName,
        [string]$meta.CompressedSize,
        $meta.CompressedSHA256,
        [string]$meta.UncompressedSize,
        $meta.UncompressedSHA256,
        $meta.UpstreamRepo,
        $meta.UpstreamCommit,
        $meta.HeaderSHA256,
        $zip,
        $payload.FullName
    ) -join "`t"
    Write-Evidence $script:ArtifactTsv $line
    Write-Summary "artifact.$Label=$path@$version"
    Write-Summary "artifact.$Label.sum=$sum"
    Write-Summary "artifact.$Label.uncompressed_sha256=$($meta.UncompressedSHA256)"
    return $meta
}

function Download-ArtifactModule($Mode, [string]$Path, [string]$Version, [string]$Label) {
    $jsonPath = Join-Path $Mode.Dir ('artifact-' + ($Label -replace '/', '-') + '.json')
    $null = Invoke-GoCapture $Mode.Consumer @('mod', 'download', '-json', ($Path + '@' + $Version)) $jsonPath $script:CommandsLog
    $json = Read-JsonFile $jsonPath
    $errorText = Get-JsonField $json 'Error'
    if ($errorText) {
        Fail "artifact download failed for ${Path}@${Version}: $errorText"
    }
    Assert-Equal "$Label artifact path" (Get-JsonField $json 'Path') $Path
    Assert-Equal "$Label artifact version" (Get-JsonField $json 'Version') $Version
    $meta = Read-ArtifactModule $json $Label
    Save-ModuleCacheSnapshot "$Label/cold/artifact" $Mode.ModCache
    return $meta
}

function Compare-ArtifactModules($Left, $Right) {
    foreach ($name in @(
        'Path', 'Version', 'Sum', 'Target', 'LibraryName', 'CompressedSize',
        'CompressedSHA256', 'UncompressedSize', 'UncompressedSHA256',
        'UpstreamRepo', 'UpstreamCommit', 'HeaderSHA256'
    )) {
        if ([string]$Left.$name -cne [string]$Right.$name) {
            Fail ("artifact metadata {0} differs between {1} and {2}" -f $name, $Left.Label, $Right.Label)
        }
    }
}

function Get-ArtifactDependencies([string]$MetadataFile) {
    $result = New-Object System.Collections.Generic.List[string]
    foreach ($line in (Get-Content -LiteralPath $MetadataFile -Encoding utf8)) {
        $fields = $line -split "`t"
        if ($fields.Count -ge 5 -and $fields[1] -eq 'dep' -and $fields[2].StartsWith($script:RootModule + '/artifacts/')) {
            $result.Add(('{0}@{1}' -f $fields[2], $fields[3]))
        }
    }
    return @($result | Sort-Object -Unique)
}

function Get-RootDependency([string]$MetadataFile) {
    foreach ($line in (Get-Content -LiteralPath $MetadataFile -Encoding utf8)) {
        $fields = $line -split "`t"
        if ($fields.Count -ge 5 -and $fields[1] -eq 'dep' -and $fields[2] -eq $script:RootModule) {
            return [pscustomobject]@{ Version = $fields[3]; Sum = $fields[4] }
        }
    }
    return $null
}

function Get-BuildTags([string]$MetadataFile) {
    foreach ($line in (Get-Content -LiteralPath $MetadataFile -Encoding utf8)) {
        $fields = $line -split "`t"
        if ($fields.Count -ge 3 -and $fields[1] -eq 'build' -and $fields[2].StartsWith('-tags=')) {
            return @($fields[2].Substring(6) -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
        }
    }
    return @()
}

function Assert-NoTag([string]$MetadataFile, [string]$Tag) {
    if (@(Get-BuildTags $MetadataFile) -contains $Tag) {
        Fail "binary unexpectedly has build tag $Tag"
    }
}

function Get-GoVersionMetadata($Mode, [string]$Binary, [string]$Label) {
    $path = Join-Path $script:EvidenceDir ("metadata-$($Label -replace '/', '-').txt")
    $null = Invoke-GoCapture $Mode.Consumer @('version', '-m', $Binary) $path $script:CommandsLog
    return $path
}

function Assert-SelectedArtifactFromMetadata([string]$MetadataFile, [string]$Label) {
    $dependencies = @(Get-ArtifactDependencies $MetadataFile)
    if ($dependencies.Count -eq 0) {
        Fail ("{0} was built without a Windows artifact dependency; the published {1}/artifacts/windows-{2} module is not adopted or its selector is inactive (no skip)" -f $Label, $script:RootModule, $script:TargetGoarch)
    }
    if ($dependencies.Count -ne 1) {
        Fail ("{0} lists {1} artifact dependencies, want one" -f $Label, $dependencies.Count)
    }
    $dependency = $dependencies[0]
    $at = $dependency.LastIndexOf('@')
    $path = $dependency.Substring(0, $at)
    $version = $dependency.Substring($at + 1)
    $expectedPath = $script:RootModule + '/artifacts/windows-' + $script:TargetGoarch
    Assert-Equal "$Label selected artifact path" $path $expectedPath
    Assert-Equal "$Label selected artifact version" $version $script:RequiredWindows.Version
    return [pscustomobject]@{ Path = $path; Version = $version }
}

function Assert-BinaryMetadata {
    param(
        $Mode,
        [string]$Binary,
        [string]$MetadataFile,
        [string]$Payload,
        [string]$ExpectedArtifact,
        [string]$ExpectedPayload,
        [string]$ExpectedTag,
        $ArtifactMeta,
        [string]$Label
    )
    $out = Join-Path $script:EvidenceDir ("verify-$($Label -replace '/', '-')-binary.json")
    $arguments = @(
        $script:Verify, 'binary',
        '--binary', $Binary,
        '--metadata', $MetadataFile,
        '--payload', $Payload,
        '--root-module', $script:RootModule,
        '--expect-root-version', $script:RootVersion,
        '--expect-root-sum', $script:RootSum,
        '--expect-artifact', $ExpectedArtifact,
        '--expect-payload', $ExpectedPayload,
        '--goos', 'windows',
        '--goarch', $script:TargetGoarch,
        '--out', $out
    )
    if ($ExpectedArtifact -ne 'none') {
        if (-not $ArtifactMeta) {
            Fail "$Label has an artifact dependency but no artifact metadata"
        }
        $arguments += @('--expect-sum', $ArtifactMeta.Sum)
    }
    if ($ExpectedTag) {
        $arguments += @('--expect-tag', $ExpectedTag)
    }
    $null = Invoke-Python $arguments $script:CommandsLog
    $report = Read-JsonFile $out
    Write-Summary "$Label.binary_artifact=$ExpectedArtifact"
    Write-Summary "$Label.binary_payload=$ExpectedPayload"
    Write-Summary "$Label.binary_payload_offset=$($report.payload_offset)"
}

function Assert-SelectedArtifactRequired {
    $line = '{0} {1}' -f $script:SelectedPath, $script:SelectedVersion
    $found = Select-String -LiteralPath $script:RequiredArtifacts -SimpleMatch -Pattern $line -Quiet
    if (-not $found) {
        Fail ("downloaded root go.mod does not require {0}" -f $line)
    }
}

function Assert-DefaultDeps([string]$DepsFile) {
    $deps = @(Get-Content -LiteralPath $DepsFile -Encoding utf8 | Where-Object { $_ })
    $artifacts = @($deps | Where-Object { $_.StartsWith($script:RootModule + '/artifacts/') })
    if ($artifacts.Count -ne 1 -or $artifacts[0] -cne $script:SelectedPath) {
        Fail ("default dependency graph artifact packages are {0}, want [{1}]" -f ($artifacts -join ','), $script:SelectedPath)
    }
    if (-not ($deps -contains $script:RootModule)) {
        Fail 'default dependency graph is missing the root package'
    }
}

function Assert-NamespaceDeps([string]$DepsFile) {
    $deps = @(Get-Content -LiteralPath $DepsFile -Encoding utf8 | Where-Object { $_ })
    $artifacts = @($deps | Where-Object { $_.StartsWith($script:RootModule + '/artifacts/') })
    if ($artifacts.Count -ne 1 -or $artifacts[0] -cne $script:SelectedPath) {
        Fail ("namespace dependency graph artifact packages are {0}, want [{1}]" -f ($artifacts -join ','), $script:SelectedPath)
    }
    if (-not ($deps -contains ($script:RootModule + '/namespace'))) {
        Fail 'namespace dependency graph is missing the namespace package'
    }
    if ($deps -contains $script:RootModule) {
        Fail 'namespace consumer must not import the root Go package'
    }
}

function Assert-ExternalDeps([string]$DepsFile) {
    $deps = @(Get-Content -LiteralPath $DepsFile -Encoding utf8 | Where-Object { $_ })
    $artifacts = @($deps | Where-Object { $_.StartsWith($script:RootModule + '/artifacts/') })
    if ($artifacts.Count -ne 0) {
        Fail ("external dependency graph links artifact packages: {0}" -f ($artifacts -join ','))
    }
    if (-not ($deps -contains $script:RootModule)) {
        Fail 'external dependency graph is missing the root package'
    }
}

function Write-AclFacts([string]$Label, $Mode, [string]$CacheLibrary) {
    $cacheRoot = Join-Path $Mode.LocalAppData 'lance-go'
    $cacheEntry = Split-Path -Parent $CacheLibrary
    foreach ($entry in @(
        @('localappdata', $Mode.LocalAppData),
        @('cache_base', $cacheRoot),
        @('cache_entry', $cacheEntry),
        @('cache_lock', (Join-Path $cacheEntry '.lock')),
        @('cache_library', $CacheLibrary)
    )) {
        $kind = $entry[0]
        $path = $entry[1]
        if (-not (Test-Path -LiteralPath $path)) {
            Fail "cannot record ACL for missing path: $path"
        }
        $item = Get-Item -LiteralPath $path -Force
        $acl = Get-Acl -LiteralPath $path
        $readonly = (($item.Attributes -band [IO.FileAttributes]::ReadOnly) -ne 0)
        $line = @(
            $Label,
            $kind,
            $path,
            [string]$item.Attributes,
            [string]$readonly,
            $acl.Owner,
            ($acl.Sddl -replace "`t", ' ')
        ) -join "`t"
        Write-Evidence $script:AclTsv $line
    }
    Write-Summary "$Label.acl_source=inherited_localappdata"
    Write-Summary "$Label.cache_root=$cacheRoot"
}

function Assert-CachePresent($Mode, $Meta, [string]$Label) {
    $cacheRoot = Join-Path $Mode.LocalAppData 'lance-go'
    $cacheEntry = Join-Path $cacheRoot $Meta.UncompressedSHA256
    $cacheLibrary = Join-Path $cacheEntry $Meta.LibraryName
    if (-not (Test-Path -LiteralPath $cacheLibrary)) {
        Fail "cached library is missing after runtime: $cacheLibrary"
    }
    $out = Join-Path $script:EvidenceDir ("verify-$($Label -replace '/', '-')-cache.json")
    $arguments = @(
        $script:Verify, 'cache',
        '--root', $Mode.LocalAppData,
        '--expect', 'present',
        '--platform', 'windows',
        '--sha', $Meta.UncompressedSHA256,
        '--library', $Meta.LibraryName,
        '--size', [string]$Meta.UncompressedSize,
        '--out', $out
    )
    $null = Invoke-Python $arguments $script:CommandsLog
    Write-AclFacts $Label $Mode $cacheLibrary
    Assert-Equal "$Label cached DLL sha256" (Get-FileSha256 $cacheLibrary) $Meta.UncompressedSHA256
    Write-Summary "$Label.cached_library=$cacheLibrary"
    return $cacheLibrary
}

function Assert-BinaryPE([string]$Binary, [string]$Label) {
    $out = Join-Path $script:EvidenceDir ("verify-$($Label -replace '/', '-')-exe-pe.json")
    $arguments = @(
        $script:Verify, 'pe',
        '--binary', $Binary,
        '--expect-machine', $script:TargetGoarch,
        '--out', $out
    )
    $null = Invoke-Python $arguments $script:CommandsLog
    $report = Read-JsonFile $out
    Write-Summary "$Label.exe_pe_machine=$($report.machine)"
}

function Assert-CachePE([string]$CacheLibrary, [string]$Label) {
    $out = Join-Path $script:EvidenceDir ("verify-$($Label -replace '/', '-')-pe.json")
    $arguments = @(
        $script:Verify, 'pe',
        '--binary', $CacheLibrary,
        '--expect-machine', $script:TargetGoarch,
        '--expect-dll',
        '--out', $out
    )
    $null = Invoke-Python $arguments $script:CommandsLog
    $report = Read-JsonFile $out
    Write-Summary "$Label.pe_machine=$($report.machine)"
    Write-Summary "$Label.pe_dll=$($report.expect_dll)"
}

function Move-CacheAside($Mode, [string]$Label) {
    $source = Join-Path $Mode.LocalAppData 'lance-go'
    $destination = Join-Path $Mode.Dir ("verified-cache-" + ($Label -replace '/', '-'))
    if (-not (Test-Path -LiteralPath $source)) {
        Fail "cache to move is missing: $source"
    }
    if (Test-Path -LiteralPath $destination) {
        Fail "cache destination already exists: $destination"
    }
    Move-Item -LiteralPath $source -Destination $destination
    if (Test-Path -LiteralPath $source) {
        Fail "cache still exists after move: $source"
    }
    Write-Summary "$Label.cache_moved_to=$destination"
}

function Copy-SuppliedDll([string]$DefaultCacheLibrary, $Meta) {
    $suppliedDir = Join-Path $script:WorkDir 'supplied'
    New-Item -ItemType Directory -Force -Path $suppliedDir | Out-Null
    $supplied = Join-Path $suppliedDir 'lance_c.dll'
    Copy-Item -LiteralPath $DefaultCacheLibrary -Destination $supplied
    Assert-Equal 'supplied DLL sha256' (Get-FileSha256 $supplied) $Meta.UncompressedSHA256
    Assert-Equal 'supplied DLL size' ([string](Get-Item -LiteralPath $supplied).Length) ([string]$Meta.UncompressedSize)
    Write-Summary "external.supplied_dll=$supplied"
    return $supplied
}

function Assert-NoRepoDll([string]$Path, [string]$Label) {
    $repoPrefix = [IO.Path]::GetFullPath($script:RepoDir).TrimEnd('\') + '\'
    $full = [IO.Path]::GetFullPath($Path)
    if ($full.StartsWith($repoPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        Fail "$Label loaded a DLL from the repository/build path: $full"
    }
}

function Assert-IdentityOutput([string]$Text, [string]$ExpectedDll, $Meta, [string]$Label) {
    $loadedDll = @($Text -split "`r?`n" | Where-Object { $_ -like 'loaded_dll=*' })
    if ($loadedDll.Count -ne 1) {
        Fail "$Label identity reported $($loadedDll.Count) loaded_dll lines, want one"
    }
    $actualDll = $loadedDll[0].Substring('loaded_dll='.Length)
    $expectedFull = [IO.Path]::GetFullPath($ExpectedDll)
    $actualFull = [IO.Path]::GetFullPath($actualDll)
    if ($actualFull -ine $expectedFull) {
        Fail "$Label loaded DLL is $actualFull, want $expectedFull"
    }
    Assert-NoRepoDll $actualFull $Label
    $sizeLines = @($Text -split "`r?`n" | Where-Object { $_ -like 'loaded_dll_file_size=*' })
    if ($sizeLines.Count -ne 1) {
        Fail "$Label identity did not report loaded_dll_file_size"
    }
    Assert-Equal "$Label loaded DLL size" $sizeLines[0].Substring('loaded_dll_file_size='.Length) ([string]$Meta.UncompressedSize)
    Assert-Equal "$Label loaded DLL sha256" (Get-FileSha256 $actualFull) $Meta.UncompressedSHA256

    $modules = New-Object System.Collections.Generic.List[object]
    foreach ($line in ($Text -split "`r?`n")) {
        if ($line -match "^loaded_module=([^\t]+)\t(.+)$") {
            $modules.Add([pscustomobject]@{ Name = $Matches[1]; Path = $Matches[2] })
        }
    }
    if ($modules.Count -eq 0) {
        Fail "$Label identity did not report loaded modules"
    }
    $lanceModules = @($modules | Where-Object { $_.Name -eq 'lance_c.dll' })
    if ($lanceModules.Count -ne 1 -or $lanceModules[0].Path -ine $actualFull) {
        Fail "$Label identity reported $($lanceModules.Count) lance_c.dll modules, want the loaded path once"
    }
    $forbidden = @($modules | Where-Object {
        $_.Name -match '^(vcruntime.*|msvcp[0-9].*|concrt.*|mfc.*|vcomp.*|ucrtbased.*)\.dll$'
    } | ForEach-Object { $_.Name })
    if ($forbidden.Count -gt 0) {
        Fail ("{0} loaded redistributable runtime DLLs: {1}" -f $Label, ($forbidden -join ','))
    }
    $moduleTsv = Join-Path $script:EvidenceDir ("loaded-modules-$($Label -replace '/', '-').tsv")
    Set-Content -LiteralPath $moduleTsv -Value "name`tpath" -Encoding utf8
    foreach ($module in $modules) {
        Write-Evidence $moduleTsv ("{0}`t{1}" -f $module.Name, $module.Path)
    }
    Write-Summary "$Label.loaded_dll=$actualFull"
    Write-Summary "$Label.loaded_module_count=$($modules.Count)"
    Write-Summary "$Label.redistributable_modules=none"
}

function Add-IdentityHelperSource([string]$Label, $Mode) {
    # Resolve the helper's pinned dependency during setup. Building the helper
    # in the cold target phase must not run go get and contaminate its archive
    # observations.
    $source = Join-Path $script:PublicConsumerDir 'windows-consumer/main.go'
    if (-not (Test-Path -LiteralPath $source)) {
        Fail "identity fixture is missing: $source"
    }
    if (-not $script:XsysVersion) {
        Fail 'root go.mod does not require golang.org/x/sys; cannot prepare the loaded-module identity helper'
    }
    $identityDir = Join-Path $Mode.Consumer 'identitycmd'
    New-Item -ItemType Directory -Force -Path $identityDir | Out-Null
    $identitySource = Join-Path $identityDir 'main.go'
    Copy-Item -LiteralPath $source -Destination $identitySource
    Write-CommandLog $Mode.Consumer @('copy', $source, $identitySource)
    $null = Invoke-Go $Mode.Consumer @('get', ('golang.org/x/sys@' + $script:XsysVersion)) $script:CommandsLog
    $consumerMod = Get-Content -LiteralPath (Join-Path $Mode.Consumer 'go.mod') -Raw -Encoding utf8
    $requires = @(Parse-GoModRequires $consumerMod | Where-Object { $_.Path -eq 'golang.org/x/sys' })
    if ($requires.Count -ne 1 -or $requires[0].Version -cne $script:XsysVersion) {
        Fail ("{0} identity helper dependency golang.org/x/sys is {1}, want the root-pinned {2}" -f $Label, (($requires | ForEach-Object { $_.Version }) -join ','), $script:XsysVersion)
    }
    Write-Summary "$Label.identity_helper=golang.org/x/sys@$($script:XsysVersion)"
    Save-ModuleCacheSnapshot "$Label/setup/identity-helper" $Mode.SetupModCache
}

function Build-IdentityHelper($Mode, [string]$Label, [bool]$External) {
    $identityDir = Join-Path $Mode.Consumer 'identitycmd'
    if (-not (Test-Path -LiteralPath (Join-Path $identityDir 'main.go'))) {
        Fail "identity helper source was not prepared during setup: $identityDir"
    }
    $tags = @()
    if ($External) {
        $tags = @('-tags', 'lance_external_lib')
    }
    $identityExe = Join-Path $Mode.Dir 'windows-consumer.exe'
    $buildArguments = @('build') + $tags + @('-o', $identityExe, './identitycmd')
    $null = Invoke-Go $Mode.Consumer $buildArguments $script:CommandsLog
    Save-ModuleCacheSnapshot "$Label/cold/identity-build" $Mode.ModCache
    return $identityExe
}

function Run-Identity($Mode, $Meta, [string]$ExpectedDll, [string]$Label, [string]$ExpectedArtifact, [string]$ExpectedPayload, [string]$ExpectedTag, [string]$Payload) {
    $identityExe = Build-IdentityHelper $Mode $Label ($ExpectedArtifact -eq 'none')
    Assert-BinaryPE $identityExe "$Label/identity"
    $identityMeta = Get-GoVersionMetadata $Mode $identityExe "$Label/identity"
    Assert-BinaryMetadata -Mode $Mode -Binary $identityExe -MetadataFile $identityMeta `
        -Payload $Payload -ExpectedArtifact $ExpectedArtifact `
        -ExpectedPayload $ExpectedPayload -ExpectedTag $ExpectedTag `
        -ArtifactMeta $script:SelectedMeta -Label "$Label/identity"
    if (-not $ExpectedTag) {
        Assert-NoTag $identityMeta 'lance_external_lib'
    }
    $root = Join-Path $Mode.Dir 'identity-root'
    New-Item -ItemType Directory -Force -Path $root | Out-Null
    $log = Join-Path $script:EvidenceDir ("runtime-$($Label -replace '/', '-')-identity.log")
    $text = Invoke-NativeProcess $Mode.Consumer $identityExe @('-root', $root) $log
    Assert-IdentityOutput $text $ExpectedDll $Meta $Label
}

function Run-Consumer($Mode, [string]$Binary, [string]$Label, [string[]]$Arguments, [string]$ExpectedLiteral, [string]$ExpectedPattern) {
    $log = Join-Path $script:EvidenceDir ("runtime-$($Label -replace '/', '-').log")
    $text = Invoke-NativeProcess $Mode.Consumer $Binary $Arguments $log
    if ($text -eq '') {
        Fail "$Label produced no output"
    }
    if ($ExpectedLiteral -and $text -cne $ExpectedLiteral) {
        Fail ("{0} output is {1}, want {2}" -f $Label, $text, $ExpectedLiteral)
    }
    if ($ExpectedPattern -and $text -notmatch $ExpectedPattern) {
        Fail ("{0} output does not match {1}: {2}" -f $Label, $ExpectedPattern, $text)
    }
    Write-Summary "$Label.output=$text"
}

# Main flow. Evidence is created before argument validation so configuration
# failures still leave a durable record for the workflow upload.
$script:RepoDir = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$script:PublicConsumerDir = $PSScriptRoot
$script:Verify = Join-Path $PSScriptRoot 'verify.py'
$evidenceRoot = if ([string]::IsNullOrWhiteSpace($EvidenceDir)) {
    Join-Path ([IO.Path]::GetTempPath()) 'public-consumer-evidence'
} else {
    $EvidenceDir
}
$script:EvidenceDir = Join-Path $evidenceRoot ('windows-run-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $script:EvidenceDir | Out-Null
$script:CommandsLog = Join-Path $script:EvidenceDir 'commands.log'
$script:SummaryFile = Join-Path $script:EvidenceDir 'summary.txt'
$script:EnvironmentFile = Join-Path $script:EvidenceDir 'environment.txt'
$script:RequiredArtifacts = Join-Path $script:EvidenceDir 'required-artifacts.txt'
$script:ModuleCacheTsv = Join-Path $script:EvidenceDir 'module-cache-inventory.tsv'
$script:ArtifactTsv = Join-Path $script:EvidenceDir 'artifact-metadata.tsv'
$script:AclTsv = Join-Path $script:EvidenceDir 'cache-acl.tsv'
Set-Content -LiteralPath $script:CommandsLog -Value '' -Encoding utf8
Set-Content -LiteralPath $script:SummaryFile -Value 'result=INCOMPLETE' -Encoding utf8
Set-Content -LiteralPath $script:EnvironmentFile -Value '' -Encoding utf8
Set-Content -LiteralPath $script:RequiredArtifacts -Value '' -Encoding utf8
Set-Content -LiteralPath $script:ModuleCacheTsv -Value "label`trelative`tmodule_path`tkind`tversion`tsize`tsha256`tpath" -Encoding utf8
Set-Content -LiteralPath $script:ArtifactTsv -Value "label`tpath`tversion`tsum`ttarget`tlibrary`tcompressed_size`tcompressed_sha256`tuncompressed_size`tuncompressed_sha256`tupstream_repo`tupstream_commit`theader_sha256`tzip`tpayload" -Encoding utf8
Set-Content -LiteralPath $script:AclTsv -Value "label`tkind`tpath`tattributes`treadonly`towner`tsddl" -Encoding utf8

$script:Passed = $false
$script:WorkDir = Join-Path ([IO.Path]::GetTempPath()) ('lance-public-consumer-' + [guid]::NewGuid().ToString('N'))
$exitCode = 1
try {
    if ([string]::IsNullOrWhiteSpace($RootModule)) {
        Fail 'ROOT_MODULE is required'
    }
    if ($RootModule -notmatch '^[A-Za-z0-9._~/-]+$') {
        Fail "ROOT_MODULE is not a safe module path: $RootModule"
    }
    if ([string]::IsNullOrWhiteSpace($RootRevision)) {
        Fail 'ROOT_REVISION is required'
    }
    if ($RootRevision -match '^[0-9a-f]{40}$') {
        # Full commit SHA; proxy version identity is checked below.
    } elseif ($RootRevision -notmatch '^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$') {
        Fail "ROOT_REVISION is not a full SHA or tag: $RootRevision"
    }
    if ($SourceCommit -and $SourceCommit -notmatch '^[0-9a-f]{40}$') {
        Fail "SOURCE_COMMIT is not a full SHA: $SourceCommit"
    }
    if ([string]::IsNullOrWhiteSpace($TargetGoos) -or $TargetGoos -ne 'windows') {
        Fail "TARGET_GOOS is '$TargetGoos', want 'windows'"
    }
    if ($TargetGoarch -notin @('amd64', 'arm64')) {
        Fail "TARGET_GOARCH is '$TargetGoarch', want amd64 or arm64"
    }
    if ([string]::IsNullOrWhiteSpace($LibraryName)) {
        $LibraryName = 'lance_c.dll'
    }
    if ($LibraryName -ne 'lance_c.dll') {
        Fail "LIBRARY_NAME is '$LibraryName', want 'lance_c.dll'"
    }
    if ([string]::IsNullOrWhiteSpace($TargetLabel)) {
        $TargetLabel = "windows-$TargetGoarch"
    }
    if ($TargetLabel -notmatch '^[A-Za-z0-9._-]+$') {
        Fail "TARGET_LABEL is not safe: $TargetLabel"
    }
    if ([string]::IsNullOrWhiteSpace($ExpectedProcessorArchitecture)) {
        $ExpectedProcessorArchitecture = if ($TargetGoarch -eq 'amd64') { 'AMD64' } else { 'ARM64' }
    }
    if ([string]::IsNullOrWhiteSpace($ExpectedOsName)) {
        $ExpectedOsName = if ($TargetGoarch -eq 'amd64') { 'Windows Server 2022' } else { 'Windows 11' }
    }
    $script:RootModule = $RootModule
    $script:RootRevision = $RootRevision
    $script:LibraryName = $LibraryName
    $script:TargetGoos = $TargetGoos
    $script:TargetGoarch = $TargetGoarch
    $script:TargetLabel = $TargetLabel
    $script:ExpectedProcessorArchitecture = $ExpectedProcessorArchitecture
    $script:ExpectedOsName = $ExpectedOsName
    $script:SourceCommit = $SourceCommit
    $script:RootRequires = @()

    New-Item -ItemType Directory -Force -Path $script:WorkDir | Out-Null
    $pythonCommand = Get-Command python -ErrorAction SilentlyContinue
    if (-not $pythonCommand) {
        $pythonCommand = Get-Command python3 -ErrorAction SilentlyContinue
    }
    if (-not $pythonCommand) {
        Fail 'python or python3 is required for verify.py'
    }
    $script:Python = $pythonCommand.Source
    if (-not (Test-Path -LiteralPath $script:Verify)) {
        Fail "verify.py is missing: $script:Verify"
    }
    Write-Evidence $script:EnvironmentFile "root_module=$RootModule"
    Write-Evidence $script:EnvironmentFile "root_revision=$RootRevision"
    Write-Evidence $script:EnvironmentFile "target_goos=$TargetGoos"
    Write-Evidence $script:EnvironmentFile "target_goarch=$TargetGoarch"
    Write-Evidence $script:EnvironmentFile "target_label=$TargetLabel"
    Write-Evidence $script:EnvironmentFile "library_name=$LibraryName"
    Write-Evidence $script:EnvironmentFile "source_commit=$SourceCommit"
    Write-Evidence $script:EnvironmentFile "python=$($script:Python)"
    Write-Evidence $script:EnvironmentFile "verify=$($script:Verify)"
    Write-Summary "target=$TargetGoos/$TargetGoarch"
    Write-Summary "base_revision=$RootRevision"

    $headOut = Join-Path $script:WorkDir 'git-head.txt'
    Write-CommandLog $script:RepoDir @('git', 'rev-parse', 'HEAD')
    & git -C $script:RepoDir rev-parse HEAD > $headOut 2>> $script:CommandsLog
    if ($LASTEXITCODE -ne 0) {
        Fail 'cannot resolve checkout HEAD'
    }
    $sourceHead = (Get-Content -LiteralPath $headOut -Raw).Trim()
    if ($SourceCommit) {
        Assert-Equal 'SOURCE_COMMIT and checkout HEAD' $SourceCommit $sourceHead
    } else {
        $SourceCommit = $sourceHead
        $script:SourceCommit = $SourceCommit
    }
    $script:SourceHead = $sourceHead
    Write-Summary "source_head=$sourceHead"

    if ($RootRevision -match '^[0-9a-f]{40}$') {
        Assert-Equal 'ROOT_REVISION and checkout HEAD' $RootRevision $sourceHead
    } else {
        Assert-Equal 'tag revision and checkout HEAD' (Resolve-TagCommit $RootRevision) $sourceHead
    }

    $bootstrap = Join-Path $script:WorkDir 'bootstrap'
    $bootstrapModCache = Join-Path $bootstrap 'gomodcache'
    $bootstrapBuildCache = Join-Path $bootstrap 'gocache'
    New-Item -ItemType Directory -Force -Path $bootstrapModCache, $bootstrapBuildCache | Out-Null
    Set-GoEnvironment -ModCache $bootstrapModCache -BuildCache $bootstrapBuildCache
    $goEnvJson = Join-Path $script:WorkDir 'go-env.json'
    $null = Invoke-GoCapture $script:RepoDir @('env', '-json') $goEnvJson $script:CommandsLog
    $script:GoEnv = Read-JsonFile $goEnvJson
    $goVersionFile = Join-Path $script:WorkDir 'go-version.txt'
    $null = Invoke-GoCapture $script:RepoDir @('version') $goVersionFile $script:CommandsLog
    $goVersionText = (Get-Content -LiteralPath $goVersionFile -Raw).Trim()
    Write-Summary "go_version=$goVersionText"
    Assert-NativeHost

    Read-Pins

    $default = New-Mode 'default'
    $rootDownload = Join-Path $default.Dir 'root-download.json'
    $null = Invoke-GoCapture $default.Consumer @('mod', 'download', '-json', ($RootModule + '@' + $RootRevision)) $rootDownload $script:CommandsLog
    $rootJson = Read-JsonFile $rootDownload
    $rootError = Get-JsonField $rootJson 'Error'
    if ($rootError) {
        Fail "root module download failed: $rootError"
    }
    $script:RootVersion = Get-JsonField $rootJson 'Version'
    $script:RootSum = Get-JsonField $rootJson 'Sum'
    $rootGoMod = Get-JsonField $rootJson 'GoMod'
    $rootZip = Get-JsonField $rootJson 'Zip'
    if (-not $script:RootVersion -or -not $script:RootSum -or -not $rootGoMod -or -not $rootZip) {
        Fail 'root module download returned incomplete metadata'
    }
    if (-not (Test-Path -LiteralPath $rootZip)) {
        Fail "root module zip is missing: $rootZip"
    }
    Assert-VersionIdentity $script:RootVersion $sourceHead $RootRevision
    Write-Summary "root_version=$($script:RootVersion)"
    Write-Summary "root_sum=$($script:RootSum)"
    $script:SetupRootGoMod = $rootGoMod
    Save-ModuleCacheSnapshot 'default/setup/root-resolve' $default.SetupModCache
    Assert-RootZip 'default/setup/root-resolve'

    $rootGoModText = Get-Content -LiteralPath $rootGoMod -Raw -Encoding utf8
    Assert-Equal 'downloaded go.mod module' (Get-RegexValue $rootGoModText '(?m)^module\s+(\S+)' 'module') $RootModule
    $rootGoDirective = Get-RegexValue $rootGoModText '(?m)^go\s+(\S+)' 'go directive'
    if ($rootGoDirective -notmatch '^1\.23\.') {
        Fail "downloaded go.mod go directive is $rootGoDirective, want 1.23.x"
    }
    if ($rootGoModText -match '(?m)^\s*(replace|exclude)\s') {
        Fail 'downloaded root go.mod contains replace or exclude'
    }
    $script:RootRequires = @(Parse-GoModRequires $rootGoModText)
    $script:ArtifactRequires = @($script:RootRequires | Where-Object { $_.Path.StartsWith($RootModule + '/artifacts/') })
    foreach ($require in $script:RootRequires) {
        if ($require.Path.StartsWith($RootModule + '/artifacts/')) {
            Write-Evidence $script:RequiredArtifacts ("{0} {1}" -f $require.Path, $require.Version)
        }
    }
    $windowsArtifactPath = $RootModule + '/artifacts/windows-' + $TargetGoarch
    $script:RequiredWindows = $null
    foreach ($require in $script:RootRequires) {
        if ($require.Path -eq $windowsArtifactPath) {
            $script:RequiredWindows = $require
        }
    }
    if (-not $script:RequiredWindows) {
        Fail ("downloaded root go.mod at {0} does not require {1}; the published static-CRT Windows artifact module must be adopted before this lane can pass (no skip)" -f $RootRevision, $windowsArtifactPath)
    }
    $script:XsysVersion = Get-RequiredVersion 'golang.org/x/sys'
    if (-not $script:XsysVersion) {
        Fail 'downloaded root go.mod does not require golang.org/x/sys'
    }
    Write-Summary "required_windows_artifact=$windowsArtifactPath@$($script:RequiredWindows.Version)"

    $nested = New-Object System.Collections.Generic.List[string]
    $artifactsRoot = Join-Path $script:RepoDir 'artifacts'
    if (Test-Path -LiteralPath $artifactsRoot) {
        foreach ($dir in @(Get-ChildItem -LiteralPath $artifactsRoot -Directory | Sort-Object Name)) {
            if (Test-Path -LiteralPath (Join-Path $dir.FullName 'go.mod')) {
                $nested.Add('artifacts/' + $dir.Name)
            }
        }
    }
    Assert-RootArchive $rootZip 'default-setup' $nested.ToArray()

    $null = Invoke-Go $default.Consumer @('get', ($RootModule + '@' + $script:RootVersion)) $script:CommandsLog
    Save-ModuleCacheSnapshot 'default/setup/go-get' $default.SetupModCache
    Add-IdentityHelperSource 'default' $default
    Assert-RootZip 'default/setup/'
    Assert-SetupArtifactZips 'default' 'default/setup/'

    Enter-ColdTargetPhase $default
    $null = Resolve-ColdRoot $default $rootZip $nested.ToArray()

    $defaultBinary = Join-Path $default.Dir 'roundtrip.exe'
    $null = Invoke-Go $default.Consumer @('build', '-o', $defaultBinary, ($RootModule + '/examples/roundtrip')) $script:CommandsLog
    Assert-BinaryPE $defaultBinary 'default'
    Save-ModuleCacheSnapshot 'default/cold/build' $default.ModCache
    $defaultMetaFile = Get-GoVersionMetadata $default $defaultBinary 'default'
    Assert-NoTag $defaultMetaFile 'lance_external_lib'
    $defaultSelected = Assert-SelectedArtifactFromMetadata $defaultMetaFile 'default'
    $defaultArtifact = Download-ArtifactModule $default $defaultSelected.Path $defaultSelected.Version 'default'
    $script:SelectedPath = $defaultArtifact.Path
    $script:SelectedVersion = $defaultArtifact.Version
    $script:SelectedMeta = $defaultArtifact
    $script:DefaultPayload = $defaultArtifact.Payload
    Assert-SelectedArtifactRequired
    Assert-RootZip 'default/cold/build'
    Assert-OnlySelectedArtifactZip 'default/cold/build'

    $defaultDeps = Join-Path $default.Dir 'default-deps.txt'
    $null = Invoke-GoCapture $default.Consumer @('list', '-deps', ($RootModule + '/examples/roundtrip')) $defaultDeps $script:CommandsLog
    Save-ModuleCacheSnapshot 'default/cold/deps' $default.ModCache
    Assert-RootZip 'default/cold/'
    Assert-OnlySelectedArtifactZip 'default/cold/'
    $defaultArtifactMods = @(Get-ArtifactMods 'default/cold/')
    Write-Summary ("default.artifact_mods_downloaded=" + ($defaultArtifactMods -join ','))
    Assert-DefaultDeps $defaultDeps
    Assert-BinaryMetadata -Mode $default -Binary $defaultBinary -MetadataFile $defaultMetaFile `
        -Payload $defaultArtifact.Payload -ExpectedArtifact ('{0}@{1}' -f $script:SelectedPath, $script:SelectedVersion) `
        -ExpectedPayload 'present' -ExpectedTag '' -ArtifactMeta $defaultArtifact -Label 'default'

    Assert-CacheAbsent $default 'default/before-runtime'
    Run-Consumer $default $defaultBinary 'default' @() 'Created, reopened and scanned 3 rows: [10 20 30]' ''
    $defaultCacheLibrary = Assert-CachePresent $default $defaultArtifact 'default'
    Assert-CachePE $defaultCacheLibrary 'default'
    $script:SuppliedLib = Copy-SuppliedDll $defaultCacheLibrary $defaultArtifact
    Run-Identity $default $defaultArtifact $defaultCacheLibrary 'default' ('{0}@{1}' -f $script:SelectedPath, $script:SelectedVersion) 'present' '' $defaultArtifact.Payload
    Assert-OnlySelectedArtifactZip 'default/cold/'
    Assert-ConsumerModuleUnchanged $default 'default'
    Move-CacheAside $default 'default'

    $namespace = New-Mode 'namespace'
    $null = Invoke-Go $namespace.Consumer @('get', ($RootModule + '@' + $script:RootVersion)) $script:CommandsLog
    Save-ModuleCacheSnapshot 'namespace/setup/go-get' $namespace.SetupModCache
    $namespaceSource = Join-Path $script:PublicConsumerDir 'namespace-consumer/main.go'
    if (-not (Test-Path -LiteralPath $namespaceSource)) {
        Fail "namespace fixture is missing: $namespaceSource"
    }
    Copy-Item -LiteralPath $namespaceSource -Destination (Join-Path $namespace.Consumer 'main.go')
    Write-CommandLog $namespace.Consumer @('copy', $namespaceSource, (Join-Path $namespace.Consumer 'main.go'))
    Add-IdentityHelperSource 'namespace' $namespace
    Assert-RootZip 'namespace/setup/'
    Assert-SetupArtifactZips 'namespace' 'namespace/setup/'

    Enter-ColdTargetPhase $namespace
    $null = Resolve-ColdRoot $namespace $rootZip $nested.ToArray()

    $namespaceBinary = Join-Path $namespace.Dir 'namespace-consumer.exe'
    $null = Invoke-Go $namespace.Consumer @('build', '-o', $namespaceBinary, '.') $script:CommandsLog
    Assert-BinaryPE $namespaceBinary 'namespace'
    Save-ModuleCacheSnapshot 'namespace/cold/build' $namespace.ModCache
    $namespaceMetaFile = Get-GoVersionMetadata $namespace $namespaceBinary 'namespace'
    Assert-NoTag $namespaceMetaFile 'lance_external_lib'
    $namespaceSelected = Assert-SelectedArtifactFromMetadata $namespaceMetaFile 'namespace'
    Assert-Equal 'namespace selected artifact' ('{0}@{1}' -f $namespaceSelected.Path, $namespaceSelected.Version) ('{0}@{1}' -f $script:SelectedPath, $script:SelectedVersion)
    $namespaceArtifact = Download-ArtifactModule $namespace $namespaceSelected.Path $namespaceSelected.Version 'namespace'
    Compare-ArtifactModules $script:SelectedMeta $namespaceArtifact
    Assert-RootZip 'namespace/cold/build'
    Assert-OnlySelectedArtifactZip 'namespace/cold/build'
    $namespaceDeps = Join-Path $namespace.Dir 'namespace-deps.txt'
    $null = Invoke-GoCapture $namespace.Consumer @('list', '-deps', '.') $namespaceDeps $script:CommandsLog
    Save-ModuleCacheSnapshot 'namespace/cold/deps' $namespace.ModCache
    Assert-RootZip 'namespace/cold/'
    Assert-OnlySelectedArtifactZip 'namespace/cold/'
    $namespaceArtifactMods = @(Get-ArtifactMods 'namespace/cold/')
    Write-Summary ("namespace.artifact_mods_downloaded=" + ($namespaceArtifactMods -join ','))
    Assert-NamespaceDeps $namespaceDeps
    Assert-BinaryMetadata -Mode $namespace -Binary $namespaceBinary -MetadataFile $namespaceMetaFile `
        -Payload $namespaceArtifact.Payload -ExpectedArtifact ('{0}@{1}' -f $script:SelectedPath, $script:SelectedVersion) `
        -ExpectedPayload 'present' -ExpectedTag '' -ArtifactMeta $namespaceArtifact -Label 'namespace'
    Assert-CacheAbsent $namespace 'namespace/before-runtime'
    $namespaceRoot = Join-Path $namespace.Dir 'namespace-root'
    New-Item -ItemType Directory -Force -Path $namespaceRoot | Out-Null
    Run-Consumer $namespace $namespaceBinary 'namespace' @('-root', $namespaceRoot) '' '^namespace id=".+" namespaces=0$'
    $namespaceCacheLibrary = Assert-CachePresent $namespace $namespaceArtifact 'namespace'
    Assert-CachePE $namespaceCacheLibrary 'namespace'
    Run-Identity $namespace $namespaceArtifact $namespaceCacheLibrary 'namespace' ('{0}@{1}' -f $script:SelectedPath, $script:SelectedVersion) 'present' '' $namespaceArtifact.Payload
    Assert-OnlySelectedArtifactZip 'namespace/cold/'
    Assert-ConsumerModuleUnchanged $namespace 'namespace'
    Move-CacheAside $namespace 'namespace'

    $external = New-Mode 'external'
    $null = Invoke-Go $external.Consumer @('get', ($RootModule + '@' + $script:RootVersion)) $script:CommandsLog
    Save-ModuleCacheSnapshot 'external/setup/go-get' $external.SetupModCache
    Add-IdentityHelperSource 'external' $external
    Assert-RootZip 'external/setup/'
    Assert-SetupArtifactZips 'external' 'external/setup/'

    Enter-ColdTargetPhase $external
    $null = Resolve-ColdRoot $external $rootZip $nested.ToArray()

    $externalBinary = Join-Path $external.Dir 'roundtrip-external.exe'
    $null = Invoke-Go $external.Consumer @('build', '-tags', 'lance_external_lib', '-o', $externalBinary, ($RootModule + '/examples/roundtrip')) $script:CommandsLog
    Assert-BinaryPE $externalBinary 'external'
    Save-ModuleCacheSnapshot 'external/cold/build' $external.ModCache
    $externalMetaFile = Get-GoVersionMetadata $external $externalBinary 'external'
    Assert-BinaryMetadata -Mode $external -Binary $externalBinary -MetadataFile $externalMetaFile `
        -Payload $script:DefaultPayload -ExpectedArtifact 'none' -ExpectedPayload 'absent' `
        -ExpectedTag 'lance_external_lib' -ArtifactMeta $null -Label 'external'
    $externalDeps = Join-Path $external.Dir 'external-deps.txt'
    $null = Invoke-GoCapture $external.Consumer @('list', '-deps', '-tags', 'lance_external_lib', ($RootModule + '/examples/roundtrip')) $externalDeps $script:CommandsLog
    Save-ModuleCacheSnapshot 'external/cold/deps' $external.ModCache
    Assert-ExternalDeps $externalDeps
    $externalBuildZips = @(Get-ArtifactZips 'external/cold/build')
    $externalArtifactZips = @(Get-ArtifactZips 'external/cold/')
    $externalArtifactMods = @(Get-ArtifactMods 'external/cold/')
    Write-Summary "external.cold_build_artifact_zips=$($externalBuildZips -join ',')"
    Write-Summary "external.cold_artifact_zips=$($externalArtifactZips -join ',')"
    Write-Summary "external.cold_artifact_mods=$($externalArtifactMods -join ',')"
    Assert-CacheAbsent $external 'external/before-runtime'
    $env:LANCE_LIB_PATH = $script:SuppliedLib
    Run-Consumer $external $externalBinary 'external' @() 'Created, reopened and scanned 3 rows: [10 20 30]' ''
    Assert-CacheAbsent $external 'external/after-roundtrip'
    Run-Identity $external $script:SelectedMeta $script:SuppliedLib 'external' 'none' 'absent' 'lance_external_lib' $script:DefaultPayload
    Remove-Item -Path Env:LANCE_LIB_PATH -ErrorAction SilentlyContinue
    if (Test-Path Env:LANCE_LIB_PATH) {
        Fail 'LANCE_LIB_PATH is still set after the supplied-DLL consumer'
    }
    Assert-CacheAbsent $external 'external/after-identity'
    Assert-ConsumerModuleUnchanged $external 'external'

    Write-Summary 'windows_cache_contract=inherited_localappdata_acl'
    Write-Summary 'posix_modes_claimed=false'
    Write-Summary 'runtime_executed=true'
    Write-Summary "evidence_dir=$($script:EvidenceDir)"
    Write-Summary 'result=PASS'
    Write-Evidence $script:SummaryFile 'exit_code=0'
    $script:Passed = $true
    $exitCode = 0
} catch {
    $message = $_.Exception.Message -replace "`r?`n", ' '
    Write-Evidence $script:SummaryFile 'result=FAIL'
    Write-Evidence $script:SummaryFile ("error=" + $message)
    Write-Evidence $script:SummaryFile 'exit_code=1'
    Write-Host "public-consumer: FAIL: $message" -ForegroundColor Red
    $exitCode = 1
} finally {
    if (-not $script:Passed) {
        Write-Evidence $script:SummaryFile ("work_dir_kept=" + $script:WorkDir)
        Write-Host "public-consumer: work directory kept at $($script:WorkDir)" -ForegroundColor Yellow
    } elseif (Test-Path -LiteralPath $script:WorkDir) {
        try {
            Remove-Item -LiteralPath $script:WorkDir -Recurse -Force
        } catch {
            Write-Evidence $script:SummaryFile ("cleanup_warning=" + ($_.Exception.Message -replace "`r?`n", ' '))
        }
    }
    Set-Content -LiteralPath (Join-Path $script:EvidenceDir 'exit-code.txt') -Value ([string]$exitCode) -Encoding utf8
}
exit $exitCode
