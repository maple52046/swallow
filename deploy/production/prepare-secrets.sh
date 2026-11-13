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
# The replica set's internal authentication key (decision 069). An installation from before the
# replica set gets one on its next upgrade; mongo-entrypoint.sh gives mongod a 0400 copy.
write_secret mongo-keyfile "$(openssl rand -base64 756)"
write_secret temporal-db-password "$(openssl rand -hex 24)"
write_secret jwt-secret "$(openssl rand -hex 32)"
write_secret bootstrap-admin-password "$(openssl rand -hex 18)"
write_secret credential-key "$(openssl rand -base64 32)"
write_secret machine-token "$(openssl rand -hex 32)"
# The co-located MAAS: its PostgreSQL role password and its own admin login. Hex values
# need no quoting in SQL or URIs. local-maas.sh adds maas-api-key after MAAS is initialized.
write_secret maas-db-password "$(openssl rand -hex 24)"
write_secret maas-admin-password "$(openssl rand -hex 18)"
# The Site's Local Repository (Pulp): its PostgreSQL role password, its admin password (the
# repository Integration's credential), and its Django secret key.
write_secret pulp-db-password "$(openssl rand -hex 24)"
write_secret pulp-admin-password "$(openssl rand -hex 18)"
write_secret pulp-secret-key "$(openssl rand -hex 32)"
# The Directory (decision 082): the password swallow's worker writes it with, and the password of
# the read-only account every Server's directory client binds as.
write_secret ldap-admin-password "$(openssl rand -hex 24)"
write_secret ldap-reader-password "$(openssl rand -hex 24)"
# The installation CA and the Directory's server certificate, which names `ldap` (the worker's
# address for it) and SWALLOW_ADDRESS (the Servers'). The CA key stays in ldap-tls/ and is never
# mounted into a container; a changed address reissues the certificate under the same CA.
"${script_dir}/ldap/tls.sh" "${secrets_dir}/ldap-tls" "${SWALLOW_ADDRESS:-}" >/dev/null
find "${secrets_dir}" -maxdepth 1 -type f -exec chmod 0444 {} +
chmod 0444 "${secrets_dir}"/ldap-tls/{ca.crt,server.crt,server.key}
chmod 0400 "${secrets_dir}"/ldap-tls/ca.key
printf 'secrets prepared in %s\n' "${secrets_dir}"
