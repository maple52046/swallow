import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Checkbox,
  FormGroup,
  List,
  ListItem,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Spinner,
  Stack,
  StackItem,
  TextArea,
} from '@patternfly/react-core'
import type { ReleaseServerInput } from '@/domain/server/types'
import { ReleaseOptionsFields } from '@/presentation/components/ReleaseOptionsFields'
import {
  emptyReleaseOptions,
  type ReleaseOptionsValue,
} from '@/presentation/components/releaseOptions'
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
 * Confirms a single or multi-Server release and makes MAAS disk-erasure semantics
 * explicit before any provider request is sent.
 *
 * A release is rejected while a target still has running durable Operations, so the dialog
 * detects that work on open and, when the operator opts in, cancels it and waits for the
 * targets to clear before releasing. The opt-in keeps the default path unchanged: leaving
 * the box unchecked releases exactly as before and surfaces the backend's conflict message
 * if the Server really is busy.
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

  const { activeOperations, cancelAll } =
    useServerActiveOperations(targets.map((target) => target.serverId))

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
      isOpen
      onClose={close}
      // Widen on desktop when the erase/comment options are shown so the content uses
      // horizontal space; PatternFly keeps a medium modal near-full-width on phones.
      variant={supportsReleaseOptions ? 'medium' : 'small'}
      aria-labelledby="server-release-title"
    >
      <ModalHeader
        title={title}
        labelId="server-release-title"
        description="Release returns the machine to the provisioner's available pool and removes its deployed OS assignment."
      />
      <ModalBody>
        <Stack hasGutter>
          {error && (
            <StackItem>
              <Alert variant={AlertVariant.danger} title="Release could not be started" isInline>
                {error}
              </Alert>
            </StackItem>
          )}
          <StackItem>
            <strong>Targets:</strong> {shownNames.join(', ')}{remaining > 0 ? ` and ${remaining} more` : ''}
          </StackItem>
          {hasActiveOperations && (
            <StackItem>
              <Alert
                variant={AlertVariant.warning}
                isInline
                title={
                  activeOperations.length === 1
                    ? 'This server has a running operation'
                    : `This server has ${activeOperations.length} running operations`
                }
              >
                <Stack hasGutter>
                  <StackItem>
                    A release is rejected while durable work is still running. Cancel the
                    running operations first, or wait for them to finish.
                  </StackItem>
                  <StackItem>
                    <List aria-label="Running operations blocking release">
                      {activeOperations.map((operation) => (
                        <ListItem key={operation.id}>
                          {operation.intent} — {operation.status}
                        </ListItem>
                      ))}
                    </List>
                  </StackItem>
                  <StackItem>
                    <Checkbox
                      id="release-cancel-running"
                      label="Cancel running operations before releasing"
                      isChecked={cancelRunning}
                      isDisabled={submitting}
                      onChange={(_event, checked) => setCancelRunning(checked)}
                    />
                  </StackItem>
                </Stack>
              </Alert>
            </StackItem>
          )}
          {phase === 'canceling' && (
            <StackItem>
              <Spinner size="md" aria-hidden />{' '}
              Canceling running operations. This can take a moment...
            </StackItem>
          )}
          <StackItem>
            <ReleaseOptionsFields
              idPrefix="release"
              value={options}
              onChange={setOptions}
              supportsReleaseOptions={supportsReleaseOptions}
              supportsNetworkConfiguration={supportsNetworkConfiguration}
            />
          </StackItem>
          {supportsReleaseOptions && (
            <StackItem>
              <FormGroup label="Provider event comment (optional)" fieldId="release-comment">
                <TextArea
                  id="release-comment"
                  value={comment}
                  onChange={(_event, value) => setComment(value)}
                  rows={3}
                  resizeOrientation="vertical"
                />
              </FormGroup>
            </StackItem>
          )}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button
          variant="danger"
          onClick={() => void submit()}
          isLoading={submitting}
          isDisabled={submitting || targets.length === 0}
        >
          {title}
        </Button>
        <Button variant="link" onClick={close} isDisabled={submitting}>Cancel</Button>
      </ModalFooter>
    </Modal>
  )
}
