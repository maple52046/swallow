import { useLocation, useNavigate } from 'react-router-dom'
import { WorkspaceTabs } from '@/presentation/components/WorkspaceTabs'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

const TABS = [
  { value: '/infrastructure/sites', label: 'Sites' },
  { value: '/infrastructure/integrations', label: 'Integrations' },
  { value: '/infrastructure/zones', label: 'Zones' },
  { value: '/infrastructure/pools', label: 'Pools' },
] as const

/** Keeps Infrastructure resource navigation aligned with the global URL-owned Site scope. */
export function InfrastructureTabs() {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const active = TABS.find((item) => location.pathname.startsWith(item.value))?.value ?? TABS[0].value

  return (
    <WorkspaceTabs
      value={active}
      items={TABS}
      label="Infrastructure navigation"
      onChange={(value) => navigate(scopedHref(value))}
    />
  )
}
