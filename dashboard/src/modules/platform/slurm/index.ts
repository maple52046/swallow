import { Cpu } from 'lucide-react'
import type { PlatformModuleUI } from '@/modules/types'
import { slurmPlatformFacts } from './slurmFacts'
import { SlurmOverview } from './SlurmOverview'
import { useSlurmDeployForm } from './useSlurmDeployForm'

/**
 * The Slurm module's UI: its deploy form, its Overview with live slurmrestd state, and the Slurm
 * facts of the Platform details card; it adds no tabs.
 */
export const slurmModule: PlatformModuleUI = {
  kind: 'platform',
  id: 'slurm',
  label: 'Slurm',
  badgePalette: 'gray',
  Overview: SlurmOverview,
  tabs: [],
  deploy: { icon: Cpu, namePlaceholder: 'lab-slurm', useForm: useSlurmDeployForm },
  facts: slurmPlatformFacts,
}
