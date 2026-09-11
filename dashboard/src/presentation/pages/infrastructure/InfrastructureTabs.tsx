import { Tabs } from '@chakra-ui/react'
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
    <Tabs.Root
      value={active}
      onValueChange={(details) => navigate(scopedHref(details.value))}
      aria-label="Infrastructure navigation"
    >
      <Tabs.List>
        {TABS.map((item) => (
          <Tabs.Trigger key={item.path} value={item.path}>
            {item.label}
          </Tabs.Trigger>
        ))}
      </Tabs.List>
    </Tabs.Root>
  )
}
