import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Checkbox,
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
  const [erase, setErase] = useState(false)
  const [secureErase, setSecureErase] = useState(false)
  const [quickErase, setQuickErase] = useState(false)
  const [comment, setComment] = useState('')
  const [unbindStaticIPs, setUnbindStaticIPs] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const multiple = targets.length > 1
  const title = multiple ? `Release ${targets.length} servers` : 'Release server'
  const shownNames = targets.slice(0, 5).map((target) => target.serverName)
  const remaining = targets.length - shownNames.length

  const close = () => {
    if (!submitting) onClose()
  }

  const changeErase = (checked: boolean) => {
    setErase(checked)
    if (!checked) {
      setSecureErase(false)
      setQuickErase(false)
    }
  }

  const submit = async () => {
    if (submitting || targets.length === 0) return
    setSubmitting(true)
    setError('')
    try {
      await onRelease({
        erase: supportsReleaseOptions && erase,
        secureErase: supportsReleaseOptions && erase && secureErase,
        quickErase: supportsReleaseOptions && erase && quickErase,
        comment: supportsReleaseOptions ? comment.trim() || undefined : undefined,
        unbindStaticIPs: supportsNetworkConfiguration && unbindStaticIPs,
      })
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not release the selected Server.')
    } finally {
      setSubmitting(false)
    }
  }

  let eraseExplanation = 'Disk erasure is off. The provisioner will release the machine without wiping its disks.'
  if (erase && secureErase && quickErase) {
    eraseExplanation = 'Secure erase is preferred. MAAS will use quick erase only when secure erase is unavailable.'
  } else if (erase && secureErase) {
    eraseExplanation = 'MAAS will request hardware secure erase. A disk without secure erase support can cause the release to fail.'
  } else if (erase && quickErase) {
    eraseExplanation = 'Quick erase only wipes the beginning and end of each disk. It is faster, but it is not a secure wipe.'
  } else if (erase) {
    eraseExplanation = 'Full erase writes zeroes to every disk and can take a long time.'
  }

  return (
    <Modal isOpen onClose={close} variant="small" aria-labelledby="server-release-title">
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
          {supportsReleaseOptions ? (
            <>
              <StackItem>
                <Checkbox
                  id="release-erase-disks"
                  label="Erase disks before release"
                  isChecked={erase}
                  onChange={(_event, checked) => changeErase(checked)}
                />
              </StackItem>
              <StackItem className="sw-release-suboptions">
                <Stack hasGutter>
                  <StackItem>
                    <Checkbox
                      id="release-secure-erase"
                      label="Use secure erase when supported"
                      isChecked={secureErase}
                      isDisabled={!erase}
                      onChange={(_event, checked) => setSecureErase(checked)}
                    />
                  </StackItem>
                  <StackItem>
                    <Checkbox
                      id="release-quick-erase"
                      label={secureErase ? 'Use quick erase if secure erase is unavailable' : 'Use quick erase'}
                      isChecked={quickErase}
                      isDisabled={!erase}
                      onChange={(_event, checked) => setQuickErase(checked)}
                    />
                  </StackItem>
                </Stack>
              </StackItem>
              <StackItem>
                <Alert
                  variant={erase ? AlertVariant.warning : AlertVariant.info}
                  title={erase ? 'Disk erasure enabled' : 'Disk contents will be retained'}
                  isInline
                >
                  {eraseExplanation}
                </Alert>
              </StackItem>
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
            </>
          ) : (
            <StackItem>
              <Alert variant={AlertVariant.info} title="Disk erasure options unavailable" isInline>
                This provisioner can release the machine but does not expose configurable disk erasure through Swallow.
              </Alert>
            </StackItem>
          )}
          <StackItem>
            <Checkbox
              id="release-unbind-static-ips"
              label="Remove static IP bindings after release"
              isChecked={unbindStaticIPs}
              isDisabled={!supportsNetworkConfiguration}
              onChange={(_event, checked) => setUnbindStaticIPs(checked)}
            />
          </StackItem>
          <StackItem>
            <Alert variant={unbindStaticIPs ? AlertVariant.warning : AlertVariant.info} title={unbindStaticIPs ? "Static IP cleanup enabled" : "Network configuration will be retained"} isInline>
              {unbindStaticIPs
                ? "Swallow will wait for Ready, then remove only unchanged Static links captured before Release. DHCP, provider-managed, Link only, and later changes are preserved."
                : "Release will leave all current network links in place."}
            </Alert>
          </StackItem>
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
