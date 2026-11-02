import { Alert } from '@/presentation/components/ui/alert'

/**
 * Warns users that an ephemeral Kubernetes Platform has no durable node-local state.
 * It is repeated at configuration and review so template-owned ephemeral intent cannot be
 * submitted without an explicit, text-based warning.
 */
export function EphemeralKubernetesWarning() {
  return (
    <Alert status="warning" title="Ephemeral Kubernetes is disposable">
      The operating system, control-plane state, container runtime, and workloads are held in memory and are lost when a Server reboots. Use this only for disposable clusters.
    </Alert>
  )
}
