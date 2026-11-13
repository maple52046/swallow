#!/bin/bash
# swallow-ldap entrypoint (decision 082). It regenerates slapd's configuration (cn=config) on every
# start from the environment, the mounted password files, and the TLS files, so a changed secret or
# certificate takes effect on restart; the database under /var/lib/ldap keeps the entries, which
# swallow's Directory controller rewrites anyway. slapd listens on LDAPS only (plus the local ldapi
# socket this script uses), so a password never crosses the network in clear.
#
# Environment:
#   LDAP_BASE_DN                base DN, must start with dc= (default dc=swallow,dc=internal)
#   LDAP_ADMIN_PASSWORD_FILE    password of cn=admin,<base>, the writer swallow binds as
#   LDAP_READER_PASSWORD_FILE   password of cn=reader,ou=services,<base>, the read-only account the
#                               Servers' directory client binds as; it can never read userPassword
#   LDAP_TLS_CERT_FILE, LDAP_TLS_KEY_FILE, LDAP_TLS_CA_FILE
#                               the server certificate, its key, and the CA that signed it
set -euo pipefail

base_dn="${LDAP_BASE_DN:-dc=swallow,dc=internal}"
admin_password_file="${LDAP_ADMIN_PASSWORD_FILE:-/run/secrets/ldap-admin-password}"
reader_password_file="${LDAP_READER_PASSWORD_FILE:-/run/secrets/ldap-reader-password}"
tls_cert="${LDAP_TLS_CERT_FILE:-/run/secrets/ldap-tls/server.crt}"
tls_key="${LDAP_TLS_KEY_FILE:-/run/secrets/ldap-tls/server.key}"
tls_ca="${LDAP_TLS_CA_FILE:-/run/secrets/ldap-tls/ca.crt}"
schema_dir=/usr/local/share/swallow-ldap/schema

die() { echo "swallow-ldap: $*" >&2; exit 1; }

case "${base_dn}" in
  dc=*) ;;
  *) die "LDAP_BASE_DN must start with dc= (got ${base_dn})" ;;
esac
for file in "${admin_password_file}" "${reader_password_file}" "${tls_cert}" "${tls_key}" "${tls_ca}"; do
  [[ -s "${file}" ]] || die "missing or empty ${file}"
done

# hash_file hashes a password file's content without its trailing newline, which the secret files
# carry and swallow strips when it reads them; the password never appears on a command line.
hash_file() {
  local plain
  plain="$(mktemp)"
  printf '%s' "$(<"$1")" >"${plain}"
  slappasswd -T "${plain}" 2>/dev/null
  rm -f "${plain}"
}
admin_hash="$(hash_file "${admin_password_file}")"
reader_hash="$(hash_file "${reader_password_file}")"
first_rdn="${base_dn%%,*}"
dc_value="${first_rdn#dc=}"

# slapd runs as openldap and must read the key; the mounted secret may be root-only, so copy it.
install -d -o openldap -g openldap -m 0700 /run/slapd/tls
install -o openldap -g openldap -m 0600 "${tls_key}" /run/slapd/tls/server.key
install -o openldap -g openldap -m 0644 "${tls_cert}" /run/slapd/tls/server.crt
install -o openldap -g openldap -m 0644 "${tls_ca}" /run/slapd/tls/ca.crt

