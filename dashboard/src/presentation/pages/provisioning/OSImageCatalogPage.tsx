import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import './os-image-catalog.css'
import {
  Badge,
  Box,
  Button,
  Flex,
  Heading,
  HStack,
  IconButton,
  Menu,
  Popover,
  Portal,
  Table,
  Text,
} from '@chakra-ui/react'
import {
  ArrowUpRight,
  ChevronDown,
  ChevronRight,
  EllipsisVertical,
  FilePlus2,
  Filter,
  PenLine,
  RefreshCw,
  RotateCcw,
  FlaskConical,
  Settings2,
  SlidersHorizontal,
  Trash2,
  Upload,
} from 'lucide-react'
import { Link as RouterLink, useNavigate, useSearchParams } from 'react-router-dom'
import type { OSImageCatalogFailure, OSImageCatalogRow } from '@/application/usecases/provisioning/loadOSImageCatalog'
import type { Site } from '@/domain/site/types'
import { useApp } from '@/di/AppProvider'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { LoadingState } from '@/presentation/components/LoadingState'
import { InventorySurface, SelectionToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { Pagination } from '@/presentation/components/Pagination'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { ResourceTag } from '@/presentation/components/ResourceTag'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Select } from '@/presentation/components/ui/select'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { formatBytes } from '@/shared/utils/bytes'
import { formatDateTime } from '@/shared/utils/time'
import { BulkImageActionDialog } from './BulkImageActionDialog'
import { ProvisioningTabs } from './ProvisioningTabs'
import { UploadImageDialog } from './UploadImageDialog'
import { TestImageDeploymentDialog } from './TestImageDeploymentDialog'
import { EditImageDialog, DeleteImageDialog } from './OSImageDialogs'
import type { OSImageBulkAction, OSImageBulkTarget } from './useOSImageBulkActions'
import { useOSImageCatalog } from './useOSImageCatalog'
import { useOSImageVerificationActivity } from './useOSImageVerificationActivity'
import {
  compareOSImages,
  groupOSImages,
  hasOSImageFilters,
  hasOSImageOverride,
  matchesOSImageFocus,
  matchesOSImageInventory,
  normalizeOSImageInventoryParams,
  osImageCatalogFacts,
  osImageContextAction,
  osImageFacetOptions,
  osImageKey,
  osImageDeployModeStatus,
  osImageOSFamilyLabel,
  parseOSImageInventoryQuery,
  type OSImageCatalogFacts,
  type OSImageFacetOption,
  type OSImageGroup,
  type OSImageInventoryQuery,
  type OSImageSort,
  type OSImageTarget,
  type OSImageDeployModeStatus,
  type OSImageVerificationActivityMap,
} from './osImageListPresentation'

const PAGE_SIZE_KEY = 'swallow.osImages.pageSize'
const DENSITY_KEY = 'swallow.osImages.density'
const PAGE_SIZES = [25, 50, 100] as const

type CatalogDensity = 'compact' | 'comfortable'

interface BulkDialogState {
  action: OSImageBulkAction
  targets: OSImageBulkTarget[]
  skipped: OSImageBulkTarget[]
}

function readPageSize(): number {
  try {
    const value = Number(localStorage.getItem(PAGE_SIZE_KEY))
    return PAGE_SIZES.includes(value as (typeof PAGE_SIZES)[number]) ? value : 25
  } catch {
    return 25
  }
}

function readDensity(): CatalogDensity {
  try {
    return localStorage.getItem(DENSITY_KEY) === 'compact' ? 'compact' : 'comfortable'
  } catch {
    return 'comfortable'
  }
}

function writePreference(key: string, value: string | number): void {
  try {
    localStorage.setItem(key, String(value))
  } catch {
    // Readability preferences are best-effort and never block the catalog.
  }
}

function provisioningHref(path: string, params: Record<string, string>, scopedHref: (path: string) => string): string {
  const target = new URL(scopedHref(path), window.location.origin)
  for (const [key, value] of Object.entries(params)) target.searchParams.set(key, value)
  return `${target.pathname}${target.search}`
}

function toBulkTarget(image: OSImageCatalogRow): OSImageBulkTarget {
  return { integrationId: image.integrationId, imageId: image.id, architecture: image.architecture, name: image.name || image.id }
}

function formatImageSize(sizeBytes?: number): string {
  if (sizeBytes === undefined || !Number.isFinite(sizeBytes) || sizeBytes <= 0) return 'Unknown'
  return formatBytes(sizeBytes)
}

function ImageID({ imageId }: { imageId: string }) {
  return <span className="sw-image-id"><span className="sw-mono">{imageId}</span><CopyButton value={imageId} label="Copy image ID" /></span>
}

function CatalogSearch({ value, onCommit }: { value: string; onCommit: (value: string) => void }) {
  const [draft, setDraft] = useState(value)
  useEffect(() => {
    if (draft === value) return
    const handle = setTimeout(() => onCommit(draft), 300)
    return () => clearTimeout(handle)
  }, [draft, onCommit, value])
  return <SearchInput value={draft} onChange={setDraft} placeholder="Search image, ID, OS, release, tag, or user" aria-label="Search OS images" maxW="30rem" size="md" />
}

