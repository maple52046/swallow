import type { PlatformOverviewProps } from '@/modules/types'
import { KubernetesPlatformView } from './KubernetesPlatformView'

/** Adapts the shell's Overview props to the Kubernetes view, which has no login hosts. */
export function KubernetesOverview({ platform, members, onSelectServer }: PlatformOverviewProps) {
  return <KubernetesPlatformView platform={platform} members={members} onSelect={onSelectServer} />
}
