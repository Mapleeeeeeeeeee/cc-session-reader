#Requires -Version 5.1
[CmdletBinding()]
param(
    [switch]$NoSkill
)

$ErrorActionPreference = 'Stop'

$Repo      = "Mapleeeeeeeeeee/cc-session-reader"
$InstallDir = Join-Path $env:LOCALAPPDATA "cc-session"
$SkillDir  = Join-Path $HOME ".claude\skills\cc-session"
$SkillUrl  = "https://raw.githubusercontent.com/$Repo/main/SKILL.md"

# ── architecture detection ────────────────────────────────────────────────────

function Get-Architecture {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        'X64'   { return 'amd64' }
        'Arm64' { return 'arm64' }
        default {
            Write-Error "Unsupported architecture: $arch"
            exit 1
        }
    }
}

# ── latest version lookup ─────────────────────────────────────────────────────

function Get-LatestVersion {
    $apiUrl = "https://api.github.com/repos/$Repo/releases/latest"
    try {
        $response = Invoke-RestMethod -Uri $apiUrl -UseBasicParsing
        $version = $response.tag_name
        if (-not $version) {
            Write-Error "Failed to parse release version from GitHub API."
            exit 1
        }
        return $version
    } catch {
        Write-Error "Failed to fetch latest release: $_"
        exit 1
    }
}

# ── binary download & install ─────────────────────────────────────────────────

function Install-Binary {
    param([string]$Version, [string]$Arch)

    $versionBare = $Version.TrimStart('v')
    $zipName     = "cc-session-reader_${versionBare}_windows_${Arch}.zip"
    $downloadUrl = "https://github.com/$Repo/releases/download/$Version/$zipName"
    $tmpDir      = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())

    Write-Host "Downloading cc-session $Version for windows/$Arch..."

    try {
        New-Item -ItemType Directory -Path $tmpDir | Out-Null
        $zipPath = Join-Path $tmpDir $zipName

        Invoke-WebRequest -Uri $downloadUrl -OutFile $zipPath -UseBasicParsing
        Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

        if (-not (Test-Path $InstallDir)) {
            New-Item -ItemType Directory -Path $InstallDir | Out-Null
        }

        $exeSrc = Join-Path $tmpDir "cc-session.exe"
        $exeDst = Join-Path $InstallDir "cc-session.exe"
        Move-Item -Path $exeSrc -Destination $exeDst -Force

        Write-Host "Installed cc-session to $exeDst"
    } finally {
        Remove-Item -Path $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# ── PATH check ────────────────────────────────────────────────────────────────

function Update-UserPath {
    $currentPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
    $dirs = $currentPath -split ';' | Where-Object { $_ -ne '' }

    if ($dirs -contains $InstallDir) {
        return
    }

    Write-Host ""
    Write-Host "Warning: $InstallDir is not in your PATH."

    if (-not [Environment]::UserInteractive) {
        Write-Host "Add it manually to your user PATH."
        return
    }

    $answer = Read-Host "Add $InstallDir to user PATH? [y/N]"
    if ($answer -match '^[Yy]$') {
        $newPath = ($dirs + $InstallDir) -join ';'
        [Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
        Write-Host "Added to user PATH. Restart your terminal to apply."
    }
}

# ── skill install ─────────────────────────────────────────────────────────────

function Install-Skill {
    if ($NoSkill) { return }

    if ([Environment]::UserInteractive) {
        $answer = Read-Host "Install Claude Code skill (cc-session)? [Y/n]"
        if ($answer -match '^[Nn]$') { return }
    }

    if (-not (Test-Path $SkillDir)) {
        New-Item -ItemType Directory -Path $SkillDir | Out-Null
    }

    $skillDst = Join-Path $SkillDir "SKILL.md"
    Write-Host "Installing Claude Code skill to $skillDst..."

    try {
        Invoke-WebRequest -Uri $SkillUrl -OutFile $skillDst -UseBasicParsing
        Write-Host "Skill installed. Use /cc-session in Claude Code to activate it."
    } catch {
        Write-Error "Failed to download skill: $_"
        exit 1
    }
}

# ── getting started ───────────────────────────────────────────────────────────

function Show-NextSteps {
    Write-Host ""
    Write-Host "── Getting started ────────────────────────────────────────────────"
    Write-Host "  cc-session list          # 列出最近的 session"
    Write-Host "  cc-session read <id>     # 讀取對話內容"
    Write-Host "  /cc-session              # 在 Claude Code 中使用 (需已安裝 Skill)"
    Write-Host ""
    Write-Host "── Token counting (optional) ──────────────────────────────────────"
    Write-Host "  For precise token counts in 'cc-session stats', create:"
    Write-Host "  $SkillDir\config.json"
    Write-Host ""
    Write-Host '  {"anthropic_api_key_file": "<path-to-your-api-key-file>"}'
    Write-Host ""
}

# ── main ──────────────────────────────────────────────────────────────────────

$version = Get-LatestVersion
$arch    = Get-Architecture

Install-Binary -Version $version -Arch $arch
Update-UserPath
Install-Skill
Show-NextSteps
