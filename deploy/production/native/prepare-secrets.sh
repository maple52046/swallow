#!/usr/bin/env bash
set -euo pipefail
umask 077

[[ "${EUID}" -eq 0 ]] || { printf 'run as root\n' >&2; exit 1; }
config_root="/etc/swallow"
secrets_dir="${config_root}/secrets"
install -d -m 0700 "${secrets_dir}" "${config_root}/tls"

write_secret() {
  local name="$1" value="$2"
  [[ -s "${secrets_dir}/${name}" ]] ||
    printf '%s\n' "${value}" >"${secrets_dir}/${name}"
}

write_secret mongo-root-username "swallow"
write_secret mongo-root-password "$(openssl rand -hex 24)"
mongo_user="$(<"${secrets_dir}/mongo-root-username")"
mongo_password="$(<"${secrets_dir}/mongo-root-password")"
write_secret mongo-uri "mongodb://${mongo_user}:${mongo_password}@127.0.0.1:27017/swallow?authSource=admin"
write_secret jwt-secret "$(openssl rand -hex 32)"
write_secret bootstrap-admin-password "$(openssl rand -hex 18)"
write_secret credential-key "$(openssl rand -base64 32)"
write_secret machine-token "$(openssl rand -hex 32)"
chmod 0600 "${secrets_dir}"/*
printf 'native secrets prepared; install CA-issued tls.crt and tls.key under %s/tls\n' "${config_root}"
