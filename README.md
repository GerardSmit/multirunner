# multirunner

**Run many GitHub Actions self-hosted runners in parallel on one machine — Linux, Windows, or both at once.**

A GitHub Actions self-hosted runner executes **one job at a time**. To run jobs in
parallel you normally stand up several runners by hand and babysit them.
multirunner does it for you: it keeps a pool of **fresh, throwaway runners**
ready, hands each one a single job, then tears it down and replaces it with a
clean one. It also bundles a **local Actions cache** and a **local git mirror**
so your jobs stop re-downloading the same gigabytes from GitHub on every run.

One small binary. One config file. No Kubernetes, no control plane.

---

## Why

- **Real parallelism** — N isolated runners on one host, each on its own fresh
  registration. Queue 10 jobs, run 10 at once.
- **Clean every time** — each job gets a pristine, ephemeral environment (a
  container or a VM). No state leaks between jobs.
- **Fast** — a self-hosted Actions cache keeps `actions/cache` on your host, and
  a git mirror makes `actions/checkout` fetch only the new commits instead of
  full-cloning every time.
- **Cheap to run** — no public endpoint required; runners dial out to GitHub, so
  it works behind NAT out of the box.

## What it can do

| Capability | Details |
|---|---|
| **Linux runners** | Docker **or** Podman (docker-compatible API). |
| **Windows runners (containers)** | Native Windows containers via **containerd + runhcs** — no Docker Desktop. |
| **Windows runners on a Linux host** | Run Windows as **QEMU VMs** from a baked golden image. |
| **Mix Linux + Windows** | Multiple pools, different OSes, one orchestrator. |
| **Self-hosted Actions cache** | Built-in v2 cache server — `actions/cache` stays on your host. |
| **Git mirror cache** | Local bare mirror; `actions/checkout` fetches only the delta. |
| **Autoscaling** | Keep N warm, or scale on demand (polling or webhook). |
| **Runs as a service** | Windows SCM, Linux systemd, macOS launchd. |
| **Metrics** | Prometheus endpoint + health check. |
| **Housekeeping** | Cache + mirror garbage collection, automatic. |

Every backend above (Linux containers, Windows containers, Windows VMs) is
validated end-to-end against a real GitHub repo — including real toolchains
(`actions/checkout`, `actions/setup-dotnet`, `dotnet build`) and `actions/cache`
save/restore against the local server.

---

## How it works

For each runner slot, multirunner:

1. Calls GitHub's `generate-jitconfig` (repo / org / enterprise scope) for a
   single-use registration.
2. Launches a clean runner — a container or a VM — that runs the **stock GitHub
   runner** with that JIT config.
3. The runner takes exactly one job, then deregisters itself (ephemeral).
4. multirunner notices it exited and immediately starts a fresh one.

It runs the official `actions/runner` binary unchanged — nothing is reimplemented,
so jobs behave exactly as on GitHub-hosted runners.

---

## Install

Download a binary for your OS/arch from the
[Releases](../../releases) page, or build from source:

```sh
go install github.com/GerardSmit/multirunner/cmd/multirunner@latest
```

Prebuilt binaries are published for **Linux, Windows, and macOS**, each in
**x64 and ARM64**.

---

## Quick start (Linux)

1. **Get a runner image.** Pull the prebuilt one:

   ```sh
   docker pull gerardsmit/multirunner-runner-linux:latest
   ```

   …or build it yourself (build context = repo root):

   ```sh
   docker build -f images/linux/Dockerfile -t multirunner/runner-linux:dev .
   ```

   Published images (built + pushed by CI on each release):
   `gerardsmit/multirunner-runner-linux`, `gerardsmit/multirunner-runner-windows`,
   `gerardsmit/multirunner-cacheserver` — Linux images are multi-arch (amd64 + arm64).

2. **Connect to GitHub.** `connect` creates and installs a GitHub App for your
   org/repo via a browser flow and writes the credentials to your config — no
   hand-made tokens:

   ```sh
   multirunner connect --org my-org --config config.local.yaml
   # or repo-scoped:
   multirunner connect --repo owner/name --config config.local.yaml
   ```

   Prefer a token? Put `pat: "${GITHUB_PAT}"` in the config (resolved from the
   environment, stays off disk) and `export GITHUB_PAT=...`.

