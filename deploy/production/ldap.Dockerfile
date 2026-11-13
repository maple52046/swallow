# The installation's Directory server (decision 082): OpenLDAP from Ubuntu 24.04's packages, with
# the schemas swallow's Directory adapter writes (POSIX accounts and groups, OpenSSH public keys,
# sudo rules). swallow is its only writer; everything in it is rebuilt from swallow's store, so the
# image keeps no state of its own besides the database volume. The entrypoint generates the server
# configuration on every start from the environment and the mounted secrets and certificates.
FROM ubuntu:24.04

ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install -y --no-install-recommends slapd ldap-utils ca-certificates \
    && rm -rf /var/lib/apt/lists/* /etc/ldap/slapd.d /var/lib/ldap \
    && mkdir -p /etc/ldap/slapd.d /var/lib/ldap /run/slapd \
    && chown -R openldap:openldap /etc/ldap/slapd.d /var/lib/ldap /run/slapd

# Paths are relative to the repository root, the build context of every release image.
COPY deploy/production/ldap/schema/ /usr/local/share/swallow-ldap/schema/
COPY --chmod=0755 deploy/production/ldap/entrypoint.sh /usr/local/bin/swallow-ldap-entrypoint

EXPOSE 636
VOLUME ["/var/lib/ldap"]
ENTRYPOINT ["/usr/local/bin/swallow-ldap-entrypoint"]
