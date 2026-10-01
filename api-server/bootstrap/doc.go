// Package bootstrap holds the idempotent first-run steps the API process performs on every start,
// currently creating the bootstrap admin user. Each step must be safe to repeat and safe when
// several API processes start together, because there is no separate installer that runs it
// exactly once. Installation-time state such as the Deployment Key does not belong here: it is
// created by `swallow-api deployment-key ensure` so API startup never depends on it (decision 039).
// Business rules stay in the owning feature; this package only sequences them and logs outcomes,
// never secret material.
package bootstrap