function PopoverButton({ title, trigger, children }: { title: string; trigger: ReactNode; children: ReactNode }) {
  return (
    <Popover.Root positioning={{ placement: 'bottom-end' }}>
      <Popover.Trigger asChild>{trigger}</Popover.Trigger>
      <Portal><Popover.Positioner><Popover.Content className="sw-os-image-popover"><Popover.Arrow /><Popover.Header fontWeight="semibold">{title}</Popover.Header><Popover.Body>{children}</Popover.Body></Popover.Content></Popover.Positioner></Portal>
    </Popover.Root>
  )
}

function controlId(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'blank'
}

function updatedValues(values: readonly string[], value: string, checked: boolean): string[] {
  return checked ? [...values, value] : values.filter((item) => item !== value)
}

function FacetOptions({ title, param, values, selected, onChange }: {
  title: string
  param: string
  values: readonly OSImageFacetOption[]
  selected: readonly string[]
  onChange: (param: string, values: readonly string[]) => void
}) {
  if (values.length === 0) return null
  return (
    <fieldset className="sw-filter-group">
      <legend>{title}</legend>
      {values.map((option) => <Checkbox key={option.value} id={`os-image-${param}-${controlId(option.value)}`} checked={selected.includes(option.value)} onCheckedChange={(checked) => onChange(param, updatedValues(selected, option.value, checked))}>{`${option.label} (${option.count})`}</Checkbox>)}
    </fieldset>
  )
}

function CatalogFilterPanel({ query, facets, onSingle, onMulti, onClear }: {
  query: OSImageInventoryQuery
  facets: ReturnType<typeof osImageFacetOptions>
  onSingle: (key: string, value: string) => void
  onMulti: (key: string, values: readonly string[]) => void
  onClear: () => void
}) {
  return (
    <div className="sw-os-image-filter-panel">
      <div className="sw-os-image-filter-panel__selects">
        <fieldset className="sw-filter-group"><legend>Deploy mode</legend><Select value={query.target} aria-label="Filter deploy mode" size="sm" onChange={(value) => onSingle('target', value === 'any' ? '' : value)} options={[{ value: 'any', label: 'Any mode' }, { value: 'disk', label: 'Disk deploy' }, { value: 'ram', label: 'RAM deploy' }]} /></fieldset>
        <fieldset className="sw-filter-group"><legend>Test status</legend><Select value={query.readiness} aria-label="Filter deployment test status" size="sm" disabled={query.target === 'any'} onChange={(value) => onSingle('readiness', value === 'any' ? '' : value)} options={[{ value: 'any', label: 'Any status' }, { value: 'available', label: 'Supported' }, { value: 'verifying', label: 'Testing' }, { value: 'requires_attention', label: 'Needs attention' }, { value: 'failed', label: 'Failed' }, { value: 'not_verified', label: 'Not tested' }]} /></fieldset>
      </div>
      <FacetOptions title="OS family" param="os" values={facets.os} selected={query.os} onChange={onMulti} />
      <FacetOptions title="Architecture" param="architecture" values={facets.architectures} selected={query.architectures} onChange={onMulti} />
      <FacetOptions title="Tags" param="tag" values={facets.tags} selected={query.tags} onChange={onMulti} />
      <FacetOptions title="Provider" param="integration" values={facets.integrations} selected={query.integrations} onChange={onMulti} />
      <Button variant="plain" size="sm" onClick={onClear}>Clear filters</Button>
    </div>
  )
}

function DisplayPanel({ query, density, pageSize, onGroup, onSort, onDirection, onDensity, onPageSize }: {
  query: OSImageInventoryQuery
  density: CatalogDensity
  pageSize: number
  onGroup: (value: OSImageGroup) => void
  onSort: (value: OSImageSort) => void
  onDirection: (value: 'asc' | 'desc') => void
  onDensity: (value: CatalogDensity) => void
  onPageSize: (value: number) => void
}) {
  return (
    <div className="sw-os-image-display-panel">
      <Select value={query.group} aria-label="Group OS images" size="sm" onChange={(value) => onGroup(value as OSImageGroup)} options={[{ value: 'none', label: 'No grouping' }, { value: 'os', label: 'Group by OS family' }, { value: 'provider', label: 'Group by Provider' }]} />
      <Select value={query.sort} aria-label="Sort OS images" size="sm" onChange={(value) => onSort(value as OSImageSort)} options={[{ value: 'os', label: 'OS and release' }, { value: 'name', label: 'Image name' }, { value: 'release', label: 'Release' }, { value: 'size', label: 'Artifact size' }, { value: 'refreshed', label: 'Catalog refresh' }]} />
      <Select value={query.direction} aria-label="Sort direction" size="sm" onChange={(value) => onDirection(value as 'asc' | 'desc')} options={[{ value: 'asc', label: 'Ascending' }, { value: 'desc', label: 'Descending' }]} />
      <Select value={density} aria-label="Table density" size="sm" onChange={(value) => onDensity(value as CatalogDensity)} options={[{ value: 'compact', label: 'Compact rows' }, { value: 'comfortable', label: 'Comfortable rows' }]} />
      <Select value={String(pageSize)} aria-label="Rows per page" size="sm" onChange={(value) => onPageSize(Number(value))} options={PAGE_SIZES.map((size) => ({ value: String(size), label: `${size} rows` }))} />
    </div>
  )
}

