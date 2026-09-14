/**
 * Per-provider explanation of what an Integration credential is, what it maps to, and what it
 * affects. UI copy only — it exists so the dashboard can tell an operator, in plain terms, why a
 * credential is needed and what breaks without it. Kept out of the domain layer because it is
 * presentation microcopy, keyed by the integration's `providerKind`.
 */
export interface CredentialGuidance {
  /** Short, provider-specific name of the secret, e.g. "MAAS API key". */
  term: string
  /** One line: what Swallow does with it. */
  purpose: string
  /** What the value actually is and where an operator gets it. */
  whatItIs: string
  /** Which features it drives, and what fails when it is missing or wrong. */
  impact: string
  /** True when the provider can be used without a credential (e.g. anonymous Prometheus). */
  optional: boolean
}

/**
 * Returns credential guidance for a provider kind (`maas`, `prometheus`, `kubernetes`, `slurm`),
 * falling back to a generic description for an unknown kind so the UI never shows nothing.
 */
export function credentialGuidance(providerKind: string): CredentialGuidance {
  switch (providerKind) {
    case 'maas':
      return {
        term: 'MAAS API key',
        purpose: "Swallow uses it to call this site's MAAS API on your behalf.",
        whatItIs:
          'A MAAS API key in the form consumer:token:secret, generated in the MAAS web UI under your user menu → API keys.',
        impact:
          'Drives every provisioner feature for this site: discovering machines as Servers, deploying and releasing operating systems, power and hardware actions, network configuration, and zone/pool management and server placement. If it is missing or wrong, all of these stop working for this site.',
        optional: false,
      }
    case 'prometheus':
      return {
        term: 'Prometheus access token',
        purpose: "Swallow uses it to query this site's metrics and, if configured, Alertmanager.",
        whatItIs: 'A bearer token. Only needed when your Prometheus or Alertmanager requires authentication.',
        impact:
          'Affects metric readings and alerts for this site. Many central stores allow anonymous access, so this can be left empty.',
        optional: true,
      }
    case 'kubernetes':
      return {
        term: 'Kubernetes access token',
        purpose: "Swallow uses it to read this Kubernetes cluster's membership and live state.",
        whatItIs: 'A token for the Kubernetes API, such as a service-account token.',
        impact:
          'Affects platform membership sync and live status for this platform. Without it, Swallow cannot read the cluster.',
        optional: false,
      }
    case 'slurm':
      return {
        term: 'Slurm REST token',
        purpose: 'Swallow uses it to read this Slurm cluster through slurmrestd.',
        whatItIs: 'A slurmrestd JWT token.',
        impact:
          'Affects Slurm cluster status reads for this platform. Without it, Swallow cannot read the cluster.',
        optional: false,
      }
    default:
      return {
        term: 'Access credential',
        purpose: 'Swallow uses it to authenticate to this external system.',
        whatItIs: 'The access secret this system requires.',
        impact: "Affects Swallow's integration features for this system.",
        optional: false,
      }
  }
}
