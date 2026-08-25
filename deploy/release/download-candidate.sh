#!/usr/bin/env bash
set -euo pipefail

commit="${1:?commit SHA required}"
mkdir -p out/release
run_id="$(gh run list --workflow Candidate --commit "${commit}" --status success \
  --json databaseId --jq '.[0].databaseId')"
[[ -n "${run_id}" ]] || { printf 'no successful candidate workflow for %s\n' "${commit}" >&2; exit 1; }
gh run download "${run_id}" --name "candidate-${commit}" --dir out/release