function CatalogOverview({ facts, partial }: { facts: OSImageCatalogFacts; partial: boolean }) {
  const metrics = [['Images loaded', facts.loaded], ['Disk supported', facts.diskSupported], ['RAM supported', facts.ramSupported], ['Failed tests', facts.failedTargets], ['Unavailable providers', facts.unavailableProviders]] as const
  return (
    <Box as="section" className="sw-os-image-overview" aria-labelledby="os-image-overview-title">
      <div className="sw-os-image-overview__heading"><div><Text className="sw-inventory-surface__eyebrow">OS image catalog</Text><Heading as="h2" id="os-image-overview-title" size="lg">Image catalog</Heading></div><Text color="fg.muted" fontSize="sm">{partial ? 'Counts cover the provider catalogs that loaded successfully.' : 'Images and supported deploy modes across connected provisioners.'}</Text></div>
      <dl className="sw-os-image-overview__metrics">{metrics.map(([label, value]) => <div key={label} data-alert={(label.includes('Failed') || label.includes('Unavailable')) && value > 0 || undefined}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
    </Box>
  )
}

function ImageTitle({ image }: { image: OSImageCatalogRow }) {
  return (
    <HStack className="sw-os-image-title" gap="2" wrap="wrap">
      <span className="sw-inventory-classifier sw-os-image-os-family">
        {osImageOSFamilyLabel(image.osSystem)}
      </span>
      <strong>{image.name || image.id}</strong>
    </HStack>
  )
}

function ImageInventoryCell({ image }: { image: OSImageCatalogRow }) {
  return (
    <div className="sw-os-image-inventory-copy">
      <ImageID imageId={image.id} />
      <span className="sw-os-image-artifact-line"><span className="sw-mono">{image.architecture || 'Unknown'}</span><span aria-hidden> · </span><span>{formatImageSize(image.sizeBytes)}</span></span>
    </div>
  )
}

function deployModeTone(status: OSImageDeployModeStatus): string {
  if (status.kind === 'supported') return 'available'
  if (status.kind === 'failed') return 'failed'
  if (status.kind === 'requires_attention') return 'attention'
  if (status.kind === 'testing') return 'changing'
  return 'neutral'
}

function DeployModeStatus({ target, status, scopedHref }: { target: OSImageTarget; status: OSImageDeployModeStatus; scopedHref: (path: string) => string }) {
  const content = <span className="sw-os-image-target" data-tone={deployModeTone(status)}><span className="sw-os-image-target__label">{target === 'disk' ? 'Disk' : 'RAM'}</span><span className="sw-os-image-target__state">{status.kind === 'testing' && <InProgressSpinner />}{status.label}</span></span>
  return <Tooltip content={status.detail}>{status.operationId ? <RouterLink className="sw-os-image-target-link" to={scopedHref(`/workflows/${status.operationId}`)}>{content}</RouterLink> : content}</Tooltip>
}

function DeployModesCell({ image, activities, scopedHref }: { image: OSImageCatalogRow; activities: OSImageVerificationActivityMap; scopedHref: (path: string) => string }) {
  return <div className="sw-os-image-deploy-modes"><DeployModeStatus target="disk" status={osImageDeployModeStatus(image, 'disk', activities)} scopedHref={scopedHref} /><DeployModeStatus target="ram" status={osImageDeployModeStatus(image, 'ram', activities)} scopedHref={scopedHref} /></div>
}

function TagSummary({ tags }: { tags: readonly string[] }) {
  if (tags.length === 0) return <span className="sw-empty-value">No tags</span>
  const sorted = [...tags].sort((left, right) => left.localeCompare(right))
  return <span className="sw-os-image-tags">{sorted.map((tag) => <ResourceTag key={tag}>{tag}</ResourceTag>)}</span>
}

function DefaultUserCell({ image }: { image: OSImageCatalogRow }) {
  return image.defaultUser ? <span className="sw-mono">{image.defaultUser}</span> : <span className="sw-empty-value">Not set</span>
}

function SourceCell({ image, siteName }: { image: OSImageCatalogRow; siteName: string }) {
  return <div className="sw-os-image-source"><RouterLink to={`/infrastructure/integrations?site=${encodeURIComponent(image.siteId)}#integration-${image.integrationId}`}>{image.integrationName}</RouterLink><RouterLink to={`/infrastructure/sites?site=${encodeURIComponent(image.siteId)}#site-${image.siteId}`}>{siteName}</RouterLink></div>
}

function DefaultUserDetail({ image }: { image: OSImageCatalogRow }) {
  if (image.defaultUser) return <span className="sw-mono">{image.defaultUser}</span>
  return <>Not set · automation falls back to the Site SSH user and built-in candidates</>
}

function ImageDetails({ image, siteName, activities, scopedHref }: { image: OSImageCatalogRow; siteName: string; activities: OSImageVerificationActivityMap; scopedHref: (path: string) => string }) {
  return (
    <div className="sw-os-image-details">
      <section><Heading as="h3" size="sm">Image</Heading><dl><div><dt>Name</dt><dd>{image.name || image.id}</dd></div><div><dt>OS family</dt><dd>{osImageOSFamilyLabel(image.osSystem)}</dd></div><div><dt>Release</dt><dd>{image.release || 'Not set'}</dd></div><div><dt>Image ID</dt><dd><ImageID imageId={image.id} /></dd></div></dl></section>
      <section><Heading as="h3" size="sm">Organization</Heading><dl><div><dt>Tags</dt><dd><TagSummary tags={image.tags} /></dd></div><div><dt>Default user</dt><dd><DefaultUserDetail image={image} /></dd></div></dl></section>
      <section><Heading as="h3" size="sm">Deploy modes</Heading><div className="sw-os-image-details__deploy-modes"><DeployModeStatus target="disk" status={osImageDeployModeStatus(image, 'disk', activities)} scopedHref={scopedHref} /><DeployModeStatus target="ram" status={osImageDeployModeStatus(image, 'ram', activities)} scopedHref={scopedHref} /></div></section>
      <section><Heading as="h3" size="sm">Artifact and source</Heading><dl><div><dt>Architecture</dt><dd className="sw-mono">{image.architecture || 'Unknown'}</dd></div><div><dt>Size</dt><dd>{formatImageSize(image.sizeBytes)}</dd></div><div><dt>Provider</dt><dd><RouterLink to={`/infrastructure/integrations?site=${encodeURIComponent(image.siteId)}#integration-${image.integrationId}`}>{image.integrationName}</RouterLink></dd></div><div><dt>Site</dt><dd>{siteName}</dd></div><div><dt>Catalog refreshed</dt><dd>{formatDateTime(image.refreshedAt)}</dd></div></dl></section>
    </div>
  )
}

function ContextAction({ image, activities, scopedHref, onTest }: { image: OSImageCatalogRow; activities: OSImageVerificationActivityMap; scopedHref: (path: string) => string; onTest: (target: OSImageTarget) => void }) {
  const action = osImageContextAction(image, activities)
  if (action.kind === 'workflow') {
    const shortLabel = action.label === 'Review workflow' ? 'Review' : 'View'
    return <Tooltip content={action.label}><Button asChild className="sw-os-image-context-action" size="sm" variant={action.label === 'Review workflow' ? 'solid' : 'outline'} colorPalette={action.label === 'Review workflow' ? 'brand' : undefined}><RouterLink aria-label={action.label} to={scopedHref(`/workflows/${action.operationId}`)}>{shortLabel}<ArrowUpRight size={14} /></RouterLink></Button></Tooltip>
  }
  if (action.kind === 'test') return <Tooltip content="Test deployment"><Button className="sw-os-image-context-action" size="sm" colorPalette="brand" aria-label="Test deployment" onClick={() => onTest(action.target)}><FlaskConical size={15} />Test</Button></Tooltip>
  return <Tooltip content="Deploy OS"><Button asChild className="sw-os-image-context-action" size="sm" colorPalette="brand"><RouterLink aria-label="Deploy OS" to={provisioningHref('/provisioning/deploy', { integrationId: image.integrationId, imageId: image.id }, scopedHref)}>Deploy<ArrowUpRight size={14} /></RouterLink></Button></Tooltip>
}

function ImageActionsMenu({ image, templatesEnabled, scopedHref, onEdit, onTest, onDelete }: { image: OSImageCatalogRow; templatesEnabled: boolean; scopedHref: (path: string) => string; onEdit?: () => void; onTest: () => void; onDelete: () => void }) {
  return (
    <Menu.Root><Menu.Trigger asChild><IconButton variant="ghost" size="sm" aria-label={`More actions for ${image.name || image.id}`}><EllipsisVertical size={17} /></IconButton></Menu.Trigger><Portal><Menu.Positioner><Menu.Content>
      {onEdit && <Menu.Item value="edit-image-settings" onClick={onEdit}><Settings2 size={15} />Edit image settings</Menu.Item>}
      {templatesEnabled && <Menu.Item value="template" asChild><RouterLink to={provisioningHref('/provisioning/templates', { create: '1', integrationId: image.integrationId, imageId: image.id }, scopedHref)}><FilePlus2 size={15} />Create deployment template</RouterLink></Menu.Item>}
      {image.providerOsSystem === 'custom' && <Menu.Item value="test-deployment" onClick={onTest}><FlaskConical size={15} />Test deployment</Menu.Item>}
      {image.providerOsSystem === 'custom' && <Menu.Item value="delete" color="fg.error" onClick={onDelete}><Trash2 size={15} />Delete image</Menu.Item>}
    </Menu.Content></Menu.Positioner></Portal></Menu.Root>
  )
}

interface ImagePresentationProps {
  image: OSImageCatalogRow
  checked: boolean
  siteName: string
  activities: OSImageVerificationActivityMap
  templatesEnabled: boolean
  scopedHref: (path: string) => string
  onToggle: () => void
  onEdit: () => void
  onTest: (target: OSImageTarget) => void
  onDelete: () => void
}

function ImageRow({ image, checked, siteName, activities, templatesEnabled, scopedHref, onToggle, onEdit, onTest, onDelete }: ImagePresentationProps) {
  const [expanded, setExpanded] = useState(false)
  const detailId = `os-image-details-${controlId(osImageKey(image))}`
  const displayName = image.name || image.id
  return <>
    <Table.Row className="sw-os-image-title-row" data-selected={checked || undefined} aria-label={`${displayName} image`}>
      <Table.Cell rowSpan={2} className="sw-cell-center sw-os-image-col--select"><Checkbox aria-label={`Select ${displayName}`} checked={checked} onCheckedChange={onToggle} /></Table.Cell>
      <Table.Cell rowSpan={2} className="sw-cell-center sw-os-image-col--expand"><IconButton variant="ghost" size="xs" aria-label={`${expanded ? 'Hide' : 'Show'} details for ${displayName}`} aria-expanded={expanded} aria-controls={detailId} onClick={() => setExpanded((value) => !value)}>{expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}</IconButton></Table.Cell>
      <Table.Cell colSpan={6} className="sw-os-image-title-cell"><ImageTitle image={image} /></Table.Cell>
    </Table.Row>
    <Table.Row className="sw-os-image-data-row" data-selected={checked || undefined} aria-label={`${displayName} catalog data`}>
      <Table.Cell className="sw-os-image-col--identity"><ImageInventoryCell image={image} /></Table.Cell>
      <Table.Cell className="sw-os-image-col--deploy-modes"><DeployModesCell image={image} activities={activities} scopedHref={scopedHref} /></Table.Cell>
      <Table.Cell className="sw-os-image-col--tags"><TagSummary tags={image.tags} /></Table.Cell>
      <Table.Cell className="sw-os-image-col--default-user"><DefaultUserCell image={image} /></Table.Cell>
      <Table.Cell className="sw-os-image-col--source"><SourceCell image={image} siteName={siteName} /></Table.Cell>
      <Table.Cell className="sw-os-image-col--action"><HStack gap="1" wrap="nowrap"><ContextAction image={image} activities={activities} scopedHref={scopedHref} onTest={onTest} /><ImageActionsMenu image={image} templatesEnabled={templatesEnabled} scopedHref={scopedHref} onEdit={onEdit} onTest={() => onTest('disk')} onDelete={onDelete} /></HStack></Table.Cell>
    </Table.Row>
    {expanded && <Table.Row id={detailId} className="sw-os-image-detail-row"><Table.Cell colSpan={8}><ImageDetails image={image} siteName={siteName} activities={activities} scopedHref={scopedHref} /></Table.Cell></Table.Row>}
  </>
}

function ImageCard({ image, checked, siteName, activities, templatesEnabled, scopedHref, onToggle, onEdit, onTest, onDelete }: ImagePresentationProps) {
  return <ResourceCard title={<ImageTitle image={image} />} status={<Checkbox aria-label={`Select ${image.name || image.id}`} checked={checked} onCheckedChange={onToggle} />} selected={checked} details={<ResourceCardField label="Catalog details"><ImageDetails image={image} siteName={siteName} activities={activities} scopedHref={scopedHref} /></ResourceCardField>} actions={<><ContextAction image={image} activities={activities} scopedHref={scopedHref} onTest={onTest} /><Button size="sm" variant="outline" onClick={onEdit}><PenLine size={14} />Edit image</Button><ImageActionsMenu image={image} templatesEnabled={templatesEnabled} scopedHref={scopedHref} onTest={() => onTest('disk')} onDelete={onDelete} /></>}>
    <ResourceCardField label="Image"><ImageInventoryCell image={image} /></ResourceCardField>
    <ResourceCardField label="Deploy modes"><DeployModesCell image={image} activities={activities} scopedHref={scopedHref} /></ResourceCardField>
    <ResourceCardField label="Tags"><TagSummary tags={image.tags} /></ResourceCardField>
    <ResourceCardField label="Default user"><DefaultUserCell image={image} /></ResourceCardField>
  </ResourceCard>
}

function activeFilterCount(query: OSImageInventoryQuery): number {
  return query.os.length + query.architectures.length + query.tags.length + query.integrations.length + (query.target === 'any' ? 0 : 1) + (query.readiness === 'any' ? 0 : 1)
}

/** Deployment-focused OS Image catalog with operator-owned discovery metadata. */
export function OSImageCatalogPage() {
  const { provisioning, servers } = useApp()
  const { sites, siteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { state, reload, isRefreshing } = useOSImageCatalog(siteId || undefined)
  const activity = useOSImageVerificationActivity(siteId || undefined, reload)
  const uploadEnabled = useExperimentalFeature('osImageUpload')
  const templatesEnabled = useExperimentalFeature('deploymentTemplates')
  const [density, setDensity] = useState<CatalogDensity>(readDensity)
  const [pageSize, setPageSize] = useState(readPageSize)
  const [editing, setEditing] = useState<OSImageCatalogRow | null>(null)
  const [deleting, setDeleting] = useState<OSImageCatalogRow | null>(null)
  const [testing, setTesting] = useState<{ image: OSImageCatalogRow; target: OSImageTarget } | null>(null)
  const [uploading, setUploading] = useState(false)
  const [bulk, setBulk] = useState<BulkDialogState | null>(null)
  const query = useMemo(() => parseOSImageInventoryQuery(searchParams), [searchParams])

  useEffect(() => {
    const normalized = normalizeOSImageInventoryParams(searchParams)
    if (normalized.toString() !== searchParams.toString()) setSearchParams(normalized, { replace: true })
  }, [searchParams, setSearchParams])

  const updateParams = useCallback((mutate: (next: URLSearchParams) => void) => {
    const next = new URLSearchParams(searchParams)
    mutate(next)
    setSearchParams(normalizeOSImageInventoryParams(next), { replace: true })
  }, [searchParams, setSearchParams])

  const commitSearch = useCallback((value: string) => updateParams((next) => {
    for (const key of ['integrationId', 'imageName', 'architecture', 'osSystem', 'release']) next.delete(key)
    if (value.trim()) next.set('q', value)
    else next.delete('q')
    next.delete('page')
  }), [updateParams])
  const clearFilters = useCallback(() => updateParams((next) => {
    for (const key of ['q', 'query', 'view', 'os', 'architecture', 'tag', 'integration', 'target', 'readiness', 'page', 'integrationId', 'imageName', 'osSystem', 'release']) next.delete(key)
  }), [updateParams])

  const items = useMemo(() => state.status === 'ready' ? state.catalog.images : [], [state])
  const failures = useMemo(() => state.status === 'ready' ? state.catalog.failures : [], [state])
  const provisioners = useMemo(() => state.status === 'ready' ? state.integrations : [], [state])
  const facts = useMemo(() => osImageCatalogFacts(items, failures), [failures, items])
  const facets = useMemo(() => osImageFacetOptions(items, query), [items, query])
  const filtered = useMemo(() => items.filter((image) => matchesOSImageFocus(image, searchParams)).filter((image) => matchesOSImageInventory(image, query, activity.activities, sites)).sort((left, right) => compareOSImages(left, right, query)), [activity.activities, items, query, searchParams, sites])
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize))
  const safePage = Math.min(query.page, totalPages)
  const pageItems = useMemo(() => filtered.slice((safePage - 1) * pageSize, safePage * pageSize), [filtered, pageSize, safePage])
  const groups = useMemo(() => groupOSImages(pageItems, query.group), [pageItems, query.group])

  useEffect(() => {
    if (state.status !== 'ready' || query.page <= totalPages) return
    updateParams((next) => { if (totalPages > 1) next.set('page', String(totalPages)); else next.delete('page') })
  }, [query.page, state.status, totalPages, updateParams])

  const selectionKey = [siteId, query.q, query.view, query.os.join('\u0000'), query.architectures.join('\u0000'), query.tags.join('\u0000'), query.integrations.join('\u0000'), query.target, query.readiness, searchParams.get('integrationId') ?? '', searchParams.get('imageName') ?? ''].join('|')
  const [selectionState, setSelectionState] = useState<{ key: string; values: ReadonlySet<string> }>({ key: selectionKey, values: new Set() })
  const selected = selectionState.key === selectionKey ? selectionState.values : new Set<string>()
  const setSelected = useCallback((change: (current: ReadonlySet<string>) => ReadonlySet<string>) => setSelectionState((current) => ({ key: selectionKey, values: change(current.key === selectionKey ? current.values : new Set()) })), [selectionKey])
  const clearSelection = useCallback(() => setSelectionState({ key: selectionKey, values: new Set() }), [selectionKey])
  const toggleOne = (key: string) => setSelected((current) => { const next = new Set(current); if (next.has(key)) next.delete(key); else next.add(key); return next })
  const setMany = (keys: readonly string[], checked: boolean) => setSelected((current) => { const next = new Set(current); keys.forEach((key) => checked ? next.add(key) : next.delete(key)); return next })
  const pageKeys = pageItems.map(osImageKey)
  const allPageSelected = pageKeys.length > 0 && pageKeys.every((key) => selected.has(key))
  const somePageSelected = pageKeys.some((key) => selected.has(key))
  const selectedImages = filtered.filter((image) => selected.has(osImageKey(image)))
  const rangeStart = pageItems.length ? (safePage - 1) * pageSize + 1 : 0
  const rangeEnd = pageItems.length ? rangeStart + pageItems.length - 1 : 0
  const summary = state.status === 'ready' ? <span>{`Showing ${rangeStart}–${rangeEnd} of ${filtered.length} images`}<span aria-hidden> · </span>{isRefreshing ? 'Refreshing…' : `Refreshed ${formatDateTime(state.refreshedAt)}`}</span> : 'Loading catalog…'
  const siteName = (id: string) => sites.find((site) => site.id === id)?.name ?? id
  const openBulk = (action: OSImageBulkAction) => {
    const eligible = action === 'delete' ? selectedImages.filter((image) => image.providerOsSystem === 'custom') : selectedImages.filter(hasOSImageOverride)
    const skipped = selectedImages.filter((image) => !eligible.includes(image))
    setBulk({ action, targets: eligible.map(toBulkTarget), skipped: skipped.map(toBulkTarget) })
  }

  return <div className="operator-page sw-os-image-page">
    <PageHeader title="OS images" subtitle="Organize deployment images and see which Disk or RAM modes each image supports." breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/deploy') }, { label: 'OS images' }]} actions={<>
      <Button variant="outline" onClick={() => navigate(scopedHref('/infrastructure/integrations'))}>Manage integrations</Button>
      <Button variant="outline" loading={isRefreshing} onClick={reload}><RefreshCw size={16} />Refresh</Button>
      {uploadEnabled && <Tooltip content={provisioners.length === 0 ? 'Add a provisioner integration before uploading an image' : 'Upload a new OS image to a provisioner'}><span><Button colorPalette="brand" disabled={provisioners.length === 0} onClick={() => setUploading(true)}><Upload size={16} />Upload image</Button></span></Tooltip>}
    </>} />
    <ProvisioningTabs />
    {state.status === 'loading' && <LoadingState rows={7} />}
    {state.status === 'error' && <ErrorState message={state.message} onRetry={reload} />}
    {state.status === 'ready' && <>
      <CatalogOverview facts={facts} partial={failures.length > 0} />
      <InventorySurface headingId="os-image-inventory-title" eyebrow="OS provisioning catalog" title="Deployment images" summary={summary} className="sw-os-image-inventory" toolbar={<div className="sw-os-image-toolbar-layout">
        <CatalogSearch key={query.q} value={query.q} onCommit={commitSearch} />
        <Flex className="sw-os-image-toolbar-controls" align="center" gap="2" wrap="wrap">
          <Flex className="sw-platform-filter-group" role="group" aria-label="Filter OS Image catalog view">{[{ value: 'all', label: 'All' }, { value: 'verification_failed', label: 'Failed tests' }].map((view) => <Button key={view.value} className="sw-platform-filter-chip" variant="plain" size="sm" aria-pressed={query.view === view.value} data-active={query.view === view.value || undefined} onClick={() => updateParams((next) => { if (view.value === 'all') next.delete('view'); else next.set('view', view.value); next.delete('page') })}>{view.label}</Button>)}</Flex>
          <PopoverButton title="OS Image filters" trigger={<Button variant="outline" size="sm"><Filter size={16} />Filters{activeFilterCount(query) > 0 && <Badge variant="subtle">{activeFilterCount(query)}</Badge>}</Button>}><CatalogFilterPanel query={query} facets={facets} onSingle={(key, value) => updateParams((next) => { if (value) next.set(key, value); else next.delete(key); if (key === 'target' && !value) next.delete('readiness'); next.delete('page') })} onMulti={(key, values) => updateParams((next) => { next.delete(key); values.forEach((value) => next.append(key, value)); next.delete('page') })} onClear={clearFilters} /></PopoverButton>
          <PopoverButton title="Display options" trigger={<Button variant="outline" size="sm"><SlidersHorizontal size={16} />Display</Button>}><DisplayPanel query={query} density={density} pageSize={pageSize} onGroup={(value) => updateParams((next) => { if (value === 'none') next.delete('group'); else next.set('group', value); next.delete('page') })} onSort={(value) => updateParams((next) => { if (value === 'os') next.delete('sort'); else next.set('sort', value); next.delete('page') })} onDirection={(value) => updateParams((next) => { if (value === 'asc') next.delete('dir'); else next.set('dir', value); next.delete('page') })} onDensity={(value) => { setDensity(value); writePreference(DENSITY_KEY, value) }} onPageSize={(value) => { setPageSize(value); writePreference(PAGE_SIZE_KEY, value); updateParams((next) => next.delete('page')) }} /></PopoverButton>
        </Flex>
      </div>}>
        <SelectionToolbar count={selectedImages.length} onClear={clearSelection}><Button size="sm" colorPalette="red" disabled={!selectedImages.some((image) => image.providerOsSystem === 'custom')} onClick={() => openBulk('delete')}><Trash2 size={15} />Delete</Button><Button size="sm" variant="outline" disabled={!selectedImages.some(hasOSImageOverride)} onClick={() => openBulk('reset-overrides')}><RotateCcw size={15} />Restore original details</Button>{selected.size < filtered.length && <Button size="sm" variant="plain" onClick={() => setMany(filtered.map(osImageKey), true)}>Select all {filtered.length} matches</Button>}</SelectionToolbar>
        {state.refreshError && <Alert status="warning" title="Catalog refresh failed">Showing the last successful catalog. {state.refreshError}</Alert>}
        {activity.refreshError && <Alert status="warning" title="Deployment test activity may be stale">{activity.refreshError}</Alert>}
        {failures.map((failure) => <CatalogFailureAlert key={failure.integrationId} failure={failure} />)}
        {filtered.length === 0 ? <div className="sw-os-image-empty"><EmptyState title={hasOSImageFilters(query, searchParams) ? 'No matching OS images' : failures.length > 0 ? 'No provider catalog is available' : provisioners.length === 0 ? 'No provisioner connected' : 'No OS images'} message={hasOSImageFilters(query, searchParams) ? 'Adjust the search or catalog facets to see other images.' : failures.length > 0 ? 'Refresh the catalog or review the unavailable provider integrations.' : provisioners.length === 0 ? 'OS Images come from a provisioner integration.' : 'The connected provisioners returned no deployable OS Images.'} action={hasOSImageFilters(query, searchParams) ? { label: 'Clear filters', onClick: clearFilters } : provisioners.length === 0 ? { label: 'Manage integrations', onClick: () => navigate(scopedHref('/infrastructure/integrations')) } : { label: 'Refresh', onClick: reload }} /></div> : <>
          <ResponsiveDataView desktop={<StickyTableFrame><Table.Root size={density === 'compact' ? 'sm' : 'md'} aria-label="OS images" className="sw-os-image-table"><Table.Header><Table.Row><Table.ColumnHeader className="sw-cell-center sw-os-image-col--select" aria-label="Row selection"><Checkbox aria-label="Select all on this page" checked={allPageSelected ? true : somePageSelected ? 'indeterminate' : false} onCheckedChange={() => setMany(pageKeys, !allPageSelected)} /></Table.ColumnHeader><Table.ColumnHeader className="sw-cell-center sw-os-image-col--expand" aria-label="Row details" /><Table.ColumnHeader className="sw-os-image-col--identity">Image</Table.ColumnHeader><Table.ColumnHeader className="sw-os-image-col--deploy-modes">Deploy modes</Table.ColumnHeader><Table.ColumnHeader className="sw-os-image-col--tags">Tags</Table.ColumnHeader><Table.ColumnHeader className="sw-os-image-col--default-user">Default user</Table.ColumnHeader><Table.ColumnHeader className="sw-os-image-col--source">Source</Table.ColumnHeader><Table.ColumnHeader className="sw-os-image-col--action" aria-label="Image actions" /></Table.Row></Table.Header><Table.Body>{groups.map((group) => <CatalogGroupRows key={group.key || 'all'} group={group} grouped={query.group !== 'none'} selected={selected} sites={sites} activities={activity.activities} templatesEnabled={templatesEnabled} scopedHref={scopedHref} onToggle={(image) => toggleOne(osImageKey(image))} onEdit={setEditing} onTest={(image, target) => setTesting({ image, target })} onDelete={setDeleting} />)}</Table.Body></Table.Root></StickyTableFrame>} mobile={<div className="sw-os-image-card-list" aria-label="OS images">{groups.map((group) => <section key={group.key || 'all'}>{query.group !== 'none' && <HStack className="sw-os-image-mobile-group"><Text fontWeight="semibold">{group.label}</Text><Badge variant="subtle">{group.items.length}</Badge></HStack>}<div className="sw-resource-card-list">{group.items.map((image) => <ImageCard key={osImageKey(image)} image={image} checked={selected.has(osImageKey(image))} siteName={siteName(image.siteId)} activities={activity.activities} templatesEnabled={templatesEnabled} scopedHref={scopedHref} onToggle={() => toggleOne(osImageKey(image))} onEdit={() => setEditing(image)} onTest={(target) => setTesting({ image, target })} onDelete={() => setDeleting(image)} />)}</div></section>)}</div>} />
          <div className="sw-os-image-pagination"><Pagination total={totalPages} value={safePage} onChange={(page) => updateParams((next) => { if (page > 1) next.set('page', String(page)); else next.delete('page') })} /></div>
        </>}
      </InventorySurface>
    </>}
    {editing && <EditImageDialog image={editing} repository={provisioning} onClose={() => setEditing(null)} onSaved={(title) => { setEditing(null); showToast({ tone: 'success', title }); reload() }} />}
    {deleting && <DeleteImageDialog image={deleting} repository={provisioning} onClose={() => setDeleting(null)} onDeleted={() => { setDeleting(null); clearSelection(); showToast({ tone: 'success', title: 'OS image deleted' }); reload() }} />}
    {testing && <TestImageDeploymentDialog image={testing.image} initialTarget={testing.target} provisioning={provisioning} servers={servers} onClose={() => setTesting(null)} onLaunched={(title) => { setTesting(null); showToast({ tone: 'success', title }); activity.reload() }} />}
    {uploadEnabled && uploading && <UploadImageDialog integrations={provisioners} repository={provisioning} onClose={() => setUploading(false)} onUploaded={(title) => { setUploading(false); showToast({ tone: 'success', title }); reload() }} />}
    {bulk && <BulkImageActionDialog action={bulk.action} targets={bulk.targets} skipped={bulk.skipped} onClose={() => setBulk(null)} onDone={() => { setBulk(null); clearSelection(); reload() }} />}
  </div>
}

