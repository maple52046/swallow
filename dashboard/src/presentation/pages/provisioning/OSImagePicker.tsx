import { useId, useMemo, useState, type ReactNode } from 'react'
import { Badge, Button, Field, HStack, Input, RadioCard, Text } from '@chakra-ui/react'
import { CircleCheck, CircleX, RefreshCw, Search } from 'lucide-react'
import type { DeployTarget } from '@/domain/provisioning/types'
import type { OSImage } from '@/domain/site/types'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { ResourceTag } from '@/presentation/components/ResourceTag'
import { formatBytes } from '@/shared/utils/bytes'
import { osImageVerificationKey } from './osImageListPresentation'
import { osImageSupportsDeployTarget } from './osDeploymentFlow'
import { useOSImageVerificationSnapshot } from './useOSImageVerificationSnapshot'

interface OSImagePickerProps {
  images: readonly OSImage[]
  value: string
  onChange: (imageId: string) => void
  disabled?: boolean
  loading?: boolean
  error?: string
  required?: boolean
  onRefresh?: () => void
  label?: string
  presentation?: 'contained' | 'plain'
  siteId?: string
  integrationId?: string
}

interface OSImageFamily {
  key: string
  label: string
  images: readonly OSImage[]
}

interface FamilyIntent {
  key: string
  valueWhenChosen: string
}

type DeployModeAvailability = 'available' | 'unavailable' | 'verifying'

const DEPLOY_TARGETS: readonly DeployTarget[] = ['disk', 'ram']

/** Stable labels for common provider family identifiers; unknown values remain readable. */
const KNOWN_FAMILY_LABELS: Readonly<Record<string, string>> = {
  alinux: 'Alibaba Cloud Linux',
  almalinux: 'AlmaLinux',
  centos: 'CentOS',
  custom: 'Custom',
  debian: 'Debian',
  fedora: 'Fedora',
  opensuse: 'openSUSE',
  oracle: 'Oracle Linux',
  rhel: 'RHEL',
  rocky: 'Rocky Linux',
  suse: 'SUSE',
  ubuntu: 'Ubuntu',
  windows: 'Windows',
}

function imageFamilyKey(value: string): string {
  return value.trim().toLocaleLowerCase() || '__unknown__'
}

/** Turns a provider family identifier into a compact operator-facing group label. */
function imageFamilyLabel(value: string): string {
  const normalized = value.trim()
  if (!normalized) return 'Unknown OS'
  const key = imageFamilyKey(normalized)
  const known = KNOWN_FAMILY_LABELS[key]
  if (known) return known
  return normalized
    .split(/[-_\s]+/u)
    .filter(Boolean)
    .map((part) => part.charAt(0).toLocaleUpperCase() + part.slice(1))
    .join(' ')
}

function imageTitle(image: OSImage): string {
  return image.name.trim() || image.id
}

/**
 * Provider IDs are useful only when they add identity beyond the display name. This intentionally
 * prevents catalogs that reuse one token for name, release, and ID from rendering it repeatedly.
 */
function hasDistinctImageId(image: OSImage): boolean {
  return image.id.trim().toLocaleLowerCase() !== imageTitle(image).trim().toLocaleLowerCase()
}

function sortedImageTags(image: OSImage): string[] {
  return [...new Set(image.tags.map((tag) => tag.trim()).filter(Boolean))]
    .sort((left, right) => left.localeCompare(right, undefined, { sensitivity: 'base' }))
}

function deployTargetLabel(target: DeployTarget): 'Disk' | 'RAM' {
  return target === 'disk' ? 'Disk' : 'RAM'
}

function ImageDeployModes({
  image,
  integrationId,
  verifyingTargets,
}: {
  image: OSImage
  integrationId?: string
  verifyingTargets: ReadonlySet<string>
}) {
  return (
    <span className="sw-os-image-picker__deploy-modes" role="group" aria-label="Deploy modes">
      {DEPLOY_TARGETS.map((target) => {
        const mode = deployTargetLabel(target)
        const verificationKey = integrationId
          ? osImageVerificationKey(integrationId, image.id, image.architecture, target)
          : ''
        const availability: DeployModeAvailability = verificationKey && verifyingTargets.has(verificationKey)
          ? 'verifying'
          : osImageSupportsDeployTarget(image, target) ? 'available' : 'unavailable'
        const statusLabel = availability === 'available'
          ? 'Available'
          : availability === 'verifying' ? 'Verification in progress' : 'Unavailable'
        return (
          <span
            key={target}
            className="sw-os-image-picker__deploy-mode"
            data-state={availability}
            aria-label={`${mode} deploy: ${statusLabel}`}
            title={`${mode} deploy: ${statusLabel}`}
          >
            <span>{mode}</span>
            {availability === 'available' && <CircleCheck size={16} aria-hidden="true" />}
            {availability === 'unavailable' && <CircleX size={16} aria-hidden="true" />}
            {availability === 'verifying' && <InProgressSpinner color="brand.fg" />}
          </span>
        )
      })}
    </span>
  )
}

