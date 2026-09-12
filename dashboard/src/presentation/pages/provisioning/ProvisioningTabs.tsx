import { useLocation, useNavigate } from 'react-router-dom'
import { WorkspaceTabs } from '@/presentation/components/WorkspaceTabs'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

const TABS = [
  { value: '/provisioning/deploy', label: 'Deploy OS' },
  { value: '/provisioning/templates', label: 'Templates' },
  { value: '/provisioning/images', label: 'OS images' },
] as const

/** Scope-preserving navigation across the provisioning workspaces. */
export function ProvisioningTabs() {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const active = TABS.find((item) => location.pathname.startsWith(item.value))?.value ?? TABS[0].value

  return (
    <WorkspaceTabs
      value={active}
      items={TABS}
      label="Provisioning navigation"
      onChange={(value) => navigate(scopedHref(value))}
    />
  )
}
