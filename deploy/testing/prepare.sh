#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
secrets_dir="${script_dir}/secrets"
install -d -m 0700 "${secrets_dir}"

write_secret() {
  local path="$1"
  local value="$2"
  if [[ ! -s "${path}" ]]; then
    printf '%s\n' "${value}" >"${path}"
  fi
}

write_secret "${secrets_dir}/mongo-root-username" "swallow"
write_secret "${secrets_dir}/mongo-root-password" "$(openssl rand -hex 24)"
mongo_user="$(<"${secrets_dir}/mongo-root-username")"
mongo_password="$(<"${secrets_dir}/mongo-root-password")"
write_secret "${secrets_dir}/mongo-uri" "mongodb://${mongo_user}:${mongo_password}@mongo:27017/?authSource=admin"
write_secret "${secrets_dir}/jwt-secret" "$(openssl rand -hex 32)"
write_secret "${secrets_dir}/bootstrap-admin-password" "$(openssl rand -hex 18)"
write_secret "${secrets_dir}/credential-key" "$(openssl rand -base64 32)"
write_secret "${secrets_dir}/machine-token" "$(openssl rand -hex 32)"

if [[ ! -s "${secrets_dir}/tls.key" || ! -s "${secrets_dir}/tls.crt" ]]; then
  openssl req -x509 -newkey rsa:3072 -nodes -days 30 \
    -subj "/CN=swallow-testing.local" \
    -keyout "${secrets_dir}/tls.key" -out "${secrets_dir}/tls.crt"
fi
chmod 0444 "${secrets_dir}"/*
printf 'testing secrets prepared in %s\n' "${secrets_dir}"