function ImageTags({ image }: { image: OSImage }) {
  const tags = sortedImageTags(image)
  if (tags.length === 0) return <span className="sw-os-image-picker__empty-metadata">No tags</span>
  return (
    <span className="sw-os-image-picker__tags" role="group" aria-label="Tags">
      {tags.map((tag) => <ResourceTag key={tag}>{tag}</ResourceTag>)}
    </span>
  )
}

/** Shared operational metadata shown by selectable image cards. */
function ImageOperationalMetadata({
  image,
  integrationId,
  verifyingTargets,
}: {
  image: OSImage
  integrationId?: string
  verifyingTargets: ReadonlySet<string>
}) {
  return (
    <div className="sw-os-image-picker__operational-metadata">
      <div className="sw-os-image-picker__metadata-row">
        <span className="sw-os-image-picker__metadata-label">Deploy modes</span>
        <ImageDeployModes
          image={image}
          integrationId={integrationId}
          verifyingTargets={verifyingTargets}
        />
      </div>
      <div className="sw-os-image-picker__metadata-row">
        <span className="sw-os-image-picker__metadata-label">Tags</span>
        <ImageTags image={image} />
      </div>
    </div>
  )
}

function OSImageCardContent({
  image,
  integrationId,
  verifyingTargets,
  title,
}: {
  image: OSImage
  integrationId?: string
  verifyingTargets: ReadonlySet<string>
  title: ReactNode
}) {
  const showId = hasDistinctImageId(image)
  return (
    <>
      <HStack align="start" justify="space-between" gap="3">
        {title}
        <Badge size="sm" variant="subtle">{image.architecture}</Badge>
      </HStack>
      {(image.sizeBytes || showId) && (
        <div className="sw-os-image-picker__card-description">
          {image.sizeBytes && <span>{formatBytes(image.sizeBytes)}</span>}
          {showId && <span className="sw-os-image-picker__id">{image.id}</span>}
        </div>
      )}
      <ImageOperationalMetadata
        image={image}
        integrationId={integrationId}
        verifyingTargets={verifyingTargets}
      />
    </>
  )
}

function imageMatches(image: OSImage, normalizedQuery: string): boolean {
  if (!normalizedQuery) return true
  return [
    image.name,
    image.id,
    image.architecture,
    ...image.tags,
  ].some((candidate) => candidate.toLocaleLowerCase().includes(normalizedQuery))
}

/** Builds stable first-seen family groups while making image comparison alphabetical within each group. */
function groupImagesByFamily(images: readonly OSImage[]): OSImageFamily[] {
  const groups = new Map<string, { label: string; images: OSImage[] }>()
  for (const image of images) {
    const key = imageFamilyKey(image.osSystem)
    const existing = groups.get(key)
    if (existing) {
      existing.images.push(image)
    } else {
      groups.set(key, {
        label: imageFamilyLabel(image.osSystem),
        images: [image],
      })
    }
  }
  return [...groups.entries()].map(([key, group]) => ({
    key,
    label: group.label,
    images: [...group.images].sort((left, right) => (
      imageTitle(left).localeCompare(imageTitle(right), undefined, {
        numeric: true,
        sensitivity: 'base',
      })
    )),
  }))
}

/**
 * Selects an OS image through two explicit levels: family, then artifact. The image grid shares
 * its parent page/dialog scroll surface, so a large catalog never creates a nested scrollbar.
 * Search is scoped to the active family and deliberately ignores provider release metadata.
 * Cards expose sorted tags and icon-only target availability without creating nested scrolling.
 * Active verification is read once when the picker opens; it never introduces polling or refresh.
 * A refresh keeps last-good results, the active family, search text, and selection interactive.
 */
