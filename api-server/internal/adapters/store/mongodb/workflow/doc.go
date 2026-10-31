// Package workflow is the MongoDB store of durable Workflows: the Workflow, Job, and Task records,
// their events, resource leases, the Ansible execution queue and its task events, Site automation
// configurations, and Workflow secrets.
//
// Workflow secrets are written once with an insert-only upsert so a store transaction never
// continues after a rejected write. Scheduling, retries, and lease semantics belong to the core
// workflow context and the executor adapter, not here.
package workflow
