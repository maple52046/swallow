#!/usr/bin/env bash
# Issues the installation CA and the Directory server's certificate (decision 082). The CA signs
# the certificate of the bundled LDAP server; swallow's worker verifies the server with it, and the
# directory client installs it on every Server so their SSSD verifies the server too.
#
# Usage: tls.sh <dir> <address>...
#   <dir>      where ca.crt, ca.key, server.crt, and server.key live (created 0700 when missing)
#   <address>  the names and IP addresses the server answers to besides `ldap` and `localhost`:
#              the installation address Servers connect to
#
# It is idempotent. The CA is created once and never replaced, because every Server trusts it; a
# server certificate is reissued (with the same CA) when it is missing, lacks one of the names, or
# expires within 30 days. It prints only file names, never key material.
set -euo pipefail
umask 077

dir="${1:?usage: tls.sh <dir> <address>...}"
shift
install -d -m 0700 "${dir}"
ca_key="${dir}/ca.key" ca_crt="${dir}/ca.crt" key="${dir}/server.key" crt="${dir}/server.crt"

if [[ ! -s "${ca_key}" || ! -s "${ca_crt}" ]]; then
  openssl ecparam -name prime256v1 -genkey -noout -out "${ca_key}"
  openssl req -x509 -new -key "${ca_key}" -sha256 -days 3650 -subj "/CN=swallow installation CA" \
    -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign,cRLSign" \
    -out "${ca_crt}"
  printf 'created %s\n' "${ca_crt}"
fi

# san lists the subject alternative names the certificate must carry, IP addresses as IP:.
san="DNS:ldap,DNS:localhost,IP:127.0.0.1"
for address in "$@"; do
  [[ -n "${address}" ]] || continue
  if [[ "${address}" =~ ^[0-9.]+$ || "${address}" == *:* ]]; then
    san+=",IP:${address}"
  else
    san+=",DNS:${address}"
  fi
done

current() {
  [[ -s "${key}" && -s "${crt}" ]] || return 1
  openssl x509 -in "${crt}" -noout -checkend $((30 * 24 * 3600)) >/dev/null || return 1
  openssl verify -CAfile "${ca_crt}" "${crt}" >/dev/null 2>&1 || return 1
  local have entry
  have="$(openssl x509 -in "${crt}" -noout -ext subjectAltName 2>/dev/null | tail -n +2 | tr -d ' ')"
  have="${have//IPAddress:/IP:}"
  IFS=, read -ra wanted <<<"${san}"
  for entry in "${wanted[@]}"; do
    [[ ",${have}," == *",${entry},"* ]] || return 1
  done
}

if ! current; then
  csr="$(mktemp)"
  rm -f "${key}" "${crt}"
  openssl ecparam -name prime256v1 -genkey -noout -out "${key}"
  openssl req -new -key "${key}" -subj "/CN=ldap" -out "${csr}"
  openssl x509 -req -in "${csr}" -CA "${ca_crt}" -CAkey "${ca_key}" -CAcreateserial -sha256 -days 825 \
    -extfile <(printf 'subjectAltName=%s\nextendedKeyUsage=serverAuth\nkeyUsage=critical,digitalSignature\n' "${san}") \
    -out "${crt}" 2>/dev/null
  rm -f "${csr}" "${dir}/ca.srl"
  printf 'issued %s for %s\n' "${crt}" "${san}"
fi
