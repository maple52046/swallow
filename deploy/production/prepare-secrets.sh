#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
secrets_dir="${script_dir}/secrets"
install -d -m 0700 "${secrets_dir}"

write_secret() {
  local name="$1" value="$2"
  [[ -s "${secrets_dir}/${name}" ]] || printf '%s\n' "${value}" >"${secrets_dir}/${name}"
}

write_secret mongo-root-username "swallow"
write_secret mongo-root-password "$(openssl rand -hex 24)"
mongo_user="$(<"${secrets_dir}/mongo-root-username")"
mongo_password="$(<"${secrets_dir}/mongo-root-password")"
write_secret mongo-uri "mongodb://${mongo_user}:${mongo_password}@mongo:27017/?authSource=admin"
write_secret jwt-secret "$(openssl rand -hex 32)"
write_secret bootstrap-admin-password "$(openssl rand -hex 18)"
write_secret credential-key "$(openssl rand -base64 32)"
write_secret machine-token "$(openssl rand -hex 32)"
chmod 0444 "${secrets_dir}"/*
printf 'secrets prepared; install CA files supplied for this installation as secrets/tls.crt and secrets/tls.key\n'
