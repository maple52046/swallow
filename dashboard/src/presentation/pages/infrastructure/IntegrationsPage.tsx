import { useCallback, useEffect, useState } from 'react'
import { Button, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Integration, IntegrationKind } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { ResourceRowActions } from '@/presentation/components/ResourceRowActions'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Select } from '@/presentation/components/ui/select'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { credentialGuidance } from './credentialGuidance'
import { InfrastructureHeader } from './InfrastructureHeader'
import { IntegrationCredentialDialog } from './IntegrationCredentialDialog'
import { IntegrationDialog } from './IntegrationDialog'
import { ResourceDeleteDialog } from './ResourceDeleteDialog'

type IntegrationsState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; integrations: Integration[] }

const KIND_LABELS: Record<IntegrationKind, string> = {
  provisioner: 'Provisioner',
  metrics: 'Metrics',
  platform: 'Platform',
}

/**
 * One-line explanation of an Integration's credential status, naming the provider-specific secret
 * and what its absence means, so the "Configured / Missing" badge is never an unexplained state.
 */
function credentialHint(integration: Integration): string {
  const guidance = credentialGuidance(integration.providerKind)
  if (integration.hasCredential) {
    return `${guidance.term} is stored. It is write-only — Swallow never shows it again.`
  }
  return guidance.optional
    ? `No ${guidance.term} set. Optional for this provider — leave empty for anonymous access.`
    : `No ${guidance.term} set. This integration cannot be used until you set one.`
}

/**
 * Admin Integration registry for Site-owned external provider connections.
 *
 * Credentials remain write-only and use a separate replacement dialog. Desktop
 * rows and mobile cards share the same filters, status semantics, and mutations.
 */
