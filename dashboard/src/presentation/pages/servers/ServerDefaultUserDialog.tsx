import { useState } from 'react'
import { Box, Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { defaultUserSourceLabel } from '@/presentation/components/serverSummary/defaultUserLabels'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { DEFAULT_USER_PATTERN, defaultUserSavedToast, deploymentKeyInstallCommand } from './serverDefaultUserPresentation'

interface ServerDefaultUserDialogProps {
  server: Server
  onClose: () => void
  /** Called after the default user was set or cleared, so the caller re-reads the Server. */
  onChanged: () => void
}

/**
 * Sets or clears one Server's Server Default User (decision 045) from the Server detail Summary.
 *
 * The account field starts at the current effective user. The optional password is sent once in the
 * request so api-server can add the Deployment Key to that account; it lives only in this dialog's
 * state, is never shown back, and is dropped when the dialog unmounts. Without a password the
 * operator authorizes the key themselves with the shown command (the Deployment Key's public key is
 * read from the SSH Keys API; if that read fails the command is simply omitted). Saving takes a few
 * seconds because the API proves a Deployment Key login before saving, so dismissal is blocked while
 * it runs and the API's error is shown inline. "Use the OS image default" clears a value set on the
 * Server without contacting the host. A locked Server explains why and disables both writes.
 */
export function ServerDefaultUserDialog({ server, onClose, onChanged }: ServerDefaultUserDialogProps) {
  const { servers, sshKeys } = useApp()
  const { showToast } = useToast()
  const current = server.defaultUser
  const [user, setUser] = useState(current?.user ?? '')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState<'save' | 'clear' | null>(null)
  const [error, setError] = useState('')
  const deploymentKey = useAsyncData(
    async () => (await sshKeys.listSSHKeys()).find((key) => key.purpose === 'deployment') ?? null,
    [sshKeys],
  )
  const name = serverDisplayName(server)
  const locked = server.provisioning?.locked ?? false
  const trimmed = user.trim()
  const valid = DEFAULT_USER_PATTERN.test(trimmed)

  const save = async () => {
    if (!valid || busy || locked) return
    setBusy('save')
    setError('')
    try {
      const result = await servers.setDefaultUser(server.id, password ? { user: trimmed, password } : { user: trimmed })
      showToast(defaultUserSavedToast(result))
      onChanged()
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The default user could not be set.')
    } finally {
      setBusy(null)
    }
  }

  const clear = async () => {
    if (busy || locked) return
    setBusy('clear')
    setError('')
    try {
      await servers.clearDefaultUser(server.id)
      showToast({ tone: 'success', title: 'Default user reset', description: `${name} uses its OS image's default user again.` })
      onChanged()
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The default user could not be reset.')
    } finally {
      setBusy(null)
    }
  }

  const command = deploymentKey.status === 'ready' && deploymentKey.data ? deploymentKeyInstallCommand(deploymentKey.data.publicKey) : ''

  return (
    <Modal
      open
      onClose={() => !busy && onClose()}
      closeOnInteractOutside={!busy}
      title="Default user"
      description={`The account swallow logs in to ${name} as with its Deployment Key. Docker CE adds it to the docker group.`}
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
      footer={
        <>
          {current?.source === 'server' && (
            <Button variant="outline" me="auto" onClick={() => void clear()} loading={busy === 'clear'} disabled={busy !== null || locked}>
              Use the OS image default
            </Button>
          )}
          <Button variant="ghost" onClick={onClose} disabled={busy !== null}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={busy === 'save'} loadingText="Verifying…" disabled={!valid || busy !== null || locked}>
            Save
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {locked && (
          <Alert status="warning" title="Read-only while locked">
            {name} is locked. Unlock it to change its default user.
          </Alert>
        )}
        {error && (
          <Alert status="error" title="The default user was not changed">
            {error}
          </Alert>
        )}
        {current && (
          <Text color="fg.muted" fontSize="sm">
            Currently <span className="sw-mono">{current.user}</span>, {defaultUserSourceLabel(current.source)}.
          </Text>
        )}
        <Field.Root required invalid={trimmed !== '' && !valid}>
          <Field.Label>
            Account <Field.RequiredIndicator />
          </Field.Label>
          <Input value={user} onChange={(event) => setUser(event.target.value)} className="sw-mono" autoComplete="off" autoFocus disabled={busy !== null} />
          <Field.HelperText>A login name on this host, for example amd or ubuntu.</Field.HelperText>
          <Field.ErrorText>Use lowercase letters, digits, _ or -, starting with a letter or _ (up to 32 characters).</Field.ErrorText>
        </Field.Root>
        <Field.Root>
          <Field.Label>Password (optional)</Field.Label>
          {/* new-password keeps browsers from offering the operator's own swallow password here. */}
          <Input type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" disabled={busy !== null} />
          <Field.HelperText>
            Used once to add the Deployment Key to this account&apos;s authorized_keys; swallow does not store it. Leave it empty if
            the key is already there.
          </Field.HelperText>
        </Field.Root>
        {command && (
          <Box>
            <Text fontSize="sm" mb="1">
              Or add the key yourself by running this as the account on the host:
            </Text>
            <HStack gap="2" bg="bg.muted" rounded="md" ps="3" pe="1" py="1" justify="space-between" align="start">
              <Text as="code" className="sw-mono" fontSize="xs" wordBreak="break-all">
                {command}
              </Text>
              <CopyButton value={command} label="Copy the key install command" />
            </HStack>
          </Box>
        )}
        {deploymentKey.status === 'ready' && !deploymentKey.data && (
          <Alert status="warning" title="No Deployment Key">
            The installation has no Deployment Key, so swallow cannot log in to any host yet.
          </Alert>
        )}
      </Stack>
    </Modal>
  )
}
