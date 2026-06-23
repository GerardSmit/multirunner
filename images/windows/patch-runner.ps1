# Renames the ACTIONS_RESULTS_URL / ACTIONS_CACHE_URL UTF-16LE string literals in
# Runner.Worker.dll (last char L->X) so the runner stops overriding those env
# vars for `uses:` actions, letting multirunner's injected cache redirect reach
# actions/cache. Same patch as the Linux image.
param([string]$Dll = 'C:\actions-runner\bin\Runner.Worker.dll')
$ErrorActionPreference = 'Stop'

$bytes = [IO.File]::ReadAllBytes($Dll)
function Set-LastCharX([byte[]]$b, [string]$s) {
    $p = [Text.Encoding]::Unicode.GetBytes($s)
    $max = $b.Length - $p.Length
    for ($i = 0; $i -le $max; $i++) {
        $match = $true
        for ($j = 0; $j -lt $p.Length; $j++) { if ($b[$i + $j] -ne $p[$j]) { $match = $false; break } }
        if ($match) { $b[$i + $p.Length - 2] = [byte][char]'X'; return $true }
    }
    return $false
}
$r1 = Set-LastCharX $bytes 'ACTIONS_RESULTS_URL'
$r2 = Set-LastCharX $bytes 'ACTIONS_CACHE_URL'
[IO.File]::WriteAllBytes($Dll, $bytes)
if (-not $r1) { Write-Error 'ACTIONS_RESULTS_URL literal not found; patch failed'; exit 1 }
if (-not $r2) { Write-Error 'ACTIONS_CACHE_URL literal not found; patch failed'; exit 1 }
Write-Host "patched RESULTS_URL=$r1 CACHE_URL=$r2"
