import { platformSettingsUI } from '@/modules/registry'
import { EmptyState } from '@/presentation/components/EmptyState'

const SlurmSettings = platformSettingsUI('slurm')

/** Renders the settings surface declared by the Slurm module through the central registry. */
export function PlatformSettingsPage() {
  return SlurmSettings ? <SlurmSettings /> : <EmptyState title="Platform settings unavailable" message="No installed Platform module provides settings." />
}
