# multirunner dotgit-cache: ACTIONS_RUNNER_HOOK_JOB_STARTED hook (PowerShell — the
# runner only runs .ps1/.sh job hooks). If a git bundle URL is injected, seed the
# workspace from the host mirror's bundle (bulk objects, no GitHub bandwidth) and
# point origin at GitHub, so actions/checkout reuses the .git and fetches only the
# delta. Best-effort: always exits 0 so checkout falls back to a normal clone.
$ErrorActionPreference = 'SilentlyContinue'
try {
    if ($env:MR_GIT_BUNDLE_URL -and $env:GITHUB_REPOSITORY -and $env:RUNNER_WORKSPACE) {
        $name = ($env:GITHUB_REPOSITORY -split '/')[-1]
        $dest = Join-Path $env:RUNNER_WORKSPACE $name
        if (-not (Test-Path (Join-Path $dest '.git'))) {
            $bundle = Join-Path $env:TEMP "mr-$name.bundle"
            & curl.exe -s -f -m 180 -o $bundle $env:MR_GIT_BUNDLE_URL
            if (Test-Path $bundle) {
                & git clone $bundle $dest
                & git -C $dest remote set-url origin "$($env:GITHUB_SERVER_URL)/$($env:GITHUB_REPOSITORY)"
                Remove-Item $bundle -Force
                Write-Host "multirunner dotgit-cache: seeded $dest from mirror bundle"
            }
        }
    }
} catch {}
exit 0
