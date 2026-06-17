#!/usr/bin/env bash
# Ephemeral runner entrypoint: start the runner with an injected JIT config.
# The runner takes exactly one job (ephemeral) then exits; multirunner detects
# the exit and provisions a replacement.
set -euo pipefail

if [ -z "${JIT_CONFIG:-}" ]; then
  echo "ERROR: JIT_CONFIG env var is required" >&2
  exit 1
fi

runner_pid=""
cleanup() {
  if [ -n "${runner_pid}" ]; then
    # RUNNER_MANUALLY_TRAP_SIG=1 lets the runner handle the signal itself.
    kill -TERM "${runner_pid}" 2>/dev/null || true
    wait "${runner_pid}" 2>/dev/null || true
  fi
}
trap cleanup TERM INT

./run.sh --jitconfig "${JIT_CONFIG}" &
runner_pid=$!
wait "${runner_pid}"
