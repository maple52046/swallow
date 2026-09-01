import { useCallback, useEffect, useState } from 'react'
import {
  Button,
  FormSelect,
  FormSelectOption,
  SearchInput,
  ToolbarItem,
} from '@patternfly/react-core'
import { PlusCircleIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { Link } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Integration, IntegrationKind } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
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
  cluster: 'Cluster',
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
      setState({
        status: 'error',
        message: caught instanceof Error ? caught.message : 'Could not load Integrations.',
      })
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
    return () => {
      cancelled = true
    }
  }, [repository, siteId])

  const siteName = (targetSiteId: string) => sites.find((site) => site.id === targetSiteId)?.name ?? targetSiteId
  const needle = query.trim().toLowerCase()
  const filtered = state.status === 'ready' ? state.integrations.filter((integration) => (
    (!kind || integration.kind === kind) &&
    (!needle || [
      integration.name,
      integration.endpoint,
      integration.providerKind,
      siteName(integration.siteId),
    ].some((value) => value.toLowerCase().includes(needle)))
  )) : []

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
    showToast({
      tone: 'success',
      title: 'Credential replaced',
      description: `${target.name} now uses the new write-only credential.`,
    })
  }

  const handleDeleted = async (deleted: Integration) => {
    setDeleting(null)
    await load()
    showToast({
      tone: 'success',
      title: 'Integration deleted',
      description: `${deleted.name} was removed from the Swallow registry.`,
    })
  }

  return (
    <div className="operator-page">
      <InfrastructureHeader
        actions={<Button icon={<PlusCircleIcon />} onClick={() => setEditor('create')} isDisabled={sites.length === 0}>Create integration</Button>}
      />
      <DataToolbar variant="plain">
        <ToolbarItem>
          <SearchInput
            value={query}
            onChange={(_event, value) => setQuery(value)}
            onClear={() => setQuery('')}
            placeholder="Search Integrations"
            aria-label="Search Integrations"
          />
        </ToolbarItem>
        <ToolbarItem>
          <FormSelect aria-label="Filter Integration role" value={kind} onChange={(_event, value) => setKind(value as IntegrationKind | '')}>
            <FormSelectOption value="" label="All roles" />
            <FormSelectOption value="provisioner" label="Provisioner" />
            <FormSelectOption value="metrics" label="Metrics" />
            <FormSelectOption value="cluster" label="Cluster" />
          </FormSelect>
        </ToolbarItem>
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
          <Table aria-label="Integrations" variant="compact" className="sw-integration-table">
            <Thead><Tr>
              <Th>Name</Th><Th>Site</Th><Th>Role</Th><Th>Provider</Th><Th>Endpoint</Th>
              <Th>Status</Th><Th>Credential</Th><Th>Last sync</Th><Th screenReaderText="Actions" />
            </Tr></Thead>
            <Tbody>{filtered.map((integration) => (
              <Tr key={integration.id} id={`integration-${integration.id}`}>
                <Td dataLabel="Name"><strong>{integration.name}</strong><small className="sw-mono">{integration.id}</small></Td>
                <Td dataLabel="Site">
                  <Link to={`/infrastructure/sites?site=${encodeURIComponent(integration.siteId)}#site-${integration.siteId}`}>
                    {siteName(integration.siteId)}
                  </Link>
                </Td>
                <Td dataLabel="Role">{KIND_LABELS[integration.kind]}</Td>
                <Td dataLabel="Provider">{integration.providerKind}</Td>
                <Td dataLabel="Endpoint" className="sw-mono">{integration.endpoint || '-'}</Td>
                <Td dataLabel="Status"><StatusBadge status={integration.enabled ? 'active' : 'offline'} label={integration.enabled ? 'Enabled' : 'Paused'} /></Td>
                <Td dataLabel="Credential"><StatusBadge status={integration.hasCredential ? 'active' : 'warning'} label={integration.hasCredential ? 'Configured' : 'Not configured'} /></Td>
                <Td dataLabel="Last sync">
                  {integration.sync.lastError
                    ? <><StatusBadge status="failed" label="Failed" /><small>{integration.sync.lastError}</small></>
                    : formatRelative(integration.sync.lastSucceededAt ?? undefined)}
                </Td>
                <Td isActionCell>
                  <span className="sw-row-actions">
                    <Button variant="link" isInline onClick={() => setEditor(integration)}>Edit</Button>
                    <Button variant="link" isInline onClick={() => setCredentialTarget(integration)}>Credential</Button>
                    <Button variant="link" isInline isDanger onClick={() => setDeleting(integration)}>Delete</Button>
                  </span>
                </Td>
              </Tr>
            ))}</Tbody>
          </Table>
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