export function IntegrationsPage() {
  const { sites: repository } = useApp()
  const { showToast } = useToast()
  const { sites, siteId } = useSiteScope()
  const [state, setState] = useState<IntegrationsState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [kind, setKind] = useState<IntegrationKind | ''>('')
  const [editor, setEditor] = useState<Integration | 'create' | null>(null)
  const [credentialTarget, setCredentialTarget] = useState<Integration | null>(null)
  const [deleting, setDeleting] = useState<Integration | null>(null)

  const load = useCallback(async () => {
    try {
      setState({ status: 'ready', integrations: await repository.listIntegrations({ siteId }) })
    } catch (caught) {
      setState({ status: 'error', message: caught instanceof Error ? caught.message : 'Could not load integrations' })
    }
  }, [repository, siteId])

  useEffect(() => {
    let cancelled = false
    repository.listIntegrations({ siteId })
      .then((integrations) => {
        if (!cancelled) setState({ status: 'ready', integrations })
      })
      .catch((caught: Error) => {
        if (!cancelled) setState({ status: 'error', message: caught.message })
      })
    return () => { cancelled = true }
  }, [repository, siteId])

  const siteName = (targetSiteId: string) => sites.find((site) => site.id === targetSiteId)?.name ?? targetSiteId
  const needle = query.trim().toLowerCase()
  const filtered = state.status === 'ready'
    ? state.integrations.filter(
        (integration) =>
          (!kind || integration.kind === kind) &&
          (!needle || [integration.name, integration.endpoint, integration.providerKind, siteName(integration.siteId)].some((value) => value.toLowerCase().includes(needle))),
      )
    : []

  const handleSaved = async () => {
    const created = editor === 'create'
    setEditor(null)
    await load()
    showToast({ tone: 'success', title: created ? 'Integration created' : 'Integration updated' })
  }

  const handleCredentialReplaced = async () => {
    setCredentialTarget(null)
    await load()
    showToast({ tone: 'success', title: 'Credential replaced' })
  }

  const handleDeleted = async () => {
    setDeleting(null)
    await load()
    showToast({ tone: 'success', title: 'Integration deleted' })
  }

  const syncState = (integration: Integration) => integration.sync.lastError
    ? <><StatusBadge status="failed" /><Text as="small" display="block" color="fg.muted">{integration.sync.lastError}</Text></>
    : formatRelative(integration.sync.lastSucceededAt ?? undefined)

  return (
    <div className="operator-page">
      <InfrastructureHeader
        actions={
          <Button colorPalette="brand" onClick={() => setEditor('create')} disabled={sites.length === 0}>
            <Plus size={16} />
            Create integration
          </Button>
        }
      />
      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={setQuery} placeholder="Search integrations" aria-label="Search integrations" />
        <Select
          aria-label="Filter integration role"
          value={kind}
          size="sm"
          width="auto"
          onChange={(value) => setKind(value as IntegrationKind | '')}
          options={[
            { value: '', label: 'All roles' },
            { value: 'provisioner', label: 'Provisioner' },
            { value: 'metrics', label: 'Metrics' },
            { value: 'platform', label: 'Platform' },
          ]}
        />
      </DataToolbar>

      {state.status === 'loading' && <LoadingState rows={7} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
      {state.status === 'ready' && filtered.length === 0 && (
        <EmptyState
          title="No integrations"
          message={query || kind ? 'No results match these filters.' : 'Connect a provider to a site.'}
          action={!query && !kind && sites.length > 0 ? { label: 'Create integration', onClick: () => setEditor('create') } : undefined}
        />
      )}
      {state.status === 'ready' && filtered.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Integrations" className="sw-integration-table">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    <Table.ColumnHeader>Site</Table.ColumnHeader>
                    <Table.ColumnHeader>Role</Table.ColumnHeader>
                    <Table.ColumnHeader>Provider</Table.ColumnHeader>
                    <Table.ColumnHeader>Endpoint</Table.ColumnHeader>
                    <Table.ColumnHeader>Status</Table.ColumnHeader>
                    <Table.ColumnHeader>Credential</Table.ColumnHeader>
                    <Table.ColumnHeader>Last sync</Table.ColumnHeader>
                    <Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {filtered.map((integration) => (
                    <Table.Row key={integration.id} id={`integration-${integration.id}`}>
                      <Table.Cell><strong>{integration.name}</strong><Text as="small" display="block" color="fg.muted" className="sw-mono">{integration.id}</Text></Table.Cell>
                      <Table.Cell><Link to={`/infrastructure/sites?site=${encodeURIComponent(integration.siteId)}#site-${integration.siteId}`}>{siteName(integration.siteId)}</Link></Table.Cell>
                      <Table.Cell>{KIND_LABELS[integration.kind]}</Table.Cell>
                      <Table.Cell>{integration.providerKind}</Table.Cell>
                      <Table.Cell className="sw-mono">{integration.endpoint || '-'}</Table.Cell>
                      <Table.Cell><StatusBadge status={integration.enabled ? 'active' : 'offline'} label={integration.enabled ? 'Enabled' : 'Paused'} /></Table.Cell>
                      <Table.Cell>
                        <Tooltip content={credentialHint(integration)}>
                          <span>
                            <StatusBadge status={integration.hasCredential ? 'active' : 'warning'} label={integration.hasCredential ? 'Configured' : 'Missing'} />
                          </span>
                        </Tooltip>
                      </Table.Cell>
                      <Table.Cell>{syncState(integration)}</Table.Cell>
                      <Table.Cell textAlign="end">
                        <ResourceRowActions
                          actions={[
                            { kind: 'edit', label: `Edit ${integration.name}`, onClick: () => setEditor(integration) },
                            { kind: 'credential', label: `${integration.hasCredential ? 'Replace' : 'Set'} credential for ${integration.name}`, onClick: () => setCredentialTarget(integration) },
                            { kind: 'delete', label: `Delete ${integration.name}`, onClick: () => setDeleting(integration) },
                          ]}
                        />
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list" aria-label="Integrations">
              {filtered.map((integration) => (
                <ResourceCard
                  key={integration.id}
                  title={integration.name}
                  description={integration.endpoint || integration.id}
                  status={<StatusBadge status={integration.enabled ? 'active' : 'offline'} label={integration.enabled ? 'Enabled' : 'Paused'} />}
                  actions={
                    <ResourceRowActions
                      actions={[
                        { kind: 'edit', label: `Edit ${integration.name}`, onClick: () => setEditor(integration) },
                        { kind: 'credential', label: `${integration.hasCredential ? 'Replace' : 'Set'} credential for ${integration.name}`, onClick: () => setCredentialTarget(integration) },
                        { kind: 'delete', label: `Delete ${integration.name}`, onClick: () => setDeleting(integration) },
                      ]}
                    />
                  }
                >
                  <ResourceCardField label="Site">{siteName(integration.siteId)}</ResourceCardField>
                  <ResourceCardField label="Provider">{integration.providerKind}</ResourceCardField>
                  <ResourceCardField label="Credential">
                    {integration.hasCredential
                      ? `${credentialGuidance(integration.providerKind).term} set`
                      : `No ${credentialGuidance(integration.providerKind).term}`}
                  </ResourceCardField>
                  <ResourceCardField label="Last sync">{syncState(integration)}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}

      {editor && (
        <IntegrationDialog
          integration={editor === 'create' ? undefined : editor}
          sites={sites}
          defaultSiteId={siteId}
          onClose={() => setEditor(null)}
          onSaved={() => void handleSaved()}
        />
      )}
      {credentialTarget && (
        <IntegrationCredentialDialog
          integration={credentialTarget}
          onClose={() => setCredentialTarget(null)}
          onReplaced={() => void handleCredentialReplaced()}
        />
      )}
      {deleting && (
        <ResourceDeleteDialog
          resourceLabel="Integration"
          name={deleting.name}
          warning="External systems are unchanged. Remove dependent resources first."
          onClose={() => setDeleting(null)}
          onDelete={() => repository.deleteIntegration(deleting.id)}
          onDeleted={() => void handleDeleted()}
        />
      )}
    </div>
  )
}
