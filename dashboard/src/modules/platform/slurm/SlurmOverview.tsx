import type { PlatformOverviewProps } from '@/modules/types'
import { SlurmPlatformView } from './SlurmPlatformView'

/** Adapts the shell's Overview props to the Slurm view, which also lists the login hosts. */
export function SlurmOverview({ platform, members, loginNodes, onSelectServer }: PlatformOverviewProps) {
  return <SlurmPlatformView platform={platform} members={members} loginNodes={loginNodes} onSelect={onSelectServer} />
}