rm -rf /etc/ldap/slapd.d/*
config="$(mktemp)"
cat >"${config}" <<EOF
dn: cn=config
objectClass: olcGlobal
cn: config
olcArgsFile: /run/slapd/slapd.args
olcPidFile: /run/slapd/slapd.pid
olcLogLevel: none
olcDisallows: bind_anon
olcTLSCertificateFile: /run/slapd/tls/server.crt
olcTLSCertificateKeyFile: /run/slapd/tls/server.key
olcTLSCACertificateFile: /run/slapd/tls/ca.crt
olcTLSProtocolMin: 3.3

dn: cn=module{0},cn=config
objectClass: olcModuleList
cn: module{0}
olcModulePath: /usr/lib/ldap
olcModuleLoad: back_mdb

dn: cn=schema,cn=config
objectClass: olcSchemaConfig
cn: schema

include: file:///etc/ldap/schema/core.ldif
include: file:///etc/ldap/schema/cosine.ldif
include: file:///etc/ldap/schema/nis.ldif
include: file:///etc/ldap/schema/inetorgperson.ldif
include: file://${schema_dir}/openssh-lpk.ldif
include: file://${schema_dir}/sudo.ldif

dn: olcDatabase={-1}frontend,cn=config
objectClass: olcDatabaseConfig
objectClass: olcFrontendConfig
olcDatabase: {-1}frontend
olcAccess: {0}to * by dn.exact=gidNumber=0+uidNumber=0,cn=peercred,cn=external,cn=auth manage by * break
olcAccess: {1}to dn.base="" by * read
olcAccess: {2}to dn.base="cn=Subschema" by * read

dn: olcDatabase={0}config,cn=config
objectClass: olcDatabaseConfig
olcDatabase: {0}config
olcAccess: {0}to * by dn.exact=gidNumber=0+uidNumber=0,cn=peercred,cn=external,cn=auth manage by * none

dn: olcDatabase={1}mdb,cn=config
objectClass: olcDatabaseConfig
objectClass: olcMdbConfig
olcDatabase: {1}mdb
olcDbDirectory: /var/lib/ldap
olcDbMaxSize: 1073741824
olcSuffix: ${base_dn}
olcRootDN: cn=admin,${base_dn}
olcRootPW: ${admin_hash}
olcDbIndex: objectClass eq
olcDbIndex: uid,cn,memberUid eq
olcDbIndex: uidNumber,gidNumber eq
olcDbIndex: host eq
olcDbIndex: sudoUser,sudoHost eq,sub
olcLimits: dn.exact="cn=reader,ou=services,${base_dn}" size=unlimited time=unlimited
olcAccess: {0}to * by dn.exact=gidNumber=0+uidNumber=0,cn=peercred,cn=external,cn=auth manage by * break
olcAccess: {1}to attrs=userPassword by anonymous auth by * none
olcAccess: {2}to * by dn.exact="cn=reader,ou=services,${base_dn}" read by * none
EOF
slapadd -n0 -F /etc/ldap/slapd.d -l "${config}" >/dev/null
rm -f "${config}"
chown -R openldap:openldap /etc/ldap/slapd.d /var/lib/ldap

if [[ -z "$(ls -A /var/lib/ldap 2>/dev/null)" ]]; then
  entries="$(mktemp)"
  cat >"${entries}" <<EOF
dn: ${base_dn}
objectClass: top
objectClass: dcObject
objectClass: organization
o: swallow
dc: ${dc_value}

dn: ou=people,${base_dn}
objectClass: organizationalUnit
ou: people

dn: ou=groups,${base_dn}
objectClass: organizationalUnit
ou: groups

dn: ou=sudoers,${base_dn}
objectClass: organizationalUnit
ou: sudoers

dn: ou=services,${base_dn}
objectClass: organizationalUnit
ou: services

dn: cn=reader,ou=services,${base_dn}
objectClass: organizationalRole
objectClass: simpleSecurityObject
cn: reader
userPassword: ${reader_hash}
EOF
  slapadd -n1 -F /etc/ldap/slapd.d -l "${entries}" >/dev/null
  rm -f "${entries}"
  chown -R openldap:openldap /var/lib/ldap
fi

slapd -h "ldaps:/// ldapi:///" -u openldap -g openldap -F /etc/ldap/slapd.d -d 0 &
slapd_pid=$!
trap 'kill -TERM "${slapd_pid}" 2>/dev/null; wait "${slapd_pid}"' TERM INT

for _ in $(seq 1 50); do
  if ldapwhoami -Q -Y EXTERNAL -H ldapi:/// >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done
# The reader's password follows its file on every start, so rotating the secret needs a restart only.
ldapmodify -Q -Y EXTERNAL -H ldapi:/// >/dev/null <<EOF
dn: cn=reader,ou=services,${base_dn}
changetype: modify
replace: userPassword
userPassword: ${reader_hash}
EOF

wait "${slapd_pid}"