3. **Run it:**

   ```sh
   multirunner run --config config.local.yaml
   ```

Your runners appear under **Settings → Actions → Runners**. Push a workflow with
`runs-on: [self-hosted, linux, x64]` and watch them pick up jobs. Start with
`config.example.yaml` — it documents every option.

---

## Backends

`pools[].backend` and `pools[].docker.host` select how a pool runs.

### Linux containers (Docker / Podman)

```yaml
pools:
  - name: linux-pool
    os: linux
    size: 3
    image: "multirunner/runner-linux:dev"
    labels: [self-hosted, linux, x64]
    docker:
      host: "tcp://127.0.0.1:2375"            # Docker (WSL2)
      # host: "npipe:////./pipe/podman-machine-default"   # Podman on Windows
```

On Podman, prefix local image tags with `localhost/` (e.g.
`localhost/multirunner/runner-linux:dev`) so the daemon resolves them.

### Windows containers (containerd, no Docker Desktop)

multirunner drives **containerd + runhcs** directly through `nerdctl`, so Windows
containers run on the OS Host Compute Service with no Docker Desktop:

```powershell
# Elevates once: installs containerd + runhcs + nerdctl + CNI, enables the
# Containers (and, on Windows client, Hyper-V) features.
multirunner install-containerd
```

```yaml
pools:
  - name: windows-pool
    os: windows
    backend: containerd
    size: 2
    image: "multirunner/runner-windows:dev"
    labels: [self-hosted, windows, x64]
    containerd:
      isolation: auto      # auto: process on Server, hyperv on Windows client
```

`multirunner doctor` reports daemon reachability and catches mismatches (e.g. a
Linux daemon assigned to a Windows pool).

### Windows runners on a Linux host (QEMU VM)

No Windows host? Run Windows runners as **VMs** on your Linux/KVM box. Bake a
golden **Server Core** image once; each job boots a clean copy-on-write overlay,
reads its JIT config from an attached ISO, runs one job, and powers off.

```sh
multirunner bake --iso WinServer2022Eval.iso --golden /var/lib/multirunner/golden.qcow2
```

```yaml
pools:
  - name: win-vm
    os: windows
    backend: qemu
    size: 1
    qemu:
      golden: /var/lib/multirunner/golden.qcow2
      work_dir: /var/lib/multirunner/run
      mem_mb: 4096
      cpus: 2
      accel: kvm           # kvm (Linux) | whpx (Windows) | hvf (macOS) | "" auto
```

Highlights:

- **Bake serves a live noVNC viewer** — watch the unattended install in your
  browser (`--vnc-web ""` to disable). The golden ships with **git** and the
  runner preinstalled, so jobs are ready to build immediately.
- **Verified completion** — the bake only ships a golden after it sees the
  `MR:GOLDEN_OK` serial marker; otherwise it fails loudly.
- **Licensing** — a Windows guest needs its own license. Server eval = 180 days,
  `slmgr /rearm`-able ~5–6× (~3 years), or supply a key/KMS.
- **Self-healing** — multirunner tracks the golden's eval clock and rearms +
  re-snapshots (or rebuilds) before it expires. Skipped when a key is configured.

---

## Self-hosted Actions cache

Keep `actions/cache` traffic on your host instead of round-tripping to GitHub's
Azure backend:

```yaml
cache:
  enabled: true
  mode: local-server
  advertise_url: "http://host.docker.internal:3000"   # reachable from runners
  max_age_days: 7        # evict entries unused this long
  max_size_gb: 0         # 0 = unlimited; otherwise LRU-evict to fit
```

multirunner runs an embedded Go server implementing the v2 twirp `CacheService`
plus the Azure block-upload data plane, stores blobs locally, and injects
`ACTIONS_RESULTS_URL` / `ACTIONS_CACHE_URL` / `ACTIONS_CACHE_SERVICE_V2=true` into
every runner. The runner image includes a small patch so the redirect reaches
`uses:` actions (not just `run:` steps). Stale entries are garbage-collected
automatically.

