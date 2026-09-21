import { useEffect, useState } from 'react'
import { Field, Spinner, Stack, Text } from '@chakra-ui/react'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { DeployTarget } from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { Button } from '@chakra-ui/react'
import { DeployTargetField } from './DeployTargetField'
import type { OSImageCatalogRow } from '@/application/usecases/provisioning/loadOSImageCatalog'

/** Normalizes a provider subarchitecture suffix (e.g. "amd64/generic") for identity comparison. */
function primaryArchitecture(architecture: string): string {
  return architecture.split('/', 1)[0]
}

/**
 * Launches an image verification: an admin picks a ready Server of the image's architecture and a
 * deploy target, then Swallow runs a real deploy to prove the image, records the verification, and
 * auto-releases the Server. The chosen Server is briefly taken out of the ready pool for the run.
 */
export function VerifyImageDialog({
  image,
  provisioning,
  servers,
  onClose,
  onLaunched,
}: {
  image: OSImageCatalogRow
  provisioning: ProvisioningRepository
  servers: ServerRepository
  onClose: () => void
  /** Called after the verify Operation is accepted, with a toast title to show. */
  onLaunched: (title: string) => void
}) {
  const [target, setTarget] = useState<DeployTarget>('disk')
  const [candidates, setCandidates] = useState<Server[]>([])
  const [serverId, setServerId] = useState('')
  // Starts true because the dialog opens straight into the fetch below; the effect only flips it
  // off (in the async callbacks), so no synchronous setState runs inside the effect body.
  const [loadingServers, setLoadingServers] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    servers
      .listServers({ integrationId: image.integrationId, provisioningState: 'ready', pageSize: 200 })
      .then((page) => {
        if (cancelled) return
        // Only a Server of the image's architecture can prove it; the backend enforces this too.
        const matches = page.items.filter(
          (server) => primaryArchitecture(server.architecture) === primaryArchitecture(image.architecture),
        )
        setCandidates(matches)
        setServerId(matches[0]?.id ?? '')
        setLoadingServers(false)
      })
      .catch((caught: unknown) => {
        if (cancelled) return
        setLoadError(caught instanceof Error ? caught.message : 'Could not load ready Servers.')
        setLoadingServers(false)
      })
    return () => {
      cancelled = true
    }
  }, [image.integrationId, image.architecture, servers])

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!serverId || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await provisioning.createImageVerification({
        integrationId: image.integrationId,
        imageId: image.id,
        architecture: image.architecture,
        deployTarget: target,
        serverId,
      })
      onLaunched(`Verifying ${image.name || image.id} — the Server will auto-release when done`)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The verification could not be started.')
      setSubmitting(false)
    }
  }

  const noReadyServers = !loadingServers && !loadError && candidates.length === 0

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Verify OS image"
      description={`Prove ${image.name || image.id} (${image.architecture}) deploys by running a real deployment on a ready Server.`}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button
            colorPalette="brand"
            onClick={() => void submit()}
            loading={submitting}
            disabled={submitting || loadingServers || !serverId}
          >
            Start verification
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Verification could not be started">
            {error}
          </Alert>
        )}
        {loadError && (
          <Alert status="error" title="Could not load ready Servers">
            {loadError}
          </Alert>
        )}
        <DeployTargetField
          value={target}
          onChange={setTarget}
          label="Deploy target to verify"
          helperText="Verify Disk and RAM separately — each proves that one deploy mode works for this image."
        />
        <Field.Root required>
          <Field.Label>Ready Server</Field.Label>
          {loadingServers ? (
            <Spinner size="sm" aria-label="Loading ready Servers" />
          ) : (
            <Select
              value={serverId}
              onChange={setServerId}
              aria-label="Ready Server"
              placeholder={noReadyServers ? 'No ready Server of this architecture' : 'Select a ready Server'}
              disabled={noReadyServers}
              options={candidates.map((server) => ({
                value: server.id,
                label: `${serverDisplayName(server)} (${server.architecture})`,
              }))}
            />
          )}
          <Field.HelperText>
            Only ready Servers of the {primaryArchitecture(image.architecture)} architecture are shown. The Server is
            deployed to, verified, then automatically released back to ready.
          </Field.HelperText>
        </Field.Root>
        {noReadyServers && (
          <Alert status="warning" title="No ready Server available">
            Release or add a ready Server on this provisioner before verifying this image.
          </Alert>
        )}
        <Text fontSize="sm" color="fg.muted">
          Verification runs a real deployment. It can take several minutes and the Server is unavailable for other work
          until it auto-releases.
        </Text>
      </Stack>
    </Modal>
  )
}
