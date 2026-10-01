import { useCallback, useEffect, useMemo, useState } from 'react'
import { Box, Button, HStack, Stack, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { FilePlus2, KeyRound, RefreshCw, Upload } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { SSHKey } from '@/domain/access/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { useToast } from '@/presentation/components/toast/toastContext'
import { formatDateTime } from '@/shared/utils/time'
import { DeleteSSHKeyDialog } from './DeleteSSHKeyDialog'
import { DeploymentKeyDialog } from './DeploymentKeyDialog'
import { GenerateSSHKeyDialog } from './GenerateSSHKeyDialog'
import { ImportSSHKeyDialog } from './ImportSSHKeyDialog'
import { SSHKeySyncStatus } from './SSHKeySyncStatus'
import { usePendingSyncRefresh } from './usePendingSyncRefresh'

/** Loaded page data: every key visible to the caller and provisioner names for sync rows. */
interface SSHKeysData {
  deploymentKey?: SSHKey
  accessKeys: SSHKey[]
  integrationNames: ReadonlyMap<string, string>
}

type PageState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: SSHKeysData }

/** Which dialog is open; at most one at a time. */
type DialogState =
  | { kind: 'import' }
  | { kind: 'generate' }
  | { kind: 'delete'; key: SSHKey }
  | { kind: 'deployment'; mode: 'regenerate' | 'replace'; key: SSHKey }
  | null

/** How long after a sync request the page re-reads, giving the backend pass time to run. */
const SYNC_REFRESH_DELAY_MS = 3000

/**
 * The signed-in admin's SSH key workspace (route `/account/ssh-keys`, decision 039).
 *
 * It shows the system-owned Deployment Key swallow logs in to Servers with, and the caller's own
 * Access Keys, each with its realization status in every provisioner. All data comes from the
 * `ssh-keys.md` contract through the SSHKeyRepository port; provisioner names come from the
 * integrations list and fall back to ids if that read fails, so a missing name never hides keys.
 * The list is read once per visit and after each action; a key whose provisioner status is
 * `pending` is then followed on its own (usePendingSyncRefresh) and patched in place, so its
 * status settles without reloading the page or touching other rows.
 * No private key is ever loaded here: the only private key the page handles is the one-time result
 * of generating an Access Key, owned by GenerateSSHKeyDialog.
 *
 * Every endpoint requires `admin`; a non-admin reaching this route sees the API's error state.
 */
