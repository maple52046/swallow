import type { ReactNode } from 'react'
import { PageHeader } from '@/presentation/components/PageHeader'
import { InfrastructureTabs } from './InfrastructureTabs'

interface InfrastructureHeaderProps {
  /** The page's own commands. */
  actions?: ReactNode
  /** Gives several commands their own full-width row on a phone (see `PageHeader`). */
  stackActionsOnMobile?: boolean
}

/**
 * Shared route identity and local navigation for the Infrastructure pages (Sites, Integrations,
 * Zones, Pools, the Local repository, and Boot ISOs).
 */
export function InfrastructureHeader({ actions, stackActionsOnMobile }: InfrastructureHeaderProps) {
  return (
    <>
      <PageHeader title="Infrastructure" actions={actions} stackActionsOnMobile={stackActionsOnMobile} />
      <InfrastructureTabs />
    </>
  )
}
