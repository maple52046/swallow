import type { ReactNode } from 'react'
import { PageHeader } from '@/presentation/components/PageHeader'
import { InfrastructureTabs } from './InfrastructureTabs'

/** Shared route identity and local navigation for Site and Integration registries. */
export function InfrastructureHeader({ actions }: { actions?: ReactNode }) {
  return (
    <>
      <PageHeader title="Infrastructure" actions={actions} />
      <InfrastructureTabs />
    </>
  )
}
