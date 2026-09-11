import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Button, IconButton, Menu, Portal, Stack, Table } from '@chakra-ui/react'
import { Columns3, FilePlus2, RefreshCw, Rocket, Trash2 } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import { loadOSImageCatalog, type OSImageCatalog, type OSImageCatalogRow } from '@/application/usecases/provisioning/loadOSImageCatalog'
import { useApp } from '@/di/AppProvider'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { ProvisioningTabs } from './ProvisioningTabs'

type CatalogState = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; data: OSImageCatalog }

function provisioningHref(path: string, params: Record<string, string>, scopedHref: (path: string) => string): string {
  const target = new URL(scopedHref(path), window.location.origin)
  for (const [key, value] of Object.entries(params)) target.searchParams.set(key, value)
  return `${target.pathname}${target.search}`
}

type ImageColumnKey = 'name' | 'imageId' | 'osSystem' | 'release' | 'architecture' | 'site' | 'integration' | 'refreshed'

/** One toggleable data column. Actions are always rendered and are not part of this set. */
interface ImageColumn {
  key: ImageColumnKey
  label: string
  /** Applied to both the header and body cell so a column carries its own sizing. */
  className?: string
  render: (image: OSImageCatalogRow) => ReactNode
}

const DEFAULT_VISIBLE_COLUMNS: ImageColumnKey[] = ['name', 'imageId', 'osSystem', 'release', 'architecture', 'site', 'integration', 'refreshed']
const COLUMNS_STORAGE_KEY = 'sw.osImages.visibleColumns'

/** Reads the operator's saved column choice, falling back to every column. */
function loadVisibleColumns(): Set<ImageColumnKey> {
  try {
    const raw = localStorage.getItem(COLUMNS_STORAGE_KEY)
    if (raw) {
      const allowed = new Set<ImageColumnKey>(DEFAULT_VISIBLE_COLUMNS)
      const saved = (JSON.parse(raw) as ImageColumnKey[]).filter((key) => allowed.has(key))
      if (saved.length > 0) return new Set(saved)
    }
  } catch {
    // Malformed or unavailable storage should never hide the catalog: show everything.
  }
  return new Set(DEFAULT_VISIBLE_COLUMNS)
}

/** Read-only live image catalog across scoped provisioner integrations. */
export function OSImagesPage() {
  const { sites: siteRepository, provisioning } = useApp()
  const { sites, siteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const [state, setState] = useState<CatalogState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [deleting, setDeleting] = useState<OSImageCatalogRow | null>(null)
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

  const fetchCatalog = useCallback(async () => {
    const integrations = await siteRepository.listIntegrations({ siteId, kind: 'provisioner' })
    return loadOSImageCatalog(provisioning, integrations)
  }, [provisioning, siteId, siteRepository])

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      setState({ status: 'ready', data: await fetchCatalog() })
    } catch (error) {
      setState({ status: 'error', message: error instanceof Error ? error.message : 'Could not load OS images.' })
    }
  }, [fetchCatalog])

  useEffect(() => {
    let cancelled = false
    fetchCatalog()
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
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
    const needle = query.trim().toLowerCase()
    if (!needle) return items
    return items.filter((item) =>
      [item.name, item.id, item.osSystem, item.release, item.architecture, item.integrationName].some((value) => value.toLowerCase().includes(needle)),
    )
  }, [items, query])

  const siteName = (id: string) => sites.find((site) => site.id === id)?.name ?? id

  const columns: ImageColumn[] = [
    { key: 'name', label: 'Name', render: (image) => <strong>{image.name || '-'}</strong> },
    {
      key: 'imageId',
      label: 'Image ID',
      className: 'sw-col-imageid',
      render: (image) =>
        image.id ? (
          <span className="sw-image-id">
            <span className="sw-mono">{image.id}</span>
            <CopyButton value={image.id} label="Copy image ID" />
          </span>
        ) : (
          '-'
        ),
    },
    { key: 'osSystem', label: 'OS', render: (image) => image.osSystem || '-' },
    {
      key: 'release',
      label: 'Release',
      render: (image) => (
        <span className="sw-release-value" title={image.release || undefined}>
          {image.release || '-'}
        </span>
      ),
    },
    { key: 'architecture', label: 'Architecture', render: (image) => image.architecture || '-' },
    {
      key: 'site',
      label: 'Site',
      render: (image) => (
        <Link to={`/infrastructure/sites?site=${encodeURIComponent(image.siteId)}#site-${image.siteId}`}>{siteName(image.siteId)}</Link>
      ),
    },
    {
      key: 'integration',
      label: 'Provider integration',
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
        subtitle="Live, read-only images offered by provisioner Integrations registered under each Site."
        breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/deploy') }, { label: 'OS images' }]}
        actions={
          <>
            <Button variant="outline" onClick={() => navigate(scopedHref('/infrastructure/integrations'))}>
              Manage integrations
            </Button>
            <Button variant="outline" onClick={() => setRefreshNonce((value) => value + 1)}>
              <RefreshCw size={16} />
              Refresh
            </Button>
          </>
        }
      />
      <ProvisioningTabs />
      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={setQuery} placeholder="Search image, OS, release, or integration" aria-label="Search OS images" />
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
        <EmptyState title="No OS images" message={query ? 'No live image matches this search.' : 'No scoped provisioner returned an image.'} />
      )}
      {state.status === 'ready' && filtered.length > 0 && (
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="OS images" className="sw-provisioning-table">
            <Table.Header>
              <Table.Row>
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
                <Table.Row key={`${image.integrationId}:${image.id}:${image.architecture}`}>
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
                      <Tooltip content={image.osSystem === 'custom' ? 'Delete this custom image from the provider' : 'Only uploaded custom images can be deleted'}>
                        <IconButton
                          variant="ghost"
                          size="sm"
                          colorPalette="red"
                          aria-disabled={image.osSystem !== 'custom'}
                          aria-label={`Delete ${image.name || image.id}`}
                          onClick={() => {
                            if (image.osSystem === 'custom') setDeleting(image)
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
    </div>
  )
}

/** Danger confirmation for removing a provider-owned custom image; deletion is irreversible. */
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
      description={`This permanently removes the image from ${image.integrationName}.`}
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
          The custom image <strong>{image.name || image.id}</strong> ({image.architecture}) is deleted from the provider. Re-uploading it is the only way
          back, and any deployment template or in-flight deployment that references it will fail until it is replaced.
        </Alert>
      </Stack>
    </Modal>
  )
}
