# install.ps1 — download and install the rkload binary on Windows.
#
# Usage:
#   iwr https://raw.githubusercontent.com/RKInnovate/rkload/main/scripts/install.ps1 -UseBasicParsing | iex
#   .\scripts\install.ps1                          # from a clone
#   .\scripts\install.ps1 -Version v1.0.0          # pin a specific tag
#   .\scripts\install.ps1 -Dir 'C:\tools\rkload'   # install elsewhere
#
# The script picks an install directory in this order:
#   1. -Dir DIR if passed
#   2. $env:RKLOAD_INSTALL_DIR if set
#   3. $env:LOCALAPPDATA\rkload\bin (per-user, no admin needed)
#
# Exits non-zero on any failure. Adds the install dir to the user PATH
# (via the registry, not just the current session) so a new shell can
# find rkload without further setup.

[CmdletBinding()]
param(
	[string]$Version = '',
	[string]$Dir = ''
)

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'  # cosmetic; speeds up Invoke-WebRequest

$Repo = 'RKInnovate/rkload'

function Resolve-Arch {
	# PROCESSOR_ARCHITECTURE is the right env var here — Windows
	# normalises x64/arm64 across all shells. Map to the goreleaser
	# archive names (x86_64 / arm64).
	switch -Wildcard ($env:PROCESSOR_ARCHITECTURE) {
		'AMD64' { return 'x86_64' }
		'ARM64' { return 'arm64'  }
		default {
			throw "Unsupported architecture: $($env:PROCESSOR_ARCHITECTURE). Build from source or open an issue."
		}
	}
}

function Resolve-LatestVersion {
	# /releases/latest redirects to the actual tag; reading the
	# Location header avoids the rate-limited /releases/latest JSON
	# endpoint for unauthenticated callers.
	$resp = Invoke-WebRequest -Uri "https://api.github.com/repos/$Repo/releases/latest" `
		-UseBasicParsing -Headers @{ 'User-Agent' = 'rkload-installer' }
	$obj  = $resp.Content | ConvertFrom-Json
	if ($obj.tag_name) { return $obj.tag_name }
	throw 'Could not determine latest release tag. Pass -Version explicitly.'
}

function Resolve-InstallDir {
	if ($Dir)                          { return $Dir }
	if ($env:RKLOAD_INSTALL_DIR)       { return $env:RKLOAD_INSTALL_DIR }
	if ($env:LOCALAPPDATA) {
		return (Join-Path $env:LOCALAPPDATA 'rkload\bin')
	}
	throw 'No install directory could be determined. Pass -Dir explicitly.'
}

function Ensure-OnUserPath {
	param([string]$Target)
	# Use [Environment]::GetEnvironmentVariable with the User scope —
	# editing $env:PATH alone only affects this process and the change
	# is gone the moment the shell closes.
	$userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
	if ($null -eq $userPath) { $userPath = '' }
	$segments = $userPath -split ';' | Where-Object { $_ -ne '' }
	if ($segments -notcontains $Target) {
		$newPath = if ($userPath) { "$userPath;$Target" } else { $Target }
		[Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
		Write-Host "Added $Target to your user PATH. Open a new terminal to pick it up."
	}
	# Make it usable in the current session as well, so the trailing
	# `rkload -version` call below works without a shell restart.
	if (-not ($env:PATH -split ';' | Where-Object { $_ -eq $Target })) {
		$env:PATH = "$env:PATH;$Target"
	}
}

if (-not $Version) { $Version = Resolve-LatestVersion }
$arch        = Resolve-Arch
$installDir  = Resolve-InstallDir
$versionBare = $Version.TrimStart('v')
$archive     = "rkload_${versionBare}_windows_${arch}.zip"
$url         = "https://github.com/$Repo/releases/download/$Version/$archive"

Write-Host "Installing rkload $Version (windows/$arch) to $installDir"
Write-Host "  archive: $url"

if (-not (Test-Path $installDir)) {
	New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

$tmp = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "rkload-install-$([guid]::NewGuid().Guid)") -Force
try {
	$archivePath = Join-Path $tmp.FullName $archive
	Invoke-WebRequest -Uri $url -OutFile $archivePath -UseBasicParsing
	Expand-Archive -Path $archivePath -DestinationPath $tmp.FullName -Force

	$binary = Join-Path $tmp.FullName 'rkload.exe'
	if (-not (Test-Path $binary)) {
		throw "Archive did not contain rkload.exe"
	}
	Move-Item -Path $binary -Destination (Join-Path $installDir 'rkload.exe') -Force
}
finally {
	Remove-Item -Path $tmp.FullName -Recurse -Force -ErrorAction SilentlyContinue
}

Ensure-OnUserPath -Target $installDir
Write-Host "Installed: $(Join-Path $installDir 'rkload.exe')"

& (Join-Path $installDir 'rkload.exe') -version
