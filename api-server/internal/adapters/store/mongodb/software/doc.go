// Package software is the MongoDB store of the Managed Software context: Software Assignments
// (`software_assignments`), keyed by (serverId, kind) and the single source of truth for what
// software swallow installed where, and Registry Credentials (`software_registry_credentials`),
// whose passwords are sealed. Both are swallow-owned data, not provider mirrors, so nothing here
// goes stale.
//
// Software kind behavior, including the Docker Engine client, belongs to the software modules.
package software
