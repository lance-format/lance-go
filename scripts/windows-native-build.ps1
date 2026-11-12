# Build the pinned lance-c source as a native Windows DLL for the requested
# architecture, verify the produced PE and C ABI, then run a native C consumer
# and the Go external-library tests against the staged DLL.
#
# Native evidence is never replaced by packaging. Only after all native gates
# pass does the script evaluate the actual DLL dependency closure. When every
# dependency is an OS component, it packages the exact tested DLL bytes as an
# unpublished companion candidate at
# github.com/lance-format/lance-go/artifacts/windows-<arch>, verifies that module
# with the shared verifier, and runs an external-tag roundtrip. A redistributable
# or unclassified dependency blocks candidate generation explicitly; hosted
# System32 presence alone is not treated as OS-only availability.
#
# The candidate is CI evidence only. This script does not publish a Go module,
# copy a selector into internal/bundled, add a module requirement, or modify the
# default bundle.
#
# -Architecture selects the native target: amd64 (x86_64-pc-windows-msvc) or
# arm64 (aarch64-pc-windows-msvc). The workflow matrix runs each architecture
# on its own hosted Windows runner.

[CmdletBinding()]
param(
	[ValidateSet("amd64", "arm64")]
	[string]$Architecture = "amd64",
	[string]$RepoRoot = "",
	[string]$WorkDir = "",
	[string]$StageDir = "",
	[string]$EvidenceDir = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$LASTEXITCODE = 0

$Architecture = $Architecture.ToLowerInvariant()
switch ($Architecture) {
	"amd64" {
		$script:TargetTriple = "x86_64-pc-windows-msvc"
		$script:ExpectedPeMachine = 0x8664
		$script:PeMachineLabel = "x86_64"
		$script:ExpectedProcessorArchitecture = "AMD64"
	}
	"arm64" {
		$script:TargetTriple = "aarch64-pc-windows-msvc"
		$script:ExpectedPeMachine = 0xAA64
		$script:PeMachineLabel = "arm64"
		$script:ExpectedProcessorArchitecture = "ARM64"
	}
	default {
		throw "unsupported -Architecture '$Architecture'; expected amd64 or arm64"
	}
}

if ([string]::IsNullOrEmpty($RepoRoot)) {
	$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
if ([string]::IsNullOrEmpty($WorkDir)) {
	$tempRoot = if ([string]::IsNullOrEmpty($env:RUNNER_TEMP)) { [IO.Path]::GetTempPath() } else { $env:RUNNER_TEMP }
	$WorkDir = Join-Path $tempRoot "lance-windows-native-$Architecture"
}
if ([string]::IsNullOrEmpty($StageDir)) {
	$StageDir = Join-Path $RepoRoot "dist\windows-$Architecture"
}
if ([string]::IsNullOrEmpty($EvidenceDir)) {
	$EvidenceDir = Join-Path $RepoRoot "dist\windows-$Architecture-evidence"
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
$script:PublicExportCount = "n/a"
$script:DependencyExportCount = "n/a"
$script:HeaderCount = "n/a"
$script:DependencySummary = "n/a"
$script:BundledDependencies = "not run"
$script:GoTestResult = "not run"
$script:ConsumerResult = "not run"
$script:AbiResult = "not run"
$script:GoHostTarget = "n/a"
$script:VsDeveloperShell = "n/a"
$script:VsHostArch = "n/a"
$script:VsTargetArch = "n/a"
$script:ProtocMachine = "n/a"
$script:PeDebugFacts = "not recorded"
$script:ProductionEligibility = "not evaluated"
$script:ProductionCandidate = "not generated"
$script:RoundtripResult = "not run"
$script:PayloadSize = "n/a"
$script:PayloadSha256 = "n/a"
$script:HostedDependencies = "not evaluated"
$script:DependencyRecords = @()
$script:ProductionGateBlocked = $false

# Reviewed OS-only Windows components. Membership is by module name, never by
# the directory where a hosted runner happens to find the file. UCRT api-set
# names are handled separately below. VC Runtime and MFC redistributables are
# deliberately absent: they are separately deployed application dependencies.
$script:OsOnlyDependencies = @(
	"advapi32.dll",
	"bcrypt.dll",
	"bcryptprimitives.dll",
	"cfgmgr32.dll",
	"combase.dll",
	"crypt32.dll",
	"gdi32.dll",
	"iphlpapi.dll",
	"kernel32.dll",
	"kernelbase.dll",
	"msvcrt.dll",
	"ntdll.dll",
	"ole32.dll",
	"oleaut32.dll",
	"pdh.dll",
	"powrprof.dll",
	"psapi.dll",
	"rpcrt4.dll",
	"sechost.dll",
	"shell32.dll",
	"shlwapi.dll",
	"ucrtbase.dll",
	"user32.dll",
	"userenv.dll",
	"version.dll",
	"winmm.dll",
	"ws2_32.dll"
)

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
		"## Windows native $Architecture evidence",
		"",
		"| Item | Value |",
		"| --- | --- |",
		"| Architecture | $Architecture |",
		"| Rust target | $script:TargetTriple |",
		"| Result | $script:Status |",
		"| Step | $script:Step |",
		"| Source commit | $script:SourceCommit |",
		"| Source tree | $script:SourceTree |",
		"| Header SHA-256 | $script:HeaderSha256 |",
		"| DLL SHA-256 | $script:DllSha256 |",
		"| DLL size | $script:DllSize |",
		"| PE machine | $script:PeMachine |",
		"| Header symbols / public exports | $script:HeaderCount / $script:PublicExportCount |",
		"| Total / dependency exports | $script:ExportCount / $script:DependencyExportCount |",
		"| DLL dependencies | $script:DependencySummary |",
		"| Bundled dependencies | $script:BundledDependencies |",
		"| Hosted-only non-OS dependencies | $script:HostedDependencies |",
		"| Production eligibility | $script:ProductionEligibility |",
		"| Production candidate | $script:ProductionCandidate |",
		"| Candidate payload | $(if ($script:PayloadSize -eq "n/a") { "n/a" } else { "$script:PayloadSize bytes (SHA-256 $script:PayloadSha256)" }) |",
		"| Supplied-library roundtrip | $script:RoundtripResult |",
		"| PE debug/COFF facts | $script:PeDebugFacts |",
		"| Visual Studio developer shell | $script:VsDeveloperShell |",
		"| Visual Studio host / target | $script:VsHostArch / $script:VsTargetArch |",
		"| Go host / target | $script:GoHostTarget |",
		"| Protoc build tool | $script:ProtocMachine |",
		"| ABI checker | $script:AbiResult |",
		"| Native C consumer | $script:ConsumerResult |",
		"| Go external-library tests | $script:GoTestResult |",
		"",
		"The pinned protoc win64 executable is a build-time tool only. On arm64 it may run under OS emulation; it is not linked into the DLL and is not part of the C consumer or Go runtime evidence.",
		"The native C consumer and selected Go tests are evidence for the staged DLL. They do not establish a cold default, cold external, or public consumer, or a supported distribution.",
		"A generated Windows companion is an unpublished CI candidate only. No selector is activated, no module requirement is added, and no module is published.",
		"The single-DLL companion requires an OS-only dependency closure. A redistributable or unclassified dependency blocks the production candidate after native evidence; the result is recorded as BLOCKED, not PASS."
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

function Get-PeImageInfo {
	param([Parameter(Mandatory = $true)][string]$Path)
	$stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
	try {
		if ($stream.Length -lt 0x40) {
			throw "$Path is too small to contain a PE header"
		}
		$reader = New-Object IO.BinaryReader($stream)
		$stream.Position = 0x3C
		$peOffset = $reader.ReadInt32()
		if ($peOffset -lt 0 -or ([int64]$peOffset + 0x40) -gt $stream.Length) {
			throw "$Path has an invalid PE offset $peOffset"
		}
		$stream.Position = $peOffset
		$signature = $reader.ReadUInt32()
		if ($signature -ne 0x00004550) {
			throw "$Path does not have a PE signature"
		}
		$machine = $reader.ReadUInt16()
		$stream.Position = $peOffset + 24
		$magic = $reader.ReadUInt16()
		return [pscustomobject]@{
			Machine = $machine
			Magic = $magic
		}
	} finally {
		$stream.Dispose()
	}
}

function Convert-GuidBytes {
	param([Parameter(Mandatory = $true)][byte[]]$Bytes)
	if ($Bytes.Count -ne 16) {
		return "invalid-guid-size-$($Bytes.Count)"
	}
	$data1 = [BitConverter]::ToUInt32($Bytes, 0)
	$data2 = [BitConverter]::ToUInt16($Bytes, 4)
	$data3 = [BitConverter]::ToUInt16($Bytes, 6)
	$data4a = ($Bytes[8..9] | ForEach-Object { $_.ToString("x2") }) -join ""
	$data4b = ($Bytes[10..15] | ForEach-Object { $_.ToString("x2") }) -join ""
	return ("{0:x8}-{1:x4}-{2:x4}-{3}-{4}" -f $data1, $data2, $data3, $data4a, $data4b)
}

function Get-PeProvenanceInfo {
	param([Parameter(Mandatory = $true)][string]$Path)
	$stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
	try {
		if ($stream.Length -lt 0x40) {
			throw "$Path is too small to contain a PE header"
		}
		$reader = New-Object IO.BinaryReader($stream)
		$stream.Position = 0x3C
		$peOffset = $reader.ReadInt32()
		if ($peOffset -lt 0 -or ([int64]$peOffset + 0x40) -gt $stream.Length) {
			throw "$Path has an invalid PE offset $peOffset"
		}
		$stream.Position = $peOffset
		if ($reader.ReadUInt32() -ne 0x00004550) {
			throw "$Path does not have a PE signature"
		}
		$machine = $reader.ReadUInt16()
		$numberOfSections = $reader.ReadUInt16()
		$stream.Position = $peOffset + 12
		$coffSymbolTablePointer = $reader.ReadUInt32()
		$coffSymbolCount = $reader.ReadUInt32()
		$optionalHeaderSize = $reader.ReadUInt16()
		$stream.Position = $peOffset + 24
		$magic = $reader.ReadUInt16()
		if ($magic -ne 0x20B) {
			throw ("{0} optional header magic is 0x{1:X4}, want PE32+ (0x20B)" -f $Path, $magic)
		}

		if ($optionalHeaderSize -lt 112 -or ([int64]$peOffset + 24 + $optionalHeaderSize) -gt $stream.Length) {
			throw "$Path has an invalid PE32+ optional header size"
		}
		$stream.Position = [int64]$peOffset + 24 + 108
		$numberOfRvaAndSizes = $reader.ReadUInt32()

		$sectionTableOffset = [int64]$peOffset + 24 + $optionalHeaderSize
		if (($sectionTableOffset + ([int64]$numberOfSections * 40)) -gt $stream.Length) {
			throw "$Path section table extends past the end of the file"
		}
		$sections = New-Object System.Collections.Generic.List[object]
		for ($index = 0; $index -lt $numberOfSections; $index++) {
			$sectionOffset = $sectionTableOffset + ([int64]$index * 40)
			$stream.Position = $sectionOffset
			$nameBytes = $reader.ReadBytes(8)
			$name = [Text.Encoding]::ASCII.GetString($nameBytes).TrimEnd([char]0)
			$stream.Position = $sectionOffset + 8
			$virtualSize = $reader.ReadUInt32()
			$virtualAddress = $reader.ReadUInt32()
			$rawSize = $reader.ReadUInt32()
			$rawPointer = $reader.ReadUInt32()
			$stream.Position = $sectionOffset + 36
			$characteristics = $reader.ReadUInt32()
			$sections.Add([pscustomobject]@{
				Name = $name
				VirtualSize = $virtualSize
				VirtualAddress = $virtualAddress
				RawSize = $rawSize
				RawPointer = $rawPointer
				Characteristics = $characteristics
			})
		}

		$debugSections = @($sections | Where-Object { $_.Name -like ".debug*" })
		$pdataSections = @($sections | Where-Object { $_.Name -eq ".pdata" })
		$pdata = if ($pdataSections.Count -gt 0) { $pdataSections[0] } else { $null }

		$debugDirectoryPresent = $false
		$debugEntries = New-Object System.Collections.Generic.List[object]
		$dataDirectoryOffset = [int64]$peOffset + 24 + 112
		$debugDirectoryOffset = $dataDirectoryOffset + (6 * 8)
		if ($numberOfRvaAndSizes -ge 7) {
			if ($optionalHeaderSize -lt (112 + 7 * 8)) {
				throw "$Path optional header does not contain its declared debug directory"
			}
			$stream.Position = $debugDirectoryOffset
			$debugRva = $reader.ReadUInt32()
			$debugSize = $reader.ReadUInt32()
			if ($debugRva -ne 0 -and $debugSize -ne 0) {
				$debugDirectoryPresent = $true
				$containing = @($sections | Where-Object {
					$debugRva -ge $_.VirtualAddress -and $debugRva -lt ([uint64]$_.VirtualAddress + [uint64]$_.VirtualSize)
				}) | Select-Object -First 1
				if ($null -eq $containing) {
					throw ("debug directory RVA 0x{0:X8} is not inside a section" -f $debugRva)
				}
				$debugFileOffset = [int64]$containing.RawPointer + ([int64]$debugRva - [int64]$containing.VirtualAddress)
				$entryCount = [int]($debugSize / 28)
				for ($index = 0; $index -lt $entryCount; $index++) {
					$entryOffset = $debugFileOffset + ([int64]$index * 28)
					if (($entryOffset + 28) -gt $stream.Length) {
						throw "$Path debug directory entry extends past the end of the file"
					}
					$stream.Position = $entryOffset
					$characteristics = $reader.ReadUInt32()
					$timeDateStamp = $reader.ReadUInt32()
					$majorVersion = $reader.ReadUInt16()
					$minorVersion = $reader.ReadUInt16()
					$type = $reader.ReadUInt32()
					$sizeOfData = $reader.ReadUInt32()
					$addressOfRawData = $reader.ReadUInt32()
					$pointerToRawData = $reader.ReadUInt32()
					$typeName = switch ($type) {
						2 { "codeview" }
						11 { "vc-feature" }
						12 { "pogo" }
						13 { "iltcg" }
						16 { "embedded-portable-pdb" }
						17 { "pdb-checksum" }
						default { "type-$type" }
					}
					$entry = [pscustomobject]@{
						Type = $type
						TypeName = $typeName
						Size = $sizeOfData
						DataPointer = $pointerToRawData
						PdbName = $null
						PdbAge = $null
						PdbGuid = $null
					}
					if ($type -eq 2 -and $sizeOfData -ge 24 -and $pointerToRawData -ne 0 -and ([int64]$pointerToRawData + [int64]$sizeOfData) -le $stream.Length) {
						$stream.Position = $pointerToRawData
						$signatureBytes = $reader.ReadBytes(4)
						if ([Text.Encoding]::ASCII.GetString($signatureBytes) -eq "RSDS") {
							$guidBytes = $reader.ReadBytes(16)
							$entry.PdbGuid = Convert-GuidBytes -Bytes $guidBytes
							$entry.PdbAge = $reader.ReadUInt32()
							$pathBytes = $reader.ReadBytes(([int]$sizeOfData - 24))
							$pdbPath = [Text.Encoding]::UTF8.GetString($pathBytes).TrimEnd([char]0)
							if (-not [string]::IsNullOrEmpty($pdbPath)) {
								$entry.PdbName = ($pdbPath -split "[\\/]")[-1]
							}
						}
					}
					$debugEntries.Add($entry)
				}
			}
		}

		return [pscustomobject]@{
			Machine = $machine
			Magic = $magic
			NumberOfSections = $numberOfSections
			CoffSymbolTablePointer = $coffSymbolTablePointer
			CoffSymbolCount = $coffSymbolCount
			Sections = $sections.ToArray()
			DebugSections = [object[]]$debugSections
			PdataSection = $pdata
			DebugDirectoryPresent = $debugDirectoryPresent
			DebugEntries = $debugEntries.ToArray()
		}
	} finally {
		$stream.Dispose()
	}
}

function Get-DependencyClass {
	param([Parameter(Mandatory = $true)][string]$Name)
	$lower = $Name.ToLowerInvariant()
	if ($lower -match "^(api-ms-win-|ext-ms-)") {
		return "system-apiset"
	}
	if ($script:OsOnlyDependencies -contains $lower) {
		return "system"
	}
	if ($lower -match "^(vcruntime[0-9_]*d?|msvcp[0-9_]*d?|msvcr[0-9]+d?|concrt[0-9_]*d?|vcomp[0-9_]*d?|vccorlib[0-9_]*d?|ucrtbased|mfc[0-9]+u?d?|mfcm[0-9]+u?d?)\.dll$") {
		return "redistributable"
	}
	return "unclassified"
}

function Get-ProductionEligibility {
	param([object[]]$Records)
	$blocking = @($Records | Where-Object { $_.Class -notin @("system", "system-apiset") })
	return [pscustomobject]@{
		Eligible = ($blocking.Count -eq 0)
		Blocking = $blocking
	}
}

function Write-ProductionEligibility {
	param([bool]$Eligible, [string]$CandidateStatus)
	$blocking = @($script:DependencyRecords | Where-Object { $_.Class -notin @("system", "system-apiset") })
	$lines = @(
		"native_evidence=PASS",
		"native_runtime_scope=staged lance_c.dll with native C consumer and selected Go external-library tests",
		"contract=single-dll-companion",
		"os_only_dependency_closure_required=true",
		"eligible=$($Eligible.ToString().ToLowerInvariant())",
		"blocking_count=$($blocking.Count)"
	)
	if ($blocking.Count -eq 0) {
		$lines += "blocking=none"
	} else {
		foreach ($record in $blocking) {
			$lines += "blocking_dependency=$($record.Name) class=$($record.Class) reason=$($record.Reason)"
		}
	}
	$lines += @(
		"hosted_only_non_os=$script:HostedDependencies",
		"issue_215=https://github.com/lance-format/lance-c/issues/215",
		"issue_215_scope=supported Windows deployment contract is unresolved",
		"issue_214=https://github.com/lance-format/lance-c/issues/214",
		"issue_214_scope=dependency export inventory is distinct from runtime availability",
		"candidate_status=$CandidateStatus"
	)
	$lines | Set-Content -LiteralPath (Join-Path $EvidenceDir "production-eligibility.txt")
}

function Assert-NativePe {
	param(
		[Parameter(Mandatory = $true)][string]$Path,
		[Parameter(Mandatory = $true)][string]$Label
	)
	$info = Get-PeImageInfo -Path $Path
	if ($info.Machine -ne $script:ExpectedPeMachine) {
		throw ("{0} PE machine is 0x{1:X4}, want 0x{2:X4} ({3}); refusing to accept a foreign-architecture or emulated binary at {4}" -f $Label, $info.Machine, $script:ExpectedPeMachine, $script:PeMachineLabel, $Path)
	}
	if ($info.Magic -ne 0x20B) {
		throw ("{0} optional header magic is 0x{1:X4}, want 0x20B (PE32+) at {2}" -f $Label, $info.Magic, $Path)
	}
	return $info
}

function Import-VsDevShell {
	if ($Architecture -eq "amd64") {
		$component = "Microsoft.VisualStudio.Component.VC.Tools.x86.x64"
		$hostArch = "x64"
		$targetArch = "x64"
		$vcvarsNames = @("vcvars64.bat")
		$vcvarsArguments = @()
	} elseif ($Architecture -eq "arm64") {
		$component = "Microsoft.VisualStudio.Component.VC.Tools.ARM64"
		$hostArch = "arm64"
		$targetArch = "arm64"
		# VS ships a native ARM64 developer shell. vcvarsall.bat arm64 is the
		# fallback for installations that do not include vcvarsarm64.bat; the
		# VSCMD_ARG_* and compiler PE assertions below reject a cross shell or
		# an x64 compiler running under emulation either way.
		$vcvarsNames = @("vcvarsarm64.bat", "vcvarsall.bat")
		$vcvarsArguments = @("arm64")
	} else {
		throw "unsupported architecture $Architecture"
	}

	$vswhereCandidates = New-Object System.Collections.Generic.List[string]
	$vswhereCommand = Get-Command vswhere -ErrorAction SilentlyContinue
	if ($null -ne $vswhereCommand -and -not [string]::IsNullOrWhiteSpace($vswhereCommand.Source)) {
		$vswhereCandidates.Add($vswhereCommand.Source)
	}
	foreach ($root in @(${env:ProgramFiles(x86)}, $env:ProgramFiles)) {
		if (-not [string]::IsNullOrWhiteSpace($root)) {
			$vswhereCandidates.Add((Join-Path $root "Microsoft Visual Studio\Installer\vswhere.exe"))
		}
	}
	$vswhere = $null
	foreach ($candidate in $vswhereCandidates) {
		if (Test-Path -LiteralPath $candidate -PathType Leaf) {
			$vswhere = $candidate
			break
		}
	}
	if ([string]::IsNullOrEmpty($vswhere)) {
		throw "vswhere.exe not found on PATH or under Program Files; install the Visual Studio C++ workload"
	}
	$vsPath = @(& $vswhere -latest -products * -requires $component -property installationPath) | Select-Object -First 1
	if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($vsPath)) {
		throw "could not locate a Visual Studio installation with $component"
	}
	$vsPath = $vsPath.Trim()

	$vcvars = $null
	foreach ($name in $vcvarsNames) {
		$candidate = Join-Path $vsPath "VC\Auxiliary\Build\$name"
		if (Test-Path -LiteralPath $candidate -PathType Leaf) {
			$vcvars = $candidate
			break
		}
	}
	if ([string]::IsNullOrEmpty($vcvars)) {
		throw "no supported Visual Studio developer shell found under $vsPath\VC\Auxiliary\Build (tried $($vcvarsNames -join ', '))"
	}
	$vcvarsBase = Split-Path -Leaf $vcvars
	$callArguments = @()
	if ($vcvarsBase -ieq "vcvarsall.bat") {
		$callArguments = $vcvarsArguments
	}
	$callLine = "call `"$vcvars`""
	if ($callArguments.Count -gt 0) {
		$callLine += " " + ($callArguments -join " ")
	}
	$callLine += " >nul 2>&1"

	$vcvarsScript = Join-Path ([IO.Path]::GetTempPath()) ("lance-vcvars-{0}.cmd" -f [guid]::NewGuid().ToString("N"))
	@($callLine, "set") | Set-Content -LiteralPath $vcvarsScript -Encoding ASCII
	try {
		$lines = & cmd.exe /c "`"$vcvarsScript`""
		if ($LASTEXITCODE -ne 0) {
			throw "$vcvarsBase failed with exit code $LASTEXITCODE"
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
	$script:VsDeveloperShell = $vcvars
	$script:VsHostArch = "$env:VSCMD_ARG_HOST_ARCH"
	$script:VsTargetArch = "$env:VSCMD_ARG_TGT_ARCH"
	if (-not [string]::Equals($env:VSCMD_ARG_HOST_ARCH, $hostArch, [StringComparison]::OrdinalIgnoreCase)) {
		throw "Visual Studio host architecture is '$env:VSCMD_ARG_HOST_ARCH', want $hostArch"
	}
	if (-not [string]::Equals($env:VSCMD_ARG_TGT_ARCH, $targetArch, [StringComparison]::OrdinalIgnoreCase)) {
		throw "Visual Studio target architecture is '$env:VSCMD_ARG_TGT_ARCH', want $targetArch"
	}
	Write-Log "visual_studio=$vsPath"
	Write-Log "vs_developer_shell=$vcvars"
	Write-Log "vs_host_arch=$hostArch"
	Write-Log "vs_target_arch=$targetArch"
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
	if ($env:PROCESSOR_ARCHITECTURE -ne $script:ExpectedProcessorArchitecture) {
		throw "runner processor architecture is $env:PROCESSOR_ARCHITECTURE, want $($script:ExpectedProcessorArchitecture) for native $Architecture evidence"
	}
	Write-Log "architecture=$Architecture"
	Write-Log "target_triple=$script:TargetTriple"
	Write-Log "go=$((Get-Command go -ErrorAction Stop).Source)"
	Write-Log "git=$((Get-Command git -ErrorAction Stop).Source)"
	Write-Log "cargo=$((Get-Command cargo -ErrorAction Stop).Source)"
	Write-Log "rustc=$((Get-Command rustc -ErrorAction Stop).Source)"
	Write-Log "cmake=$((Get-Command cmake -ErrorAction Stop).Source)"

	$goVersion = (Invoke-Capture -FilePath "go" -ArgumentList @("version") -Label "go version") -join " "
	$goEnvLines = Invoke-Capture -FilePath "go" -ArgumentList @("env", "-json", "GOOS", "GOARCH", "GOHOSTOS", "GOHOSTARCH", "GOVERSION", "CGO_ENABLED", "GOMODCACHE", "GOCACHE") -Label "go env"
	$goEnv = (($goEnvLines -join "`n") | ConvertFrom-Json)
	if ($goEnv.GOOS -ne "windows" -or $goEnv.GOARCH -ne $Architecture -or $goEnv.GOHOSTOS -ne "windows" -or $goEnv.GOHOSTARCH -ne $Architecture -or "$($goEnv.CGO_ENABLED)" -ne "0") {
		throw "go environment is not native windows/$Architecture with CGO disabled: $($goEnvLines -join '; ')"
	}
	$script:GoHostTarget = "$($goEnv.GOHOSTOS)/$($goEnv.GOHOSTARCH)"
	$rustcVerbose = Invoke-Capture -FilePath "rustc" -ArgumentList @("-vV") -Label "rustc -vV"
	$rustcVerbose | Set-Content -LiteralPath (Join-Path $EvidenceDir "rustc-vV.txt")
	$hostLine = @($rustcVerbose | Where-Object { $_ -match "^host:" } | ForEach-Object { $_.Trim() }) | Select-Object -First 1
	$hostPattern = "^host:\s*" + [regex]::Escape($script:TargetTriple) + "$"
	if ($hostLine -notmatch $hostPattern) {
		throw "rustc host is not $($script:TargetTriple): $($rustcVerbose -join '; ')"
	}
	$rustupShow = Invoke-Capture -FilePath "rustup" -ArgumentList @("show", "active-toolchain") -Label "rustup show active-toolchain"
	$rustupTargets = @(Invoke-Capture -FilePath "rustup" -ArgumentList @("target", "list", "--installed") -Label "rustup target list" | ForEach-Object { $_.Trim() })
	if (-not (@($rustupTargets) -contains $script:TargetTriple)) {
		throw "$($script:TargetTriple) target is not installed"
	}

	Import-VsDevShell
	$clCommand = Get-Command cl -ErrorAction Stop
	$dumpbinCommand = Get-Command dumpbin -ErrorAction Stop
	[void](Assert-NativePe -Path $clCommand.Source -Label "cl.exe")
	[void](Assert-NativePe -Path $dumpbinCommand.Source -Label "dumpbin.exe")
	$clVersion = @(& $clCommand.Source 2>&1) | Select-Object -First 1
	$dumpbinVersion = @(& $dumpbinCommand.Source /nologo /version 2>&1) | Select-Object -First 1
	if ($LASTEXITCODE -ne 0) {
		throw "dumpbin /version failed with exit code $LASTEXITCODE"
	}

	$protocDir = Join-Path $WorkDir "protoc-$protocVersion"
	$protocZip = Join-Path $WorkDir "protoc-$protocVersion-win64.zip"
	# protoc-21.12-win64.exe is an x86_64 build tool. On windows arm64 it runs
	# under OS emulation while building the Rust library; it is not linked into
	# the DLL and is not part of the C consumer or Go runtime evidence.
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
	$protocPe = Get-PeImageInfo -Path $env:PROTOC
	if ($protocPe.Machine -ne 0x8664) {
		throw ("protoc.exe PE machine is 0x{0:X4}, want 0x8664 for the pinned protoc-{1}-win64 archive" -f $protocPe.Machine, $protocVersion)
	}
	$script:ProtocMachine = "x86_64 (0x8664)"
	if ($Architecture -eq "arm64") {
		$script:ProtocMachine = "x86_64 (0x8664), build-time only under OS emulation on arm64"
	}
	$env:PATH = "$(Split-Path -Parent $env:PROTOC);$env:PATH"
	$protocVersionOutput = Invoke-Capture -FilePath $env:PROTOC -ArgumentList @("--version") -Label "protoc --version"
	Write-Log "protoc=$($protocVersionOutput -join ' ')"
	Write-Log "protoc_sha256=$actualProtocSha"

	$provenance = @(
		"architecture: $Architecture",
		"target_triple: $($script:TargetTriple)",
		"source_repo: $upstreamRepo",
		"source_commit: $sourceCommit",
		"source_header_sha256: $headerSha",
		"protoc_version: $protocVersion",
		"protoc_archive_sha256: $actualProtocSha",
		"protoc_pe_machine: $($script:ProtocMachine)",
		"rustc: $(($rustcVerbose | Select-Object -First 1) -as [string])",
		"rustup_active_toolchain: $(($rustupShow -join ' ') -as [string])",
		"rust_targets: $(($rustupTargets -join ', ') -as [string])",
		"cargo: $(Get-ToolVersion 'cargo')",
		"go: $goVersion",
		"go_environment: GOOS=$($goEnv.GOOS) GOARCH=$($goEnv.GOARCH) GOHOSTOS=$($goEnv.GOHOSTOS) GOHOSTARCH=$($goEnv.GOHOSTARCH) GOVERSION=$($goEnv.GOVERSION) CGO_ENABLED=$($goEnv.CGO_ENABLED)",
		"vs_developer_shell: $($script:VsDeveloperShell)",
		"vs_host_arch: $($script:VsHostArch)",
		"vs_target_arch: $($script:VsTargetArch)",
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
	$targetDir = Join-Path $targetRoot "$($script:TargetTriple)\release"
	$cargoLog = Join-Path $EvidenceDir "cargo-build.log"
	Push-Location $src
	try {
		Invoke-Logged -FilePath "cargo" -ArgumentList @("build", "--locked", "--release", "--lib", "--target", $script:TargetTriple) -LogPath $cargoLog -Label "cargo build --locked --release --lib --target $($script:TargetTriple)"
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

	$dllPe = Assert-NativePe -Path $stagedDll -Label "staged lance_c.dll"
	$script:PeMachine = "$($script:PeMachineLabel) (0x{0:X4})" -f $dllPe.Machine
	$manifestPeMachine = "0x{0:X4}" -f $dllPe.Machine

	$script:Step = "pe-provenance"
	$peFacts = Get-PeProvenanceInfo -Path $stagedDll
	$debugSectionText = if ($peFacts.DebugSections.Count -eq 0) {
		"none"
	} else {
		($peFacts.DebugSections | ForEach-Object { $_.Name }) -join ","
	}
	$pdataText = if ($null -eq $peFacts.PdataSection) {
		"absent"
	} else {
		"present raw_size=$($peFacts.PdataSection.RawSize) virtual_size=$($peFacts.PdataSection.VirtualSize)"
	}
	$pdbEntries = @($peFacts.DebugEntries | Where-Object { -not [string]::IsNullOrEmpty($_.PdbName) })
	$pdbText = if ($pdbEntries.Count -eq 0) {
		"none"
	} else {
		($pdbEntries | ForEach-Object { "$($_.PdbName) (not packaged)" }) -join ", "
	}
	$script:PeDebugFacts = "COFF symbol table=$($peFacts.CoffSymbolTablePointer)/$($peFacts.CoffSymbolCount); .debug=$debugSectionText; .pdata=$pdataText; PDB=$pdbText; unix strip=not applied"

	$peFactLines = New-Object System.Collections.Generic.List[string]
	$peFactLines.Add("file=$stagedDll")
	$peFactLines.Add("sha256=$script:DllSha256")
	$peFactLines.Add("bytes=$script:DllSize")
	$peFactLines.Add("coff_symbol_table_pointer=$($peFacts.CoffSymbolTablePointer)")
	$peFactLines.Add("coff_symbol_count=$($peFacts.CoffSymbolCount)")
	$peFactLines.Add("debug_sections=$debugSectionText")
	if ($null -eq $peFacts.PdataSection) {
		$peFactLines.Add("pdata_section=absent")
	} else {
		$peFactLines.Add("pdata_section=present")
		$peFactLines.Add("pdata_raw_size=$($peFacts.PdataSection.RawSize)")
		$peFactLines.Add("pdata_virtual_size=$($peFacts.PdataSection.VirtualSize)")
	}
	$peFactLines.Add("debug_directory_present=$($peFacts.DebugDirectoryPresent.ToString().ToLowerInvariant())")
	$peFactLines.Add("debug_entry_count=$($peFacts.DebugEntries.Count)")
	foreach ($entry in $peFacts.DebugEntries) {
		$entryLine = "debug_entry=$($entry.TypeName) size=$($entry.Size) pointer=$($entry.DataPointer)"
		if (-not [string]::IsNullOrEmpty($entry.PdbName)) {
			$entryLine += " pdb=$($entry.PdbName) guid=$($entry.PdbGuid) age=$($entry.PdbAge)"
		}
		$peFactLines.Add($entryLine)
	}
	foreach ($section in $peFacts.Sections) {
		$peFactLines.Add("section=$($section.Name) virtual_size=$($section.VirtualSize) raw_size=$($section.RawSize)")
	}
	$peFactLines.Add("unix_strip_applied=false")
	$peFactLines.Add("packaging_note=PE .pdata is unwind metadata, not debug data; the exact staged DLL bytes are packaged unmodified")
	$peFactLines | Set-Content -LiteralPath (Join-Path $EvidenceDir "pe-debug-facts.txt")
	Write-Log "pe_debug_facts=$script:PeDebugFacts"

	$manifest = @(
		"architecture=$Architecture",
		"target_triple=$($script:TargetTriple)",
		"source_repo=$upstreamRepo",
		"source_commit=$sourceCommit",
		"source_tree=$sourceTree",
		"source_header_sha256=$headerSha",
		"cargo_lock_sha256=$cargoLockSha",
		"dll=bin/lance_c.dll",
		"dll_sha256=$script:DllSha256",
		"dll_size=$script:DllSize",
		"pe_machine=$manifestPeMachine",
		"coff_symbol_table_pointer=$($peFacts.CoffSymbolTablePointer)",
		"coff_symbol_count=$($peFacts.CoffSymbolCount)",
		"debug_sections=$debugSectionText",
		"pdata_section=$pdataText",
		"pdb_reference=$pdbText",
		"unix_strip_applied=false",
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
	if ($headerNames.Count -eq 0) {
		throw "header parser found no top-level lance_* declarations in $stagedHeader"
	}
	Write-Log "header parser found $($headerNames.Count) callable lance_* declarations (derived from the pinned header)"
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
	$publicExports = @($exportNames | Where-Object { $_ -cmatch "^lance_" })
	$extraExports = @($publicExports | Where-Object { -not $headerSet.Contains($_) })
	$nonLanceExports = @($exportNames | Where-Object { $_ -cnotmatch "^lance_" })
	$script:PublicExportCount = $publicExports.Count
	$script:DependencyExportCount = $nonLanceExports.Count
	$publicExports | Set-Content -LiteralPath (Join-Path $EvidenceDir "public-exports.txt")
	$nonLanceExports | Set-Content -LiteralPath (Join-Path $EvidenceDir "non-lance-exports.txt")
	if ($missingExports.Count -gt 0) {
		$missingExports | Set-Content -LiteralPath (Join-Path $EvidenceDir "missing-exports.txt")
		throw "DLL is missing $($missingExports.Count) header-required exports; first: $($missingExports[0])"
	}
	if ($extraExports.Count -gt 0) {
		$extraExports | Set-Content -LiteralPath (Join-Path $EvidenceDir "unexpected-public-exports.txt")
		throw "DLL has $($extraExports.Count) public lance_* exports not declared by the pinned header; first: $($extraExports[0])"
	}
	# Upstream Windows builds also export symbols from static dependencies.
	# Record those separately; their removal is not a public ABI prerequisite.
	Write-Log "public exports match all $($headerNames.Count) header declarations; dependency exports=$($nonLanceExports.Count)"

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
	$script:DependencySummary = $dependencies -join ", "
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
	$dependencyInventory.Add("# Classification is by module name. A file under the hosted runner's System32 is a resolution diagnostic, not proof of OS-only availability.")
	$bundled = New-Object System.Collections.Generic.List[string]
	$hostedOnly = New-Object System.Collections.Generic.List[string]
	$dependencyRecords = New-Object System.Collections.Generic.List[object]
	foreach ($dependency in $dependencies) {
		$class = Get-DependencyClass -Name $dependency
		$osFound = Find-Dependency -Name $dependency -Directories @($system32, $syswow64)
		$found = Find-Dependency -Name $dependency -Directories $searchDirs
		$resolution = if ($null -ne $osFound) { $osFound } elseif ($null -ne $found) { $found } else { "unresolved" }
		$bundledHere = $false
		$eligible = $true
		$reason = $null

		if ($class -eq "system-apiset") {
			$resolution = "os-api-set"
		} elseif ($class -eq "system") {
			if ($null -eq $osFound) {
				$resolution = "not-found-under-os-directories"
			}
		} else {
			if ($class -eq "redistributable") {
				$reason = "separately deployed VC Runtime/MFC component; hosted System32 presence is not OS-only availability"
			} else {
				$reason = "not on the reviewed OS-only Windows component allowlist"
			}
			$eligible = $false
			if ($null -eq $found) {
				$resolution = "unresolved"
				$reason = "$reason; no file found on the build host"
			} else {
				$resolution = $found
				$inOsDir = $false
				foreach ($osDir in @($system32, $syswow64)) {
					if (-not [string]::IsNullOrWhiteSpace($osDir)) {
						$osPrefix = $osDir.TrimEnd([char]'\') + '\'
						if ($found.StartsWith($osPrefix, [StringComparison]::OrdinalIgnoreCase)) {
							$inOsDir = $true
						}
					}
				}
				if ($inOsDir) {
					# Do not copy arbitrary System32 files into the companion. Keep
					# the hosted runtime usable for native tests, but record that the
					# dependency is not part of an OS-only distribution contract.
					$hostedOnly.Add($dependency)
					$reason = "$reason; hosted resolution $found was not bundled"
				} else {
					[void](Assert-NativePe -Path $found -Label "DLL dependency $dependency")
					$dependencyTarget = Join-Path $runtimeDir $dependency
					if (-not [string]::Equals($found, $dependencyTarget, [StringComparison]::OrdinalIgnoreCase)) {
						Copy-Item -LiteralPath $found -Destination $dependencyTarget
					}
					$bundled.Add($dependency)
					$bundledHere = $true
					$reason = "$reason; bundled for native evidence only"
				}
			}
		}

		$bundledText = if ($bundledHere) { "true" } else { "false" }
		$eligibleText = if ($eligible) { "true" } else { "false" }
		$recordLines = @(
			"dependency=$dependency",
			"class=$class",
			"resolution=$resolution",
			"bundled=$bundledText",
			"eligible=$eligibleText"
		)
		if (-not [string]::IsNullOrEmpty($reason)) {
			$recordLines += "reason=$reason"
		}
		$dependencyInventory.Add(($recordLines -join "`n"))
		$dependencyRecords.Add([pscustomobject]@{
			Name = $dependency
			Class = $class
			Resolution = $resolution
			Bundled = $bundledHere
			Eligible = $eligible
			Reason = $reason
		})
		# Persist each decision so a later staging failure still leaves the
		# dependency classification available for review.
		$dependencyInventory | Set-Content -LiteralPath (Join-Path $EvidenceDir "dependency-inventory.txt")
	}
	if ($bundled.Count -gt 0) {
		$script:BundledDependencies = $bundled -join ", "
	} else {
		$script:BundledDependencies = "none"
	}
	if ($hostedOnly.Count -gt 0) {
		$script:HostedDependencies = $hostedOnly -join ", "
	} else {
		$script:HostedDependencies = "none"
	}
	$script:DependencyRecords = $dependencyRecords.ToArray()
	$dependencyInventory | Set-Content -LiteralPath (Join-Path $EvidenceDir "dependency-inventory.txt")
	Write-Log "runtime dependencies: bundled=$script:BundledDependencies; hosted_non_os=$script:HostedDependencies"
	foreach ($record in $script:DependencyRecords) {
		Write-Log "dependency $($record.Name): class=$($record.Class) eligible=$($record.Eligible) resolution=$($record.Resolution)"
	}

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
	[void](Assert-NativePe -Path $consumerExe -Label "native consumer executable")

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

	$script:Step = "production-eligibility"
	$eligibility = Get-ProductionEligibility -Records $script:DependencyRecords
	if (-not $eligibility.Eligible) {
		$blockingText = ($eligibility.Blocking | ForEach-Object { "$($_.Name) ($($_.Class))" }) -join ", "
		$script:ProductionGateBlocked = $true
		$script:ProductionEligibility = "BLOCKED: $blockingText"
		$script:ProductionCandidate = "not generated (dependency eligibility blocked)"
		Write-ProductionEligibility -Eligible $false -CandidateStatus "not_generated"
		throw "production companion is not eligible: $blockingText; see production-eligibility.txt and https://github.com/lance-format/lance-c/issues/215"
	}
	$script:ProductionEligibility = "PASS: all dependencies are reviewed OS-only components"
	Write-ProductionEligibility -Eligible $true -CandidateStatus "pending_generation"

	$script:Step = "production-candidate"
	$modulePath = "github.com/lance-format/lance-go/artifacts/windows-$Architecture"
	$candidateRoot = Join-Path $WorkDir "candidate"
	$candidateOut = Join-Path $candidateRoot "windows-$Architecture"
	$candidateModule = Join-Path $candidateOut "module"
	$candidateSelector = Join-Path $candidateOut "selector"
	$candidateFull = [IO.Path]::GetFullPath($candidateOut)
	$separator = [IO.Path]::DirectorySeparatorChar
	$repoBase = ([IO.Path]::GetFullPath($RepoRoot)).TrimEnd([char[]]@('\', '/'))
	if ($candidateFull.Equals($repoBase, [StringComparison]::OrdinalIgnoreCase) -or $candidateFull.StartsWith($repoBase + $separator, [StringComparison]::OrdinalIgnoreCase)) {
		throw "production candidate output must be outside the checkout: $candidateFull"
	}
	foreach ($cachePath in @($goEnv.GOMODCACHE, $goEnv.GOCACHE)) {
		if ([string]::IsNullOrWhiteSpace($cachePath)) {
			continue
		}
		$cacheBase = ([IO.Path]::GetFullPath($cachePath)).TrimEnd([char[]]@('\', '/'))
		if ($candidateFull.Equals($cacheBase, [StringComparison]::OrdinalIgnoreCase) -or $candidateFull.StartsWith($cacheBase + $separator, [StringComparison]::OrdinalIgnoreCase)) {
			throw "production candidate output must be outside Go caches: $candidateFull is under $cacheBase"
		}
	}
	if (Test-Path -LiteralPath $candidateRoot) {
		Remove-Item -LiteralPath $candidateRoot -Recurse -Force
	}
	New-Item -ItemType Directory -Force -Path $candidateRoot | Out-Null

	$rootGoMod = Join-Path $RepoRoot "go.mod"
	$rootGoSum = Join-Path $RepoRoot "go.sum"
	$rootGoModSha = (Get-FileHash -LiteralPath $rootGoMod -Algorithm SHA256).Hash.ToLowerInvariant()
	$rootGoSumSha = (Get-FileHash -LiteralPath $rootGoSum -Algorithm SHA256).Hash.ToLowerInvariant()
	$guardStatus = @(Invoke-Capture -FilePath "git" -ArgumentList @("-C", $RepoRoot, "status", "--porcelain", "--untracked-files=all", "--", "go.mod", "go.sum", "internal/bundled") -Label "git status guarded paths") -join "`n"
	if (-not [string]::IsNullOrWhiteSpace($guardStatus)) {
		throw "guarded paths are already dirty before packaging: $guardStatus"
	}

	$generatorLog = Join-Path $EvidenceDir "production-generator.log"
	Push-Location $RepoRoot
	try {
		Invoke-Logged -FilePath "go" -ArgumentList @(
			"run", "./scripts/sync-lance-c-gen",
			"-library", $stagedDll,
			"-header", $stagedHeader,
			"-target", "windows/$Architecture",
			"-out", $candidateOut,
			"-module-path", $modulePath,
			"-selector-package", "bundled",
			"-upstream-repo", $upstreamRepo,
			"-upstream-commit", $sourceCommit,
			"-header-sha256", $headerSha
		) -LogPath $generatorLog -Label "generate Windows production companion"
	} finally {
		Pop-Location
	}

	if (-not (Test-Path -LiteralPath (Join-Path $candidateModule "go.mod") -PathType Leaf)) {
		throw "generator did not produce $candidateModule\go.mod"
	}
	$script:ProductionCandidate = "generated, verification pending (unpublished)"
	$moduleNames = @(Get-ChildItem -LiteralPath $candidateModule -File -Force | ForEach-Object { $_.Name })
	$expectedModuleNames = @("artifact.go", "go.mod", "lance_c.dll.zst")
	$missingModuleNames = @($expectedModuleNames | Where-Object { $moduleNames -notcontains $_ })
	$unexpectedModuleNames = @($moduleNames | Where-Object { $_ -notin @("artifact.go", "go.mod", "lance_c.dll.zst", "LICENSE") })
	if ($missingModuleNames.Count -gt 0) {
		throw "generated module is missing: $($missingModuleNames -join ', ')"
	}
	if ($unexpectedModuleNames.Count -gt 0) {
		throw "generated module contains unexpected files: $($unexpectedModuleNames -join ', ')"
	}
	$selectorName = "lance_bundle_windows_$Architecture.go"
	$selectorFiles = @(Get-ChildItem -LiteralPath $candidateSelector -File -Force)
	if ($selectorFiles.Count -ne 1 -or $selectorFiles[0].Name -ne $selectorName) {
		throw "generated selector must be exactly $selectorName, found: $($selectorFiles.Name -join ', ')"
	}
	$selectorText = Get-Content -Raw -LiteralPath $selectorFiles[0].FullName
	$expectedBuildTag = "//go:build !lance_external_lib && windows && $Architecture"
	if (-not $selectorText.Contains($expectedBuildTag)) {
		throw "generated selector is missing build tag: $expectedBuildTag"
	}
	if (-not $selectorText.Contains('"' + $modulePath + '"')) {
		throw "generated selector does not reference module $modulePath"
	}
	$forbidden = @(Get-ChildItem -LiteralPath $candidateOut -Recurse -File -Force | Where-Object { $_.Extension -in @(".dll", ".pdb", ".lib", ".exp") })
	if ($forbidden.Count -gt 0) {
		throw "candidate contains native binary side files that do not belong in a data-only module: $($forbidden.FullName -join ', ')"
	}

	$payloadFile = Join-Path $candidateModule "lance_c.dll.zst"
	$payloadBytes = (Get-Item -LiteralPath $payloadFile).Length
	$payloadSha = (Get-FileHash -LiteralPath $payloadFile -Algorithm SHA256).Hash.ToLowerInvariant()
	$script:PayloadSize = $payloadBytes
	$script:PayloadSha256 = $payloadSha
	$maxOrdinaryBlobBytes = 104857600
	if ($payloadBytes -gt $maxOrdinaryBlobBytes) {
		$script:ProductionGateBlocked = $true
		$script:ProductionCandidate = "BLOCKED: compressed payload $payloadBytes bytes exceeds ordinary GitHub blob limit ($maxOrdinaryBlobBytes bytes); no chunking introduced"
		Write-ProductionEligibility -Eligible $true -CandidateStatus "blocked_size"
		$sizeLines = @(
			"candidate_status=blocked_size",
			"payload_bytes=$payloadBytes",
			"payload_sha256=$payloadSha",
			"ordinary_blob_limit_bytes=$maxOrdinaryBlobBytes",
			"chunking_introduced=false",
			"candidate_accepted_for_publication=false"
		)
		$sizeLines | Set-Content -LiteralPath (Join-Path $candidateOut "size-gate.txt")
		$sizeLines | Set-Content -LiteralPath (Join-Path $EvidenceDir "production-size-gate.txt")
		throw "production candidate blocked: compressed payload $payloadBytes bytes exceeds ordinary GitHub blob limit $maxOrdinaryBlobBytes bytes; no chunking introduced"
	}

	$script:Step = "production-verifier"
	$verifierLog = Join-Path $EvidenceDir "production-verifier.log"
	Push-Location $RepoRoot
	try {
		Invoke-Logged -FilePath "go" -ArgumentList @(
			"run", "./scripts/verify-lance-c-artifact",
			"-module", $candidateModule,
			"-library", $stagedDll,
			"-pin", (Join-Path $RepoRoot "internal\ffi\upstream.go"),
			"-target", "windows/$Architecture",
			"-module-path", $modulePath
		) -LogPath $verifierLog -Label "verify Windows production companion"
	} finally {
		Pop-Location
	}
	$verifyValues = @{}
	foreach ($line in Get-Content -LiteralPath $verifierLog) {
		if ($line -match "^([a-z0-9_]+)=(.*)$") {
			$verifyValues[$Matches[1]] = $Matches[2]
		}
	}
	$requiredVerifierKeys = @(
		"artifact_verification", "target", "module_path", "library_bytes", "library_sha256",
		"payload", "payload_bytes", "payload_sha256", "decompressed_bytes", "decompressed_sha256",
		"byte_equality", "upstream_repo", "upstream_commit", "header_sha256"
	)
	foreach ($key in $requiredVerifierKeys) {
		if (-not $verifyValues.ContainsKey($key)) {
			throw "production verifier output is missing $key; see $verifierLog"
		}
	}
	if ($verifyValues["artifact_verification"] -ne "ok") { throw "production verifier did not report artifact_verification=ok" }
	if ($verifyValues["target"] -ne "windows/$Architecture") { throw "production verifier target is $($verifyValues['target']), want windows/$Architecture" }
	if ($verifyValues["module_path"] -ne $modulePath) { throw "production verifier module_path is $($verifyValues['module_path']), want $modulePath" }
	if ($verifyValues["library_sha256"] -ne $script:DllSha256) { throw "production verifier library sha does not match the staged DLL" }
	if ([int64]$verifyValues["library_bytes"] -ne [int64]$script:DllSize) { throw "production verifier library size does not match the staged DLL" }
	if ($verifyValues["payload_sha256"] -ne $payloadSha -or $verifyValues["decompressed_sha256"] -ne $script:DllSha256) { throw "production verifier payload/decompressed sha does not match" }
	if ([int64]$verifyValues["payload_bytes"] -ne [int64]$payloadBytes -or [int64]$verifyValues["decompressed_bytes"] -ne [int64]$script:DllSize) { throw "production verifier payload/decompressed size does not match" }
	if ($verifyValues["byte_equality"] -ne "identical") { throw "production verifier did not prove byte equality between payload and staged DLL" }
	if ($verifyValues["upstream_repo"] -ne $upstreamRepo) { throw "production verifier upstream_repo is $($verifyValues['upstream_repo']), want $upstreamRepo" }
	if ($verifyValues["upstream_commit"] -ne $sourceCommit) { throw "production verifier upstream_commit is $($verifyValues['upstream_commit']), want $sourceCommit" }
	if ($verifyValues["header_sha256"] -ne $headerSha) { throw "production verifier header sha is $($verifyValues['header_sha256']), want $headerSha" }
	$verifiedPayload = $verifyValues["payload"]
	if (-not [IO.Path]::IsPathRooted($verifiedPayload)) {
		$verifiedPayload = Join-Path $candidateModule $verifiedPayload
	}
	if ((Get-FileHash -LiteralPath $verifiedPayload -Algorithm SHA256).Hash.ToLowerInvariant() -ne $payloadSha) { throw "verified payload file hash changed: $verifiedPayload" }

	$script:Step = "supplied-library-roundtrip"
	$script:ProductionCandidate = "verified, supplied-library roundtrip pending (unpublished)"
	$roundtripLog = Join-Path $EvidenceDir "roundtrip.log"
	$oldPath = $env:PATH
	$oldLibPath = $env:LANCE_LIB_PATH
	try {
		$env:LANCE_LIB_PATH = $stagedDllPath
		$env:PATH = "$runtimeDir;$system32;$env:SystemRoot;$goBin"
		Push-Location $RepoRoot
		try {
			Invoke-Logged -FilePath $goExe -ArgumentList @("run", "-tags", "lance_external_lib", "./examples/roundtrip") -LogPath $roundtripLog -Label "supplied-library roundtrip"
		} finally {
			Pop-Location
		}
	} finally {
		$env:PATH = $oldPath
		$env:LANCE_LIB_PATH = $oldLibPath
	}
	$roundtripText = Get-Content -Raw -LiteralPath $roundtripLog
	$expectedRoundtrip = "Created, reopened and scanned 3 rows: [10 20 30]"
	if ($roundtripText -notmatch [regex]::Escape($expectedRoundtrip)) {
		throw "supplied-library roundtrip did not print: $expectedRoundtrip"
	}
	$script:RoundtripResult = "PASS (supplied staged DLL, external tag; not cold default, cold external, or public)"

	$script:Step = "candidate-acceptance"
	$script:ProductionCandidate = "verified with roundtrip, acceptance pending (unpublished)"
	$finalDllSha = (Get-FileHash -LiteralPath $stagedDll -Algorithm SHA256).Hash.ToLowerInvariant()
	$finalDllSize = (Get-Item -LiteralPath $stagedDll).Length
	$finalHeaderSha = (Get-FileHash -LiteralPath $stagedHeader -Algorithm SHA256).Hash.ToLowerInvariant()
	$finalGoModSha = (Get-FileHash -LiteralPath $rootGoMod -Algorithm SHA256).Hash.ToLowerInvariant()
	$finalGoSumSha = (Get-FileHash -LiteralPath $rootGoSum -Algorithm SHA256).Hash.ToLowerInvariant()
	if ($finalDllSha -ne $script:DllSha256 -or [int64]$finalDllSize -ne [int64]$script:DllSize) { throw "staged DLL bytes changed during candidate generation" }
	if ($finalHeaderSha -ne $headerSha) { throw "staged header bytes changed during candidate generation" }
	if ($finalGoModSha -ne $rootGoModSha -or $finalGoSumSha -ne $rootGoSumSha) { throw "root go.mod/go.sum changed during candidate generation" }
	$guardStatusAfter = @(Invoke-Capture -FilePath "git" -ArgumentList @("-C", $RepoRoot, "status", "--porcelain", "--untracked-files=all", "--", "go.mod", "go.sum", "internal/bundled") -Label "git status guarded paths after packaging") -join "`n"
	if (-not [string]::IsNullOrWhiteSpace($guardStatusAfter)) {
		throw "go.mod/go.sum/internal/bundled changed during candidate generation: $guardStatusAfter"
	}

	$candidateManifest = @(
		"candidate=windows-$Architecture",
		"candidate_status=verified-unpublished",
		"native_evidence=PASS",
		"dependency_eligibility=PASS",
		"module_path=$modulePath",
		"target=windows/$Architecture",
		"library_sha256=$script:DllSha256",
		"library_bytes=$script:DllSize",
		"payload_sha256=$payloadSha",
		"payload_bytes=$payloadBytes",
		"decompressed_sha256=$script:DllSha256",
		"decompressed_bytes=$script:DllSize",
		"verifier=ok",
		"roundtrip=supplied-library PASS",
		"selector_activated=false",
		"module_requirement_added=false",
		"publication_attempted=false",
		"cold_default_consumer=not_run",
		"cold_external_consumer=not_run",
		"public_consumer=not_run",
		"public_module=not_published",
		"supported_distribution=not_claimed"
	)
	$candidateManifest | Set-Content -LiteralPath (Join-Path $candidateOut "candidate-manifest.txt")
	$candidateManifest | Set-Content -LiteralPath (Join-Path $EvidenceDir "production-candidate.txt")
	$script:ProductionCandidate = "verified-unpublished: $modulePath (selector not activated)"
	Write-ProductionEligibility -Eligible $true -CandidateStatus "verified_unpublished"

	$script:Step = "complete"
	Set-Status "PASS" "all native Windows $Architecture checks passed; production candidate verified but unpublished"
	Write-Summary
	Stop-ScriptTranscript
	Write-Log "PASS: native evidence and verified unpublished Windows production candidate; no publication or selector activation"
	exit 0
} catch {
	$script:ExitCode = 1
	$message = $_.Exception.Message
	if ($script:ProductionGateBlocked) {
		$script:Status = "BLOCKED"
		Write-Log "BLOCKED at $script:Step`: $message"
		try {
			Set-Status "BLOCKED" "$script:Step`: $message"
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
	$script:Status = "FAIL"
	Write-Log "FAILED at $script:Step`: $message"
	if ($script:ProductionEligibility -like "PASS:*") {
		if ($script:ProductionCandidate -notlike "verified-unpublished*") {
			$script:ProductionCandidate = "failed before acceptance (unpublished)"
		}
		try {
			Write-ProductionEligibility -Eligible $true -CandidateStatus "failed"
		} catch {
			Write-Host "warning: could not update production eligibility after failure: $($_.Exception.Message)"
		}
	}
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
