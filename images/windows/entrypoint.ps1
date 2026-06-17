# Ephemeral runner entrypoint (Windows): start the runner with an injected JIT
# config. One job, then exit; multirunner provisions a replacement.
$ErrorActionPreference = 'Stop'

if (-not $env:JIT_CONFIG) {
    Write-Error 'JIT_CONFIG env var is required'
    exit 1
}

Set-Location 'C:\actions-runner'
& .\run.cmd --jitconfig $env:JIT_CONFIG
exit $LASTEXITCODE
