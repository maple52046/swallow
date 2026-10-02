import { useCallback, useRef, useState } from 'react'
import { Badge, Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { Download, RefreshCw, Trash2 } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { imageReferenceRegistry, shortDockerId, type DockerImage } from '@/domain/software/docker'
import { formatBytes } from '@/shared/utils/bytes'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { SectionSurface } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { softwareSettingsPath } from '@/presentation/pages/software/softwarePresentation'
import { ResponsiveResourceList } from '@/presentation/components/ResponsiveResourceList'
import { DOCKER_EXPLORER_UNAVAILABLE_HINT, type DockerSectionProps } from './dockerPresentation'
import { useDockerMutation } from './useDockerMutation'

/** Display name of an image: its first tag, or a neutral label for an untagged (dangling) image. */
function imageLabel(image: DockerImage): string {
  return image.repoTags[0] ?? `<untagged> ${shortDockerId(image.id)}`
}

/** A pull this section started and is still waiting for. */
interface ActivePull {
  key: number
  reference: string
  startedAt: string
}

/**
 * Images section of the Docker Host Explorer (P0): the host's local images live from the Engine,
 * with Pull and Remove.
 *
 * The list is read on mount and after every write; nothing is cached. A pull is one synchronous
 * api-server request (up to 55 minutes — GPU images run to tens of gigabytes), owned by this section
 * rather than the dialog: the operator can close the dialog while it runs, the section lists every
 * pull still running, and the outcome arrives as a toast (plus a list refresh). While the dialog is
 * open, a failure is shown inline there instead. Remove asks for confirmation and offers the
 * Engine's `force` option, explaining what it adds. Writes are disabled while the Server is locked
 * (`readOnlyReason`).
 */
export function DockerImagesSection({ serverId, readOnlyReason, onChanged }: DockerSectionProps) {
  const { dockerHosts } = useApp()
  const { showToast } = useToast()
  const images = useAsyncData(() => dockerHosts.listImages(serverId), [serverId])
  const reloadImages = images.reload
  const changed = useCallback(() => {
    reloadImages()
    onChanged()
  }, [reloadImages, onChanged])
  const { busyKey, run } = useDockerMutation(changed)
  const [pullDialogOpen, setPullDialogOpen] = useState(false)
  const [activePulls, setActivePulls] = useState<ActivePull[]>([])
  const nextPullKey = useRef(0)
  /** The pull whose outcome the open dialog shows; null once the dialog is closed or idle. */
  const watchedPull = useRef<number | null>(null)
  const [pendingRemove, setPendingRemove] = useState<DockerImage | null>(null)
  const [forceRemove, setForceRemove] = useState(false)
  const readOnly = readOnlyReason !== undefined

  /** Runs one pull; rejects only while the dialog still watches it, so the dialog can show the error. */
  const startPull = async (reference: string) => {
    nextPullKey.current += 1
    const pull: ActivePull = { key: nextPullKey.current, reference, startedAt: new Date().toISOString() }
    watchedPull.current = pull.key
    setActivePulls((current) => [...current, pull])
    try {
      const result = await dockerHosts.pullImage(serverId, reference)
      showToast({
        tone: 'success',
        title: 'Image pulled',
        description: `${result.reference} — ${result.status}${result.authenticated ? ` (signed in to ${result.registry})` : ''}`,
      })
      if (watchedPull.current === pull.key) {
        watchedPull.current = null
        setPullDialogOpen(false)
      }
      changed()
    } catch (caught) {
      if (watchedPull.current === pull.key) throw caught
      showToast({
        tone: 'error',
        title: 'Image could not be pulled',
        description: `${reference}: ${caught instanceof Error ? caught.message : 'the pull failed.'}`,
      })
    } finally {
      setActivePulls((current) => current.filter((item) => item.key !== pull.key))
    }
  }

  const closePullDialog = () => {
    watchedPull.current = null
    setPullDialogOpen(false)
  }

  const remove = async () => {
    if (!pendingRemove) return
    const target = imageLabel(pendingRemove)
    await run(`remove:${pendingRemove.id}`, () => dockerHosts.removeImage(serverId, pendingRemove.id, forceRemove), {
      success: 'Image removed',
      failure: 'Image could not be removed',
      target,
    })
    setPendingRemove(null)
  }

  return (
    <SectionSurface
        title="Images"
        description="Images stored on this host, read live from its Docker Engine."
        actions={
          <HStack gap="2">
            <Button size="sm" colorPalette="brand" onClick={() => setPullDialogOpen(true)} disabled={readOnly}>
              <Download size={16} aria-hidden /> Pull image
            </Button>
            <Button variant="plain" size="sm" onClick={images.reload}>
              <RefreshCw size={16} aria-hidden /> Refresh
            </Button>
          </HStack>
        }
      >
      {activePulls.length > 0 && (
        <Alert status="info" title={activePulls.length === 1 ? 'Pulling 1 image' : `Pulling ${activePulls.length} images`} mb="4">
          <Stack gap="1" as="ul" listStyleType="none" aria-label="Image pulls in progress" aria-live="polite">
            {activePulls.map((pull) => (
              <li key={pull.key}>
                <span className="mono">{pull.reference}</span> — started {formatDateTime(pull.startedAt)}
              </li>
            ))}
          </Stack>
          <Text fontSize="sm" mt="1">
            Large images can take many minutes. The pull keeps running on the host if you leave this page; the image appears
            here when it finishes.
          </Text>
        </Alert>
      )}
      <AsyncSection state={images} unavailableTitle="Docker explorer is unavailable" unavailableHint={DOCKER_EXPLORER_UNAVAILABLE_HINT}>
        {(items) =>
          items.length === 0 ? (
            <EmptyState title="No images" message="This host has no local images. Pull one to create containers from it." />
          ) : (
            <ResponsiveResourceList
              label="Docker images"
              items={items}
              rowKey={(image) => image.id}
              identityHeader="Image"
              title={(image) => (
                <HStack gap="2" wrap="wrap">
                  <span>{imageLabel(image)}</span>
                  {image.dangling && <Badge colorPalette="gray" variant="subtle">dangling</Badge>}
                  {image.repoTags.length > 1 && <Text as="span" color="fg.muted" fontSize="xs">+{image.repoTags.length - 1} tags</Text>}
                </HStack>
              )}
              subtitle={(image) => (
                <span className="sw-cell-inline">
                  {shortDockerId(image.id)}
                  <CopyButton value={image.id} label={`Copy image id of ${imageLabel(image)}`} />
                </span>
              )}
              columns={[
                { header: 'Size', cell: (image) => (image.sizeBytes > 0 ? formatBytes(image.sizeBytes) : '—') },
                {
                  header: 'Created',
                  cell: (image) => <span title={formatDateTime(image.createdAt)}>{formatRelative(image.createdAt)}</span>,
                },
              ]}
              actions={(image) => (
                <Button
                  size="sm"
                  variant="outline"
                  colorPalette="red"
                  disabled={readOnly || busyKey !== null}
                  loading={busyKey === `remove:${image.id}`}
                  aria-label={`Remove image ${imageLabel(image)}`}
                  onClick={() => {
                    setForceRemove(false)
                    setPendingRemove(image)
                  }}
                >
                  <Trash2 size={16} aria-hidden /> Remove
                </Button>
              )}
            />
          )
        }
      </AsyncSection>

      {pullDialogOpen && <PullImageDialog onPull={startPull} onClose={closePullDialog} />}
      <ConfirmDialog
        open={pendingRemove !== null}
        title="Remove image"
        confirmLabel="Remove image"
        busy={pendingRemove !== null && busyKey === `remove:${pendingRemove.id}`}
        onCancel={() => setPendingRemove(null)}
        onConfirm={() => void remove()}
      >
        <Stack gap="3">
          <Text>
            Removing <strong>{pendingRemove ? imageLabel(pendingRemove) : ''}</strong> deletes it from this host&apos;s
            image store. Containers cannot be created from it again until it is pulled. An image used by a running
            container cannot be removed.
          </Text>
          <Checkbox checked={forceRemove} onCheckedChange={setForceRemove}>
            Force: also remove it when it has several tags or is referenced by stopped containers
          </Checkbox>
        </Stack>
      </ConfirmDialog>
    </SectionSurface>
  )
}

/**
 * Pull dialog. The reference is `name[:tag]` or `name@digest`; an untagged reference pulls `latest`
 * (api-server never pulls every tag). The saved Registry Credentials are read once when the dialog
 * opens so it can say, as the operator types, whether the pull will sign in to the reference's
 * registry (and as whom) or be anonymous; api-server makes the authoritative choice. If that list
 * cannot be read the hint is omitted and pulling still works. `onPull` is owned by the section: it
 * closes the dialog on success and rejects with the API's error (for example "access denied") while
 * the dialog is open, shown inline. Closing the dialog mid-pull hands the outcome to the section.
 */
function PullImageDialog({ onPull, onClose }: { onPull: (reference: string) => Promise<void>; onClose: () => void }) {
  const { registryCredentials } = useApp()
  const { scopedHref } = useSiteScope()
  const credentials = useAsyncData(() => registryCredentials.list(), [registryCredentials])
  const [reference, setReference] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const trimmed = reference.trim()
  const valid = trimmed !== '' && !/\s/.test(trimmed)
  const registry = valid ? imageReferenceRegistry(trimmed) : ''
  const credential = credentials.status === 'ready' ? credentials.data.find((item) => item.registry === registry) : undefined

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await onPull(trimmed)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The image could not be pulled.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      closeOnInteractOutside={!submitting}
      title="Pull image"
      description="Download an image from a registry into this host's image store."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {submitting ? 'Continue in background' : 'Cancel'}
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} loadingText="Pulling…" disabled={!valid || submitting}>
            Pull
          </Button>
        </>
      }
    >
      <Stack gap="3">
        {error && (
          <Alert status="error" title="The image could not be pulled">
            {error}
          </Alert>
        )}
        <Field.Root required invalid={trimmed !== '' && !valid}>
          <Field.Label>
            Image reference <Field.RequiredIndicator />
          </Field.Label>
          <Input
            value={reference}
            onChange={(event) => setReference(event.target.value)}
            placeholder="nginx:1.27"
            autoFocus
            disabled={submitting}
          />
          <Field.HelperText>
            name[:tag] or name@digest, optionally with a registry host. Without a tag, latest is pulled.
          </Field.HelperText>
          <Field.ErrorText>A reference cannot contain spaces.</Field.ErrorText>
        </Field.Root>
        {registry && credentials.status === 'ready' && (
          <Text color="fg.muted" fontSize="sm" role="status">
            {credential ? (
              <>
                Signs in to <span className="mono">{registry}</span> as <span className="mono">{credential.username}</span> with the
                saved registry credential.
              </>
            ) : (
              <>
                No saved credential for <span className="mono">{registry}</span>, so the pull is anonymous. Private images need a{' '}
                <RouterLink to={scopedHref(softwareSettingsPath('docker-ce'))}>
                  registry credential
                </RouterLink>
                .
              </>
            )}
          </Text>
        )}
        {submitting && (
          <Text color="fg.muted" fontSize="sm" role="status">
            Large images can take many minutes. You can continue in the background: the pull keeps running and you are
            notified when it finishes.
          </Text>
        )}
      </Stack>
    </Modal>
  )
}
