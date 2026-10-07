import { useEffect, useState } from 'react'
import { Field, Spinner, Stack, Text } from '@chakra-ui/react'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { DeployTarget } from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
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
 * Starts a target-specific image deployment test on an operator-selected ready Server. The durable
 * backend still records canonical verification evidence; this presentation describes the concrete
 * user-visible behavior: deploy in Disk or RAM mode, confirm SSH access, then return the Server.
 */
export function TestImageDeploymentDialog({
  image,
  initialTarget = 'disk',
  provisioning,
  servers,
  onClose,
  onLaunched,
}: {
  image: OSImageCatalogRow
  initialTarget?: DeployTarget
  provisioning: ProvisioningRepository
  servers: ServerRepository
  onClose: () => void
  /** Called after the deployment-test Workflow is accepted, with a toast title to show. */
  onLaunched: (title: string) => void
}) {
  const [target, setTarget] = useState<DeployTarget>(initialTarget)
  const [candidates, setCandidates] = useState<Server[]>([])
  const [serverId, setServerId] = useState('')
  // Starts true because the dialog opens straight into the fetch below; the effect only flips it
  // off (in the async callbacks), so no synchronous setState runs inside the effect body.
  const [loadingServers, setLoadingServers] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  // Default false: a verification borrows the Server and returns it to the ready pool when done.
  // When kept, the Server is left deployed (on success) for the operator to use or inspect.
  const [keepServer, setKeepServer] = useState(false)

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
        keepServer,
      })
      onLaunched(
        keepServer
          ? `Testing ${image.name || image.id} — the Server will stay deployed when done`
          : `Testing ${image.name || image.id} — the Server will auto-return to Ready when done`,
      )
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The deployment test could not be started.')
      setSubmitting(false)
    }
  }

  const noReadyServers = !loadingServers && !loadError && candidates.length === 0

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Test image deployment"
      description={`Run a real deployment of ${image.name || image.id} (${image.architecture}) on a ready Server and confirm Swallow can log in over SSH.`}
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
            Start test
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Deployment test could not be started">
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
          label="Deploy mode to test"
          helperText="Disk and RAM are tested separately. A successful test means the selected deployment completed and SSH login succeeded."
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
            used for the selected deployment test{keepServer ? ' and left deployed afterwards.' : ', then returned to Ready afterwards.'}
          </Field.HelperText>
        </Field.Root>
        {noReadyServers && (
          <Alert status="warning" title="No ready Server available">
            Release or add a ready Server on this provisioner before testing this image.
          </Alert>
        )}
        <Stack gap="1">
          <Checkbox id="verify-keep-server" checked={keepServer} disabled={submitting} onCheckedChange={setKeepServer}>
            Keep the Server deployed after the test
          </Checkbox>
          <Text fontSize="sm" color="fg.muted">
            By default the Server returns to the ready pool when the run finishes. Keep it deployed to use or inspect the
            tested deployment — you can Release it later. On failure the Server is left as-is for you to recover.
          </Text>
        </Stack>
        <Text fontSize="sm" color="fg.muted">
          This test only checks the selected deploy mode and SSH access; it does not certify platform compatibility,
          security, or any other property of the image. It can take several minutes, and the Server is unavailable for
          other work until it {keepServer ? 'finishes' : 'returns to Ready'}.
        </Text>
      </Stack>
    </Modal>
  )
}
