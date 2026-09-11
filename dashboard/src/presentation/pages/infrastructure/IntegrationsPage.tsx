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
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Select } from '@/presentation/components/ui/select'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
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
 * Admin Integration registry showing the Site parent beside each concrete provider.
 * Credentials remain write-only and use a separate transient replacement flow.
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
      setState({ status: 'error', message: caught instanceof Error ? caught.message : 'Could not load Integrations.' })
    }
  }, [repository, siteId])

  useEffect(() => {
    let cancelled = false
    repository
      .listIntegrations({ siteId })
      .then((integrations) => {
        if (!cancelled) setState({ status: 'ready', integrations })
      })
      .catch((caught: Error) => {
        if (!cancelled) setState({ status: 'error', message: caught.message })
      })
    return () => {
      cancelled = true
    }
  }, [repository, siteId])

  const siteName = (targetSiteId: string) => sites.find((site) => site.id === targetSiteId)?.name ?? targetSiteId
  const needle = query.trim().toLowerCase()
  const filtered =
    state.status === 'ready'
      ? state.integrations.filter(
          (integration) =>
            (!kind || integration.kind === kind) &&
            (!needle ||
              [integration.name, integration.endpoint, integration.providerKind, siteName(integration.siteId)].some((value) =>
                value.toLowerCase().includes(needle),
              )),
        )
      : []

  const handleSaved = async (saved: Integration) => {
    setEditor(null)
    await load()
    showToast({
      tone: 'success',
      title: editor === 'create' ? 'Integration created' : 'Integration updated',
      description: `${saved.name} is registered under ${siteName(saved.siteId)}.`,
    })
  }

  const handleCredentialReplaced = async (target: Integration) => {
    setCredentialTarget(null)
    await load()
    showToast({ tone: 'success', title: 'Credential replaced', description: `${target.name} now uses the new write-only credential.` })
  }

  const handleDeleted = async (deleted: Integration) => {
    setDeleting(null)
    await load()
    showToast({ tone: 'success', title: 'Integration deleted', description: `${deleted.name} was removed from the Swallow registry.` })
  }

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
        <SearchInput value={query} onChange={setQuery} placeholder="Search Integrations" aria-label="Search Integrations" />
        <Select
          aria-label="Filter Integration role"
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
          title="No Integrations"
          message={query || kind ? 'No Integration matches these filters.' : 'Register an external provider under a Site.'}
          action={!query && !kind && sites.length > 0 ? { label: 'Create integration', onClick: () => setEditor('create') } : undefined}
        />
      )}
      {state.status === 'ready' && filtered.length > 0 && (
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
                  <Table.Cell>
                    <strong>{integration.name}</strong>
                    <Text as="small" display="block" color="fg.muted" className="sw-mono">
                      {integration.id}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Link to={`/infrastructure/sites?site=${encodeURIComponent(integration.siteId)}#site-${integration.siteId}`}>
                      {siteName(integration.siteId)}
                    </Link>
                  </Table.Cell>
                  <Table.Cell>{KIND_LABELS[integration.kind]}</Table.Cell>
                  <Table.Cell>{integration.providerKind}</Table.Cell>
                  <Table.Cell className="sw-mono">{integration.endpoint || '-'}</Table.Cell>
                  <Table.Cell>
                    <StatusBadge status={integration.enabled ? 'active' : 'offline'} label={integration.enabled ? 'Enabled' : 'Paused'} />
                  </Table.Cell>
                  <Table.Cell>
                    <StatusBadge
                      status={integration.hasCredential ? 'active' : 'warning'}
                      label={integration.hasCredential ? 'Configured' : 'Not configured'}
                    />
                  </Table.Cell>
                  <Table.Cell>
                    {integration.sync.lastError ? (
                      <>
                        <StatusBadge status="failed" label="Failed" />
                        <Text as="small" display="block" color="fg.muted">
                          {integration.sync.lastError}
                        </Text>
                      </>
                    ) : (
                      formatRelative(integration.sync.lastSucceededAt ?? undefined)
                    )}
                  </Table.Cell>
                  <Table.Cell textAlign="end">
                    <span className="sw-row-actions">
                      <Button variant="plain" size="sm" px="1" h="auto" colorPalette="brand" onClick={() => setEditor(integration)}>
                        Edit
                      </Button>
                      <Button variant="plain" size="sm" px="1" h="auto" colorPalette="brand" onClick={() => setCredentialTarget(integration)}>
                        Credential
                      </Button>
                      <Button variant="plain" size="sm" px="1" h="auto" colorPalette="red" onClick={() => setDeleting(integration)}>
                        Delete
                      </Button>
                    </span>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      )}
      {editor && (
        <IntegrationDialog
          integration={editor === 'create' ? undefined : editor}
          sites={sites}
          defaultSiteId={siteId}
          onClose={() => setEditor(null)}
          onSaved={(saved) => void handleSaved(saved)}
        />
      )}
      {credentialTarget && (
        <IntegrationCredentialDialog
          integration={credentialTarget}
          onClose={() => setCredentialTarget(null)}
          onReplaced={() => void handleCredentialReplaced(credentialTarget)}
        />
      )}
      {deleting && (
        <ResourceDeleteDialog
          resourceLabel="Integration"
          name={deleting.name}
          warning="External systems are not changed. Servers or deployment templates that still reference this Integration will block deletion."
          onClose={() => setDeleting(null)}
          onDelete={() => repository.deleteIntegration(deleting.id)}
          onDeleted={() => void handleDeleted(deleting)}
        />
      )}
    </div>
  )
}
