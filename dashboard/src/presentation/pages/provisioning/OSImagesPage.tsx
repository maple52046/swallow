import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Badge, Button, Field, HStack, IconButton, Input, Menu, Portal, Stack, Table } from '@chakra-ui/react'
import { Columns3, FilePlus2, Pencil, RefreshCw, Rocket, RotateCcw, Trash2, Upload, X } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import { loadOSImageCatalog, type OSImageCatalog, type OSImageCatalogRow } from '@/application/usecases/provisioning/loadOSImageCatalog'
import type { Integration } from '@/domain/site/types'
import { useApp } from '@/di/AppProvider'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, SelectionToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatBytes } from '@/shared/utils/bytes'
import { formatDateTime } from '@/shared/utils/time'
import { BulkImageActionDialog } from './BulkImageActionDialog'
import { ProvisioningTabs } from './ProvisioningTabs'
import { UploadImageDialog } from './UploadImageDialog'
import type { OSImageBulkAction, OSImageBulkTarget } from './useOSImageBulkActions'

type CatalogState = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; data: OSImageCatalog }

function provisioningHref(path: string, params: Record<string, string>, scopedHref: (path: string) => string): string {
  const target = new URL(scopedHref(path), window.location.origin)
  for (const [key, value] of Object.entries(params)) target.searchParams.set(key, value)
  return `${target.pathname}${target.search}`
}

/** Normalizes a provider subarchitecture suffix so machine and image identities can be compared. */
function primaryArchitecture(architecture: string): string {
  return architecture.split('/', 1)[0]
}

/**
 * Stable selection key for one image row. An image is identified across the fleet by its
 * integration plus the provider identity (id and architecture), the same key the catalog uses
 * for React keys and the overlay endpoints.
 */
function imageKey(image: OSImageCatalogRow): string {
  return `${image.integrationId}:${image.id}:${image.architecture}`
}

/** Renders the provider image identity with its copy action kept directly beside the ID. */
function ImageID({ imageId }: { imageId: string }) {
  if (!imageId) return null
  return (
    <span className="sw-image-id">
      <span className="sw-mono">{imageId}</span>
      <CopyButton value={imageId} label="Copy image ID" />
    </span>
  )
}

/** Projects a catalog row onto the identity a bulk action needs, with a label for messages. */
function toBulkTarget(image: OSImageCatalogRow): OSImageBulkTarget {
  return {
    integrationId: image.integrationId,
    imageId: image.id,
    architecture: image.architecture,
    name: image.name || image.id,
  }
}

/** Whether the image carries any swallow override or tag (name, OS, release, or tags). */
function hasOverride(image: OSImageCatalogRow): boolean {
  return Boolean(image.customName || image.customOsSystem || image.customRelease || image.tags.length)
}

/** Formats provider bytes with binary units while preserving an explicit unknown state. */
function formatImageSize(sizeBytes?: number): string {
  if (sizeBytes === undefined || !Number.isFinite(sizeBytes) || sizeBytes <= 0) return '—'
  return formatBytes(sizeBytes)
}

type ImageColumnKey = 'name' | 'osSystem' | 'release' | 'tags' | 'architecture' | 'size' | 'site' | 'integration' | 'refreshed'

/** One toggleable data column. Actions are always rendered and are not part of this set. */
interface ImageColumn {
  key: ImageColumnKey
  label: string
  /** Applied to both the header and body cell so a column carries its own sizing. */
  className?: string
  render: (image: OSImageCatalogRow) => ReactNode
}

// Every toggleable column, used to validate a saved choice. Kept separate from the default
// visible set so a column can exist (and be toggled on) without being shown by default.
const ALL_COLUMN_KEYS: ImageColumnKey[] = ['name', 'osSystem', 'release', 'tags', 'architecture', 'size', 'site', 'integration', 'refreshed']
// Release, Architecture, and Refreshed stay available from the Columns menu but are hidden by
// default; Tags and Size remain visible for at-a-glance catalog comparison.
const DEFAULT_VISIBLE_COLUMNS: ImageColumnKey[] = ['name', 'osSystem', 'tags', 'size', 'site', 'integration']
const COLUMNS_STORAGE_KEY = 'sw.osImages.visibleColumns'

