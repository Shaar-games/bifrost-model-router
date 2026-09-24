<#
.SYNOPSIS
Installs the native Bifrost Model Router for the current Windows user.

.DESCRIPTION
Copies dist\native\server.exe (built by `mise run build`) to
%LOCALAPPDATA%\bifrost-model-router\bin, registers a hidden launcher in the
user's Startup folder, restarts the router and waits for /health. No
administrator rights are required. The configuration is read from
%APPDATA%\bifrost-model-router\config.json and providers.env.
#>
param(
	[string]$Address = "127.0.0.1:80",
	[switch]$RestartOnly
)

$ErrorActionPreference = "Stop"
$installDir = Join-Path $env:LOCALAPPDATA "bifrost-model-router"
$binary = Join-Path $installDir "bin\bifrost-model-router-server.exe"
$launcher = Join-Path $installDir "start-hidden.vbs"
$configFile = Join-Path $env:APPDATA "bifrost-model-router\config.json"
$shortcut = Join-Path ([Environment]::GetFolderPath("Startup")) "Bifrost Model Router.lnk"
$healthHost = ($Address -split ":")[0]
$healthPort = ($Address -split ":")[-1]

if (-not (Test-Path -LiteralPath $configFile)) {
	throw "Missing $configFile. Create it before installing."
}

Get-Process bifrost-model-router-server -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1

if (-not $RestartOnly) {
	$built = Join-Path $PSScriptRoot "..\dist\native\server.exe"
	if (-not (Test-Path -LiteralPath $built)) {
		throw "Missing $built. Run 'mise run build' first."
	}
	New-Item -ItemType Directory -Force -Path (Split-Path $binary) | Out-Null
	Copy-Item -Force -LiteralPath $built -Destination $binary

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
	Write-Host "Installed $binary and startup shortcut $shortcut"
}

if (-not (Test-Path -LiteralPath $launcher)) {
	throw "Missing $launcher. Run 'mise run install' first."
}
Start-Process -FilePath (Join-Path $env:WINDIR "System32\wscript.exe") -ArgumentList "`"$launcher`""

$healthUrl = "http://${healthHost}:${healthPort}/health"
for ($attempt = 0; $attempt -lt 120; $attempt++) {
	try {
		if ((Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200) {
			Write-Host "Router healthy at $healthUrl"
			exit 0
		}
	} catch {
		Start-Sleep -Seconds 1
	}
}
Write-Error "Router did not become healthy; see $(Join-Path $installDir 'router.log')"
exit 1
