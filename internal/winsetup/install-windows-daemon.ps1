<#
.SYNOPSIS
  Installs a standalone Windows-container dockerd (Moby static binaries) as a
  service, so multirunner can run Windows runners WITHOUT Docker Desktop. The
  daemon listens on its own named pipe to avoid colliding with Podman/Docker
  Desktop's default \\.\pipe\docker_engine.

.NOTES
  Run elevated (Administrator). Enabling the Containers feature may require a reboot.
  Writes a status file + transcript under %ProgramData%\multirunner so the caller
  can report the outcome.
#>
[CmdletBinding()]
param(
    [string]$DockerVersion = '27.3.1',
    [string]$InstallDir    = 'C:\multirunner-docker',
    [string]$Pipe          = 'npipe:////./pipe/docker_engine_windows',
    [string]$ServiceName   = 'multirunner-dockerd',
    [ValidateSet('process', 'hyperv')]
    [string]$Isolation     = 'process'
)
$ErrorActionPreference = 'Stop'

$StatusDir = Join-Path $env:ProgramData 'multirunner'
New-Item -ItemType Directory -Force -Path $StatusDir | Out-Null
$StatusFile = Join-Path $StatusDir 'winsetup-status.txt'
$LogFile = Join-Path $StatusDir 'winsetup.log'
Set-Content -Path $StatusFile -Value 'running' -Encoding ascii
try { Start-Transcript -Path $LogFile -Force | Out-Null } catch {}

function Set-Status([string]$s) { Set-Content -Path $StatusFile -Value $s -Encoding ascii }

try {
    $id = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not $id.IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)) {
        throw 'This script must be run as Administrator.'
    }

    # 1. Containers feature
    $feature = Get-WindowsOptionalFeature -Online -FeatureName Containers
    if ($feature.State -ne 'Enabled') {
        Write-Host 'Enabling Windows Containers feature...'
        $res = Enable-WindowsOptionalFeature -Online -FeatureName Containers -All -NoRestart
        if ($res.RestartNeeded) {
            Write-Warning 'A reboot is required to finish enabling Containers. Reboot, then re-run.'
            Set-Status 'reboot-required'
            return
        }
    }

    # 2. Download Moby static binaries
    $binDir = Join-Path $InstallDir 'bin'
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    $dockerd = Join-Path $binDir 'dockerd.exe'
    if (-not (Test-Path $dockerd)) {
        $url = "https://download.docker.com/win/static/stable/x86_64/docker-$DockerVersion.zip"
        $zip = Join-Path $env:TEMP "docker-$DockerVersion.zip"
        Write-Host "Downloading $url ..."
        Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing
        Expand-Archive -Path $zip -DestinationPath $InstallDir -Force   # extracts <InstallDir>\docker\*.exe
        Copy-Item (Join-Path $InstallDir 'docker\*.exe') $binDir -Force
        Remove-Item $zip -Force
    }

    # 3. daemon.json
    $cfgDir = Join-Path $InstallDir 'config'
    $dataDir = Join-Path $InstallDir 'data'
    New-Item -ItemType Directory -Force -Path $cfgDir, $dataDir | Out-Null
    $cfgPath = Join-Path $cfgDir 'daemon.json'
    $daemon = [ordered]@{
        hosts       = @($Pipe)
        'exec-opts' = @("isolation=$Isolation")
        'data-root' = $dataDir
    }
    $daemon | ConvertTo-Json | Set-Content -Path $cfgPath -Encoding ascii
    Write-Host "Wrote $cfgPath"

    # 4. Register + start the service
    if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
        Write-Host "Service $ServiceName already exists; reconfiguring."
        Stop-Service $ServiceName -ErrorAction SilentlyContinue
        & sc.exe delete $ServiceName | Out-Null
        Start-Sleep -Seconds 2
    }
    Write-Host "Registering service $ServiceName ..."
    & $dockerd --register-service --service-name $ServiceName --config-file $cfgPath
    Start-Service $ServiceName

    Write-Host ''
    Write-Host "Done. Windows dockerd is running on: $Pipe"
    Set-Status 'ok'
}
catch {
    Set-Status ("error: " + $_.Exception.Message)
    Write-Error $_
}
finally {
    try { Stop-Transcript | Out-Null } catch {}
}