/** Reads the operator's saved column choice, falling back to the default visible set. */
function loadVisibleColumns(): Set<ImageColumnKey> {
  try {
    const raw = localStorage.getItem(COLUMNS_STORAGE_KEY)
    if (raw) {
      const allowed = new Set<ImageColumnKey>(ALL_COLUMN_KEYS)
      const saved = (JSON.parse(raw) as ImageColumnKey[]).filter((key) => allowed.has(key))
      if (saved.length > 0) return new Set(saved)
    }
  } catch {
    // Malformed or unavailable storage should never hide the catalog: show the default set.
  }
  return new Set(DEFAULT_VISIBLE_COLUMNS)
}

/** Read-only live image catalog across scoped provisioner integrations. */
export function OSImagesPage() {
  const { sites: siteRepository, provisioning } = useApp()
  const { sites, siteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const focusedIntegrationId = searchParams.get('integrationId') ?? ''
  const focusedImageName = searchParams.get('imageName') ?? ''
  const focusedArchitecture = searchParams.get('architecture') ?? ''
  const focusedOSSystem = searchParams.get('osSystem') ?? ''
  const focusedRelease = searchParams.get('release') ?? ''
  const query = searchParams.get('query') ?? focusedImageName
  const [state, setState] = useState<CatalogState>({ status: 'loading' })
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [deleting, setDeleting] = useState<OSImageCatalogRow | null>(null)
  const [editing, setEditing] = useState<OSImageCatalogRow | null>(null)
  const [uploading, setUploading] = useState(false)
  // Scoped provisioner integrations, captured while loading the catalog so the upload dialog can
  // offer them without a second fetch. Upload targets a provisioner, so it is disabled until one
  // exists in scope; the backend still enforces which provisioners actually support upload.
  const [provisioners, setProvisioners] = useState<Integration[]>([])
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [bulkAction, setBulkAction] = useState<OSImageBulkAction | null>(null)
  const [visibleColumns, setVisibleColumns] = useState<Set<ImageColumnKey>>(loadVisibleColumns)

  useEffect(() => {
    try {
      localStorage.setItem(COLUMNS_STORAGE_KEY, JSON.stringify([...visibleColumns]))
    } catch {
      // Persisting the choice is best-effort; a storage failure must not break the page.
    }
  }, [visibleColumns])

  const toggleColumn = (key: ImageColumnKey) => {
    setVisibleColumns((prev) => {
      const next = new Set(prev)
      if (next.has(key)) {
        if (next.size === 1) return prev // keep at least one data column visible
        next.delete(key)
      } else {
        next.add(key)
      }
      return next
    })
  }

  // Load the scoped provisioner integrations and their live catalogs together, returning both so
  // callers set React state in their own callback rather than inside this async body (which would
  // trigger the set-state-in-effect lint) and so the upload dialog reuses the same integrations.
  const fetchCatalog = useCallback(async () => {
    const integrations = await siteRepository.listIntegrations({ siteId, kind: 'provisioner' })
    const catalog = await loadOSImageCatalog(provisioning, integrations)
    return { catalog, integrations }
  }, [provisioning, siteId, siteRepository])

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      const { catalog, integrations } = await fetchCatalog()
      setProvisioners(integrations)
      setState({ status: 'ready', data: catalog })
    } catch (error) {
      setState({ status: 'error', message: error instanceof Error ? error.message : 'Could not load OS images.' })
    }
  }, [fetchCatalog])

  useEffect(() => {
    let cancelled = false
    fetchCatalog()
      .then(({ catalog, integrations }) => {
        if (!cancelled) {
          setProvisioners(integrations)
          setState({ status: 'ready', data: catalog })
        }
      })
      .catch((error: Error) => {
        if (!cancelled) setState({ status: 'error', message: error.message })
      })
    return () => {
      cancelled = true
    }
  }, [fetchCatalog, refreshNonce])

  const items = useMemo(() => (state.status === 'ready' ? state.data.images : []), [state])
  const filtered = useMemo(() => {
    if (focusedIntegrationId && focusedImageName) {
      const architecture = primaryArchitecture(focusedArchitecture)
      const scoped = items.filter(
        (item) =>
          item.integrationId === focusedIntegrationId &&
          (!architecture || primaryArchitecture(item.architecture) === architecture),
      )
      const expectedID = focusedOSSystem && focusedRelease ? `${focusedOSSystem}/${focusedRelease}` : ''
      const byID = expectedID ? scoped.filter((item) => item.id === expectedID) : []
      if (byID.length > 0) return byID
      const byOSRelease =
        focusedOSSystem && focusedRelease
          ? scoped.filter((item) => item.osSystem === focusedOSSystem && item.release === focusedRelease)
          : []
      if (byOSRelease.length > 0) return byOSRelease
      return scoped.filter((item) => item.name === focusedImageName)
    }

    const needle = query.trim().toLowerCase()
    if (!needle) return items
    return items.filter((item) =>
      [item.name, item.id, item.osSystem, item.release, item.architecture, item.integrationName, ...item.tags].some((value) =>
        value.toLowerCase().includes(needle),
      ),
    )
  }, [
    focusedArchitecture,
    focusedImageName,
    focusedIntegrationId,
    focusedOSSystem,
    focusedRelease,
    items,
    query,
  ])

  /**
   * Editing the visible search leaves the focused-image mode and makes the URL-backed query
   * authoritative, so operators can broaden a Server deep link without a hidden exact filter.
   */
  const updateQuery = useCallback((value: string) => {
    const next = new URLSearchParams(searchParams)
    for (const key of ['integrationId', 'imageName', 'architecture', 'osSystem', 'release']) {
      next.delete(key)
    }
    if (value) next.set('query', value)
    else next.delete('query')
    setSearchParams(next, { replace: true })
  }, [searchParams, setSearchParams])

  const siteName = (id: string) => sites.find((site) => site.id === id)?.name ?? id

  // Selection is keyed by imageKey over the currently filtered rows. Keys that no longer match a
  // row (after a refresh or a narrower search) are harmless: every consumer intersects with the
  // visible rows, and a completed bulk action clears the set.
  const toggleOne = useCallback((key: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }, [])
  const setMany = useCallback((keys: string[], value: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev)
      for (const key of keys) {
        if (value) next.add(key)
        else next.delete(key)
      }
      return next
    })
  }, [])
  const clearSelection = useCallback(() => setSelected(new Set()), [])

  const filteredKeys = useMemo(() => filtered.map(imageKey), [filtered])
  const allFilteredSelected = filteredKeys.length > 0 && filteredKeys.every((key) => selected.has(key))
  const someFilteredSelected = filteredKeys.some((key) => selected.has(key))

  // Bulk targets are the selected rows still visible, split by eligibility: only custom images
  // are deletable, and only images carrying a swallow override can be reset.
  const selectedImages = useMemo(() => filtered.filter((image) => selected.has(imageKey(image))), [filtered, selected])
  const deletableTargets = selectedImages.filter((image) => image.providerOsSystem === 'custom').map(toBulkTarget)
  const deleteSkipped = selectedImages.filter((image) => image.providerOsSystem !== 'custom').map(toBulkTarget)
  const resettableTargets = selectedImages.filter(hasOverride).map(toBulkTarget)
  const resetSkipped = selectedImages.filter((image) => !hasOverride(image)).map(toBulkTarget)

  const columns: ImageColumn[] = [
    {
      key: 'name',
      label: 'Name',
      className: 'sw-col-name',
      render: (image) => (
        <span className="sw-image-name">
          <strong>{image.name || '-'}</strong>
          <ImageID imageId={image.id} />
        </span>
      ),
    },
    { key: 'osSystem', label: 'OS', render: (image) => image.osSystem || '-' },
    { key: 'release', label: 'Release', render: (image) => image.release || '-' },
    {
      key: 'tags',
      label: 'Tags',
      render: (image) =>
        image.tags.length ? (
          <span className="sw-image-tags">
            {image.tags.map((tag) => (
              <Badge key={tag} variant="subtle">
                {tag}
              </Badge>
            ))}
          </span>
        ) : (
          '-'
        ),
    },
    { key: 'architecture', label: 'Architecture', render: (image) => image.architecture || '-' },
    { key: 'size', label: 'Size', className: 'sw-col-size', render: (image) => formatImageSize(image.sizeBytes) },
    {
      key: 'site',
      label: 'Site',
      render: (image) => (
        <Link to={`/infrastructure/sites?site=${encodeURIComponent(image.siteId)}#site-${image.siteId}`}>{siteName(image.siteId)}</Link>
      ),
    },
    {
      key: 'integration',
      label: 'Provider',
      render: (image) => (
        <Link to={`/infrastructure/integrations?site=${encodeURIComponent(image.siteId)}#integration-${image.integrationId}`}>{image.integrationName}</Link>
      ),
    },
    { key: 'refreshed', label: 'Refreshed', className: 'sw-col-refreshed', render: (image) => formatDateTime(image.refreshedAt) },
  ]
  const activeColumns = columns.filter((column) => visibleColumns.has(column.key))

  return (
    <div className="operator-page">
      <PageHeader
        title="OS images"
        breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/deploy') }, { label: 'OS images' }]}
        actions={
          <>
            <Button variant="outline" onClick={() => navigate(scopedHref('/infrastructure/integrations'))}>
              Manage integrations
            </Button>
            <Tooltip content={provisioners.length === 0 ? 'Add a provisioner integration before uploading an image' : 'Upload a new OS image to a provisioner'}>
              <span>
                <Button colorPalette="brand" disabled={provisioners.length === 0} onClick={() => setUploading(true)}>
                  <Upload size={16} />
                  Upload image
                </Button>
              </span>
            </Tooltip>
            <Button variant="outline" onClick={() => setRefreshNonce((value) => value + 1)}>
              <RefreshCw size={16} />
              Refresh
            </Button>
          </>
        }
      />
      <ProvisioningTabs />
      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={updateQuery} placeholder="Search image, OS, release, or integration" aria-label="Search OS images" />
        <Menu.Root closeOnSelect={false}>
          <Menu.Trigger asChild>
            <Button variant="outline" size="sm" ms="auto">
              <Columns3 size={16} />
              Columns
            </Button>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content>
                {columns.map((column) => (
                  <Menu.CheckboxItem key={column.key} value={column.key} checked={visibleColumns.has(column.key)} onCheckedChange={() => toggleColumn(column.key)}>
                    {column.label}
                    <Menu.ItemIndicator />
                  </Menu.CheckboxItem>
                ))}
              </Menu.Content>
            </Menu.Positioner>
          </Portal>
        </Menu.Root>
        <SelectionToolbar count={selectedImages.length} onClear={clearSelection}>
          <Tooltip
            content={
              deletableTargets.length === 0
                ? 'None of the selected images can be deleted from their provider'
                : `Delete ${deletableTargets.length} image${deletableTargets.length === 1 ? '' : 's'}`
            }
          >
            <span>
              <Button size="sm" colorPalette="red" disabled={deletableTargets.length === 0} onClick={() => setBulkAction('delete')}>
                <Trash2 size={16} />
                Delete
              </Button>
            </span>
          </Tooltip>
          <Tooltip
            content={
              resettableTargets.length === 0
                ? 'Only images with a swallow override can be reset to provider values'
                : `Reset ${resettableTargets.length} image${resettableTargets.length === 1 ? '' : 's'} to provider values`
            }
          >
            <span>
              <Button size="sm" variant="outline" disabled={resettableTargets.length === 0} onClick={() => setBulkAction('reset-overrides')}>
                <RotateCcw size={16} />
                Reset overrides
              </Button>
            </span>
          </Tooltip>
        </SelectionToolbar>
      </DataToolbar>
      {state.status === 'loading' && <LoadingState rows={7} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
      {state.status === 'ready' &&
        state.data.failures.map((failure) => (
          <Alert key={failure.integrationId} status="warning" title={`${failure.integrationName} image catalog unavailable`}>
            {failure.message}
          </Alert>
        ))}
      {state.status === 'ready' && filtered.length === 0 && (
        <EmptyState title="No OS images" message={query ? 'Try another search.' : 'Refresh after adding a provisioner integration.'} />
      )}
      {state.status === 'ready' && filtered.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
          <Table.Root size="sm" aria-label="OS images" className="sw-provisioning-table">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader className="sw-cell-center sw-col-selection">
                  <Checkbox
                    aria-label="Select all shown images"
                    checked={allFilteredSelected ? true : someFilteredSelected ? 'indeterminate' : false}
                    onCheckedChange={() => setMany(filteredKeys, !allFilteredSelected)}
                  />
                </Table.ColumnHeader>
                {activeColumns.map((column) => (
                  <Table.ColumnHeader key={column.key} className={column.className}>
                    {column.label}
                  </Table.ColumnHeader>
                ))}
                <Table.ColumnHeader className="sw-col-actions" />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filtered.map((image) => (
                <Table.Row key={imageKey(image)} data-selected={selected.has(imageKey(image)) || undefined}>
                  <Table.Cell className="sw-cell-center sw-col-selection">
                    <Checkbox
                      aria-label={`Select ${image.name || image.id}`}
                      checked={selected.has(imageKey(image))}
                      onCheckedChange={() => toggleOne(imageKey(image))}
                    />
                  </Table.Cell>
                  {activeColumns.map((column) => (
                    <Table.Cell key={column.key} className={column.className}>
                      {column.render(image)}
                    </Table.Cell>
                  ))}
                  <Table.Cell className="sw-col-actions">
                    <span className="sw-row-actions">
                      <Tooltip content="Deploy this image">
                        <IconButton
                          variant="ghost"
                          size="sm"
                          aria-label={`Deploy ${image.name || image.id}`}
                          onClick={() => navigate(provisioningHref('/provisioning/deploy', { integrationId: image.integrationId, imageId: image.id }, scopedHref))}
                        >
                          <Rocket size={18} />
                        </IconButton>
                      </Tooltip>
                      <Tooltip content="Create a deployment template from this image">
                        <IconButton
                          variant="ghost"
                          size="sm"
                          aria-label={`Create template from ${image.name || image.id}`}
                          onClick={() =>
                            navigate(provisioningHref('/provisioning/templates', { create: '1', integrationId: image.integrationId, imageId: image.id }, scopedHref))
                          }
                        >
                          <FilePlus2 size={18} />
                        </IconButton>
                      </Tooltip>
                      <Tooltip content="Edit image labels (stored by swallow)">
                        <IconButton
                          variant="ghost"
                          size="sm"
                          aria-label={`Edit ${image.name || image.id}`}
                          onClick={() => setEditing(image)}
                        >
                          <Pencil size={18} />
                        </IconButton>
                      </Tooltip>
                      <Tooltip content={image.providerOsSystem === 'custom' ? 'Delete this image from its provider' : 'This image cannot be deleted from its provider'}>
                        <IconButton
                          variant="ghost"
                          size="sm"
                          colorPalette="red"
                          aria-disabled={image.providerOsSystem !== 'custom'}
                          aria-label={`Delete ${image.name || image.id}`}
                          onClick={() => {
                            if (image.providerOsSystem === 'custom') setDeleting(image)
                          }}
                        >
                          <Trash2 size={18} />
                        </IconButton>
                      </Tooltip>
                    </span>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list">
              {filtered.map((image) => (
                <ResourceCard
                  key={imageKey(image)}
                  title={image.name || image.id}
                  description={<ImageID imageId={image.id} />}
                  selected={selected.has(imageKey(image))}
                  status={
                    <Checkbox
                      aria-label={`Select ${image.name || image.id}`}
                      checked={selected.has(imageKey(image))}
                      onCheckedChange={() => toggleOne(imageKey(image))}
                    />
                  }
                  actions={
                    <>
                      <Button size="sm" colorPalette="brand" onClick={() => navigate(provisioningHref('/provisioning/deploy', { integrationId: image.integrationId, imageId: image.id }, scopedHref))}>
                        Deploy image
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => navigate(provisioningHref('/provisioning/templates', { create: '1', integrationId: image.integrationId, imageId: image.id }, scopedHref))}>
                        Create template
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => setEditing(image)}>
                        Edit
                      </Button>
                      {image.providerOsSystem === 'custom' && <Button size="sm" variant="plain" colorPalette="red" onClick={() => setDeleting(image)}>Delete image</Button>}
                    </>
                  }
                >
                  {activeColumns.filter((column) => column.key !== 'name').map((column) => (
                    <ResourceCardField key={column.key} label={column.label}>{column.render(image)}</ResourceCardField>
                  ))}
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}
      {deleting && (
        <DeleteImageDialog
          image={deleting}
          repository={provisioning}
          onClose={() => setDeleting(null)}
          onDeleted={() => {
            setDeleting(null)
            showToast({ tone: 'success', title: 'OS image deleted' })
            setRefreshNonce((value) => value + 1)
          }}
        />
      )}
      {editing && (
        <EditImageDialog
          image={editing}
          repository={provisioning}
          onClose={() => setEditing(null)}
          onSaved={(title) => {
            setEditing(null)
            showToast({ tone: 'success', title })
            setRefreshNonce((value) => value + 1)
          }}
        />
      )}
      {uploading && (
        <UploadImageDialog
          integrations={provisioners}
          repository={provisioning}
          onClose={() => setUploading(false)}
          onUploaded={(title) => {
            setUploading(false)
            showToast({ tone: 'success', title })
            setRefreshNonce((value) => value + 1)
          }}
        />
      )}
      {bulkAction && (
        <BulkImageActionDialog
          action={bulkAction}
          targets={bulkAction === 'delete' ? deletableTargets : resettableTargets}
          skipped={bulkAction === 'delete' ? deleteSkipped : resetSkipped}
          onClose={() => setBulkAction(null)}
          onDone={() => {
            setBulkAction(null)
            clearSelection()
            setRefreshNonce((value) => value + 1)
          }}
        />
      )}
    </div>
  )
}

/**
 * Sets or clears the swallow-owned display overlay (name, OS, release) for one image. Each field
 * is stored by swallow and merged over the provider value at read; a blank field uses the
 * provider value. Saving or resetting never mutates the provider.
 */
function EditImageDialog({
  image,
  repository,
  onClose,
  onSaved,
}: {
  image: OSImageCatalogRow
  repository: ProvisioningRepository
  onClose: () => void
  /** Called after a successful save or reset with the toast title to show. */
  onSaved: (title: string) => void
}) {
  const [name, setName] = useState(image.customName ?? '')
  const [osSystem, setOsSystem] = useState(image.customOsSystem ?? '')
  const [release, setRelease] = useState(image.customRelease ?? '')
  const [tags, setTags] = useState<string[]>(image.tags)
  const [tagDraft, setTagDraft] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  const close = () => {
    if (!submitting) onClose()
  }

  // Commit the in-progress tag text as a chip. Duplicates and blanks are ignored so the list
  // stays clean before it is sent; the backend normalizes again as the source of truth.
  const addTag = () => {
    const trimmed = tagDraft.trim()
    if (trimmed && !tags.includes(trimmed)) setTags([...tags, trimmed])
    setTagDraft('')
  }
  const removeTag = (tag: string) => setTags(tags.filter((entry) => entry !== tag))

  // Each field is optional: a blank field is sent as an empty override, which the backend treats
  // as "use the provider value". An all-blank save therefore reverts every field, mirroring reset.
  // The in-progress tag text is folded in so a value typed but not yet committed is not lost.
  const save = async () => {
    if (submitting) return
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
      })
      onSaved('OS image updated')
    } catch (caught) {
      // Keep the dialog open on failure so the operator can correct and retry; only a success
      // unmounts it, so submitting is not reset there.
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
      onSaved('OS image reset to provider values')
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The image could not be reset.')
      setSubmitting(false)
    }
  }

  const hasCustom = hasOverride(image)

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      title="Edit OS image labels"
      description={`Swallow label overrides do not change the image in ${image.integrationName} or what it deploys.`}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          {hasCustom && (
            <Button variant="outline" onClick={() => void reset()} disabled={submitting}>
              Reset to provider values
            </Button>
          )}
          <Button colorPalette="brand" onClick={() => void save()} loading={submitting} disabled={submitting}>
            Save
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Image could not be updated">
            {error}
          </Alert>
        )}
        <Field.Root>
          <Field.Label htmlFor="os-image-name">Name</Field.Label>
          <Input
            id="os-image-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={image.providerName}
            maxLength={200}
            autoFocus
          />
          <Field.HelperText>Leave blank to use the provider name: {image.providerName}</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="os-image-os">OS</Field.Label>
          <Input
            id="os-image-os"
            value={osSystem}
            onChange={(event) => setOsSystem(event.target.value)}
            placeholder={image.providerOsSystem}
            maxLength={200}
          />
          <Field.HelperText>Leave blank to use the provider OS: {image.providerOsSystem}</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="os-image-release">Release</Field.Label>
          <Input
            id="os-image-release"
            value={release}
            onChange={(event) => setRelease(event.target.value)}
            placeholder={image.providerRelease}
            maxLength={200}
          />
          <Field.HelperText>Leave blank to use the provider release: {image.providerRelease}</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label htmlFor="os-image-tags">Tags</Field.Label>
          <Input
            id="os-image-tags"
            value={tagDraft}
            onChange={(event) => setTagDraft(event.target.value)}
            onKeyDown={(event) => {
              // Enter or comma commits the current tag without submitting the form.
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
                <Badge key={tag} variant="subtle" gap="1">
                  {tag}
                  <button type="button" aria-label={`Remove tag ${tag}`} onClick={() => removeTag(tag)}>
                    <X size={12} />
                  </button>
                </Badge>
              ))}
            </HStack>
          )}
          <Field.HelperText>Swallow-owned labels for organizing and searching images.</Field.HelperText>
        </Field.Root>
      </Stack>
    </Modal>
  )
}

/** Confirms permanent provider-side OS Image deletion before the destructive request is sent. */
function DeleteImageDialog({
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
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button colorPalette="red" onClick={() => void submit()} loading={submitting} disabled={submitting}>
            Delete image
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Image could not be deleted">
            {error}
          </Alert>
        )}
        <Alert status="warning" title="This cannot be undone">
          This will permanently remove <strong>{image.name || image.id}</strong> ({image.architecture}) from its provider. Deployment templates and
          in-flight deployments that reference this image may fail after deletion. Make the image available again or update the affected templates before retrying.
        </Alert>
      </Stack>
    </Modal>
  )
}
