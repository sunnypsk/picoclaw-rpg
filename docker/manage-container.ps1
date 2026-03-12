param(
    [ValidateSet('', 'menu', 'status', 'build', 'run', 'start', 'stop', 'restart', 'remove', 'logs', 'recreate')]
    [string]$Action = '',
    [string]$ContainerName = 'picoclaw-gateway',
    [string]$ImageName = $(if ($env:PICOCLAW_IMAGE) { $env:PICOCLAW_IMAGE } else { 'ghcr.io/your-github-user/picoclaw-rpg:main' }),
    [switch]$UseNonRoot,
    [switch]$Force
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$dockerfilePath = Join-Path $PSScriptRoot 'Dockerfile'
$dataDir = Join-Path $PSScriptRoot 'data'
$containerHome = if ($UseNonRoot) { '/home/picoclaw/.picoclaw' } else { '/root/.picoclaw' }
$containerUser = if ($UseNonRoot) { $null } else { 'root' }

function Write-Section {
    param([string]$Text)
    Write-Host "`n== $Text ==" -ForegroundColor Cyan
}

function Invoke-DockerCommand {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,
        [switch]$IgnoreExitCode
    )

    Write-Host ('> docker ' + ($Arguments -join ' ')) -ForegroundColor DarkCyan
    & docker @Arguments
    if (-not $IgnoreExitCode -and $LASTEXITCODE -ne 0) {
        throw "docker command failed with exit code $LASTEXITCODE"
    }
}

function Test-DockerInstalled {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw 'Docker CLI is not installed or not in PATH.'
    }
}

function Get-ContainerRecord {
    $result = & docker ps -a --filter "name=^/${ContainerName}$" --format '{{.ID}}|{{.Status}}|{{.Names}}'
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to query container state for $ContainerName"
    }

    if ([string]::IsNullOrWhiteSpace($result)) {
        return $null
    }

    $parts = $result.Trim() -split '\|', 3
    return [pscustomobject]@{
        Id     = $parts[0]
        Status = $parts[1]
        Name   = $parts[2]
    }
}

function Get-ContainerState {
    $record = Get-ContainerRecord
    if ($null -eq $record) {
        return 'missing'
    }
    if ($record.Status -like 'Up*') {
        return 'running'
    }
    return 'stopped'
}

function Ensure-DataDirectory {
    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
}

function Show-Status {
    Write-Section 'Container Status'
    $state = Get-ContainerState
    Write-Host "Container: $ContainerName"
    Write-Host "Image:     $ImageName"
    Write-Host "Data dir:  $dataDir"
    Write-Host "Mount:     $containerHome"
    Write-Host "User:      $(if ($containerUser) { $containerUser } else { 'image default' })"
    Write-Host "State:     $state"

    $record = Get-ContainerRecord
    if ($null -ne $record) {
        Write-Host "Status:    $($record.Status)"
    }
}

function Build-Image {
    Write-Section 'Build Image'
    Invoke-DockerCommand -Arguments @('build', '-t', $ImageName, '-f', $dockerfilePath, $repoRoot)
}

function Run-Container {
    Write-Section 'Run Container'
    Ensure-DataDirectory

    $state = Get-ContainerState
    if ($state -eq 'running') {
        Write-Host "$ContainerName is already running."
        return
    }
    if ($state -eq 'stopped') {
        Write-Host "$ContainerName already exists but is stopped. Starting it instead."
        Start-Container
        return
    }

    $volumeSpec = ('{0}:{1}' -f $dataDir, $containerHome)
    $arguments = @('run', '-d', '--name', $ContainerName, '--restart', 'on-failure')
    if ($containerUser) {
        $arguments += @('--user', $containerUser)
    }
    $arguments += @('-v', $volumeSpec, $ImageName, 'gateway')
    Invoke-DockerCommand -Arguments $arguments
}

function Start-Container {
    Write-Section 'Start Container'
    $state = Get-ContainerState
    if ($state -eq 'missing') {
        Write-Host "$ContainerName does not exist yet. Use Run first."
        return
    }
    if ($state -eq 'running') {
        Write-Host "$ContainerName is already running."
        return
    }
    Invoke-DockerCommand -Arguments @('start', $ContainerName)
}

function Stop-Container {
    Write-Section 'Stop Container'
    $state = Get-ContainerState
    if ($state -eq 'missing') {
        Write-Host "$ContainerName does not exist."
        return
    }
    if ($state -eq 'stopped') {
        Write-Host "$ContainerName is already stopped."
        return
    }
    Invoke-DockerCommand -Arguments @('stop', $ContainerName)
}

function Restart-Container {
    Write-Section 'Restart Container'
    $state = Get-ContainerState
    if ($state -eq 'missing') {
        Write-Host "$ContainerName does not exist yet. Running a new container instead."
        Run-Container
        return
    }
    Invoke-DockerCommand -Arguments @('restart', $ContainerName)
}

function Remove-Container {
    Write-Section 'Remove Container'
    $state = Get-ContainerState
    if ($state -eq 'missing') {
        Write-Host "$ContainerName does not exist."
        return
    }

    if (-not $Force) {
        $confirmation = Read-Host "Remove $ContainerName? This keeps docker/data but deletes the container. [y/N]"
        if ($confirmation -notin @('y', 'Y', 'yes', 'YES')) {
            Write-Host 'Cancelled.'
            return
        }
    }

    Invoke-DockerCommand -Arguments @('rm', '-f', $ContainerName)
}

function Show-Logs {
    Write-Section 'Container Logs'
    $state = Get-ContainerState
    if ($state -eq 'missing') {
        Write-Host "$ContainerName does not exist."
        return
    }
    Invoke-DockerCommand -Arguments @('logs', '-f', $ContainerName)
}

function Recreate-Container {
    Write-Section 'Recreate Container'
    if (Get-ContainerRecord) {
        Invoke-DockerCommand -Arguments @('rm', '-f', $ContainerName)
    }
    Run-Container
}

function Show-Menu {
    while ($true) {
        Write-Section 'PicoClaw Container Manager'
        Write-Host '1. Status'
        Write-Host '2. Build image'
        Write-Host '3. Run gateway container'
        Write-Host '4. Start container'
        Write-Host '5. Stop container'
        Write-Host '6. Restart container'
        Write-Host '7. Show logs'
        Write-Host '8. Remove container'
        Write-Host '9. Rebuild and recreate'
        Write-Host '0. Exit'

        $choice = Read-Host 'Choose an action'
        switch ($choice) {
            '1' { Show-Status }
            '2' { Build-Image }
            '3' { Run-Container }
            '4' { Start-Container }
            '5' { Stop-Container }
            '6' { Restart-Container }
            '7' { Show-Logs }
            '8' { Remove-Container }
            '9' { Build-Image; Recreate-Container }
            '0' { return }
            default { Write-Host 'Unknown choice.' -ForegroundColor Yellow }
        }
    }
}

Test-DockerInstalled

switch ($Action) {
    '' { Show-Menu }
    'menu' { Show-Menu }
    'status' { Show-Status }
    'build' { Build-Image }
    'run' { Run-Container }
    'start' { Start-Container }
    'stop' { Stop-Container }
    'restart' { Restart-Container }
    'remove' { Remove-Container }
    'logs' { Show-Logs }
    'recreate' { Recreate-Container }
}