> **Podman on Windows:** runners reach the host bridge as
> `host.containers.internal` (the Podman VM, `10.88.0.1`), not the Windows host.
> Run the cache as a published container and set
> `cache.external_url: http://host.containers.internal:3000`.

---

## Git mirror cache

Avoid full-cloning big repos on every job. multirunner keeps a host-side bare
mirror per repo and updates it in the background:

```yaml
git_cache:
  mode: mirror           # mirror | dotgit-cache | off
  path: /var/lib/multirunner/gitmirror
  max_age_days: 30       # remove mirrors unused this long
```

- **`mirror`** — mounts the bare mirror read-only into the runner; `checkout`
  uses it as a reference and fetches only the tip delta.
- **`dotgit-cache`** — serves the mirror as a git bundle over the cache server; a
  job-started hook seeds the workspace from it. Works where bind-mounts can't
  (the QEMU VM), so VM jobs get the same fast checkout. The per-job token still
  fetches the delta from GitHub, so private-repo auth is unaffected.

---

## Autoscaling

```yaml
provisioning: pool       # pool | autoscale
```

- **`pool`** (default) — keep N runners warm per pool. Zero inbound; works behind
  NAT with no extra setup.
- **`autoscale`** — launch runners on demand up to each pool's `size`:
  - **Polling** (outbound, NAT-safe) — multirunner polls GitHub for queued work
    and scales up.
  - **Webhook** (low-latency) — set `webhook.listen` to receive `workflow_job`
    events (needs a reachable URL; use a tunnel like smee.io / cloudflared).
    Signatures verified with `webhook.secret`.

---

## Run as a service

multirunner installs itself as a native OS service (Windows SCM, Linux systemd,
macOS launchd):

```sh
sudo multirunner service install --config /etc/multirunner/config.yaml   # Linux/macOS
sudo multirunner service start
```

```powershell
multirunner service install --config C:\multirunner\config.yaml          # Windows (elevated)
multirunner service start
```

`service uninstall` removes it.

---

## Metrics & health

Set `metrics.listen` (e.g. `127.0.0.1:9090`) to expose:

- `GET /metrics` — Prometheus: `multirunner_runners_active{pool}`,
  `multirunner_jobs_total{pool,result}`, `multirunner_reprovision_errors_total{pool}`.
- `GET /health` — liveness.

---

## CLI

Built with cobra — `multirunner <command> --help` for details; `--config` is global.

```
multirunner [run]                   run the orchestrator (default)
multirunner connect --org <org>     create + install a GitHub App, write auth to config
multirunner doctor                  check daemons + container mode, no runners
multirunner bake                    build a golden Windows VM image (qemu backend)
multirunner install-containerd      install the Windows-container stack (elevates)
multirunner service ...             install | uninstall | start | stop | restart
multirunner completion <shell>      shell completion script
```

---

## Build from source

Pure Go (CGO-free), so it cross-compiles to every target:

```sh
go build ./cmd/multirunner
# cross-compile, e.g.:
GOOS=linux   GOARCH=arm64 go build -o multirunner       ./cmd/multirunner
GOOS=windows GOARCH=amd64 go build -o multirunner.exe    ./cmd/multirunner
GOOS=darwin  GOARCH=arm64 go build -o multirunner        ./cmd/multirunner
```

## Layout

```
cmd/multirunner    orchestrator + CLI
cmd/cacheserver    standalone cache server
internal/config    config schema + loader
internal/github    JIT client (repo/org/enterprise, PAT/App)
internal/backend   container backends (Docker/Podman, containerd Windows)
internal/winvm     QEMU Windows-VM backend + golden bake
internal/pool      per-OS ephemeral pool + reprovision loop
internal/cache     self-hosted v2 cache server
internal/gitcache  host git mirror manager
images/            runner + cacheserver Dockerfiles
```

## Tests

```sh
go test ./...
```
