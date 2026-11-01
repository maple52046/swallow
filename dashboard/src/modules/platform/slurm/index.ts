import type { PlatformModuleUI } from '@/modules/types'
import { SlurmOverview } from './SlurmOverview'

/** The Slurm module's UI: its Overview with live slurmrestd state; it adds no tabs. */
export const slurmModule: PlatformModuleUI = {
  kind: 'platform',
  id: 'slurm',
  label: 'Slurm',
  badgePalette: 'gray',
  Overview: SlurmOverview,
  tabs: [],
}
