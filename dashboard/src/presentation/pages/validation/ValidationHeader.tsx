import type { ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { PageHeader } from '@/presentation/components/PageHeader'
import { WorkspaceTabs } from '@/presentation/components/WorkspaceTabs'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

const TABS = [
  { value: '/validation/runs', label: 'Runs' },
  { value: '/validation/suites', label: 'Suites' },
  { value: '/validation/schedules', label: 'Schedules' },
] as const

interface ValidationHeaderProps {
  /** What the open tab shows, in one sentence under the area title. */
  subtitle: string
  /** The open tab's own commands, such as Run a suite. */
  actions?: ReactNode
}

/**
 * Route identity and local navigation of the Validation area (decision 081): its Runs
 * (`/validation/runs`), the Suites catalog (`/validation/suites`), and the Schedules
 * (`/validation/schedules`). Each tab keeps its own URL; the tab follows the URL and switching tabs
 * keeps the URL-owned Site scope. A run's detail page has no tabs: its breadcrumbs lead back here.
 * The whole area is an experimental feature, guarded by its routes and its navigation entry.
 */
export function ValidationHeader({ subtitle, actions }: ValidationHeaderProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const active = TABS.find((item) => location.pathname.startsWith(item.value))?.value ?? TABS[0].value

  return (
    <>
      <PageHeader title="Validation" subtitle={subtitle} actions={actions} />
      <WorkspaceTabs
        value={active}
        items={TABS}
        label="Validation navigation"
        onChange={(value) => navigate(scopedHref(value))}
      />
    </>
  )
}
