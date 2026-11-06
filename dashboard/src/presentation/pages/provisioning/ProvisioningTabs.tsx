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
  { value: '/provisioning/images', label: 'OS images' },
  { value: '/provisioning/templates', label: 'Templates', feature: 'deploymentTemplates' },
]

/**
 * Scope-preserving navigation of the OS Images area (the sidebar entry above Infrastructure): the OS image
 * catalog and the Deployment Templates that use its images. The area keeps its `/provisioning/*`
 * URLs. Workspaces of hidden in-development features are omitted; their routes redirect separately.
 * Boot ISOs, Site wiring rather than a catalog to choose from, are an Infrastructure tab.
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
      label="OS Images navigation"
      onChange={(value) => navigate(scopedHref(value))}
    />
  )
}
