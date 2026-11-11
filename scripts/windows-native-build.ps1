# Build the pinned lance-c source as a native Windows x86_64 DLL, verify the
# produced PE and C ABI, then run a native C consumer and the Go external-library
# tests against the staged DLL.
#
# This is an evidence workflow, not a release or packaging pipeline. It uses a
# fresh runner-local source checkout and Cargo target directory, stages only the
# DLL, its MSVC import library, and public headers, and uploads the stage plus
# logs for review. It does not publish a Go module or modify the default bundle.

[CmdletBinding()]
param(
	[string]$RepoRoot = "",
	[string]$WorkDir = "",
	[string]$StageDir = "",
	[string]$EvidenceDir = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$LASTEXITCODE = 0

if ([string]::IsNullOrEmpty($RepoRoot)) {
	$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
if ([string]::IsNullOrEmpty($WorkDir)) {
	$tempRoot = if ([string]::IsNullOrEmpty($env:RUNNER_TEMP)) { [IO.Path]::GetTempPath() } else { $env:RUNNER_TEMP }
	$WorkDir = Join-Path $tempRoot "lance-windows-native"
}
if ([string]::IsNullOrEmpty($StageDir)) {
	$StageDir = Join-Path $RepoRoot "dist\windows-amd64"
}
if ([string]::IsNullOrEmpty($EvidenceDir)) {
	$EvidenceDir = Join-Path $RepoRoot "dist\windows-amd64-evidence"
}

$RepoRoot = [IO.Path]::GetFullPath($RepoRoot)
$WorkDir = [IO.Path]::GetFullPath($WorkDir)
$StageDir = [IO.Path]::GetFullPath($StageDir)
$EvidenceDir = [IO.Path]::GetFullPath($EvidenceDir)

foreach ($path in @($WorkDir, $StageDir, $EvidenceDir)) {
	$repoPrefix = $RepoRoot.TrimEnd([char]'\') + '\'
	$pathPrefix = $path.TrimEnd([char]'\') + '\'
	if ($repoPrefix.StartsWith($pathPrefix, [StringComparison]::OrdinalIgnoreCase)) {
		throw "refusing to use $path as an output directory: it is the repository root or an ancestor"
	}
}

$script:Step = "setup"
$script:ExitCode = 0
$script:Status = "RUNNING"
$script:StatusMessage = ""
$script:TranscriptStarted = $false
$script:DllSha256 = "n/a"
$script:DllSize = "n/a"
$script:PeMachine = "n/a"
$script:HeaderSha256 = "n/a"
$script:SourceCommit = "n/a"
$script:SourceTree = "n/a"
$script:ExportCount = "n/a"
$script:HeaderCount = "n/a"
$script:Dependencies = "n/a"
$script:BundledDependencies = "not run"
$script:GoTestResult = "not run"
$script:ConsumerResult = "not run"
$script:AbiResult = "not run"

function Write-Log {
	param([string]$Message)
	$line = "[{0}] {1}" -f (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"), $Message
	Write-Host $line
	if (-not [string]::IsNullOrEmpty($script:LogPath)) {
		Add-Content -LiteralPath $script:LogPath -Value $line
	}
}

function Set-Status {
	param([string]$Status, [string]$Message)
	$script:Status = $Status
	$script:StatusMessage = $Message
	$lines = @(
		"status=$Status",
		"step=$script:Step",
		"message=$Message",
		"updated=$((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"))"
	)
	$lines | Set-Content -LiteralPath $script:StatusPath
}

function Write-Summary {
	$lines = @(
		"## Windows native amd64 evidence",
		"",
		"| Item | Value |",
		"| --- | --- |",
		"| Result | $script:Status |",
		"| Step | $script:Step |",
		"| Source commit | $script:SourceCommit |",
		"| Source tree | $script:SourceTree |",
		"| Header SHA-256 | $script:HeaderSha256 |",
		"| DLL SHA-256 | $script:DllSha256 |",
		"| DLL size | $script:DllSize |",
		"| PE machine | $script:PeMachine |",
		"| Header symbols / exports | $script:HeaderCount / $script:ExportCount |",
		"| DLL dependencies | $script:Dependencies |",
		"| Bundled dependencies | $script:BundledDependencies |",
		"| ABI checker | $script:AbiResult |",
		"| Native C consumer | $script:ConsumerResult |",
		"| Go external-library tests | $script:GoTestResult |",
		"",
		"Windows arm64 is not covered by this job. The staged DLL/import library/headers are CI evidence only and are not a published release or Go module."
	)
	$lines | Set-Content -LiteralPath (Join-Path $EvidenceDir "summary.md")
	if (-not [string]::IsNullOrEmpty($env:GITHUB_STEP_SUMMARY)) {
		Add-Content -LiteralPath $env:GITHUB_STEP_SUMMARY -Value $lines
	}
}

function Invoke-Logged {
	param(
		[Parameter(Mandatory = $true)][string]$FilePath,
		[Parameter(Mandatory = $true)][string[]]$ArgumentList,
		[Parameter(Mandatory = $true)][string]$LogPath,
		[Parameter(Mandatory = $true)][string]$Label
	)
	Write-Log "run: $Label"
	& $FilePath @ArgumentList 2>&1 | Tee-Object -FilePath $LogPath
	$code = $LASTEXITCODE
	if ($code -ne 0) {
		throw "$Label failed with exit code $code (log: $LogPath)"
	}
}

function Invoke-Capture {
	param(
		[Parameter(Mandatory = $true)][string]$FilePath,
		[Parameter(Mandatory = $true)][string[]]$ArgumentList,
		[string]$LogPath = "",
		[Parameter(Mandatory = $true)][string]$Label
	)
	$output = & $FilePath @ArgumentList 2>&1
	$code = $LASTEXITCODE
	if (-not [string]::IsNullOrEmpty($LogPath)) {
		$output | Set-Content -LiteralPath $LogPath
	}
	if ($code -ne 0) {
		throw "$Label failed with exit code $code"
	}
	return @($output)
}

function Read-Pin {
	param([string]$Path, [string]$Name)
	$text = Get-Content -Raw -LiteralPath $Path
	$pattern = '(?m)^\s*' + [regex]::Escape($Name) + '\s*=\s*"([^"]+)"\s*$'
	$found = [regex]::Matches($text, $pattern)
	if ($found.Count -ne 1) {
		throw "expected exactly one $Name in $Path, found $($found.Count)"
	}
	return $found[0].Groups[1].Value
}

function Import-VsDevShell {
	$vswhere = Join-Path ${env:ProgramFiles(x86)} "Microsoft Visual Studio\Installer\vswhere.exe"
	if (-not (Test-Path -LiteralPath $vswhere -PathType Leaf)) {
		throw "vswhere.exe not found at $vswhere"
	}
	$vsPath = @(& $vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath) |
		Select-Object -First 1
	if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($vsPath)) {
		throw "could not locate a Visual Studio installation with the MSVC x64 toolchain"
	}
	$vsPath = $vsPath.Trim()
	$vcvars = Join-Path $vsPath "VC\Auxiliary\Build\vcvars64.bat"
	if (-not (Test-Path -LiteralPath $vcvars -PathType Leaf)) {
		throw "vcvars64.bat not found at $vcvars"
	}

	$vcvarsScript = Join-Path ([IO.Path]::GetTempPath()) ("lance-vcvars-{0}.cmd" -f [guid]::NewGuid().ToString("N"))
	@("call `"$vcvars`" >nul 2>&1", "set") | Set-Content -LiteralPath $vcvarsScript -Encoding ASCII
	try {
		$lines = & cmd.exe /c "`"$vcvarsScript`""
		if ($LASTEXITCODE -ne 0) {
			throw "vcvars64.bat failed with exit code $LASTEXITCODE"
		}
	} finally {
		Remove-Item -LiteralPath $vcvarsScript -Force -ErrorAction SilentlyContinue
	}
	foreach ($line in $lines) {
		$index = $line.IndexOf("=")
		if ($index -le 0) {
			continue
		}
		$name = $line.Substring(0, $index)
		$value = $line.Substring($index + 1)
		[Environment]::SetEnvironmentVariable($name, $value, "Process")
	}
	Write-Log "visual_studio=$vsPath"
}

function Find-Dependency {
	param([string]$Name, [string[]]$Directories)
	foreach ($directory in $Directories) {
		if ([string]::IsNullOrWhiteSpace($directory)) {
			continue
		}
		$candidate = Join-Path $directory $Name
		if (Test-Path -LiteralPath $candidate -PathType Leaf) {
			return (Resolve-Path -LiteralPath $candidate).Path
		}
	}
	return $null
}

function Get-ToolVersion {
	param([string]$Name)
	$command = Get-Command $Name -ErrorAction SilentlyContinue
	if ($null -eq $command) {
		return "unavailable"
	}
	try {
		$output = & $command.Source --version 2>&1
		if ($LASTEXITCODE -ne 0) {
			return "unavailable (exit $LASTEXITCODE)"
		}
		return (($output | Select-Object -First 1) -as [string])
	} catch {
		return "unavailable ($($_.Exception.Message))"
	}
}

function Stop-ScriptTranscript {
	if ($script:TranscriptStarted) {
		try {
			Stop-Transcript | Out-Null
		} catch {
			Write-Host "warning: could not stop transcript: $($_.Exception.Message)"
		}
		$script:TranscriptStarted = $false
	}
}

if (Test-Path -LiteralPath $EvidenceDir) {
	Remove-Item -LiteralPath $EvidenceDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $EvidenceDir | Out-Null
$script:LogPath = Join-Path $EvidenceDir "run.log"
$script:StatusPath = Join-Path $EvidenceDir "status.txt"
Set-Status "RUNNING" "starting"

try {
	$transcriptPath = Join-Path $EvidenceDir "powershell-transcript.txt"
	try {
		Start-Transcript -Path $transcriptPath -Force | Out-Null
		$script:TranscriptStarted = $true
	} catch {
		Write-Host "warning: could not start transcript: $($_.Exception.Message)"
	}

	Write-Log "repository=$RepoRoot"
	Write-Log "work_dir=$WorkDir"
	Write-Log "stage_dir=$StageDir"
	Write-Log "evidence_dir=$EvidenceDir"
	Write-Summary
	# Keep a minimal provenance record even if toolchain discovery fails before
	# the detailed record below is written.
	@(
		"repository: $RepoRoot",
		"runner_os: $env:ImageOS",
		"runner_image: $env:ImageVersion",
		"os_version: $([Environment]::OSVersion.VersionString)",
		"processor_architecture: $env:PROCESSOR_ARCHITECTURE",
		"generated_at: $((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"))"
	) | Set-Content -LiteralPath (Join-Path $EvidenceDir "provenance.txt")

	$upstreamGo = Join-Path $RepoRoot "internal\ffi\upstream.go"
	if (-not (Test-Path -LiteralPath $upstreamGo -PathType Leaf)) {
		throw "pinned upstream metadata not found at $upstreamGo"
	}
	$upstreamRepo = Read-Pin $upstreamGo "upstreamRepo"
	$sourceCommit = Read-Pin $upstreamGo "upstreamCommit"
	$headerSha = Read-Pin $upstreamGo "upstreamHeaderSHA256"
	$script:SourceCommit = $sourceCommit
	$script:HeaderSha256 = $headerSha
	if ($sourceCommit -notmatch "^[0-9a-f]{40}$") {
		throw "upstreamCommit is not a full 40-character SHA-1: $sourceCommit"
	}
	if ($headerSha -notmatch "^[0-9a-f]{64}$") {
		throw "upstreamHeaderSHA256 is not a 64-character SHA-256: $headerSha"
	}
	Write-Log "pinned_repo=$upstreamRepo"
	Write-Log "pinned_commit=$sourceCommit"
	Write-Log "pinned_header_sha256=$headerSha"

	$protocVersion = if ([string]::IsNullOrEmpty($env:PROTOC_VERSION)) { "21.12" } else { $env:PROTOC_VERSION }
	$protocSha = if ([string]::IsNullOrEmpty($env:PROTOC_SHA256)) {
		"71852a30cf62975358edfcbbff93086e8857a079c8e4d6904881aa968d65c7f9"
	} else {
		$env:PROTOC_SHA256.ToLowerInvariant()
	}
	if ($protocSha -notmatch "^[0-9a-f]{64}$") {
		throw "PROTOC_SHA256 is not a lowercase SHA-256: $protocSha"
	}

	if ($WorkDir -eq $RepoRoot -or $WorkDir -eq (Split-Path -Parent $RepoRoot)) {
		throw "refusing to use $WorkDir as the build work directory"
	}
	foreach ($path in @($WorkDir, $StageDir)) {
		if (Test-Path -LiteralPath $path) {
			Remove-Item -LiteralPath $path -Recurse -Force
		}
	}
	New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
	New-Item -ItemType Directory -Force -Path (Join-Path $StageDir "bin") | Out-Null
	New-Item -ItemType Directory -Force -Path (Join-Path $StageDir "lib") | Out-Null
	New-Item -ItemType Directory -Force -Path (Join-Path $StageDir "include") | Out-Null

	$env:CGO_ENABLED = "0"
	$env:GOTOOLCHAIN = "local"
	$env:CARGO_TERM_COLOR = "never"
	$env:CARGO_INCREMENTAL = "0"
	$env:CARGO_NET_RETRY = "3"
	$env:CARGO_NET_GIT_FETCH_WITH_CLI = "true"

	$script:Step = "toolchain"
	if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64") {
		throw "runner processor architecture is $env:PROCESSOR_ARCHITECTURE, want AMD64 for native x86_64 evidence"
	}
	Write-Log "go=$((Get-Command go -ErrorAction Stop).Source)"
	Write-Log "git=$((Get-Command git -ErrorAction Stop).Source)"
	Write-Log "cargo=$((Get-Command cargo -ErrorAction Stop).Source)"
	Write-Log "rustc=$((Get-Command rustc -ErrorAction Stop).Source)"
	Write-Log "cmake=$((Get-Command cmake -ErrorAction Stop).Source)"

	$goVersion = (Invoke-Capture -FilePath "go" -ArgumentList @("version") -Label "go version") -join " "
	$goEnvLines = Invoke-Capture -FilePath "go" -ArgumentList @("env", "-json", "GOOS", "GOARCH", "GOVERSION", "CGO_ENABLED") -Label "go env"
	$goEnv = (($goEnvLines -join "`n") | ConvertFrom-Json)
	if ($goEnv.GOOS -ne "windows" -or $goEnv.GOARCH -ne "amd64" -or "$($goEnv.CGO_ENABLED)" -ne "0") {
		throw "go environment is not native windows/amd64 with CGO disabled: $($goEnvLines -join '; ')"
	}
	$rustcVerbose = Invoke-Capture -FilePath "rustc" -ArgumentList @("-vV") -Label "rustc -vV"
	$rustcVerbose | Set-Content -LiteralPath (Join-Path $EvidenceDir "rustc-vV.txt")
	$hostLine = @($rustcVerbose | Where-Object { $_ -match "^host:" } | ForEach-Object { $_.Trim() }) | Select-Object -First 1
	if ($hostLine -notmatch "^host:\s*x86_64-pc-windows-msvc$") {
		throw "rustc host is not x86_64-pc-windows-msvc: $($rustcVerbose -join '; ')"
	}
	$rustupShow = Invoke-Capture -FilePath "rustup" -ArgumentList @("show", "active-toolchain") -Label "rustup show active-toolchain"
	$rustupTargets = @(Invoke-Capture -FilePath "rustup" -ArgumentList @("target", "list", "--installed") -Label "rustup target list" | ForEach-Object { $_.Trim() })
	if (-not (@($rustupTargets) -match "^x86_64-pc-windows-msvc$")) {
		throw "x86_64-pc-windows-msvc target is not installed"
	}

	Import-VsDevShell
	$clCommand = Get-Command cl -ErrorAction Stop
	$dumpbinCommand = Get-Command dumpbin -ErrorAction Stop
	$clVersion = @(& $clCommand.Source 2>&1) | Select-Object -First 1
	$dumpbinVersion = @(& $dumpbinCommand.Source /nologo /version 2>&1) | Select-Object -First 1
	if ($LASTEXITCODE -ne 0) {
		throw "dumpbin /version failed with exit code $LASTEXITCODE"
	}

	$protocDir = Join-Path $WorkDir "protoc-$protocVersion"
	$protocZip = Join-Path $WorkDir "protoc-$protocVersion-win64.zip"
	$protocUrl = "https://github.com/protocolbuffers/protobuf/releases/download/v$protocVersion/protoc-$protocVersion-win64.zip"
	Write-Log "downloading $protocUrl"
	Invoke-WebRequest -Uri $protocUrl -OutFile $protocZip -UseBasicParsing
	$actualProtocSha = (Get-FileHash -LiteralPath $protocZip -Algorithm SHA256).Hash.ToLowerInvariant()
	if ($actualProtocSha -ne $protocSha) {
		throw "protoc archive sha256 is $actualProtocSha, want $protocSha"
	}
	New-Item -ItemType Directory -Force -Path $protocDir | Out-Null
	Expand-Archive -LiteralPath $protocZip -DestinationPath $protocDir -Force
	$env:PROTOC = Join-Path $protocDir "bin\protoc.exe"
	$env:PROTOC_INCLUDE = Join-Path $protocDir "include"
	if (-not (Test-Path -LiteralPath $env:PROTOC -PathType Leaf)) {
		throw "protoc.exe not found after extracting $protocZip"
	}
	if (-not (Test-Path -LiteralPath (Join-Path $env:PROTOC_INCLUDE "google\protobuf\descriptor.proto") -PathType Leaf)) {
		throw "protoc include tree not found under $env:PROTOC_INCLUDE"
	}
	$env:PATH = "$(Split-Path -Parent $env:PROTOC);$env:PATH"
	$protocVersionOutput = Invoke-Capture -FilePath $env:PROTOC -ArgumentList @("--version") -Label "protoc --version"
	Write-Log "protoc=$($protocVersionOutput -join ' ')"
	Write-Log "protoc_sha256=$actualProtocSha"

	$provenance = @(
		"source_repo: $upstreamRepo",
		"source_commit: $sourceCommit",
		"source_header_sha256: $headerSha",
		"protoc_version: $protocVersion",
		"protoc_archive_sha256: $actualProtocSha",
		"rustc: $(($rustcVerbose | Select-Object -First 1) -as [string])",
		"rustup_active_toolchain: $(($rustupShow -join ' ') -as [string])",
		"rust_targets: $(($rustupTargets -join ', ') -as [string])",
		"cargo: $(Get-ToolVersion 'cargo')",
		"go: $goVersion",
		"go_environment: GOOS=$($goEnv.GOOS) GOARCH=$($goEnv.GOARCH) GOVERSION=$($goEnv.GOVERSION) CGO_ENABLED=$($goEnv.CGO_ENABLED)",
		"cmake: $(Get-ToolVersion 'cmake')",
		"git: $(Get-ToolVersion 'git')",
		"cl: $clVersion",
		"dumpbin: $dumpbinVersion",
		"runner_os: $env:ImageOS",
		"runner_image: $env:ImageVersion",
		"os_version: $([Environment]::OSVersion.VersionString)",
		"processor_architecture: $env:PROCESSOR_ARCHITECTURE",
		"generated_at: $((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"))"
	)
	$provenance | Set-Content -LiteralPath (Join-Path $EvidenceDir "provenance.txt")
	Write-Log "provenance recorded"

	$script:Step = "source"
	$src = Join-Path $WorkDir "src"
	Invoke-Logged -FilePath "git" -ArgumentList @("clone", "--filter=blob:none", "--no-checkout", $upstreamRepo, $src) -LogPath (Join-Path $EvidenceDir "git-clone.log") -Label "git clone"
	Invoke-Logged -FilePath "git" -ArgumentList @("-C", $src, "config", "core.autocrlf", "false") -LogPath (Join-Path $EvidenceDir "git-config.log") -Label "git config core.autocrlf"
	Invoke-Logged -FilePath "git" -ArgumentList @("-C", $src, "config", "core.eol", "lf") -LogPath (Join-Path $EvidenceDir "git-config.log") -Label "git config core.eol"
	Invoke-Logged -FilePath "git" -ArgumentList @("-C", $src, "config", "advice.detachedHead", "false") -LogPath (Join-Path $EvidenceDir "git-config.log") -Label "git config advice.detachedHead"
	Invoke-Logged -FilePath "git" -ArgumentList @("-C", $src, "fetch", "--depth", "1", "origin", $sourceCommit) -LogPath (Join-Path $EvidenceDir "git-fetch.log") -Label "git fetch pinned commit"
	Invoke-Logged -FilePath "git" -ArgumentList @("-C", $src, "checkout", "--detach", "FETCH_HEAD") -LogPath (Join-Path $EvidenceDir "git-checkout.log") -Label "git checkout pinned commit"
	$actualCommit = (Invoke-Capture -FilePath "git" -ArgumentList @("-C", $src, "rev-parse", "HEAD") -Label "git rev-parse HEAD") -join ""
	$actualCommit = $actualCommit.Trim()
	if ($actualCommit -ne $sourceCommit) {
		throw "source checkout is at $actualCommit, want $sourceCommit"
	}
	$sourceTree = (Invoke-Capture -FilePath "git" -ArgumentList @("-C", $src, "rev-parse", "HEAD^{tree}") -Label "git rev-parse HEAD tree") -join ""
	$sourceTree = $sourceTree.Trim()
	if ($sourceTree -notmatch "^[0-9a-f]{40}$") {
		throw "source tree is not a full 40-character SHA-1: $sourceTree"
	}
	$script:SourceTree = $sourceTree
	Add-Content -LiteralPath (Join-Path $EvidenceDir "provenance.txt") -Value "source_tree: $sourceTree"
	$dirty = (Invoke-Capture -FilePath "git" -ArgumentList @("-C", $src, "status", "--porcelain") -Label "git status") -join "`n"
	if (-not [string]::IsNullOrWhiteSpace($dirty)) {
		throw "source checkout is dirty after checkout: $dirty"
	}

	$sourceHeader = Join-Path $src "include\lance\lance.h"
	if (-not (Test-Path -LiteralPath $sourceHeader -PathType Leaf)) {
		throw "pinned source does not contain include\lance\lance.h"
	}
	$actualHeaderSha = (Get-FileHash -LiteralPath $sourceHeader -Algorithm SHA256).Hash.ToLowerInvariant()
	if ($actualHeaderSha -ne $headerSha) {
		throw "source header sha256 is $actualHeaderSha, want $headerSha (line ending or checkout mismatch)"
	}
	Write-Log "source header verified"

	$cargoLock = Join-Path $src "Cargo.lock"
	if (-not (Test-Path -LiteralPath $cargoLock -PathType Leaf)) {
		throw "pinned source does not contain Cargo.lock"
	}
	Copy-Item -LiteralPath $cargoLock -Destination (Join-Path $EvidenceDir "Cargo.lock")
	$cargoLockSha = (Get-FileHash -LiteralPath $cargoLock -Algorithm SHA256).Hash.ToLowerInvariant()
	Write-Log "Cargo.lock sha256=$cargoLockSha"

	$script:Step = "cargo-build"
	$targetRoot = Join-Path $WorkDir "target"
	$env:CARGO_TARGET_DIR = $targetRoot
	$targetDir = Join-Path $targetRoot "x86_64-pc-windows-msvc\release"
	$cargoLog = Join-Path $EvidenceDir "cargo-build.log"
	Push-Location $src
	try {
		Invoke-Logged -FilePath "cargo" -ArgumentList @("build", "--locked", "--release", "--lib", "--target", "x86_64-pc-windows-msvc") -LogPath $cargoLog -Label "cargo build --locked --release --lib"
	} finally {
		Pop-Location
	}

	$dll = Join-Path $targetDir "lance_c.dll"
	$importLib = Join-Path $targetDir "lance_c.dll.lib"
	if (-not (Test-Path -LiteralPath $dll -PathType Leaf)) {
		$listing = @(Get-ChildItem -LiteralPath $targetDir -File -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name)
		throw "cargo build succeeded but $dll is missing; target files: $($listing -join ', ')"
	}
	if (-not (Test-Path -LiteralPath $importLib -PathType Leaf)) {
		$listing = @(Get-ChildItem -LiteralPath $targetDir -File -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name)
		throw "cargo build succeeded but the MSVC import library $importLib is missing; target files: $($listing -join ', ')"
	}

	$script:Step = "stage"
	Copy-Item -LiteralPath $dll -Destination (Join-Path $StageDir "bin\lance_c.dll")
	Copy-Item -LiteralPath $importLib -Destination (Join-Path $StageDir "lib\lance_c.dll.lib")
	$stageIncludeLance = Join-Path $StageDir "include\lance"
	New-Item -ItemType Directory -Force -Path $stageIncludeLance | Out-Null
	Get-ChildItem -LiteralPath (Join-Path $src "include\lance") -Force |
		Copy-Item -Destination $stageIncludeLance -Recurse -Force
	$stagedDll = Join-Path $StageDir "bin\lance_c.dll"
	$stagedHeader = Join-Path $StageDir "include\lance\lance.h"
	if (-not (Test-Path -LiteralPath $stagedHeader -PathType Leaf)) {
		throw "staged header missing at $stagedHeader"
	}
	$stagedHeaderSha = (Get-FileHash -LiteralPath $stagedHeader -Algorithm SHA256).Hash.ToLowerInvariant()
	if ($stagedHeaderSha -ne $headerSha) {
		throw "staged header sha256 is $stagedHeaderSha, want $headerSha"
	}
	$script:DllSha256 = (Get-FileHash -LiteralPath $stagedDll -Algorithm SHA256).Hash.ToLowerInvariant()
	$script:DllSize = (Get-Item -LiteralPath $stagedDll).Length

	$bytes = [IO.File]::ReadAllBytes($stagedDll)
	if ($bytes.Length -lt 0x40) {
		throw "DLL is too small to contain a PE header"
	}
	$peOffset = [BitConverter]::ToInt32($bytes, 0x3C)
	if ($peOffset -lt 0 -or ($peOffset + 26) -ge $bytes.Length) {
		throw "DLL has an invalid PE offset $peOffset"
	}
	$peSignature = [BitConverter]::ToUInt32($bytes, $peOffset)
	if ($peSignature -ne 0x00004550) {
		throw "DLL does not have a PE signature"
	}
	$machine = [BitConverter]::ToUInt16($bytes, $peOffset + 4)
	if ($machine -ne 0x8664) {
		throw ("DLL PE machine is 0x{0:X4}, want 0x8664 (x86_64)" -f $machine)
	}
	$optionalMagic = [BitConverter]::ToUInt16($bytes, $peOffset + 24)
	if ($optionalMagic -ne 0x20B) {
		throw ("DLL optional header magic is 0x{0:X4}, want 0x20B (PE32+)" -f $optionalMagic)
	}
	$script:PeMachine = "x86_64 (0x8664)"

	$manifest = @(
		"source_repo=$upstreamRepo",
		"source_commit=$sourceCommit",
		"source_tree=$sourceTree",
		"source_header_sha256=$headerSha",
		"cargo_lock_sha256=$cargoLockSha",
		"dll=bin/lance_c.dll",
		"dll_sha256=$script:DllSha256",
		"dll_size=$script:DllSize",
		"pe_machine=0x8664",
		"import_library=lib/lance_c.dll.lib",
		"protoc_version=$protocVersion",
		"protoc_archive_sha256=$actualProtocSha",
		"staged_at=$((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"))"
	)
	$manifest | Set-Content -LiteralPath (Join-Path $StageDir "manifest.txt")

	$script:Step = "pe-and-abi"
	$headersLog = Join-Path $EvidenceDir "dumpbin-headers.txt"
	$exportsLog = Join-Path $EvidenceDir "dumpbin-exports.txt"
	$dependentsLog = Join-Path $EvidenceDir "dumpbin-dependents.txt"
	Invoke-Logged -FilePath "dumpbin" -ArgumentList @("/nologo", "/headers", $stagedDll) -LogPath $headersLog -Label "dumpbin /headers"
	Invoke-Logged -FilePath "dumpbin" -ArgumentList @("/nologo", "/exports", $stagedDll) -LogPath $exportsLog -Label "dumpbin /exports"
	Invoke-Logged -FilePath "dumpbin" -ArgumentList @("/nologo", "/dependents", $stagedDll) -LogPath $dependentsLog -Label "dumpbin /dependents"

	$exportNames = New-Object System.Collections.Generic.List[string]
	$inExports = $false
	foreach ($line in Get-Content -LiteralPath $exportsLog) {
		if ($line -match "ordinal\s+hint\s+RVA\s+name") {
			$inExports = $true
			continue
		}
		if ($inExports -and $line -match "^\s*(\d+)\s+[0-9A-Fa-f]+\s+[0-9A-Fa-f]+\s+(\S+)") {
			$name = $Matches[2]
			if ($name.Contains("=")) {
				$name = $name.Split("=")[0]
			}
			$exportNames.Add($name)
		}
	}
	$exportNames = @($exportNames | Sort-Object -Unique)
	if ($exportNames.Count -eq 0) {
		throw "dumpbin /exports found no named exports in $stagedDll"
	}

	$headerText = Get-Content -Raw -LiteralPath $stagedHeader
	$headerText = [regex]::Replace($headerText, "(?s)/\*.*?\*/", "")
	$headerText = [regex]::Replace($headerText, "//[^\n]*", "")
	$headerPattern = "(?m)^[ \t]*(?!typedef\b|struct\b|enum\b|union\b|#)(?:[A-Za-z_][A-Za-z0-9_]*[ \t*]+)+?(lance_[A-Za-z0-9_]+)[ \t]*\("
	$headerMatches = [regex]::Matches($headerText, $headerPattern)
	$headerNames = @($headerMatches | ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
	if ($headerNames.Count -ne 876) {
		throw "header parser found $($headerNames.Count) top-level lance_* declarations, want 876 for the pinned header"
	}
	$headerNames | Set-Content -LiteralPath (Join-Path $EvidenceDir "header-symbols.txt")
	$exportNames | Set-Content -LiteralPath (Join-Path $EvidenceDir "exports.txt")
	$script:HeaderCount = $headerNames.Count
	$script:ExportCount = $exportNames.Count

	$headerSet = New-Object "System.Collections.Generic.HashSet[string]"
	foreach ($name in $headerNames) {
		[void]$headerSet.Add($name)
	}
	$exportSet = New-Object "System.Collections.Generic.HashSet[string]"
	foreach ($name in $exportNames) {
		[void]$exportSet.Add($name)
	}
	$missingExports = @($headerNames | Where-Object { -not $exportSet.Contains($_) })
	$extraExports = @($exportNames | Where-Object { -not $headerSet.Contains($_) })
	$nonLanceExports = @($exportNames | Where-Object { $_ -notmatch "^lance_" })
	if ($missingExports.Count -gt 0) {
		$missingExports | Set-Content -LiteralPath (Join-Path $EvidenceDir "missing-exports.txt")
		throw "DLL is missing $($missingExports.Count) header-required exports; first: $($missingExports[0])"
	}
	if ($extraExports.Count -gt 0) {
		$extraExports | Set-Content -LiteralPath (Join-Path $EvidenceDir "extra-exports.txt")
		throw "DLL has $($extraExports.Count) exports not declared by the pinned header; first: $($extraExports[0])"
	}
	if ($nonLanceExports.Count -gt 0) {
		$nonLanceExports | Set-Content -LiteralPath (Join-Path $EvidenceDir "non-lance-exports.txt")
		throw "DLL exports non-lance_* names; first: $($nonLanceExports[0])"
	}
	Write-Log "exports match all $($headerNames.Count) header declarations and are lance_* only"

	$dependencies = New-Object System.Collections.Generic.List[string]
	$inDependencies = $false
	foreach ($line in Get-Content -LiteralPath $dependentsLog) {
		if ($line -match "Image has the following dependencies") {
			$inDependencies = $true
			continue
		}
		if (-not $inDependencies) {
			continue
		}
		if ($line -match "^\s*Summary") {
			break
		}
		if ($line -match "^\s*([A-Za-z0-9_.-]+\.dll)\s*$") {
			$dependencies.Add($Matches[1])
		}
	}
	$dependencies = @($dependencies | Sort-Object -Unique)
	if ($dependencies.Count -eq 0) {
		throw "dumpbin /dependents found no dependency list for $stagedDll"
	}
	$script:Dependencies = $dependencies -join ", "
	$dependencies | Set-Content -LiteralPath (Join-Path $EvidenceDir "dependencies.txt")

	$script:Step = "runtime-dependencies"
	$runtimeDir = Join-Path $StageDir "runtime"
	New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
	$runtimeDll = Join-Path $runtimeDir "lance_c.dll"
	Copy-Item -LiteralPath $stagedDll -Destination $runtimeDll
	$runtimeDllSha = (Get-FileHash -LiteralPath $runtimeDll -Algorithm SHA256).Hash.ToLowerInvariant()
	if ($runtimeDllSha -ne $script:DllSha256) {
		throw "runtime DLL sha256 is $runtimeDllSha, want $($script:DllSha256)"
	}
	$system32 = Join-Path $env:SystemRoot "System32"
	$syswow64 = Join-Path $env:SystemRoot "SysWOW64"
	# Only the build output, staged tree, and OS directories are valid sources
	# for the distributed runtime. Do not satisfy dependencies from an arbitrary
	# developer PATH entry: those DLLs would not be present for a consumer.
	$searchDirs = @(
		$targetDir,
		(Join-Path $StageDir "bin"),
		$runtimeDir,
		$system32,
		$syswow64
	) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
	$dependencyInventory = New-Object System.Collections.Generic.List[string]
	$bundled = New-Object System.Collections.Generic.List[string]
	foreach ($dependency in $dependencies) {
		$systemCandidate = Join-Path $system32 $dependency
		$isApiSet = $dependency -match "(?i)^(api-ms-win-|ext-ms-)"
		if ((Test-Path -LiteralPath $systemCandidate -PathType Leaf) -or $isApiSet) {
			$dependencyInventory.Add("system: $dependency")
			continue
		}
		$found = Find-Dependency -Name $dependency -Directories $searchDirs
		if ($null -eq $found) {
			$dependencyInventory | Set-Content -LiteralPath (Join-Path $EvidenceDir "dependency-inventory.txt")
			throw "DLL dependency $dependency is neither an OS component nor available on the build machine"
		}
		$dependencyTarget = Join-Path $runtimeDir $dependency
		if (-not [string]::Equals($found, $dependencyTarget, [StringComparison]::OrdinalIgnoreCase)) {
			Copy-Item -LiteralPath $found -Destination $dependencyTarget
		}
		$bundled.Add($dependency)
		$dependencyInventory.Add("bundled: $dependency <- $found")
	}
	if ($bundled.Count -gt 0) {
		$script:BundledDependencies = $bundled -join ", "
	} else {
		$script:BundledDependencies = "none"
	}
	$dependencyInventory | Set-Content -LiteralPath (Join-Path $EvidenceDir "dependency-inventory.txt")
	Write-Log "runtime dependencies: $($dependencyInventory -join '; ')"

	$script:Step = "abi-checker"
	$env:LANCE_C_HEADER = $stagedHeader
	$abiLog = Join-Path $EvidenceDir "abi-check.log"
	Push-Location $RepoRoot
	try {
		Invoke-Logged -FilePath "go" -ArgumentList @("run", "./scripts/check-abi-symbols.go") -LogPath $abiLog -Label "check-abi-symbols"
	} finally {
		Pop-Location
	}
	$script:AbiResult = "PASS"

	$script:Step = "native-consumer"
	$consumerSource = Join-Path $RepoRoot "scripts\windows-native\consumer.c"
	if (-not (Test-Path -LiteralPath $consumerSource -PathType Leaf)) {
		throw "native consumer source not found at $consumerSource"
	}
	$consumerDir = Join-Path $WorkDir "consumer"
	New-Item -ItemType Directory -Force -Path $consumerDir | Out-Null
	$consumerExe = Join-Path $consumerDir "lance_native_consumer.exe"
	Push-Location $consumerDir
	try {
		Invoke-Logged -FilePath "cl" -ArgumentList @(
			"/nologo", "/W4", "/O2", "/MD", "/D_CRT_SECURE_NO_WARNINGS",
			"/I$StageDir\include",
			"/Fe:$consumerExe",
			$consumerSource,
			"$StageDir\lib\lance_c.dll.lib",
			"kernel32.lib"
		) -LogPath (Join-Path $EvidenceDir "consumer-build.log") -Label "compile native consumer"
	} finally {
		Pop-Location
	}
	if (-not (Test-Path -LiteralPath $consumerExe -PathType Leaf)) {
		throw "native consumer executable was not produced at $consumerExe"
	}

	$datasetDir = Join-Path $WorkDir "data"
	New-Item -ItemType Directory -Force -Path $datasetDir | Out-Null
	$datasetPath = Join-Path $datasetDir "roundtrip.lance"
	$consumerLog = Join-Path $EvidenceDir "consumer.log"
	$oldPath = $env:PATH
	try {
		$env:PATH = "$runtimeDir;$system32;$env:SystemRoot"
		Push-Location $consumerDir
		try {
			Invoke-Logged -FilePath $consumerExe -ArgumentList @($datasetPath, $runtimeDir) -LogPath $consumerLog -Label "run native consumer"
		} finally {
			Pop-Location
		}
	} finally {
		$env:PATH = $oldPath
	}
	$consumerText = Get-Content -Raw -LiteralPath $consumerLog
	if ($consumerText -notmatch "PASS: windows native consumer") {
		throw "native consumer did not report overall PASS"
	}
	$loadedLine = @(Get-Content -LiteralPath $consumerLog | Where-Object { $_ -like "loaded_module=*" }) | Select-Object -First 1
	if ([string]::IsNullOrEmpty($loadedLine)) {
		throw "native consumer did not report the loaded DLL path"
	}
	$loadedPath = $loadedLine.Substring("loaded_module=".Length)
	Write-Log "consumer_loaded_module=$loadedPath"
	$script:ConsumerResult = "PASS"

	$script:Step = "go-native-tests"
	$stagedDllPath = (Resolve-Path -LiteralPath $stagedDll).Path
	$env:LANCE_LIB_PATH = $stagedDllPath
	$goExe = (Get-Command go -ErrorAction Stop).Source
	$goBin = Split-Path -Parent $goExe
	$goDownloadLog = Join-Path $EvidenceDir "go-mod-download.log"
	Push-Location $RepoRoot
	try {
		Invoke-Logged -FilePath $goExe -ArgumentList @("mod", "download") -LogPath $goDownloadLog -Label "go mod download"
	} finally {
		Pop-Location
	}

	$requiredTests = @(
		"TestLoadRealLibrary",
		"TestLoadConcurrentSingleInstance",
		"TestNativeErrorSemantics",
		"TestOpenDatasetReportsNativeErrors",
		"TestCreateReopenMetadataAndScan",
		"TestWriteEmptyDatasetAndSlicedNullableBatch",
		"TestRoundTrip",
		"TestNamespaceManagedWriteOpenAppendCommit",
		"TestExportStreamBorrowsReaderBatch"
	)
	# Compile every package, but run only the named native lifecycle tests. This
	# keeps the evidence lane bounded and prevents unrelated Windows feature gaps
	# from being reported as DLL-loading failures.
	$testPattern = "^(" + (($requiredTests | ForEach-Object { [regex]::Escape($_) }) -join "|") + ")$"
	$goLog = Join-Path $EvidenceDir "go-test.log"
	$oldPath = $env:PATH
	try {
		$env:PATH = "$runtimeDir;$system32;$env:SystemRoot;$goBin"
		Push-Location $RepoRoot
		try {
			Invoke-Logged -FilePath $goExe -ArgumentList @("test", "-buildvcs=false", "-tags", "lance_external_lib", "-run", $testPattern, "-count=1", "-v", "./...") -LogPath $goLog -Label "go test -tags lance_external_lib -run <selected> ./..."
		} finally {
			Pop-Location
		}
	} finally {
		$env:PATH = $oldPath
	}

	$goText = Get-Content -Raw -LiteralPath $goLog
	$requiredTests | Set-Content -LiteralPath (Join-Path $EvidenceDir "required-tests.txt")
	foreach ($name in $requiredTests) {
		$escaped = [regex]::Escape($name)
		if ($goText -notmatch "(?m)^--- PASS: $escaped \(") {
			throw "required native test did not pass: $name"
		}
	}
	if ($goText -match "(?m)^\s*--- SKIP: ") {
		throw "go test reported a skipped test in the selected native lane; see $goLog"
	}
	if ($goText -match "(?m)^--- FAIL: ") {
		throw "go test reported a failure; see $goLog"
	}
	$script:GoTestResult = "PASS ($($requiredTests.Count) required tests, no skips)"

	$script:Step = "complete"
	Set-Status "PASS" "all native Windows amd64 checks passed"
	Write-Summary
	Stop-ScriptTranscript
	Write-Log "PASS: staged DLL, import library, headers, ABI, consumer, and Go external-library tests"
	exit 0
} catch {
	$script:ExitCode = 1
	$message = $_.Exception.Message
	$script:Status = "FAIL"
	Write-Log "FAILED at $script:Step`: $message"
	try {
		Set-Status "FAIL" "$script:Step`: $message"
	} catch {
		Write-Host "warning: could not write status: $($_.Exception.Message)"
	}
	try {
		Write-Summary
	} catch {
		Write-Host "warning: could not write summary: $($_.Exception.Message)"
	}
	Stop-ScriptTranscript
	exit 1
}
