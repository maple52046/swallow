import { useCallback, useEffect, useState } from 'react'
import { Badge, Button, HStack, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { FilePlus2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { isApiKeyExpired, type ApiKey } from '@/domain/access/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionSurface, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { useToast } from '@/presentation/components/toast/toastContext'
import { formatDateTime } from '@/shared/utils/time'
import { apiKeyExpiryLabel, apiKeyLastUsedLabel } from './apiKeyPresentation'
import { CreateApiKeyDialog } from './CreateApiKeyDialog'
import { DeleteApiKeyDialog } from './DeleteApiKeyDialog'

type PageState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; keys: ApiKey[] }

/** Which dialog is open; at most one at a time. */
type DialogState = { kind: 'create' } | { kind: 'delete'; key: ApiKey } | null

/**
 * The signed-in admin's API Keys (route `/account/api-keys`, decision 042): long-lived secrets
 * that let scripts, CI, and the CLI call Swallow as this user without a password.
 *
 * Data comes from the `api-keys.md` contract through the ApiKeyRepository port; the list is read
 * once per visit and after each action. No secret is ever loaded here — the only secret the page
 * handles is the one-time create result, owned by CreateApiKeyDialog. Expiry is computed against
 * the time of the last read, so an expired key is labelled in words. Every endpoint requires
 * `admin`; a non-admin reaching this route sees the API's error state.
 */
export function ApiKeysPage() {
  const { apiKeys } = useApp()
  const { showToast } = useToast()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [dialog, setDialog] = useState<DialogState>(null)
  const [reloadNonce, setReloadNonce] = useState(0)
  // Captured with each read rather than during render, so rendering stays pure.
  const [readAt, setReadAt] = useState(0)

  // The `cancelled` flag drops a slower, older response so it cannot overwrite a newer one.
  useEffect(() => {
    let cancelled = false
    void apiKeys.listApiKeys()
      .then((keys) => {
        if (cancelled) return
        setReadAt(Date.now())
        setState({ status: 'ready', keys })
      })
      .catch((caught: unknown) => {
        if (!cancelled) setState({ status: 'error', message: caught instanceof Error ? caught.message : 'Could not load API keys.' })
      })
    return () => {
      cancelled = true
    }
  }, [apiKeys, reloadNonce])

  const reload = useCallback(() => {
    setState({ status: 'loading' })
    setReloadNonce((value) => value + 1)
  }, [])

  const finishDialog = (title: string) => {
    setDialog(null)
    showToast({ tone: 'success', title })
    setReloadNonce((value) => value + 1)
  }

  const createAction = (
    <Button colorPalette="brand" onClick={() => setDialog({ kind: 'create' })} disabled={state.status !== 'ready'}>
      <FilePlus2 size={16} />
      Create API key
    </Button>
  )

  return (
    <div className="operator-page">
      <PageHeader
        title="API keys"
        subtitle="Keys that let scripts, CI, and the CLI call Swallow as you without a password."
        actions={createAction}
      />

      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={reload} />}
      {state.status === 'ready' && (
        <SectionSurface
          title="Your API keys"
          description="A key acts with your permissions until it expires or you delete it. Its secret was shown once, at creation."
          flush={state.keys.length > 0}
        >
          {state.keys.length === 0 ? (
            <EmptyState
              title="No API keys"
              message="Create a key for the CLI or a CI job. Signing in with your password keeps working too."
              action={{ label: 'Create API key', onClick: () => setDialog({ kind: 'create' }) }}
            />
          ) : (
            <ApiKeyList keys={state.keys} now={readAt} onDelete={(key) => setDialog({ kind: 'delete', key })} />
          )}
        </SectionSurface>
      )}

      {dialog?.kind === 'create' && <CreateApiKeyDialog onClose={() => setDialog(null)} onCreated={finishDialog} />}
      {dialog?.kind === 'delete' && <DeleteApiKeyDialog apiKey={dialog.key} onClose={() => setDialog(null)} onDeleted={finishDialog} />}
    </div>
  )
}

/** Expiry text with an "expired" badge, so state is conveyed by words as well as colour. */
function ExpiryValue({ apiKey, now }: { apiKey: ApiKey; now: number }) {
  const label = apiKeyExpiryLabel(apiKey, now)
  if (!isApiKeyExpired(apiKey, now)) return <>{label}</>
  return (
    <HStack gap="2">
      <Badge colorPalette="red" variant="subtle">expired</Badge>
      <Text as="span">{label}</Text>
    </HStack>
  )
}

/**
 * The caller's API Keys as a table on desktop and cards on mobile, both built from the same rows
 * and callbacks. Delete opens a confirmation; nothing is deleted from here directly.
 */
function ApiKeyList({ keys, now, onDelete }: { keys: ApiKey[]; now: number; onDelete: (key: ApiKey) => void }) {
  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="API keys">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Name</Table.ColumnHeader>
                <Table.ColumnHeader>Key</Table.ColumnHeader>
                <Table.ColumnHeader>Created</Table.ColumnHeader>
                <Table.ColumnHeader>Expires</Table.ColumnHeader>
                <Table.ColumnHeader>Last used</Table.ColumnHeader>
                <Table.ColumnHeader>
                  <VisuallyHidden>Actions</VisuallyHidden>
                </Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {keys.map((key) => (
                <Table.Row key={key.id}>
                  <Table.Cell><strong>{key.name}</strong></Table.Cell>
                  <Table.Cell>
                    <Text as="span" className="sw-mono" fontSize="xs">{key.prefix}…</Text>
                  </Table.Cell>
                  <Table.Cell>{formatDateTime(key.createdAt)}</Table.Cell>
                  <Table.Cell><ExpiryValue apiKey={key} now={now} /></Table.Cell>
                  <Table.Cell>{apiKeyLastUsedLabel(key)}</Table.Cell>
                  <Table.Cell textAlign="end">
                    <Button size="xs" variant="outline" colorPalette="red" onClick={() => onDelete(key)} aria-label={`Delete ${key.name}`}>
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
        <div className="sw-resource-card-list" aria-label="API keys">
          {keys.map((key) => (
            <ResourceCard
              key={key.id}
              title={key.name}
              description={`${key.prefix}…`}
              actions={
                <Button size="xs" variant="outline" colorPalette="red" onClick={() => onDelete(key)} aria-label={`Delete ${key.name}`}>
                  Delete
                </Button>
              }
            >
              <ResourceCardField label="Created">{formatDateTime(key.createdAt)}</ResourceCardField>
              <ResourceCardField label="Expires"><ExpiryValue apiKey={key} now={now} /></ResourceCardField>
              <ResourceCardField label="Last used">{apiKeyLastUsedLabel(key)}</ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
