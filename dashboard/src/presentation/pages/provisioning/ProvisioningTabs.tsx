import { Tab, Tabs, TabTitleText } from '@patternfly/react-core'
import { useLocation, useNavigate } from 'react-router-dom'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

const TABS = [
  { path: '/provisioning/deploy', label: 'Deploy OS' },
  { path: '/provisioning/templates', label: 'Templates' },
  { path: '/provisioning/images', label: 'OS images' },
] as const

/** Scope-preserving navigation across the provisioning workspaces. */
export function ProvisioningTabs() {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const active = TABS.find((item) => location.pathname.startsWith(item.path))?.path ?? TABS[0].path
  return (
    <div className="sw-detail-tabs">
      <Tabs
        activeKey={active}
        aria-label="Provisioning navigation"
        onSelect={(_event, key) => navigate(scopedHref(String(key)))}
      >
        {TABS.map((item) => (
          <Tab key={item.path} eventKey={item.path} title={<TabTitleText>{item.label}</TabTitleText>} />
        ))}
      </Tabs>
    </div>
  )
}
