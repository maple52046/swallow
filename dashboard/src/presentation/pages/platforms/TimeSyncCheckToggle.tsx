import { FeatureToggleCard } from './FeatureToggleCard'

interface TimeSyncCheckToggleProps {
  /** Whether a time-sync problem fails the deployment (`enforce`); off means it only warns. */
  enforced: boolean
  onEnforcedChange: (enforced: boolean) => void
}

/**
 * The deploy wizard's switch for the Platform time-sync check (platforms.md "Time sync check",
 * decision 065), shared by the Kubernetes and Slurm Features steps.
 *
 * Every Platform deployment checks each node's clock before the platform is installed; this only
 * chooses what a problem does. On, the default, fails the deployment and names the nodes at fault;
 * off lets it continue with a warning, for operators who accept the risk. The copy says what is
 * checked, because the switch itself never enables or disables the check.
 */
export function TimeSyncCheckToggle({ enforced, onEnforcedChange }: TimeSyncCheckToggleProps) {
  return (
    <FeatureToggleCard
      title="Require synchronized clocks"
      description={
        'Before installing, every node must report a synchronized clock within 1 second of the swallow NTP server. ' +
        (enforced
          ? 'A node that does not fails the deployment.'
          : 'A node that does not is only reported as a warning; runtimes such as MUNGE or etcd may then fail.')
      }
      checked={enforced}
      onCheckedChange={onEnforcedChange}
      switchLabel="Require synchronized clocks"
    />
  )
}
