import { useState } from 'react'
import { Button, Field, HStack, List, Spinner, Stack, Text, Textarea } from '@chakra-ui/react'
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

/** Submit progress so the primary button and body can explain the current phase. */
type ReleasePhase = 'idle' | 'canceling' | 'releasing'

/**
 * Confirms a single or multi-Server release and makes MAAS disk-erasure semantics explicit
 * before any provider request is sent.
 *
 * A release is rejected while a target still has running durable Operations, so the dialog
 * detects that work on open and, when the operator opts in, cancels it and waits for the
 * targets to clear before releasing. The opt-in keeps the default path unchanged: leaving the
 * box unchecked releases exactly as before and surfaces the backend's conflict message.
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
  const [phase, setPhase] = useState<ReleasePhase>('idle')
  const [cancelRunning, setCancelRunning] = useState(false)
  const [error, setError] = useState('')

  const { activeOperations, cancelAll } = useServerActiveOperations(targets.map((target) => target.serverId))

  const multiple = targets.length > 1
  const title = multiple ? `Release ${targets.length} servers` : 'Release server'
  const shownNames = targets.slice(0, 5).map((target) => target.serverName)
  const remaining = targets.length - shownNames.length
  const hasActiveOperations = activeOperations.length > 0

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (submitting || targets.length === 0) return
    setSubmitting(true)
    setError('')
    try {
      if (cancelRunning && hasActiveOperations) {
        setPhase('canceling')
        await cancelAll()
      }
      setPhase('releasing')
      await onRelease({
        erase: supportsReleaseOptions && options.erase,
        secureErase: supportsReleaseOptions && options.erase && options.secureErase,
        quickErase: supportsReleaseOptions && options.erase && options.quickErase,
        comment: supportsReleaseOptions ? comment.trim() || undefined : undefined,
        unbindStaticIPs: supportsNetworkConfiguration && options.unbindStaticIPs,
      })
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not release the selected Server.')
    } finally {
      setSubmitting(false)
      setPhase('idle')
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
            title={activeOperations.length === 1 ? 'This server has a running operation' : `This server has ${activeOperations.length} running operations`}
          >
            <Stack gap="2">
              <Text>A release is rejected while durable work is still running. Cancel the running operations first, or wait for them to finish.</Text>
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
        {phase === 'canceling' && (
          <HStack gap="2">
            <Spinner size="sm" aria-hidden />
            <Text>Canceling running operations. This can take a moment...</Text>
          </HStack>
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
