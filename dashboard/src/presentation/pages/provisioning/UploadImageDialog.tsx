import { useState } from 'react'
import { Button, Field, Input, Progress, Stack, Text } from '@chakra-ui/react'
import type { OSImageUploadProgress, ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import { isValidDefaultUser, type Integration } from '@/domain/site/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
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
 * default (MAAS: a root tarball), so the user only picks a value for a disk image.
 */
const FILETYPE_OPTIONS = [
  { value: '', label: 'Provider default' },
  { value: 'tgz', label: 'Root tarball (tgz)' },
  { value: 'ddtgz', label: 'Raw disk image, gzip tar (ddtgz)' },
  { value: 'ddtar', label: 'Raw disk image, tar (ddtar)' },
  { value: 'ddraw', label: 'Raw disk image (ddraw)' },
]

/**
 * What the upload status line says while a submission runs; the percentage is `null` when it
 * is unknown or no longer meaningful.
 */
interface UploadStatus {
  label: string
  percent: number | null
}

/**
 * Turns the repository's upload progress into the status line. `null` progress is the moment
 * between pressing Upload and the first progress event. Once every byte has left the browser the
 * request is still open while the provisioner stages the image, so that wait is named instead
 * of showing a stuck 100%.
 */
function uploadStatus(progress: OSImageUploadProgress | null): UploadStatus {
  if (progress === null) return { label: 'Preparing…', percent: null }
  const percent = progress.total > 0 ? Math.round((progress.loaded / progress.total) * 100) : null
  if (progress.phase === 'hashing') {
    return { label: percent === null ? 'Checking file…' : `Checking file… ${percent}%`, percent }
  }
  if (percent === 100) return { label: 'Waiting for the provisioner to finish staging the image…', percent: null }
  return { label: percent === null ? 'Uploading…' : `Uploading… ${percent}%`, percent }
}

/**
 * Uploads a new provider-owned OS image to one scoped provisioner.
 *
 * The dialog collects the swallow-neutral intent (target provisioner, name, architecture,
 * optional title, file type, and default login user) plus the file, and hands it to the
 * provisioning repository, which checks the file (computes its SHA-256 locally), records the
 * upload, and streams the file. The status line follows those phases for a potentially
 * multi-gigabyte artifact, including the wait while the provisioner stages the image. It never
 * decides whether the result is a custom image: that is the provisioner's classification,
 * surfaced afterwards in the catalog. A failure keeps the dialog open with the provider's own
 * message so the user can correct and retry; only a success unmounts it.
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
  // Optional; a custom image has no built-in default user, so the uploader is the one who knows
  // the account its cloud-init creates. Validated locally so a typo never costs the upload.
  const [defaultUser, setDefaultUser] = useState('')
  const defaultUserInvalid = defaultUser.trim() !== '' && !isValidDefaultUser(defaultUser.trim())
  const [file, setFile] = useState<File | null>(null)
  const [submitting, setSubmitting] = useState(false)
  // The latest progress event of the running submission; null before the first one.
  const [progress, setProgress] = useState<OSImageUploadProgress | null>(null)
  const status = uploadStatus(progress)
  const [error, setError] = useState<DisplayedFailure | null>(null)

  const close = () => {
    if (!submitting) onClose()
  }

  const canSubmit = Boolean(integrationId && name.trim() && architecture && file) && !defaultUserInvalid && !submitting

  const submit = async () => {
    if (!canSubmit || !file) return
    setSubmitting(true)
    setError(null)
    setProgress(null)
    try {
      await repository.uploadOSImage(
        integrationId,
        {
          name: name.trim(),
          architecture,
          title: title.trim() || undefined,
          filetype: filetype || undefined,
          defaultUser: defaultUser.trim() || undefined,
          file,
        },
        setProgress,
      )
      onUploaded('OS image uploaded')
    } catch (caught) {
      // Keep the dialog open on failure so the user can correct and retry; only a success
      // unmounts it, so submitting is reset here.
      setError(failureOf(caught, 'The image could not be uploaded.'))
      setSubmitting(false)
      setProgress(null)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Upload OS image"
      description="Stream a new image to the selected provisioner. Swallow checks the file in your browser first, keeps no copy, and the provisioner decides whether it becomes a custom image."
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
            <FailureMessage failure={error} />
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
        <Field.Root invalid={defaultUserInvalid}>
          <Field.Label htmlFor="upload-image-default-user">Default user</Field.Label>
          <Input
            id="upload-image-default-user"
            value={defaultUser}
            onChange={(event) => setDefaultUser(event.target.value)}
            maxLength={32}
            placeholder="e.g. cloud-user"
            autoComplete="off"
            spellCheck={false}
            disabled={submitting}
          />
          {defaultUserInvalid ? (
            <Field.ErrorText>Use a login name: lowercase letters, digits, "_" or "-", starting with a letter or "_".</Field.ErrorText>
          ) : (
            <Field.HelperText>The login user the image's cloud-init creates. Swallow automation logs in as this user; it can also be set later.</Field.HelperText>
          )}
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
            <Text fontSize="sm">{status.label}</Text>
            <Progress.Root value={status.percent} size="sm" aria-label="Upload progress">
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
