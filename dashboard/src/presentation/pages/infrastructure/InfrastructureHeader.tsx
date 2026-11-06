import type { ReactNode } from 'react'
import { PageHeader } from '@/presentation/components/PageHeader'
import { InfrastructureTabs } from './InfrastructureTabs'

/**
 * Shared route identity and local navigation for the Infrastructure pages (Sites, Integrations,
 * Zones, Pools, and the Local repository); `actions` are the page's own commands.
 */
export function InfrastructureHeader({ actions }: { actions?: ReactNode }) {
  return (
    <>
      <PageHeader title="Infrastructure" actions={actions} />
      <InfrastructureTabs />
    </>
  )
}
