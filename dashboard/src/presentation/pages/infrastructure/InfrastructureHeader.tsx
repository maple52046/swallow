import type { ReactNode } from 'react'
import { PageHeader } from '@/presentation/components/PageHeader'
import { InfrastructureTabs } from './InfrastructureTabs'

/**
 * Shared hierarchy header for both registry resources.
 * The copy names the ownership direction once so Site and Integration columns elsewhere
 * can remain concise without hiding the platform model.
 */
export function InfrastructureHeader({ actions }: { actions?: ReactNode }) {
  return (
    <>
      <PageHeader
        title="Infrastructure"
        subtitle="Sites define infrastructure locations. Each Integration connects one Site to an external provider."
        actions={actions}
      />
      <InfrastructureTabs />
    </>
  )
}
