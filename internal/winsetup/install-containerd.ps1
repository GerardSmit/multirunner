<#
.SYNOPSIS
  Installs containerd + the runhcs shim + nerdctl + Windows CNI plugins so
  multirunner can run Windows containers WITHOUT Docker Desktop. This is the
  supported Windows-container path: standalone Moby dockerd's bundled hcsshim
  cannot create the Hyper-V utility VM on current Windows builds, and process
  isolation is Server-only. containerd/runhcs runs Windows containers on both
  Windows Server (process isolation) and client (Hyper-V isolation).

.NOTES
  Run elevated (Administrator). Enabling the Containers/Hyper-V features may
  require a reboot. Writes a status file + transcript under %ProgramData%\multirunner.
#>
[CmdletBinding()]
param(
    [string]$ContainerdVersion = '2.3.1',
    [string]$NerdctlVersion    = '2.3.2',
    [string]$CniVersion        = '0.3.3',
    [string]$InstallDir        = 'C:\Program Files\containerd',
    [string]$ServiceName       = 'containerd'
)
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

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

    # 1. Windows features: Containers (+ Hyper-V, needed for client Hyper-V isolation).
    $reboot = $false
    foreach ($f in 'Containers', 'Microsoft-Hyper-V') {
        $state = (Get-WindowsOptionalFeature -Online -FeatureName $f).State
        if ($state -ne 'Enabled') {
            Write-Host "Enabling $f ..."
            if ((Enable-WindowsOptionalFeature -Online -FeatureName $f -All -NoRestart).RestartNeeded) { $reboot = $true }
        }
    }
    if ($reboot) {
        Write-Warning 'A reboot is required to finish enabling features. Reboot, then re-run.'
        Set-Status 'reboot-required'; return
    }

    $bin = Join-Path $InstallDir 'bin'
    $cniBin = Join-Path $InstallDir 'cni\bin'
    New-Item -ItemType Directory -Force -Path $bin, $cniBin | Out-Null
    $tmp = Join-Path $env:TEMP "mr-ctd"
    New-Item -ItemType Directory -Force -Path $tmp | Out-Null

    # 2. containerd (containerd.exe, ctr.exe, containerd-shim-runhcs-v1.exe)
    if (-not (Test-Path (Join-Path $bin 'containerd.exe'))) {
        $url = "https://github.com/containerd/containerd/releases/download/v$ContainerdVersion/containerd-$ContainerdVersion-windows-amd64.tar.gz"
        Write-Host "Downloading $url ..."
        Invoke-WebRequest $url -OutFile "$tmp\containerd.tar.gz" -UseBasicParsing
        tar.exe -xf "$tmp\containerd.tar.gz" -C $InstallDir   # extracts bin\*.exe
    }

    # 3. nerdctl
    if (-not (Test-Path (Join-Path $bin 'nerdctl.exe'))) {
        $url = "https://github.com/containerd/nerdctl/releases/download/v$NerdctlVersion/nerdctl-$NerdctlVersion-windows-amd64.tar.gz"
        Write-Host "Downloading $url ..."
        Invoke-WebRequest $url -OutFile "$tmp\nerdctl.tar.gz" -UseBasicParsing
        tar.exe -xf "$tmp\nerdctl.tar.gz" -C $bin
    }

    # 4. Windows CNI plugins (nat) — needed for container networking
    if (-not (Test-Path (Join-Path $cniBin 'nat.exe'))) {
        $url = "https://github.com/microsoft/windows-container-networking/releases/download/v$CniVersion/windows-container-networking-cni-amd64-v$CniVersion.zip"
        Write-Host "Downloading $url ..."
        Invoke-WebRequest $url -OutFile "$tmp\cni.zip" -UseBasicParsing
        Expand-Archive "$tmp\cni.zip" "$tmp\cni" -Force
        Get-ChildItem "$tmp\cni" -Recurse -Filter *.exe | ForEach-Object { Copy-Item $_.FullName $cniBin -Force }
    }

    # 5. config.toml (containerd defaults: root/state under ProgramData, pipe \\.\pipe\containerd-containerd)
    $cfg = Join-Path $InstallDir 'config.toml'
    & (Join-Path $bin 'containerd.exe') config default | Out-File -Encoding ascii $cfg
    Write-Host "Wrote $cfg"

    # 6. PATH so the shim + nerdctl resolve
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    if ($machinePath -notlike "*$bin*") {
        [Environment]::SetEnvironmentVariable('Path', "$machinePath;$bin", 'Machine')
    }

    # 7. Register + start the containerd service
    if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
        Write-Host "Service $ServiceName already exists; reconfiguring."
        Stop-Service $ServiceName -ErrorAction SilentlyContinue
        & (Join-Path $bin 'containerd.exe') --unregister-service --service-name $ServiceName 2>$null
        Start-Sleep -Seconds 2
    }
    Write-Host "Registering service $ServiceName ..."
    & (Join-Path $bin 'containerd.exe') --register-service --config $cfg --service-name $ServiceName
    Start-Service $ServiceName

    Write-Host ''
    Write-Host "containerd is running. nerdctl: $bin\nerdctl.exe  (pipe \\.\pipe\containerd-$ServiceName)"
    Set-Status 'ok'
}
catch {
    Set-Status ("error: " + $_.Exception.Message)
    Write-Error $_
}
finally {
    try { Stop-Transcript | Out-Null } catch {}
}
