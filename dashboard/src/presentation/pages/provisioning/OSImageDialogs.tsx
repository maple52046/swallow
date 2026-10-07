import { useState } from 'react'
import { Button, Field, HStack, Input, Stack } from '@chakra-ui/react'
import { X } from 'lucide-react'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { OSImageCatalogRow } from '@/application/usecases/provisioning/loadOSImageCatalog'
import { isValidDefaultUser } from '@/domain/site/types'
import { ResourceTag } from '@/presentation/components/ResourceTag'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { hasOSImageOverride } from './osImageListPresentation'

/** Edits operator-facing image details without mutating the underlying image artifact. */
export function EditImageDialog({
  image,
  repository,
  onClose,
  onSaved,
}: {
  image: OSImageCatalogRow
  repository: ProvisioningRepository
  onClose: () => void
  onSaved: (title: string) => void
}) {
  const [name, setName] = useState(image.customName ?? '')
  const [osSystem, setOsSystem] = useState(image.customOsSystem ?? '')
  const [release, setRelease] = useState(image.customRelease ?? '')
  const [tags, setTags] = useState<string[]>(image.tags)
  const [tagDraft, setTagDraft] = useState('')
  const [defaultUser, setDefaultUser] = useState(image.customDefaultUser ?? '')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const defaultUserInvalid = defaultUser.trim() !== '' && !isValidDefaultUser(defaultUser.trim())
  const builtinDefaultUser = image.customDefaultUser ? '' : image.defaultUser ?? ''

  const close = () => {
    if (!submitting) onClose()
  }
  const addTag = () => {
    const trimmed = tagDraft.trim()
    if (trimmed && !tags.includes(trimmed)) setTags([...tags, trimmed])
    setTagDraft('')
  }
  const removeTag = (tag: string) => setTags(tags.filter((entry) => entry !== tag))

  const save = async () => {
    if (submitting || defaultUserInvalid) return
    const pending = tagDraft.trim()
    const finalTags = pending && !tags.includes(pending) ? [...tags, pending] : tags
    setSubmitting(true)
    setError('')
    try {
      await repository.setOSImageOverlay(image.integrationId, image.id, image.architecture, {
        name: name.trim(),
        osSystem: osSystem.trim(),
        release: release.trim(),
        tags: finalTags,
        defaultUser: defaultUser.trim(),
      })
      onSaved('OS image updated')
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The image could not be updated.')
      setSubmitting(false)
    }
  }

  const reset = async () => {
    if (submitting) return
    setSubmitting(true)
    setError('')
    try {
      await repository.clearOSImageOverlay(image.integrationId, image.id, image.architecture)
      onSaved('Original OS image details restored')
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The image could not be reset.')
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Edit OS image"
      description="Changes how this image is displayed and selected in Swallow. The underlying image artifact is unchanged."
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>Cancel</Button>
          {hasOSImageOverride(image) && (
            <Button variant="outline" onClick={() => void reset()} disabled={submitting}>Restore original details</Button>
          )}
          <Button colorPalette="brand" onClick={() => void save()} loading={submitting} disabled={submitting || defaultUserInvalid}>Save</Button>
        </>
      }
    >
      <Stack gap="4">
        {error && <Alert status="error" title="Image could not be updated">{error}</Alert>}
        <Field.Root>
          <Field.Label htmlFor="os-image-name">Name</Field.Label>
          <Input id="os-image-name" value={name} onChange={(event) => setName(event.target.value)} placeholder="Optional display name" maxLength={200} autoFocus />
          <Field.HelperText>Leave blank to keep the original catalog name.</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="os-image-os">OS</Field.Label>
          <Input id="os-image-os" value={osSystem} onChange={(event) => setOsSystem(event.target.value)} placeholder="OS family" maxLength={200} />
          <Field.HelperText>Leave blank to keep the original catalog OS family.</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="os-image-release">Release</Field.Label>
          <Input id="os-image-release" value={release} onChange={(event) => setRelease(event.target.value)} placeholder="Release" maxLength={200} />
          <Field.HelperText>Leave blank to keep the original catalog release.</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="os-image-tags">Tags</Field.Label>
          <Input
            id="os-image-tags"
            value={tagDraft}
            onChange={(event) => setTagDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ',') {
                event.preventDefault()
                addTag()
              }
            }}
            onBlur={addTag}
            placeholder="Add a tag and press Enter"
            maxLength={200}
          />
          {tags.length > 0 && (
            <HStack wrap="wrap" gap="1" mt="2">
              {tags.map((tag) => (
                <ResourceTag key={tag}>
                  {tag}
                  <button type="button" aria-label={`Remove tag ${tag}`} onClick={() => removeTag(tag)}><X size={12} /></button>
                </ResourceTag>
              ))}
            </HStack>
          )}
          <Field.HelperText>Labels for organizing and searching images.</Field.HelperText>
        </Field.Root>
        <Field.Root invalid={defaultUserInvalid}>
          <Field.Label htmlFor="os-image-default-user">Default user</Field.Label>
          <Input
            id="os-image-default-user"
            value={defaultUser}
            onChange={(event) => setDefaultUser(event.target.value)}
            placeholder={builtinDefaultUser}
            maxLength={32}
            autoComplete="off"
            spellCheck={false}
          />
          {defaultUserInvalid
            ? <Field.ErrorText>Use a login name: lowercase letters, digits, "_" or "-", starting with a letter or "_".</Field.ErrorText>
            : <Field.HelperText>{defaultUserHelp(builtinDefaultUser)}</Field.HelperText>}
        </Field.Root>
      </Stack>
    </Modal>
  )
}

function defaultUserHelp(builtinDefaultUser: string): string {
  const purpose = 'The login user automation uses on Servers deployed with this image.'
  if (builtinDefaultUser) return `${purpose} Leave blank to keep the current default: ${builtinDefaultUser}.`
  return `${purpose} For example cloud-user. Leave blank to use the configured fallback candidates.`
}

/** Confirms permanent provider-side deletion before sending the destructive request. */
export function DeleteImageDialog({
  image,
  repository,
  onClose,
  onDeleted,
}: {
  image: OSImageCatalogRow
  repository: ProvisioningRepository
  onClose: () => void
  onDeleted: () => void
}) {
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const close = () => {
    if (!submitting) onClose()
  }
  const submit = async () => {
    if (submitting) return
    setSubmitting(true)
    setError('')
    try {
      await repository.deleteOSImage(image.integrationId, image.id, image.architecture)
      onDeleted()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The image could not be deleted.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      role="alertdialog"
      closeOnInteractOutside={!submitting}
      title="Delete OS image"
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>Cancel</Button>
          <Button colorPalette="red" onClick={() => void submit()} loading={submitting} disabled={submitting}>Delete image</Button>
        </>
      }
    >
      <Stack gap="4">
        {error && <Alert status="error" title="Image could not be deleted">{error}</Alert>}
        <Alert status="warning" title="This cannot be undone">
          This will permanently remove <strong>{image.name || image.id}</strong> ({image.architecture}) from its provider. Deployment templates and
          in-flight deployments that reference this image may fail after deletion. Make the image available again or update the affected templates before retrying.
        </Alert>
      </Stack>
    </Modal>
  )
}
