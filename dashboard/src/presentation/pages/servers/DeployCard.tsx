import { useEffect, useState } from 'react'
import { Box, Button, Callout, Card, Flex, Heading, Select, Text, TextArea } from '@radix-ui/themes'
import { ExclamationTriangleIcon } from '@radix-ui/react-icons'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/radix/toast/toastContext'
import type { OSImage } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'

interface DeployCardProps {
  server: Server
  /** Called after a deploy is accepted, to refetch the detail. */
  onActed: () => void
}

/**
 * The OS deployment form on the Summary tab.
 *
 * Deploy needs inputs (image, optional cloud-init, ephemeral), so unlike the other
 * actions it is a form rather than a menu item. Images come from this server's own
 * provisioner, since they differ per site. Deploy is only enabled when the machine is
 * `ready`; the image is required so the provisioner never silently chooses one. Ephemeral
 * is surfaced with a warning because anything installed afterwards is lost on reboot.
 */
export function DeployCard({ server, onActed }: DeployCardProps) {
  const { servers, sites } = useApp()
  const { showToast } = useToast()

  const [images, setImages] = useState<OSImage[]>([])
  const [distroSeries, setDistroSeries] = useState<string | null>(null)
  const [userData, setUserData] = useState('')
  const [ephemeral, setEphemeral] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let cancelled = false
    sites
      .listOSImages(server.source.integrationId)
      .then((result) => {
        if (!cancelled) setImages(result)
      })
      .catch(() => {
        if (!cancelled) setImages([])
      })
    return () => {
      cancelled = true
    }
  }, [sites, server.source.integrationId])

  const canDeploy = server.provisioning?.state === 'ready'

  const handleDeploy = async () => {
    if (!distroSeries) return
    setBusy(true)
    try {
      const result = await servers.deployServer(server.id, {
        distroSeries,
        userData: userData || undefined,
        ephemeral: ephemeral || undefined,
      })
      showToast({
        tone: 'success',
        title: 'Deployment accepted',
        description: `The provisioner reports "${result.state}"; the reconciler will follow it.`,
      })
      onActed()
    } catch (err) {
      showToast({
        tone: 'error',
        title: 'Deploy failed',
        description: err instanceof Error ? err.message : 'Unknown error',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card>
      <Heading as="h2" size="3" mb="2">
        Deploy an operating system
      </Heading>
      <Flex direction="column" gap="3">
        <label>
          <Text as="div" size="2" weight="medium" mb="1">
            Image
          </Text>
          <Select.Root
            value={distroSeries ?? undefined}
            onValueChange={setDistroSeries}
            disabled={!canDeploy || images.length === 0}
          >
            <Select.Trigger
              placeholder={images.length === 0 ? 'No deployable images' : 'Select an image'}
              style={{ width: '100%' }}
            />
            <Select.Content>
              {images.map((image) => (
                <Select.Item key={image.id} value={image.id}>
                  {image.name}
                </Select.Item>
              ))}
            </Select.Content>
          </Select.Root>
          <Text size="1" color="gray">
            Required: the provisioner must not choose for you.
          </Text>
        </label>

        <label>
          <Text as="div" size="2" weight="medium" mb="1">
            Cloud-init user data
          </Text>
          <TextArea
            placeholder="#cloud-config"
            value={userData}
            onChange={(event) => setUserData(event.currentTarget.value)}
            disabled={!canDeploy}
            rows={4}
          />
        </label>

        <Text as="label" size="2">
          <Flex align="center" gap="2">
            <input
              type="checkbox"
              checked={ephemeral}
              onChange={(event) => setEphemeral(event.currentTarget.checked)}
              disabled={!canDeploy}
            />
            Deploy in memory
          </Flex>
        </Text>

        {ephemeral && (
          <Callout.Root color="orange">
            <Callout.Icon>
              <ExclamationTriangleIcon />
            </Callout.Icon>
            <Callout.Text>
              Nothing installed afterwards will survive a reboot, including anything an
              operation configures on this machine.
            </Callout.Text>
          </Callout.Root>
        )}

        <Box>
          <Button onClick={() => void handleDeploy()} loading={busy} disabled={!canDeploy || !distroSeries}>
            Deploy
          </Button>
        </Box>

        {!canDeploy && (
          <Text size="1" color="gray">
            Deploying needs the machine to be ready. It is currently{' '}
            {server.provisioning?.state ?? 'unknown'}.
          </Text>
        )}
      </Flex>
    </Card>
  )
}
