<#
.SYNOPSIS
Installs the published Bifrost Model Router binary for the current Windows user.

.DESCRIPTION
Downloads the Windows server binary from the GitHub release, checks its
SHA-256, and installs it under %LOCALAPPDATA%\bifrost-model-router. When
%APPDATA%\bifrost-model-router\config.json already exists, the script registers
a Startup shortcut and waits until http://<address>/health succeeds. It does
not create or print provider credentials. Do not run it as administrator.
#>
& {
param(
	[string]$Version = "latest",
	[string]$Address = "127.0.0.1:80",
	[string]$Repository = "Shaar-games/bifrost-model-router"
)

$ErrorActionPreference = "Stop"

if ($Address -notmatch '^[A-Za-z0-9._-]+:[0-9]+$') {
	throw "Invalid address '$Address'. Expected host:port, for example 127.0.0.1:80."
}

$processor = $env:PROCESSOR_ARCHITEW6432
if (-not $processor) {
	$processor = $env:PROCESSOR_ARCHITECTURE
}
switch ($processor) {
	"AMD64" { $goArch = "amd64" }
	"ARM64" { $goArch = "arm64" }
	default { throw "Unsupported Windows architecture: $processor" }
}

$asset = "bifrost-model-router-server-windows-$goArch.exe"
if ($Version -eq "latest") {
	$base = "https://github.com/$Repository/releases/latest/download"
} else {
	$base = "https://github.com/$Repository/releases/download/$Version"
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("bifrost-model-router-" + [guid]::NewGuid().ToString("n"))
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
	$downloaded = Join-Path $tempDir $asset
	$sumsFile = Join-Path $tempDir "SHA256SUMS"
	Write-Host "Downloading $base/$asset"
	Invoke-WebRequest -Uri "$base/$asset" -OutFile $downloaded -UseBasicParsing
	Invoke-WebRequest -Uri "$base/SHA256SUMS" -OutFile $sumsFile -UseBasicParsing

	$expected = $null
	foreach ($line in Get-Content -LiteralPath $sumsFile) {
		$parts = $line.Trim() -split '\s+', 2
		if ($parts.Length -eq 2 -and $parts[1] -eq $asset) {
			$expected = $parts[0].ToLowerInvariant()
		}
	}
	if (-not $expected) {
		throw "SHA256SUMS has no entry for $asset"
	}
	$actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $downloaded).Hash.ToLowerInvariant()
	if ($actual -ne $expected) {
		throw "Checksum mismatch for $asset"
	}

	$installDir = Join-Path $env:LOCALAPPDATA "bifrost-model-router"
	$binary = Join-Path $installDir "bin\bifrost-model-router-server.exe"
	$launcher = Join-Path $installDir "start-hidden.vbs"
	$configFile = Join-Path $env:APPDATA "bifrost-model-router\config.json"
	$shortcut = Join-Path ([Environment]::GetFolderPath("Startup")) "Bifrost Model Router.lnk"

	Get-Process bifrost-model-router-server -ErrorAction SilentlyContinue |
		Stop-Process -Force -ErrorAction SilentlyContinue
	Start-Sleep -Seconds 1
	$stillRunning = @(Get-Process bifrost-model-router-server -ErrorAction SilentlyContinue)
	if ($stillRunning.Count -gt 0) {
		$pids = ($stillRunning | ForEach-Object { $_.Id }) -join ", "
		throw "bifrost-model-router-server is still running (pid $pids). If it was started as administrator, end it in Task Manager, then run this script again as a normal user."
	}

	New-Item -ItemType Directory -Force -Path (Split-Path $binary) | Out-Null
	Copy-Item -Force -LiteralPath $downloaded -Destination $binary

	if (-not (Test-Path -LiteralPath $configFile)) {
		Write-Host "Installed $binary"
		Write-Host "Create $configFile, then run this script again to start the router."
		return
	}

	@"
' Starts Bifrost Model Router without a console window and appends its output to router.log.
Set shell = CreateObject("WScript.Shell")
base = shell.ExpandEnvironmentStrings("%LOCALAPPDATA%\bifrost-model-router")
command = "cmd.exe /c """"" & base & "\bin\bifrost-model-router-server.exe"" -addr $Address >> """ & base & "\router.log"" 2>&1"""
shell.Run command, 0, False
"@ | Set-Content -LiteralPath $launcher -Encoding ASCII

	$shell = New-Object -ComObject WScript.Shell
	$link = $shell.CreateShortcut($shortcut)
	$link.TargetPath = Join-Path $env:WINDIR "System32\wscript.exe"
	$link.Arguments = "`"$launcher`""
	$link.WorkingDirectory = $installDir
	$link.Description = "Bifrost Model Router (Codex local proxy)"
	$link.Save()

	Start-Process -FilePath (Join-Path $env:WINDIR "System32\wscript.exe") -ArgumentList "`"$launcher`""
	$healthHost = ($Address -split ":")[0]
	$healthPort = ($Address -split ":")[-1]
	$healthUrl = "http://${healthHost}:${healthPort}/health"
	for ($attempt = 0; $attempt -lt 120; $attempt++) {
		try {
			if ((Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200) {
				Write-Host "Installed $binary"
				Write-Host "Router healthy at $healthUrl"
				return
			}
		} catch {
			Start-Sleep -Seconds 1
		}
	}
	throw "Router did not become healthy; see $(Join-Path $installDir 'router.log')"
} finally {
	Remove-Item -Recurse -Force -LiteralPath $tempDir -ErrorAction SilentlyContinue
}
}
