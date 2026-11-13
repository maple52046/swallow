#!/usr/bin/env bash
# Prepares the dev stack's Directory (decision 082): the passwords of the writer (cn=admin) and the
# read-only account the Servers' directory client binds as, and the CA and server certificate the
# `ldap` service (compose profile `directory`) serves LDAPS with. Everything goes to ./ldap, which
# git ignores; existing passwords and the CA are kept, the certificate is reissued when the address
# changes.
#
# Usage: ./ldap-secrets.sh [address]
#   address  where lab Servers reach this host (default: SWALLOW_API_NTP_SERVER from .env, which is
#            the same address)
set -euo pipefail
umask 077

dev_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
out="${dev_dir}/ldap"
address="${1:-}"
if [[ -z "${address}" && -f "${dev_dir}/.env" ]]; then
  address="$(sed -n 's/^SWALLOW_API_NTP_SERVER=//p' "${dev_dir}/.env" | tail -n 1)"
fi
[[ -n "${address}" ]] || { echo "pass the address lab Servers reach this host at" >&2; exit 1; }

install -d -m 0700 "${out}"
for name in admin-password reader-password; do
  [[ -s "${out}/${name}" ]] || openssl rand -hex 24 >"${out}/${name}"
done
"${dev_dir}/../production/ldap/tls.sh" "${out}" "${address}"
printf 'Directory secrets ready in %s; Servers connect to ldaps://%s:636\n' "${out}" "${address}"
