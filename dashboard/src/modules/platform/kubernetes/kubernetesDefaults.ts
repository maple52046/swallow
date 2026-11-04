/**
 * The Pod and Service ranges the API gives a Kubernetes Platform whose spec omits them
 * (platforms.md "Create A Platform"). The deploy form proposes them, and the Platform details card
 * shows them as defaults for a spec written without them (for example through the CLI).
 */
export const DEFAULT_POD_CIDR = '10.244.0.0/16'

/** The Service range counterpart of `DEFAULT_POD_CIDR`. */
export const DEFAULT_SERVICE_CIDR = '10.96.0.0/12'
