import { useState } from 'react'
import { HStack, Tabs } from '@chakra-ui/react'
import { Navigate, Outlet, useLocation, useNavigate, useParams } from 'react-router-dom'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { isMaasProvisioner, providerLabel, roleLabel } from './integrationSettings'
import {
  useIntegration,
  type IntegrationGeneralDraft,
  type IntegrationSettingsContext,
} from './useIntegrationSettings'

/** One settings tab: its route segment and label, and whether only MAAS provisioners have it. */
const TABS: ReadonlyArray<{ value: string; label: string; maasOnly?: boolean }> = [
  { value: 'general', label: 'General' },
  { value: 'connection', label: 'Connection' },
  { value: 'credential', label: 'Credential' },
  { value: 'http-proxy', label: 'HTTP proxy', maasOnly: true },
  { value: 'subnet-dns', label: 'Subnet DNS', maasOnly: true },
]


/**
 * The settings page of one Integration (`/infrastructure/integrations/:id/<tab>`), which replaces
 * the Edit integration dialog: its settings outgrew a modal, and they are of two kinds that each
 * need their own action — swallow's record (General, Connection, Credential), saved per tab, and a
 * MAAS provisioner's provider-owned live settings (HTTP proxy, Subnet DNS), applied to MAAS
 * directly. Each tab has its own URL, so a tab can be linked and bookmarked.
 *
 * The page loads the Integration once and hands it, the drafts of the General and Connection tabs,
 * and save/reload callbacks to the tab routes through the outlet. Drafts are page state so they
 * survive switching tabs and are discarded when the page is left or the Integration changes.
 * Loading, an unknown id, and a failed read use the shared states.
 */
export function IntegrationSettingsPage() {
  const { id } = useParams()
  const { state, reload, replace } = useIntegration(id)
  const { sites, scopedHref } = useSiteScope()
  const location = useLocation()
  const navigate = useNavigate()
  // Drafts belong to one Integration; keying them by id drops another Integration's draft when the
  // route changes to it without unmounting the page.
  const [drafts, setDrafts] = useState<{ id?: string; general: IntegrationGeneralDraft | null; connection: Record<string, string> | null }>({
    general: null,
    connection: null,
  })
  const ownDrafts = drafts.id === id ? drafts : { id, general: null, connection: null }

  if (state.status === 'loading') return <LoadingState />
  if (state.status === 'error') return <ErrorState message={state.message} onRetry={reload} />
  if (state.status === 'not-found') {
    return (
      <EmptyState
        title="Integration not found"
        message="It may have been deleted."
        action={{ label: 'Back to Integrations', onClick: () => navigate(scopedHref('/infrastructure/integrations')) }}
      />
    )
  }

  const { integration } = state
  const tabs = TABS.filter((tab) => !tab.maasOnly || isMaasProvisioner(integration))
  const segment = location.pathname.split('/').pop() ?? ''
  const current = tabs.some((tab) => tab.value === segment) ? segment : 'general'
  const siteName = sites.find((site) => site.id === integration.siteId)?.name ?? integration.siteId

  const context: IntegrationSettingsContext = {
    integration,
    siteName,
    generalDraft: ownDrafts.general,
    setGeneralDraft: (general) => setDrafts({ ...ownDrafts, id, general }),
    connectionDraft: ownDrafts.connection,
    setConnectionDraft: (connection) => setDrafts({ ...ownDrafts, id, connection }),
    onSaved: replace,
    reload,
  }

  return (
    <div className="operator-page">
      <PageHeader
        title={integration.name}
        subtitle={`${providerLabel(integration.providerKind)} ${roleLabel(integration.kind)} · ${siteName}`}
        breadcrumbs={[
          { label: 'Infrastructure', href: scopedHref('/infrastructure/sites') },
          { label: 'Integrations', href: scopedHref('/infrastructure/integrations') },
          { label: integration.name },
        ]}
        metadata={
          // The same badges as the Integrations list, each carrying its state in words.
          <HStack gap="2" wrap="wrap">
            <StatusBadge status={integration.enabled ? 'active' : 'offline'} label={integration.enabled ? 'Enabled' : 'Paused'} />
            <StatusBadge
              status={integration.hasCredential ? 'active' : 'warning'}
              label={integration.hasCredential ? 'Credential configured' : 'Credential missing'}
            />
          </HStack>
        }
      />
      <Tabs.Root
        value={current}
        onValueChange={(details) => navigate(scopedHref(`/infrastructure/integrations/${integration.id}/${details.value}`))}
      >
        {/* The label goes on the tablist itself, the element assistive technology announces. */}
        <Tabs.List
          aria-label="Integration settings"
          maxW="full"
          overflowX="auto"
          overscrollBehaviorX="contain"
          scrollbarWidth="none"
          css={{ '&::-webkit-scrollbar': { display: 'none' } }}
        >
          {tabs.map((tab) => (
            <Tabs.Trigger key={tab.value} value={tab.value} flex="none">
              {tab.label}
            </Tabs.Trigger>
          ))}
        </Tabs.List>
      </Tabs.Root>
      <div className="sw-integration-settings-page__panel">
        <Outlet context={context} />
      </div>
    </div>
  )
}

/**
 * The index route of the settings page: opens General and keeps the query string (the Site
 * scope), so `/infrastructure/integrations/:id?site=…` lands on a shareable tab URL.
 */
export function IntegrationSettingsIndexRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: 'general', search: location.search }} replace />
}
