<#
.SYNOPSIS
Installs the published Bifrost Model Router binary for the current Windows user.

.DESCRIPTION
Downloads the Windows server binary from the GitHub release, checks its
SHA-256, and installs it under %LOCALAPPDATA%\bifrost-model-router. When
%APPDATA%\bifrost-model-router\config.json already exists, the script registers
a Startup shortcut and waits until http://<address>/health succeeds. It does
not create or print provider credentials. Do not run it as administrator.

After install, the script prints one command that points Codex at the router.
Set BIFROST_ROUTER_CONFIGURE_CODEX=1 to run that step: it backs up
%USERPROFILE%\.codex\config.toml, writes the bifrost-router provider, and
reads the virtual key from providers.env without printing it.
#>
& {
param(
	[string]$Version = "latest",
	[string]$Address = "127.0.0.1:80",
	[string]$Repository = "Shaar-games/bifrost-model-router"
)

$ErrorActionPreference = "Stop"

function Get-CodexBaseUrl([string]$ListenAddress) {
	$parts = $ListenAddress -split ":", 2
	if ($parts[1] -eq "80") {
		return "http://$($parts[0])/v1"
	}
	return "http://$($parts[0]):$($parts[1])/v1"
}

function Read-EnvAssignment([string]$Path, [string]$Name) {
	if (-not (Test-Path -LiteralPath $Path)) {
		return $null
	}
	foreach ($line in [System.IO.File]::ReadAllLines($Path)) {
		if ($line -match '^\s*#' -or $line -notmatch '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
			continue
		}
		if ($Matches[1] -ne $Name) {
			continue
		}
		$value = $Matches[2].Trim()
		if (($value.StartsWith('"') -and $value.EndsWith('"') -and $value.Length -ge 2) -or ($value.StartsWith("'") -and $value.EndsWith("'") -and $value.Length -ge 2)) {
			$value = $value.Substring(1, $value.Length - 2)
		}
		if ($value) {
			return $value
		}
	}
	return $null
}

function Get-RouterVirtualKey([string]$ConfigFile) {
	$envFile = Join-Path (Split-Path -Parent $ConfigFile) "providers.env"
	$fromQuickstart = Read-EnvAssignment $envFile "BIFROST_QUICKSTART_VK"
	if ($fromQuickstart) {
		return $fromQuickstart
	}
	if (-not (Test-Path -LiteralPath $ConfigFile)) {
		return $null
	}
	$json = [System.IO.File]::ReadAllText($ConfigFile) | ConvertFrom-Json
	foreach ($entry in @($json.governance.virtual_keys)) {
		if (-not $entry) {
			continue
		}
		$value = [string]$entry.value
		if ($value -match '^env\.([A-Za-z_][A-Za-z0-9_]*)$') {
			$resolved = Read-EnvAssignment $envFile $Matches[1]
			if ($resolved) {
				return $resolved
			}
		} elseif ($value -like "sk-bf-*") {
			return $value
		}
	}
	return $null
}

function ConvertTo-NormalizedLines([string]$Text) {
	if (-not $Text) {
		return @()
	}
	return @(($Text -replace "`r`n", "`n" -replace "`r", "`n").Split("`n"))
}

function Remove-MarkedLines($Lines, [string]$Begin, [string]$End) {
	$result = New-Object System.Collections.Generic.List[string]
	$skipping = $false
	$seen = 0
	foreach ($line in @($Lines)) {
		$trim = $line.Trim()
		if (-not $skipping -and $trim -eq $Begin) {
			$seen++
			if ($seen -gt 1) {
				throw "Multiple Codex blocks start with $Begin"
			}
			$skipping = $true
			continue
		}
		if ($skipping -and $trim -eq $End) {
			$skipping = $false
			continue
		}
		if (-not $skipping) {
			$result.Add($line)
		}
	}
	if ($skipping) {
		throw "Malformed Codex block starting with $Begin"
	}
	return ,@($result)
}

function Remove-ProviderTable($Lines) {
	$result = New-Object System.Collections.Generic.List[string]
	$skipping = $false
	foreach ($line in @($Lines)) {
		$trim = $line.Trim()
		if (-not $skipping -and ($trim -eq "[model_providers.bifrost-router]" -or $trim.StartsWith("[model_providers.bifrost-router."))) {
			$skipping = $true
			continue
		}
		if ($skipping -and $trim.StartsWith("[")) {
			$skipping = $false
		}
		if (-not $skipping) {
			$result.Add($line)
		}
	}
	return ,@($result)
}

function Remove-RootModelKeys($Lines) {
	$result = New-Object System.Collections.Generic.List[string]
	$atRoot = $true
	foreach ($line in @($Lines)) {
		$trim = $line.Trim()
		if ($trim.StartsWith("[")) {
			$atRoot = $false
		}
		if ($atRoot -and ($trim.StartsWith("model =") -or $trim.StartsWith("model_provider =") -or $trim.StartsWith("model_reasoning_effort ="))) {
			continue
		}
		$result.Add($line)
	}
	return ,@($result)
}

function New-CodexQuickstart([string]$Existing, [string]$BaseUrl, [string]$VirtualKey) {
	$lines = ConvertTo-NormalizedLines $Existing
	$lines = Remove-MarkedLines $lines "# BEGIN bifrost-model-router quickstart defaults (managed)" "# END bifrost-model-router quickstart defaults (managed)"
	$lines = Remove-MarkedLines $lines "# BEGIN bifrost-model-router quickstart provider (managed)" "# END bifrost-model-router quickstart provider (managed)"
	$lines = Remove-MarkedLines $lines "# BEGIN bifrost-model-router (managed)" "# END bifrost-model-router (managed)"
	$lines = Remove-ProviderTable $lines
	$lines = Remove-RootModelKeys $lines
	$body = ((@($lines) | Where-Object { $_ -ne $null }) -join "`n").Trim()
	$defaults = @(
		"# BEGIN bifrost-model-router quickstart defaults (managed)"
		'model = "gpt-5.6-sol"'
		'model_provider = "bifrost-router"'
		'model_reasoning_effort = "medium"'
		"# END bifrost-model-router quickstart defaults (managed)"
	) -join "`n"
	$block = @(
		"# BEGIN bifrost-model-router quickstart provider (managed)"
		"[model_providers.bifrost-router]"
		'name = "Local Bifrost Router"'
		"base_url = `"$BaseUrl`""
		'wire_api = "responses"'
		"requires_openai_auth = true"
		('http_headers = { "x-bf-vk" = "' + $VirtualKey + '" }')
		"# END bifrost-model-router quickstart provider (managed)"
	) -join "`n"
	if ($body) {
		return $defaults + "`n`n" + $body + "`n`n" + $block + "`n"
	}
	return $defaults + "`n`n" + $block + "`n"
}

function Test-CodexRouterReady([string]$Existing, [string]$BaseUrl) {
	if (-not $Existing) {
		return $false
	}
	$normalized = $Existing -replace "`r`n", "`n" -replace "`r", "`n"
	$hasProvider = $normalized -match '(?m)^model_provider = "bifrost-router"$'
	$hasBase = $normalized -match ("(?m)^base_url = `"" + [regex]::Escape($BaseUrl) + "`"`$")
	$hasHeader = $normalized -match "x-bf-vk"
	return [bool]($hasProvider -and $hasBase -and $hasHeader)
}

function Write-CodexFile([string]$Path, [string]$Text, [string]$Previous) {
	$directory = Split-Path -Parent $Path
	New-Item -ItemType Directory -Force -Path $directory | Out-Null
	$backup = $null
	if ($Previous) {
		$backup = "$Path.bak.$(Get-Date -Format 'yyyyMMdd-HHmmss')"
		[System.IO.File]::WriteAllText($backup, $Previous, (New-Object System.Text.UTF8Encoding $false))
	}
	$temporary = Join-Path $directory (".bifrost-router-config-" + [guid]::NewGuid().ToString("n"))
	[System.IO.File]::WriteAllText($temporary, $Text, (New-Object System.Text.UTF8Encoding $false))
	Move-Item -Force -LiteralPath $temporary -Destination $Path
	return $backup
}

function Install-CodexProvider([string]$ListenAddress) {
	$baseUrl = Get-CodexBaseUrl $ListenAddress
	$codexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE ".codex" }
	$codexConfig = Join-Path $codexHome "config.toml"
	$configFile = Join-Path $env:APPDATA "bifrost-model-router\config.json"
	if (Test-Path -LiteralPath $codexConfig) {
		$item = Get-Item -Force -LiteralPath $codexConfig
		if ($item.PSIsContainer -or ($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint)) {
			throw "Refusing to edit $codexConfig. Codex config must be a regular file."
		}
	}
	$existing = ""
	if (Test-Path -LiteralPath $codexConfig) {
		$existing = [System.IO.File]::ReadAllText($codexConfig)
	}
	if (Test-CodexRouterReady $existing $baseUrl) {
		Write-Host "Codex already uses bifrost-router at $baseUrl"
		Write-Host "Config: $codexConfig"
		Write-Host "Fully quit Codex and open a new task."
		return
	}
	$envFile = Join-Path (Split-Path -Parent $configFile) "providers.env"
	$key = Get-RouterVirtualKey $configFile
	if (-not $key) {
		throw "No virtual key found. Set BIFROST_QUICKSTART_VK in $envFile, then run the configure command again."
	}
	if ($key -match '[\r\n"\\$]') {
		throw "Virtual key contains characters that cannot be stored in the Codex config."
	}
	$updated = New-CodexQuickstart $existing $baseUrl $key
	$backup = Write-CodexFile $codexConfig $updated $existing
	Write-Host "Installed Codex provider in $codexConfig"
	if ($backup) {
		Write-Host "Backup: $backup"
	}
	Write-Host "New threads use gpt-5.6-sol with medium reasoning through $baseUrl"
	Write-Host "Fully quit Codex and open a new task. Existing tasks keep their previous provider."
}

function Write-CodexConfigureHint([string]$ListenAddress, [string]$Repo) {
	Write-Host ""
	Write-Host "Codex does not use the router until its config.toml selects bifrost-router."
	Write-Host "Run this once. It backs up that file, writes the provider, and does not print the virtual key."
	Write-Host "If bifrost-router already points at this address, the file is left unchanged."
	Write-Host ""
	if ($PSCommandPath) {
		$command = '$env:BIFROST_ROUTER_ADDRESS=''{0}''; $env:BIFROST_ROUTER_CONFIGURE_CODEX=''1''; & ''{1}''' -f $ListenAddress, $PSCommandPath
	} else {
		$url = "https://github.com/$Repo/releases/latest/download/setup-binary.ps1"
		$command = '$env:BIFROST_ROUTER_ADDRESS=''{0}''; $env:BIFROST_ROUTER_CONFIGURE_CODEX=''1''; iex (irm ''{1}'')' -f $ListenAddress, $url
	}
	Write-Host $command
	Write-Host ""
	Write-Host "Then fully quit Codex and open a new task. Existing tasks keep their previous provider."
}

if ($env:BIFROST_ROUTER_ADDRESS) {
	$Address = $env:BIFROST_ROUTER_ADDRESS
}
$configureCodex = $env:BIFROST_ROUTER_CONFIGURE_CODEX -eq "1"
Remove-Item Env:\BIFROST_ROUTER_CONFIGURE_CODEX -ErrorAction SilentlyContinue
Remove-Item Env:\BIFROST_ROUTER_ADDRESS -ErrorAction SilentlyContinue

if ($Address -notmatch '^[A-Za-z0-9._-]+:[0-9]+$') {
	throw "Invalid address '$Address'. Expected host:port, for example 127.0.0.1:80."
}

if ($configureCodex) {
	Install-CodexProvider $Address
	return
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
		Write-CodexConfigureHint $Address $Repository
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
				Write-CodexConfigureHint $Address $Repository
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
