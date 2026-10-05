import { useLocation, useNavigate } from 'react-router-dom'
import { WorkspaceTabs } from '@/presentation/components/WorkspaceTabs'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useExperimentalFeatures } from '@/presentation/contexts/ExperimentalFeaturesContext'
import type { ExperimentalFeature } from '@/application/ports/ExperimentalFeatureSettings'

interface ProvisioningTab {
  value: string
  label: string
  /** In-development feature the workspace belongs to; listed only while it is shown. */
  feature?: ExperimentalFeature
}

const TABS: readonly ProvisioningTab[] = [
  { value: '/provisioning/deploy', label: 'Deploy OS' },
  { value: '/provisioning/templates', label: 'Templates', feature: 'deploymentTemplates' },
  { value: '/provisioning/images', label: 'OS images' },
  { value: '/provisioning/boot-isos', label: 'Boot ISOs' },
]

/**
 * Scope-preserving navigation across the provisioning workspaces. Workspaces of hidden
 * in-development features are omitted; their routes redirect separately.
 */
export function ProvisioningTabs() {
  const location = useLocation()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const { enabled: features } = useExperimentalFeatures()
  const tabs = TABS.filter((item) => !item.feature || features[item.feature])
  const active = tabs.find((item) => location.pathname.startsWith(item.value))?.value ?? tabs[0].value

  return (
    <WorkspaceTabs
      value={active}
      items={tabs}
      label="Provisioning navigation"
      onChange={(value) => navigate(scopedHref(value))}
    />
  )
}