export function SSHKeysPage() {
  const { sshKeys, sites } = useApp()
  const { showToast } = useToast()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [dialog, setDialog] = useState<DialogState>(null)
  const [reloadNonce, setReloadNonce] = useState(0)
  // Counts accepted sync requests; each increment schedules one delayed re-read.
  const [syncRequests, setSyncRequests] = useState(0)
  const [syncing, setSyncing] = useState(false)

  // Loads keys and provisioner names together. The `cancelled` flag drops a slower, older response
  // so it cannot overwrite a newer one after a reload or unmount.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const [keys, integrations] = await Promise.all([
          sshKeys.listSSHKeys(),
          // Names are cosmetic; a failed integrations read must not hide the keys.
          sites.listIntegrations({ kind: 'provisioner' }).catch(() => []),
        ])
        if (cancelled) return
        setState({
          status: 'ready',
          data: {
            deploymentKey: keys.find((key) => key.purpose === 'deployment'),
            accessKeys: keys.filter((key) => key.purpose === 'access'),
            integrationNames: new Map(integrations.map((integration) => [integration.id, integration.name])),
          },
        })
      } catch (caught) {
        if (!cancelled) setState({ status: 'error', message: caught instanceof Error ? caught.message : 'Could not load SSH keys.' })
      }
    })()
    return () => {
      cancelled = true
    }
  }, [sshKeys, sites, reloadNonce])

  // After a sync request, re-read once the backend pass has had time to run. The timer is cleared
  // if another request replaces it or the page unmounts.
  useEffect(() => {
    if (syncRequests === 0) return
    const timer = setTimeout(() => setReloadNonce((value) => value + 1), SYNC_REFRESH_DELAY_MS)
    return () => clearTimeout(timer)
  }, [syncRequests])

  // Replaces one key in place with a fresh read. A key no longer in the loaded data (deleted while
  // its read was in flight) is ignored rather than re-added.
  const updateKey = useCallback((updated: SSHKey) => {
    setState((current) => {
      if (current.status !== 'ready') return current
      const { deploymentKey, accessKeys } = current.data
      if (deploymentKey?.id === updated.id) return { ...current, data: { ...current.data, deploymentKey: updated } }
      if (!accessKeys.some((key) => key.id === updated.id)) return current
      return { ...current, data: { ...current.data, accessKeys: accessKeys.map((key) => (key.id === updated.id ? updated : key)) } }
    })
  }, [])

  const visibleKeys = useMemo(
    () => (state.status === 'ready' ? [state.data.deploymentKey, ...state.data.accessKeys].filter((key): key is SSHKey => key !== undefined) : []),
    [state],
  )
  const followedKeyIds = usePendingSyncRefresh(visibleKeys, updateKey)

  const reload = useCallback(() => {
    setState({ status: 'loading' })
    setReloadNonce((value) => value + 1)
  }, [])

  const integrationName = useCallback(
    (integrationId: string) => (state.status === 'ready' ? state.data.integrationNames.get(integrationId) : undefined) ?? integrationId,
    [state],
  )

  const finishDialog = (title: string) => {
    setDialog(null)
    showToast({ tone: 'success', title })
    setReloadNonce((value) => value + 1)
  }

  const requestSync = async () => {
    setSyncing(true)
    try {
      await sshKeys.requestSync()
      showToast({ tone: 'info', title: 'Sync requested', description: 'Provisioner status refreshes in a few seconds.' })
      setSyncRequests((value) => value + 1)
    } catch (caught) {
      showToast({ tone: 'error', title: 'Sync could not be requested', description: caught instanceof Error ? caught.message : undefined })
    } finally {
      setSyncing(false)
    }
  }

  const accessKeys = useMemo(() => (state.status === 'ready' ? state.data.accessKeys : []), [state])

  return (
    <div className="operator-page">
      <PageHeader
        title="SSH keys"
        subtitle="The deployment key swallow uses to manage Servers, and your own keys for logging in to them."
        actions={
          <Button variant="outline" onClick={() => void requestSync()} loading={syncing} disabled={state.status !== 'ready'}>
            <RefreshCw size={16} />
            Sync to provisioners
          </Button>
        }
      />

      {state.status === 'loading' && <LoadingState rows={6} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={reload} />}
      {state.status === 'ready' && (
        <Stack gap="6">
          <SectionSurface
            title="Deployment key"
            description="Generated when swallow was installed. Swallow logs in with it for SSH readiness and Ansible unless a Site overrides the key."
            actions={
              state.data.deploymentKey && (
                <HStack gap="2" wrap="wrap">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => state.data.deploymentKey && setDialog({ kind: 'deployment', mode: 'replace', key: state.data.deploymentKey })}
                  >
                    <Upload size={14} />
                    Replace
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    colorPalette="red"
                    onClick={() => state.data.deploymentKey && setDialog({ kind: 'deployment', mode: 'regenerate', key: state.data.deploymentKey })}
                  >
                    <KeyRound size={14} />
                    Regenerate
                  </Button>
                </HStack>
              )
            }
          >
            {state.data.deploymentKey ? (
              <DeploymentKeyDetails
                sshKey={state.data.deploymentKey}
                integrationName={integrationName}
                following={followedKeyIds.has(state.data.deploymentKey.id)}
              />
            ) : (
              <EmptyState
                title="No deployment key"
                message="It is created during installation. On the installation host run `swallowctl install` or `swallowctl upgrade` (or `swallow-api deployment-key ensure`), then refresh this page. OS and Platform deployment are refused until it exists."
              />
            )}
          </SectionSurface>

          <SectionSurface
            title="Your access keys"
            description="Public keys you log in with. Servers deployed after a key is synced accept it for their default user."
            actions={
              <HStack gap="2" wrap="wrap">
                <Button size="sm" variant="outline" onClick={() => setDialog({ kind: 'import' })}>
                  <Upload size={14} />
                  Import key
                </Button>
                <Button size="sm" colorPalette="brand" onClick={() => setDialog({ kind: 'generate' })}>
                  <FilePlus2 size={14} />
                  Generate key pair
                </Button>
              </HStack>
            }
            flush={accessKeys.length > 0}
          >
            {accessKeys.length === 0 ? (
              <EmptyState
                title="No access keys"
                message="Import a public key you already use, or generate a new key pair."
                action={{ label: 'Import key', onClick: () => setDialog({ kind: 'import' }) }}
              />
            ) : (
              <AccessKeyList
                accessKeys={accessKeys}
                integrationName={integrationName}
                followedKeyIds={followedKeyIds}
                onDelete={(key) => setDialog({ kind: 'delete', key })}
              />
            )}
          </SectionSurface>
        </Stack>
      )}

      {dialog?.kind === 'import' && <ImportSSHKeyDialog onClose={() => setDialog(null)} onImported={finishDialog} />}
      {dialog?.kind === 'generate' && <GenerateSSHKeyDialog onClose={() => setDialog(null)} onGenerated={finishDialog} />}
      {dialog?.kind === 'delete' && <DeleteSSHKeyDialog sshKey={dialog.key} onClose={() => setDialog(null)} onDeleted={finishDialog} />}
      {dialog?.kind === 'deployment' && (
        <DeploymentKeyDialog mode={dialog.mode} current={dialog.key} onClose={() => setDialog(null)} onChanged={finishDialog} />
      )}
    </div>
  )
}

