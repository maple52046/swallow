import { useState } from 'react'
import { Stack, Switch, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { ErrorState } from '@/presentation/components/ErrorState'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'

/**
 * The installation's access settings on the Users page (access.md, admins only).
 *
 * Whether admins' sudo asks for a password is the only writable setting; turning it on or off is a
 * change to who can become root without re-entering a password, so the switch asks for
 * confirmation before it writes. The POSIX id start is shown read-only: it was fixed at
 * installation and cannot change once ids were handed out. `onChanged` lets the page refresh its
 * audit list after a write.
 */
export function AccessSettingsSection({ onChanged }: { onChanged: () => void }) {
  const { accounts } = useApp()
  const { showToast } = useToast()
  const settings = useAsyncData(() => accounts.getAccessSettings(), [accounts], { unavailableStatuses: [] })
  const [pending, setPending] = useState<boolean | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const save = async (adminSudoPasswordless: boolean) => {
    setSaving(true)
    setError(null)
    try {
      await accounts.setAccessSettings({ adminSudoPasswordless })
      setPending(null)
      showToast({ tone: 'success', title: 'Access settings saved' })
      settings.reload()
      onChanged()
    } catch (caught) {
      setError(failureOf(caught, 'The access settings could not be saved.'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <SectionSurface title="Access settings" description="Installation-wide rules that apply to every admin.">
      {settings.status === 'loading' && <LoadingState rows={2} />}
      {(settings.status === 'error' || settings.status === 'unavailable') && (
        <ErrorState message={settings.message} requestId={settings.status === 'error' ? settings.requestId : undefined} onRetry={settings.reload} />
      )}
      {settings.status === 'ready' && (
        <Stack gap="4">
          {error && (
            <Alert status="error" title="Settings not saved">
              <FailureMessage failure={error} />
            </Alert>
          )}
          <Switch.Root
            checked={settings.data.adminSudoPasswordless}
            onCheckedChange={(details) => setPending(details.checked === true)}
            disabled={saving}
          >
            <Switch.HiddenInput />
            <Switch.Control><Switch.Thumb /></Switch.Control>
            <Switch.Label>
              Admins use sudo without a password: {settings.data.adminSudoPasswordless ? 'on' : 'off'}
            </Switch.Label>
          </Switch.Root>
          <DescriptionList items={[{ label: 'First uid and gid', value: String(settings.data.posixIdStart) }]} />
          <Text color="fg.muted" fontSize="sm">
            The id range was chosen at installation and cannot change after the first account received an id.
          </Text>
        </Stack>
      )}
      <ConfirmDialog
        open={pending !== null}
        title={pending ? 'Let admins use sudo without a password?' : 'Make admins enter a password for sudo?'}
        confirmLabel={pending ? 'Turn on' : 'Turn off'}
        destructive={false}
        busy={saving}
        onConfirm={() => pending !== null && void save(pending)}
        onCancel={() => setPending(null)}
      >
        <Text>
          {pending
            ? 'Every admin can become root on every Server without typing their password. The change reaches the Servers within a minute.'
            : 'Every admin must type their Swallow password for sudo on every Server. Admins who sign in only with SSH keys still need that password. The change reaches the Servers within a minute.'}
        </Text>
      </ConfirmDialog>
    </SectionSurface>
  )
}