function CatalogFailureAlert({ failure }: { failure: OSImageCatalogFailure }) {
  return <Alert status="warning" title={`${failure.integrationName} image catalog unavailable`}>{failure.message}</Alert>
}

function CatalogGroupRows({ group, grouped, selected, sites, activities, templatesEnabled, scopedHref, onToggle, onEdit, onTest, onDelete }: {
  group: ReturnType<typeof groupOSImages>[number]
  grouped: boolean
  selected: ReadonlySet<string>
  sites: readonly Site[]
  activities: OSImageVerificationActivityMap
  templatesEnabled: boolean
  scopedHref: (path: string) => string
  onToggle: (image: OSImageCatalogRow) => void
  onEdit: (image: OSImageCatalogRow) => void
  onTest: (image: OSImageCatalogRow, target: OSImageTarget) => void
  onDelete: (image: OSImageCatalogRow) => void
}) {
  return <>{grouped && <Table.Row className="sw-os-image-group-row"><Table.Cell colSpan={8}><HStack><strong>{group.label}</strong><Badge variant="subtle">{group.items.length}</Badge></HStack></Table.Cell></Table.Row>}{group.items.map((image) => <ImageRow key={osImageKey(image)} image={image} checked={selected.has(osImageKey(image))} siteName={sites.find((site) => site.id === image.siteId)?.name ?? image.siteId} activities={activities} templatesEnabled={templatesEnabled} scopedHref={scopedHref} onToggle={() => onToggle(image)} onEdit={() => onEdit(image)} onTest={(target) => onTest(image, target)} onDelete={() => onDelete(image)} />)}</>
}
