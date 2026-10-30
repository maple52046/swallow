#!/usr/bin/env bash
# MongoDB entrypoint and healthcheck for the swallow production stack.
#
# swallow requires a store with multi-document transactions and change streams (decision 069),
# so MongoDB runs as the single-node replica set rs0. Authentication is on, and a replica set with
# authentication needs a keyFile for its internal membership authentication.
#
#   mongo-entrypoint.sh [mongod args...]   start mongod (the default)
#   mongo-entrypoint.sh healthcheck        initiate the replica set once, then report healthy only
#                                          while this member is the writable primary
#
# The official image's entrypoint creates the root user on first start; it drops --replSet and
# --keyFile for that temporary initdb mongod and passes them to the real one.
set -euo pipefail

keyfile_secret=/run/secrets/mongo_keyfile
# The root filesystem is read-only; /tmp is the container's tmpfs, so the copy never persists.
keyfile=/tmp/mongo-keyfile
replica_set=rs0
# The member name every client resolves: the compose service name on the control network.
member_host=mongo:27017

start() {
  # mongod rejects a keyFile that is readable by group or others, or owned by another user. The
  # secret is bind-mounted root-owned and 0444 (every swallow secret is), so mongod gets a private
  # copy owned by the mongodb user it drops to. The mode is set while root still owns the copy:
  # the container drops CAP_FOWNER, so root cannot chmod a file it no longer owns.
  cp "${keyfile_secret}" "${keyfile}"
  chmod 0400 "${keyfile}"
  chown mongodb:mongodb "${keyfile}"
  exec docker-entrypoint.sh mongod --replSet "${replica_set}" --keyFile "${keyfile}" --bind_ip_all "$@"
}

healthcheck() {
  # rs.status() throws NotYetInitialized only on a replica-set mongod that was never initiated;
  # during initdb the temporary mongod runs without --replSet, which throws something else and
  # simply reports unhealthy until the real mongod is up. Initiating an existing standalone data
  # directory keeps its data, which is how an upgraded installation converts.
  mongosh --quiet \
    --username "$(<"/run/secrets/mongo_root_username")" \
    --password "$(<"/run/secrets/mongo_root_password")" \
    --authenticationDatabase admin \
    --eval "try { rs.status() } catch (e) {
              if (e.codeName !== 'NotYetInitialized') throw e;
              rs.initiate({_id: '${replica_set}', members: [{_id: 0, host: '${member_host}'}]})
            }
            quit(db.hello().isWritablePrimary ? 0 : 1)" >/dev/null
}

case "${1:-}" in
  healthcheck) healthcheck ;;
  *) start "$@" ;;
esac
