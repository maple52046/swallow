#!/usr/bin/env bash
set -euo pipefail

# node_modules lives in the bind-mounted source tree so the host IDE can resolve
# types too. Reinstall only when the lockfile is newer than the last install.
if [ ! -d node_modules ] || [ package-lock.json -nt node_modules/.package-lock.json ]; then
  echo "[entrypoint] installing dependencies (npm ci)"
  npm ci
else
  echo "[entrypoint] dependencies up to date, skipping npm ci"
fi

exec npm run dev -- --host 0.0.0.0 --port "${DASHBOARD_CONTAINER_PORT:-5173}"
