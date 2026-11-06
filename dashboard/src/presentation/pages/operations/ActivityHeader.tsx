import type { ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { PageHeader } from '@/presentation/components/PageHeader'
import { WorkspaceTabs } from '@/presentation/components/WorkspaceTabs'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

const TABS = [
  { value: '/workflows', label: 'Workflows' },
  { value: '/change-plans', label: 'Change Plans' },
] as const

interface ActivityHeaderProps {
  /** What the open tab shows, in one sentence under the area title. */
  subtitle: string
  /** The open tab's own commands, such as Refresh. */
  actions?: ReactNode
}

/**
 * Shared route identity and local navigation of the Activity area: the Workflows list
 * (`/workflows`) and the Change Plans (`/change-plans`). Both keep their own URLs; the tab
 * follows the URL and switching tabs keeps the URL-owned Site scope. A Workflow's detail page
 * (`/workflows/:id`) has no tabs: its breadcrumbs lead back here.
 */
export function ActivityHeader({ subtitle, actions }: ActivityHeaderProps) {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const active = TABS.find((item) => location.pathname.startsWith(item.value))?.value ?? TABS[0].value

  return (
    <>
      <PageHeader title="Activity" subtitle={subtitle} actions={actions} />
      <WorkspaceTabs
        value={active}
        items={TABS}
        label="Activity navigation"
        onChange={(value) => navigate(scopedHref(value))}
      />
    </>
  )
}
