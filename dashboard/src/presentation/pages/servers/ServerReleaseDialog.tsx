import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  FormGroup,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
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
import type { ServerActionTarget } from './serverActionResults'

interface ServerReleaseDialogProps {
  targets: readonly ServerActionTarget[]
  supportsReleaseOptions?: boolean
  supportsNetworkConfiguration?: boolean
  onClose: () => void
  onRelease: (input: ReleaseServerInput) => Promise<void>
}

/**
 * Confirms a single or multi-Server release and makes MAAS disk-erasure semantics
 * explicit before any provider request is sent.
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
  const [error, setError] = useState('')

  const multiple = targets.length > 1
  const title = multiple ? `Release ${targets.length} servers` : 'Release server'
  const shownNames = targets.slice(0, 5).map((target) => target.serverName)
  const remaining = targets.length - shownNames.length

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