export function OSImagePicker({
  images,
  value,
  onChange,
  disabled,
  loading,
  error,
  required,
  onRefresh,
  label = 'OS image',
  presentation = 'contained',
  siteId,
  integrationId,
}: OSImagePickerProps) {
  const familyHeadingId = useId()
  const imageHeadingId = useId()
  const searchId = useId()
  const [query, setQuery] = useState('')
  const [familyIntent, setFamilyIntent] = useState<FamilyIntent | null>(null)
  const [keyboardNavigation, setKeyboardNavigation] = useState(false)
  const hasCustomImages = images.some((image) => image.providerOsSystem === 'custom')
  const verifyingTargets = useOSImageVerificationSnapshot(
    hasCustomImages && integrationId ? siteId : undefined,
  )
  const families = useMemo(() => groupImagesByFamily(images), [images])
  const selectedImage = images.find((image) => image.id === value)
  const selectedFamilyKey = selectedImage ? imageFamilyKey(selectedImage.osSystem) : ''
  const intendedFamily = familyIntent?.valueWhenChosen === value
    ? families.find((family) => family.key === familyIntent.key)
    : undefined
  const activeFamily = intendedFamily
    ?? families.find((family) => family.key === selectedFamilyKey)
    ?? families[0]
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const filteredImages = useMemo(
    () => activeFamily?.images.filter((image) => imageMatches(image, normalizedQuery)) ?? [],
    [activeFamily, normalizedQuery],
  )
  const initialLoading = Boolean(loading && images.length === 0)

  const chooseFamily = (family: OSImageFamily) => {
    setFamilyIntent({ key: family.key, valueWhenChosen: value })
    setQuery('')
  }

  return (
    <Field.Root required={required} invalid={Boolean(error)} className="sw-os-image-picker-field">
      {presentation === 'contained' && (
        <Field.Label width="full">
          <HStack justify="space-between" width="full" gap="3">
            <span>{label}</span>
            {onRefresh && (
              <Button
                variant="plain"
                size="xs"
                loading={loading}
                disabled={loading}
                onClick={onRefresh}
              >
                <RefreshCw size={14} />
                Refresh
              </Button>
            )}
          </HStack>
        </Field.Label>
      )}

      <div
        className={presentation === 'plain'
          ? 'sw-os-image-picker sw-os-image-picker--plain'
          : 'sw-os-image-picker'}
        data-focus-modality={keyboardNavigation ? 'keyboard' : 'pointer'}
        onKeyDownCapture={() => setKeyboardNavigation(true)}
        onPointerDownCapture={() => setKeyboardNavigation(false)}
      >
        {initialLoading ? (
          <div className="sw-os-image-picker__message">Loading deployable images…</div>
        ) : images.length === 0 ? (
          <div className="sw-os-image-picker__message">No deployable images are available.</div>
        ) : (
          <>
            <section className="sw-os-image-picker__stage" aria-labelledby={familyHeadingId}>
              <div className="sw-os-image-picker__stage-header">
                <div>
                  <Text id={familyHeadingId} fontWeight="semibold">
                    1. Choose an OS family
                  </Text>
                  <Text color="fg.muted" fontSize="xs">
                    {families.length} {families.length === 1 ? 'family' : 'families'} · {images.length} images total
                  </Text>
                </div>
              </div>
              <div
                className="sw-os-image-picker__families"
                role="group"
                aria-labelledby={familyHeadingId}
              >
                {families.map((family) => {
                  const active = family.key === activeFamily?.key
                  return (
                    <Button
                      key={family.key}
                      type="button"
                      size="sm"
                      variant="outline"
                      className="sw-os-image-picker__family"
                      aria-pressed={active}
                      disabled={disabled}
                      onClick={() => chooseFamily(family)}
                    >
                      <span>{family.label}</span>
                      <Badge size="sm" variant={active ? 'solid' : 'subtle'}>
                        {family.images.length}
                      </Badge>
                    </Button>
                  )
                })}
              </div>
            </section>

            {activeFamily && (
              <section className="sw-os-image-picker__stage" aria-labelledby={imageHeadingId}>
                <div className="sw-os-image-picker__stage-header">
                  <Text id={imageHeadingId} fontWeight="semibold">
                    2. Choose a {activeFamily.label} image
                  </Text>
                </div>

                <div className="sw-os-image-picker__search">
                  <Search size={16} aria-hidden="true" />
                  <Input
                    id={searchId}
                    value={query}
                    size="sm"
                    disabled={disabled || initialLoading}
                    aria-label="Search deployment images"
                    placeholder={'Search ' + activeFamily.label + ' images'}
                    onChange={(event) => setQuery(event.target.value)}
                  />
                </div>

                <HStack
                  className="sw-os-image-picker__result-summary"
                  justify="space-between"
                  aria-live="polite"
                >
                  <Text color="fg.muted" fontSize="xs">
                    {filteredImages.length} {filteredImages.length === 1 ? 'image' : 'images'} in {activeFamily.label}
                    {loading ? ' · Refreshing…' : ''}
                  </Text>
                  {normalizedQuery && (
                    <Button variant="plain" size="xs" onClick={() => setQuery('')}>
                      Clear search
                    </Button>
                  )}
                </HStack>

                {filteredImages.length === 0 ? (
                  <div className="sw-os-image-picker__message">No images match this search.</div>
                ) : (
                  <RadioCard.Root
                    aria-label={label}
                    colorPalette="brand"
                    value={value}
                    disabled={disabled}
                    className="sw-os-image-picker__group"
                    onValueChange={(details) => {
                      if (details.value) onChange(details.value)
                    }}
                  >
                    <div className="sw-os-image-picker__list">
                      {filteredImages.map((image) => (
                        <RadioCard.Item key={image.id} value={image.id}>
                          <RadioCard.ItemHiddenInput />
                          <RadioCard.ItemControl className="sw-os-image-picker__card">
                            <RadioCard.ItemContent className="sw-os-image-picker__card-content">
                              <OSImageCardContent
                                image={image}
                                integrationId={integrationId}
                                verifyingTargets={verifyingTargets}
                                title={(
                                  <RadioCard.ItemText className="sw-os-image-picker__card-title">
                                    {imageTitle(image)}
                                  </RadioCard.ItemText>
                                )}
                              />
                            </RadioCard.ItemContent>
                            <RadioCard.ItemIndicator />
                          </RadioCard.ItemControl>
                        </RadioCard.Item>
                      ))}
                    </div>
                  </RadioCard.Root>
                )}
              </section>
            )}
          </>
        )}
      </div>
      {error && <Field.ErrorText>{error}</Field.ErrorText>}
    </Field.Root>
  )
}
