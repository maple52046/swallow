import { useState } from 'react'
import { Button, Field, Input, Progress, Stack, Text } from '@chakra-ui/react'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { Integration } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { formatBytes } from '@/shared/utils/bytes'

/**
 * CPU architectures an uploaded image can target. The backend expands the choice into the
 * provider's own form (for MAAS, "<arch>/generic"), so this list is swallow-neutral.
 */
const ARCHITECTURE_OPTIONS = [
  { value: 'amd64', label: 'amd64' },
  { value: 'arm64', label: 'arm64' },
]

/**
 * Artifact formats a provisioner accepts. The empty value lets the backend apply the provider
 * default (MAAS: a root tarball), so the operator only picks a value for a disk image.
 */
const FILETYPE_OPTIONS = [
  { value: '', label: 'Provider default' },
  { value: 'tgz', label: 'Root tarball (tgz)' },
  { value: 'ddtgz', label: 'Raw disk image, gzip tar (ddtgz)' },
  { value: 'ddtar', label: 'Raw disk image, tar (ddtar)' },
  { value: 'ddraw', label: 'Raw disk image (ddraw)' },
]

/**
 * Uploads a new provider-owned OS image to one scoped provisioner.
 *
 * The dialog collects the swallow-neutral intent (target provisioner, name, architecture,
 * optional title and file type) plus the file, and streams it through the provisioning
 * repository, showing upload progress for a potentially multi-gigabyte artifact. It never
 * decides whether the result is a custom image: that is the provisioner's classification,
 * surfaced afterwards in the catalog. A failure keeps the dialog open with the provider's own
 * message so the operator can correct and retry; only a success unmounts it.
 */
export function UploadImageDialog({
  integrations,
  repository,
  onClose,
  onUploaded,
}: {
  /** Scoped provisioner integrations the image can be uploaded to; at least one is expected. */
  integrations: Integration[]
  repository: ProvisioningRepository
  onClose: () => void
  /** Called after a successful upload with the toast title to show. */
  onUploaded: (title: string) => void
}) {
  const [integrationId, setIntegrationId] = useState(integrations.length === 1 ? integrations[0].id : '')
  const [name, setName] = useState('')
  const [architecture, setArchitecture] = useState('amd64')
  const [title, setTitle] = useState('')
  const [filetype, setFiletype] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [submitting, setSubmitting] = useState(false)
  // null while the total is unknown (before the first progress event or when the browser cannot
  // report length), otherwise the whole-number percentage shown to the operator.
  const [percent, setPercent] = useState<number | null>(null)
  const [error, setError] = useState('')

  const close = () => {
    if (!submitting) onClose()
  }

  const canSubmit = Boolean(integrationId && name.trim() && architecture && file) && !submitting

  const submit = async () => {
    if (!canSubmit || !file) return
    setSubmitting(true)
    setError('')
    setPercent(null)
    try {
      await repository.uploadOSImage(
        integrationId,
        {
          name: name.trim(),
          architecture,
          title: title.trim() || undefined,
          filetype: filetype || undefined,
          file,
        },
        ({ loaded, total }) => setPercent(total > 0 ? Math.round((loaded / total) * 100) : null),
      )
      onUploaded('OS image uploaded')
    } catch (caught) {
      // Keep the dialog open on failure so the operator can correct and retry; only a success
      // unmounts it, so submitting is reset here.
      setError(caught instanceof Error ? caught.message : 'The image could not be uploaded.')
      setSubmitting(false)
      setPercent(null)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Upload OS image"
      description="Stream a new image to the selected provisioner. Swallow keeps no copy, and the provisioner decides whether it becomes a custom image."
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button colorPalette="brand" onClick={() => void submit()} loading={submitting} disabled={!canSubmit}>
            Upload
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Image could not be uploaded">
            {error}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label htmlFor="upload-image-integration">
            Provisioner <Field.RequiredIndicator />
          </Field.Label>
          <Select
            id="upload-image-integration"
            value={integrationId}
            onChange={setIntegrationId}
            aria-label="Provisioner integration"
            placeholder="Select a provisioner"
            disabled={submitting}
            options={integrations.map((integration) => ({ value: integration.id, label: integration.name }))}
          />
        </Field.Root>
        <Field.Root required>
          <Field.Label htmlFor="upload-image-name">
            Name <Field.RequiredIndicator />
          </Field.Label>
          <Input
            id="upload-image-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={200}
            placeholder="e.g. ubuntu-24.04-rocm"
            disabled={submitting}
            autoFocus
          />
        </Field.Root>
        <Field.Root required>
          <Field.Label htmlFor="upload-image-arch">
            Architecture <Field.RequiredIndicator />
          </Field.Label>
          <Select
            id="upload-image-arch"
            value={architecture}
            onChange={setArchitecture}
            aria-label="Architecture"
            options={ARCHITECTURE_OPTIONS}
            disabled={submitting}
          />
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="upload-image-title">Title</Field.Label>
          <Input
            id="upload-image-title"
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            maxLength={200}
            placeholder="Optional display title"
            disabled={submitting}
          />
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="upload-image-filetype">File type</Field.Label>
          <Select
            id="upload-image-filetype"
            value={filetype}
            onChange={setFiletype}
            aria-label="File type"
            options={FILETYPE_OPTIONS}
            disabled={submitting}
          />
          <Field.HelperText>Leave as the provider default unless uploading a disk image.</Field.HelperText>
        </Field.Root>
        <Field.Root required>
          <Field.Label htmlFor="upload-image-file">
            Image file <Field.RequiredIndicator />
          </Field.Label>
          <input
            id="upload-image-file"
            type="file"
            disabled={submitting}
            onChange={(event) => setFile(event.target.files?.[0] ?? null)}
          />
          {file && (
            <Field.HelperText>
              {file.name} ({formatBytes(file.size)})
            </Field.HelperText>
          )}
        </Field.Root>
        {submitting && (
          <Stack gap="1" role="status" aria-live="polite">
            <Text fontSize="sm">{percent === null ? 'Uploading…' : `Uploading… ${percent}%`}</Text>
            <Progress.Root value={percent} size="sm" aria-label="Upload progress">
              <Progress.Track>
                <Progress.Range />
              </Progress.Track>
            </Progress.Root>
          </Stack>
        )}
      </Stack>
    </Modal>
  )
}
