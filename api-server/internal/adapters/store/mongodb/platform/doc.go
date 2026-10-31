// Package platform is the MongoDB store of the Platform context: Platforms (`platforms`) and their
// deployment requirements (`platform_deployment_requirements`).
//
// It maps documents to and from the platform domain. Deployment policy, membership, and credential
// handling belong to the core platform context; platform-specific API clients belong to their
// modules. Neither may be placed here.
package platform