/** The Deployment Key's identity, public key (copyable for manual authorization), and sync rows. */
function DeploymentKeyDetails({
  sshKey,
  integrationName,
  following,
}: {
  sshKey: SSHKey
  integrationName: (id: string) => string
  /** The key has a pending entry that is being re-read. */
  following: boolean
}) {
  return (
    <Stack gap="5">
      <DescriptionList
        items={[
          { label: 'Name', value: sshKey.name },
          { label: 'Type', value: sshKey.keyType },
          { label: 'Fingerprint', value: <KeyValue value={sshKey.fingerprint} copyLabel="Copy fingerprint" /> },
          { label: 'Public key', value: <KeyValue value={sshKey.publicKey} copyLabel="Copy public key" /> },
          { label: 'Last changed', value: formatDateTime(sshKey.updatedAt) },
        ]}
      />
      <Box>
        <Text fontWeight="semibold" mb="2">Provisioners</Text>
        <SSHKeySyncStatus sshKey={sshKey} integrationName={integrationName} variant="list" following={following} />
      </Box>
    </Stack>
  )
}

/** Monospace key material with its copy action kept beside it. */
function KeyValue({ value, copyLabel }: { value: string; copyLabel: string }) {
  return (
    <HStack gap="1" align="flex-start">
      <Text as="span" className="sw-mono" fontSize="sm" wordBreak="break-all">{value}</Text>
      <CopyButton value={value} label={copyLabel} />
    </HStack>
  )
}

/**
 * The caller's Access Keys as a table on desktop and cards on mobile, both built from the same
 * rows and callbacks. Delete opens a confirmation; nothing is deleted from here directly.
 */
function AccessKeyList({
  accessKeys,
  integrationName,
  followedKeyIds,
  onDelete,
}: {
  accessKeys: SSHKey[]
  integrationName: (id: string) => string
  /** Ids of keys whose pending status is being re-read; their status cells show it. */
  followedKeyIds: ReadonlySet<string>
  onDelete: (key: SSHKey) => void
}) {
  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Access keys">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Name</Table.ColumnHeader>
                <Table.ColumnHeader>Type</Table.ColumnHeader>
                <Table.ColumnHeader>Fingerprint</Table.ColumnHeader>
                <Table.ColumnHeader>Provisioners</Table.ColumnHeader>
                <Table.ColumnHeader>Added</Table.ColumnHeader>
                <Table.ColumnHeader>
                  <VisuallyHidden>Actions</VisuallyHidden>
                </Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {accessKeys.map((key) => (
                <Table.Row key={key.id}>
                  <Table.Cell>
                    <HStack gap="1">
                      <strong>{key.name}</strong>
                      <CopyButton value={key.publicKey} label={`Copy public key of ${key.name}`} />
                    </HStack>
                  </Table.Cell>
                  <Table.Cell>{key.keyType}</Table.Cell>
                  <Table.Cell>
                    <Text as="span" className="sw-mono" fontSize="xs">{key.fingerprint}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <SSHKeySyncStatus sshKey={key} integrationName={integrationName} variant="summary" following={followedKeyIds.has(key.id)} />
                  </Table.Cell>
                  <Table.Cell>{formatDateTime(key.createdAt)}</Table.Cell>
                  <Table.Cell textAlign="end">
                    <Button size="xs" variant="outline" colorPalette="red" onClick={() => onDelete(key)}>
                      Delete
                    </Button>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label="Access keys">
          {accessKeys.map((key) => (
            <ResourceCard
              key={key.id}
              title={key.name}
              description={key.fingerprint}
              status={<SSHKeySyncStatus sshKey={key} integrationName={integrationName} variant="summary" following={followedKeyIds.has(key.id)} />}
              actions={
                <HStack gap="2">
                  <CopyButton value={key.publicKey} label={`Copy public key of ${key.name}`} />
                  <Button size="xs" variant="outline" colorPalette="red" onClick={() => onDelete(key)}>
                    Delete
                  </Button>
                </HStack>
              }
            >
              <ResourceCardField label="Type">{key.keyType}</ResourceCardField>
              <ResourceCardField label="Added">{formatDateTime(key.createdAt)}</ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
