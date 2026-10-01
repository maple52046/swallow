#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
secrets_dir="${script_dir}/secrets"
install -d -m 0700 "${secrets_dir}"

# Existing secrets are never replaced: rotating one (for example the credential key) would
# orphan data sealed with it.
write_secret() {
  local name="$1" value="$2"
  [[ -s "${secrets_dir}/${name}" ]] || printf '%s\n' "${value}" >"${secrets_dir}/${name}"
}

write_secret mongo-root-username "swallow"
write_secret mongo-root-password "$(openssl rand -hex 24)"
mongo_user="$(<"${secrets_dir}/mongo-root-username")"
mongo_password="$(<"${secrets_dir}/mongo-root-password")"
write_secret mongo-uri "mongodb://${mongo_user}:${mongo_password}@mongo:27017/?authSource=admin"
write_secret temporal-db-password "$(openssl rand -hex 24)"
write_secret jwt-secret "$(openssl rand -hex 32)"
write_secret bootstrap-admin-password "$(openssl rand -hex 18)"
write_secret credential-key "$(openssl rand -base64 32)"
write_secret machine-token "$(openssl rand -hex 32)"
# The co-located MAAS: its PostgreSQL role password and its own admin login. Hex values
# need no quoting in SQL or URIs. local-maas.sh adds maas-api-key after MAAS is initialized.
write_secret maas-db-password "$(openssl rand -hex 24)"
write_secret maas-admin-password "$(openssl rand -hex 18)"
chmod 0444 "${secrets_dir}"/*
printf 'secrets prepared in %s\n' "${secrets_dir}"
