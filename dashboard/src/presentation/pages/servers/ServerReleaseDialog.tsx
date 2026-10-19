import { useState } from 'react'
import { Button, Field, List, Stack, Text, Textarea } from '@chakra-ui/react'
import type { ReleaseServerInput } from '@/domain/server/types'
import { ReleaseOptionsFields } from '@/presentation/components/ReleaseOptionsFields'
import { emptyReleaseOptions, type ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { useServerActiveOperations } from './useServerActiveOperations'
import type { ServerActionTarget } from './serverActionResults'

interface ServerReleaseDialogProps {
  targets: readonly ServerActionTarget[]
  supportsReleaseOptions?: boolean
  supportsNetworkConfiguration?: boolean
  onClose: () => void
  onRelease: (input: ReleaseServerInput) => Promise<void>
}

/**
 * Confirms a single or multi-Server release and makes MAAS disk-erasure semantics explicit
 * before any provider request is sent.
 *
 * A release is rejected while a target still has running durable Operations, so the dialog
 * detects that work on open. When the operator opts in, the release request asks the backend to
 * cancel that work (`cancelActiveWorkflows`, decision 061): the Release Operation is accepted at
 * once and starts by itself after the canceled work ends, so the dialog closes within the request
 * instead of polling for minutes with every control disabled. Leaving the box unchecked releases
 * exactly as before and surfaces the backend's conflict message; a cancel the backend refuses is
 * shown the same way.
 */
export function ServerReleaseDialog({
  targets,
  supportsReleaseOptions = true,
  supportsNetworkConfiguration = true,
  onClose,
  onRelease,
}: ServerReleaseDialogProps) {
  const [options, setOptions] = useState<ReleaseOptionsValue>(emptyReleaseOptions)
  const [comment, setComment] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [cancelRunning, setCancelRunning] = useState(false)
  const [error, setError] = useState('')

  const { activeOperations } = useServerActiveOperations(targets.map((target) => target.serverId))

  const multiple = targets.length > 1
  const title = multiple ? `Release ${targets.length} servers` : 'Release server'
  const shownNames = targets.slice(0, 5).map((target) => target.serverName)
  const remaining = targets.length - shownNames.length
  const hasActiveOperations = activeOperations.length > 0
  const holder = multiple ? 'These servers have' : 'This server has'
  const activeOperationsTitle = activeOperations.length === 1
    ? `${holder} a running operation`
    : `${holder} ${activeOperations.length} running operations`

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (submitting || targets.length === 0) return
    setSubmitting(true)
    setError('')
    try {
      await onRelease({
        erase: supportsReleaseOptions && options.erase,
        secureErase: supportsReleaseOptions && options.erase && options.secureErase,
        quickErase: supportsReleaseOptions && options.erase && options.quickErase,
        comment: supportsReleaseOptions ? comment.trim() || undefined : undefined,
        unbindStaticIPs: supportsNetworkConfiguration && options.unbindStaticIPs,
        cancelActiveWorkflows: cancelRunning && hasActiveOperations ? true : undefined,
      })
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not release the selected Server.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      size={supportsReleaseOptions ? 'lg' : 'md'}
      closeOnInteractOutside={!submitting}
      title={title}
      description="Release returns the machine to the provisioner's available pool and removes its deployed OS assignment."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="red" loading={submitting} disabled={submitting || targets.length === 0}>
            {title}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Release could not be started">
            {error}
          </Alert>
        )}
        <Text>
          <strong>Targets:</strong> {shownNames.join(', ')}
          {remaining > 0 ? ` and ${remaining} more` : ''}
        </Text>
        {hasActiveOperations && (
          <Alert
            status="warning"
            title={activeOperationsTitle}
          >
            <Stack gap="2">
              <Text>
                A release is rejected while durable work is still running. Check the box to cancel it: the Release is
                accepted now and starts by itself once the canceled work has ended, which can take a few minutes.
              </Text>
              <List.Root aria-label="Running operations blocking release">
                {activeOperations.map((operation) => (
                  <List.Item key={operation.id}>
                    {operation.intent} — {operation.status}
                  </List.Item>
                ))}
              </List.Root>
              <Checkbox id="release-cancel-running" checked={cancelRunning} disabled={submitting} onCheckedChange={setCancelRunning}>
                Cancel running operations before releasing
              </Checkbox>
            </Stack>
          </Alert>
        )}
        <ReleaseOptionsFields
          idPrefix="release"
          value={options}
          onChange={setOptions}
          supportsReleaseOptions={supportsReleaseOptions}
          supportsNetworkConfiguration={supportsNetworkConfiguration}
        />
        {supportsReleaseOptions && (
          <Field.Root>
            <Field.Label>Provider event comment (optional)</Field.Label>
            <Textarea value={comment} onChange={(event) => setComment(event.target.value)} rows={3} />
          </Field.Root>
        )}
      </Stack>
    </Modal>
  )
}
