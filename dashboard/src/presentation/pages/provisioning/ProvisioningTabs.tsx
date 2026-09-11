import { Tabs } from '@chakra-ui/react'
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
    <Tabs.Root
      value={active}
      onValueChange={(details) => navigate(scopedHref(details.value))}
      aria-label="Provisioning navigation"
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
