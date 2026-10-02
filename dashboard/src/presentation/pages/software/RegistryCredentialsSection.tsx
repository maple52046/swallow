import { useState } from 'react'
import { Badge, Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { KeyRound, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import { DOCKER_HUB_REGISTRY, normalizeRegistryInput, type RegistryCredential } from '@/domain/software/docker'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { ResponsiveResourceList } from '@/presentation/components/ResponsiveResourceList'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'

/** Which dialog is open; at most one at a time. */
type DialogState = { kind: 'create' } | { kind: 'replace'; credential: RegistryCredential } | { kind: 'delete'; credential: RegistryCredential } | null

/**
 * Docker CE's Registry credentials, shown in the Docker CE group of Software settings (decision
 * 044): the installation-wide, sealed credentials the Server Containers tab uses to pull private
 * images.
 *
 * A credential is keyed by registry host, so this section lists one row per registry with its
 * username and last change. Passwords are typed into the create/replace dialog, sent once, and never
 * shown again — no response carries them. Changes never contact a registry or a host; a wrong
 * credential surfaces on the next pull with the registry's reason. The list is read on mount and
 * after every change; the API error is shown inline and the section never blocks the rest of the
 * page.
 */
export function RegistryCredentialsSection() {
  const { registryCredentials } = useApp()
  const { showToast } = useToast()
  const credentials = useAsyncData(() => registryCredentials.list(), [registryCredentials])
  const [dialog, setDialog] = useState<DialogState>(null)
  const [deleting, setDeleting] = useState(false)

  const remove = async (credential: RegistryCredential) => {
    setDeleting(true)
    try {
      await registryCredentials.remove(credential.id)
      showToast({ tone: 'success', title: 'Registry credential deleted', description: credential.registry })
      credentials.reload()
      setDialog(null)
    } catch (caught) {
      showToast({ tone: 'error', title: 'Registry credential could not be deleted', description: caught instanceof Error ? caught.message : undefined })
    } finally {
      setDeleting(false)
    }
  }

  return (
    <SectionSurface
      title="Registry credentials"
      description="Saved sign-ins for pulling private images from a Server's Containers tab. A pull uses the credential of the registry in its image reference; without one it is anonymous."
      actions={
        <HStack gap="2">
          <Button size="sm" colorPalette="brand" onClick={() => setDialog({ kind: 'create' })}>
            <Plus size={16} aria-hidden /> Add credential
          </Button>
          <Button variant="plain" size="sm" onClick={credentials.reload}>
            <RefreshCw size={16} aria-hidden /> Refresh
          </Button>
        </HStack>
      }
    >
      <AsyncSection
        state={credentials}
        unavailableTitle="Registry credentials are unavailable"
        unavailableHint="Try again once the API is reachable."
      >
        {(items) =>
          items.length === 0 ? (
            <EmptyState
              title="No registry credentials"
              message="Pulls are anonymous. Add a credential to pull private images from a registry."
              icon={<KeyRound />}
            />
          ) : (
            <ResponsiveResourceList
              label="Registry credentials"
              items={items}
              rowKey={(credential) => credential.id}
              identityHeader="Registry"
              title={(credential) => (
                <HStack gap="2" wrap="wrap">
                  <span className="mono">{credential.registry}</span>
                  {credential.registry === DOCKER_HUB_REGISTRY && <Badge colorPalette="gray" variant="subtle">Docker Hub</Badge>}
                </HStack>
              )}
              columns={[
                { header: 'Username', cell: (credential) => <span className="mono">{credential.username}</span> },
                {
                  header: 'Updated',
                  cell: (credential) => (
                    <span title={formatDateTime(credential.updatedAt)}>
                      {formatRelative(credential.updatedAt)}
                      {credential.updatedBy ? ` by ${credential.updatedBy}` : ''}
                    </span>
                  ),
                },
              ]}
              actions={(credential) => (
                <>
                  <Button
                    size="sm"
                    variant="outline"
                    aria-label={`Replace credential for ${credential.registry}`}
                    onClick={() => setDialog({ kind: 'replace', credential })}
                  >
                    <KeyRound size={16} aria-hidden /> Replace
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    colorPalette="red"
                    aria-label={`Delete credential for ${credential.registry}`}
                    onClick={() => setDialog({ kind: 'delete', credential })}
                  >
                    <Trash2 size={16} aria-hidden /> Delete
                  </Button>
                </>
              )}
            />
          )
        }
      </AsyncSection>

      {(dialog?.kind === 'create' || dialog?.kind === 'replace') && (
        <RegistryCredentialDialog
          credential={dialog.kind === 'replace' ? dialog.credential : null}
          onClose={() => setDialog(null)}
          onSaved={() => {
            setDialog(null)
            credentials.reload()
          }}
        />
      )}
      <ConfirmDialog
        open={dialog?.kind === 'delete'}
        title="Delete registry credential"
        confirmLabel="Delete credential"
        busy={deleting}
        onCancel={() => setDialog(null)}
        onConfirm={() => {
          if (dialog?.kind === 'delete') void remove(dialog.credential)
        }}
      >
        <Text>
          Pulls from <strong className="mono">{dialog?.kind === 'delete' ? dialog.credential.registry : ''}</strong> become
          anonymous, so private images there can no longer be pulled. Images already on hosts are not affected.
        </Text>
      </ConfirmDialog>
    </SectionSurface>
  )
}

/**
 * Create or replace one credential. Creating asks for the registry; replacing keeps it fixed (the
 * contract cannot change it) and requires a new password, because the stored one is never sent back
 * to pre-fill. The password field uses `new-password` autocomplete so browsers do not offer the
 * operator's own swallow password for a registry. Errors from the API (an invalid host, a duplicate
 * registry) are shown inline; dismissal is blocked while saving.
 */
function RegistryCredentialDialog({
  credential,
  onClose,
  onSaved,
}: {
  credential: RegistryCredential | null
  onClose: () => void
  onSaved: () => void
}) {
  const { registryCredentials } = useApp()
  const { showToast } = useToast()
  const [registry, setRegistry] = useState(credential?.registry ?? '')
  const [username, setUsername] = useState(credential?.username ?? '')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const replacing = credential !== null
  const valid = (replacing || registry.trim() !== '') && username.trim() !== '' && password !== ''
  const storedRegistry = normalizeRegistryInput(registry)

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const saved = credential
        ? await registryCredentials.replace(credential.id, { username: username.trim(), password })
        : await registryCredentials.create({ registry: registry.trim(), username: username.trim(), password })
      showToast({ tone: 'success', title: replacing ? 'Registry credential replaced' : 'Registry credential saved', description: saved.registry })
      onSaved()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The credential could not be saved.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => !submitting && onClose()}
      closeOnInteractOutside={!submitting}
      title={replacing ? `Replace credential for ${credential.registry}` : 'Add registry credential'}
      description="The password is stored encrypted and is never shown again. A registry access token works in place of a password."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!valid || submitting}>
            {replacing ? 'Replace' : 'Save'}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The credential could not be saved">
            {error}
          </Alert>
        )}
        <Field.Root required={!replacing} disabled={replacing}>
          <Field.Label>
            Registry {!replacing && <Field.RequiredIndicator />}
          </Field.Label>
          <Input
            value={registry}
            onChange={(event) => setRegistry(event.target.value)}
            placeholder="harbor.example.com"
            className="mono"
            autoFocus={!replacing}
            readOnly={replacing}
          />
          <Field.HelperText>
            {replacing ? (
              'The registry of a credential cannot change; delete it and add a new one instead.'
            ) : storedRegistry === '' ? (
              'Host with an optional port, as it appears in image references. Use docker.io for Docker Hub.'
            ) : storedRegistry === DOCKER_HUB_REGISTRY ? (
              <>
                Saved as <span className="mono">docker.io</span> (Docker Hub): used for images without a registry host, such as{' '}
                <span className="mono">team/app</span> or <span className="mono">nginx</span>.
              </>
            ) : (
              <>
                Saved as <span className="mono">{storedRegistry}</span>: used only for images named{' '}
                <span className="mono">{storedRegistry}/…</span>. Use docker.io for Docker Hub.
              </>
            )}
          </Field.HelperText>
        </Field.Root>
        <Field.Root required>
          <Field.Label>
            Username <Field.RequiredIndicator />
          </Field.Label>
          <Input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="off" autoFocus={replacing} />
        </Field.Root>
        <Field.Root required>
          <Field.Label>
            {replacing ? 'New password or token' : 'Password or token'} <Field.RequiredIndicator />
          </Field.Label>
          <Input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
