import type { ReactNode } from 'react'
import { Stack } from '@chakra-ui/react'
import { useSearchParams } from 'react-router-dom'
import type { SoftwareKind } from '@/domain/software/types'
import { PageHeader } from '@/presentation/components/PageHeader'
import { WorkspaceTabs } from '@/presentation/components/WorkspaceTabs'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { RegistryCredentialsSection } from './RegistryCredentialsSection'
import { SOFTWARE_SETTINGS_KIND_PARAM, softwareKindLabel } from './softwarePresentation'

/** One software kind's settings group: the kind it belongs to and what it renders. */
interface KindSettings {
  kind: SoftwareKind
  render: () => ReactNode
}

// Only kinds that have settings are listed; a new kind adds its own group here rather than putting
// kind-specific settings on the software-wide list page.
const KIND_SETTINGS: readonly KindSettings[] = [
  { kind: 'docker-ce', render: () => <RegistryCredentialsSection /> },
]

/**
 * Software settings (route `/software/settings`): settings that belong to one software kind,
 * grouped by kind (decision 044 introduced the first one, Docker CE's Registry credentials).
 *
 * It mirrors `/platforms/settings`: the list page links here, and nothing on it applies to every
 * software kind. The selected group is owned by the URL (`?kind=docker-ce`) so the Pull image
 * dialog and bookmarks can open a specific group; an unknown or missing value shows the first group.
 * Changing the group keeps every other query parameter, including the Site scope, although these
 * settings are installation-wide and not Site-scoped.
 */
export function SoftwareSettingsPage() {
  const { scopedHref } = useSiteScope()
  const [searchParams, setSearchParams] = useSearchParams()
  const requested = searchParams.get(SOFTWARE_SETTINGS_KIND_PARAM)
  const selected = KIND_SETTINGS.find((group) => group.kind === requested) ?? KIND_SETTINGS[0]

  return (
    <div className="operator-page">
      <PageHeader
        title="Software settings"
        subtitle="Settings that belong to one software kind. They apply to every Server, not to one Site."
        breadcrumbs={[{ label: 'Software', href: scopedHref('/software') }, { label: 'Settings' }]}
      />
      <Stack gap="5">
        <WorkspaceTabs
          label="Software kinds"
          value={selected.kind}
          items={KIND_SETTINGS.map((group) => ({ value: group.kind, label: softwareKindLabel(group.kind) }))}
          onChange={(kind) =>
            setSearchParams((current) => {
              const next = new URLSearchParams(current)
              next.set(SOFTWARE_SETTINGS_KIND_PARAM, kind)
              return next
            })
          }
        />
        {selected.render()}
      </Stack>
    </div>
  )
}
