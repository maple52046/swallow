import { Tab, Tabs, TabTitleText } from '@patternfly/react-core'
import { useLocation, useNavigate } from 'react-router-dom'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

const TABS = [
  { path: '/infrastructure/sites', label: 'Sites' },
  { path: '/infrastructure/integrations', label: 'Integrations' },
] as const

/** Keeps Infrastructure resource navigation aligned with the global URL-owned Site scope. */
export function InfrastructureTabs() {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const active = TABS.find((item) => location.pathname.startsWith(item.path))?.path ?? TABS[0].path

  return (
    <div className="sw-detail-tabs">
      <Tabs
        activeKey={active}
        aria-label="Infrastructure navigation"
        onSelect={(_event, key) => navigate(scopedHref(String(key)))}
      >
        {TABS.map((item) => (
          <Tab key={item.path} eventKey={item.path} title={<TabTitleText>{item.label}</TabTitleText>} />
        ))}
      </Tabs>
    </div>
  )
}
