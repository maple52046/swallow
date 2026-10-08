import type { ReactNode } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import type { SoftwareKind } from '@/domain/software/types'
import { isSoftwareKind } from '@/domain/software/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { RegistryCredentialsSection } from './RegistryCredentialsSection'
import { SOFTWARE_SETTINGS_KIND_PARAM, softwareKindLabel } from './softwarePresentation'

/** One software kind's settings group: the kind it belongs to and what it renders. */
interface KindSettings {
  kind: SoftwareKind
  render: () => ReactNode
}

// Only kinds with settings belong here. Kinds without settings have no detail-page action.
const KIND_SETTINGS: readonly KindSettings[] = [
  { kind: 'docker-ce', render: () => <RegistryCredentialsSection /> },
]

/**
 * Settings owned by one Managed Software kind.
 *
 * The canonical route is `/software/:kind/settings`. The legacy `/software/settings?kind=...`
 * route remains readable for existing bookmarks, but the catalog no longer exposes a global
 * settings entry point.
 */
export function SoftwareSettingsPage() {
  const { scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const { kind: routeKind } = useParams<{ kind: string }>()
  const [searchParams] = useSearchParams()
  const requested = routeKind ?? searchParams.get(SOFTWARE_SETTINGS_KIND_PARAM)
  const requestedKind = requested && isSoftwareKind(requested) ? requested : null
  const selected = KIND_SETTINGS.find((group) => group.kind === requestedKind)
    ?? (routeKind ? null : KIND_SETTINGS[0])

  if (!selected) {
    const label = requestedKind ? softwareKindLabel(requestedKind) : 'Software'
    const detailPath = requestedKind ? `/software/${requestedKind}` : '/software'
    return (
      <div className="operator-page">
        <PageHeader
          title={`${label} settings`}
          breadcrumbs={[
            { label: 'Software', href: scopedHref('/software') },
            ...(requestedKind ? [{ label, href: scopedHref(detailPath) }] : []),
            { label: 'Settings' },
          ]}
        />
        <EmptyState
          title="No settings available"
          message={`${label} does not have software-wide settings.`}
          action={{ label: `Back to ${label}`, onClick: () => navigate(scopedHref(detailPath)) }}
        />
      </div>
    )
  }

  const label = softwareKindLabel(selected.kind)

  return (
    <div className="operator-page">
      <PageHeader
        title={`${label} settings`}
        subtitle={`Installation-wide settings used by ${label} on every Server.`}
        breadcrumbs={[
          { label: 'Software', href: scopedHref('/software') },
          { label, href: scopedHref(`/software/${selected.kind}`) },
          { label: 'Settings' },
        ]}
      />
      {selected.render()}
    </div>
  )
}
