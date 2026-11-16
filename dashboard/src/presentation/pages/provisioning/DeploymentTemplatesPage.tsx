import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, Card, Field, Heading, Input, Table, Text, Textarea } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { DeploymentNetworkMode, DeploymentTemplate } from '@/domain/provisioning/types'
import type { Pool } from '@/domain/infrastructure/types'
import type { OSImage } from '@/domain/site/types'
import { canOperatePool, isAdmin, ownsAnyPool, poolRoleOf } from '@/domain/user/access'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Select } from '@/presentation/components/ui/select'
import { DEPLOYMENT_NETWORK_MODE_LABELS, deployTargetForEphemeral, deployTargetIsEphemeral } from '@/domain/provisioning/types'
import { AddressingModeField } from './AddressingModeField'
import { DeployTargetField } from './DeployTargetField'
import { OSImagePicker } from './OSImagePicker'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { formatDateTime } from '@/shared/utils/time'
import { defaultDeployTargetForImage } from './osDeploymentFlow'
import {
  EMPTY_MINIMUM_HARDWARE_DRAFT,
  formatMinimumHardware,
  formatTemplateUses,
  minimumHardwareDraft,
  minimumHardwareIsEmpty,
  parseMinimumHardware,
  type MinimumHardwareDraft,
} from './minimumHardwareForm'

/** A provisioner Integration a template can name: its id and display name. */
interface ProvisionerChoice {
  id: string
  name: string
}

type TemplatesState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; templates: DeploymentTemplate[]; integrations: ProvisionerChoice[]; pools: Pool[] }

interface TemplateDraft {
  id?: string
  integrationId: string
  /** The owning Pool, required on create; on an edit, a change moves the template, which only an admin may do, and a template without one may stay so. */
  poolId: string
  /** The Pool the edited template had when the editor opened, so Save sends `poolId` only when it changed. */
  originalPoolId: string
  name: string
  description: string
  imageId: string
  ephemeral: boolean
  networkMode: DeploymentNetworkMode
  subnetId: string
  defaultGateway: boolean
  minimum: MinimumHardwareDraft
}

const EMPTY_DRAFT: TemplateDraft = {
  integrationId: '',
  poolId: '',
  originalPoolId: '',
  name: '',
  description: '',
  imageId: '',
  ephemeral: true,
  networkMode: 'automatic',
  subnetId: '',
  defaultGateway: false,
  minimum: EMPTY_MINIMUM_HARDWARE_DRAFT,
}

/** The editor's minimum hardware fields: label, draft key, input mode, and helper text. */
const MINIMUM_HARDWARE_FIELDS: ReadonlyArray<{ key: keyof MinimumHardwareDraft; label: string; numeric: boolean; helper: string }> = [
  { key: 'cpuCores', label: 'Minimum CPU cores', numeric: true, helper: 'Physical cores from the hardware inventory.' },
  { key: 'memoryGiB', label: 'Minimum memory (GiB)', numeric: true, helper: 'Installed memory.' },
  { key: 'storageGB', label: 'Minimum storage (GB)', numeric: true, helper: 'Total disk capacity.' },
  { key: 'gpuModel', label: 'GPU model', numeric: false, helper: 'Matched within the compute GPU model name, for example MI300X.' },
  { key: 'gpuCount', label: 'Minimum compute GPUs', numeric: true, helper: 'Compute GPUs, not display controllers.' },
]


/**
 * A template's network intent in words: the mode label, plus its subnet for Static, the only mode
 * whose template must name one. A template without network intent reads as Automatic, as the
 * contract defines. `separator` keeps the table's and the card's existing typography.
 */
function templateNetworkLabel(template: DeploymentTemplate, separator: string): string {
  const mode = template.network?.mode ?? 'automatic'
  if (mode === 'static') return `${DEPLOYMENT_NETWORK_MODE_LABELS.static}${separator}${template.network?.subnetId || '-'}`
  return DEPLOYMENT_NETWORK_MODE_LABELS[mode]
}

/**
 * CRUD workspace for swallow-owned deployment intent and write-only cloud-init: the Deployment
 * Templates page (`/provisioning/templates`, its own sidebar entry beside OS Images). Every user
 * reaches it: the API lists only the templates of the caller's Pools (all of them for an admin), so
 * a member sees what it may deploy with. Create is offered to whoever owns a Pool, and edit,
 * cloud-init, and delete only on the templates of a Pool the caller owns; these checks only avoid
 * offering what the API would refuse, which still judges every request. Only an admin moves a
 * template to another Pool. The provisioner Integrations come from the image catalogs, which every
 * user may read, because listing Integrations is an admin route. OS Images links here to create a
 * template for one of its images (`?create=1&integrationId=…&imageId=…`).
 */
export function DeploymentTemplatesPage() {
  const { provisioning, infrastructure } = useApp()
  const { currentUser } = useAuth()
  const { siteId, sites } = useSiteScope()
  const { showToast } = useToast()
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<TemplatesState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [draft, setDraft] = useState<TemplateDraft>(EMPTY_DRAFT)
  const [formOpen, setFormOpen] = useState(false)
  const [images, setImages] = useState<OSImage[]>([])
  const [imageError, setImageError] = useState<DisplayedFailure | null>(null)
  const [saving, setSaving] = useState(false)
  const [secretTemplate, setSecretTemplate] = useState<DeploymentTemplate | null>(null)
  const [secretValue, setSecretValue] = useState('')
  const [secretSaving, setSecretSaving] = useState(false)
  const pendingImageDefaultId = useRef('')

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      const [templates, catalogs, pools] = await Promise.all([
        provisioning.listTemplates({ siteId }),
        provisioning.listOSImageCatalogs(siteId),
        infrastructure.listPools(),
      ])
      const integrations = catalogs.map((catalog) => ({ id: catalog.integrationId, name: catalog.integrationName }))
      setState({ status: 'ready', templates, integrations, pools })
    } catch (error) {
      setState({ status: 'error', ...failureOf(error, 'Could not load deployment templates.') })
    }
  }, [infrastructure, provisioning, siteId])

  const admin = isAdmin(currentUser)
  // The Pools a new template may be created in: every Pool for an admin, otherwise the caller's owned ones.
  const ownedPools = useMemo(
    () => (state.status === 'ready' ? state.pools.filter((pool) => admin || poolRoleOf(currentUser, pool.id) === 'owner') : []),
    [admin, currentUser, state],
  )

  useEffect(() => {
    void load()
  }, [load])

  const openCreate = useCallback((integrationId = '', imageId = '') => {
    pendingImageDefaultId.current = imageId
    setDraft({ ...EMPTY_DRAFT, integrationId, imageId })
    setFormOpen(true)
  }, [])

  // A user who owns exactly one Pool creates there without choosing; the choice is offered otherwise.
  useEffect(() => {
    if (!formOpen || draft.id || draft.poolId || ownedPools.length !== 1) return
    setDraft((current) => ({ ...current, poolId: ownedPools[0].id }))
  }, [draft.id, draft.poolId, formOpen, ownedPools])

  useEffect(() => {
    if (searchParams.get('create') !== '1') return
    openCreate(searchParams.get('integrationId') ?? '', searchParams.get('imageId') ?? '')
  }, [openCreate, searchParams])

  useEffect(() => {
    if (!formOpen || !draft.integrationId) {
      setImages([])
      setImageError(null)
      return
    }
    let cancelled = false
    setImageError(null)
    provisioning
      .listOSImages(draft.integrationId)
      .then((items) => {
        if (cancelled) return
        setImages(items)
        const pendingImage = items.find((image) => image.id === pendingImageDefaultId.current)
        if (pendingImage) {
          setDraft((current) => ({
            ...current,
            ephemeral: defaultDeployTargetForImage(pendingImage) === 'ram',
          }))
          pendingImageDefaultId.current = ''
        }
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setImages([])
          setImageError(failureOf(error, 'OS images could not be loaded.'))
        }
      })
    return () => {
      cancelled = true
    }
  }, [draft.integrationId, formOpen, provisioning])

  const filtered = useMemo(() => {
    if (state.status !== 'ready') return []
    const needle = query.trim().toLowerCase()
    if (!needle) return state.templates
    return state.templates.filter((template) =>
      [template.name, template.description, template.imageId, state.integrations.find((item) => item.id === template.integrationId)?.name ?? ''].some((value) =>
        value.toLowerCase().includes(needle),
      ),
    )
  }, [query, state])

  const closeForm = () => {
    setFormOpen(false)
    setDraft(EMPTY_DRAFT)
    const next = new URLSearchParams(searchParams)
    next.delete('create')
    next.delete('integrationId')
    next.delete('imageId')
    setSearchParams(next, { replace: true })
  }

  // The floor is parsed on every render so the form names a bad value before Save, not after.
  const parsedMinimum = parseMinimumHardware(draft.minimum)
  const minimumError = 'error' in parsedMinimum ? parsedMinimum.error : ''

  const submit = async () => {
    if (saving || !draft.name.trim() || !draft.integrationId || (!draft.id && !draft.poolId) || !draft.imageId || (Boolean(imageError) && !draft.id) || !('minimum' in parsedMinimum)) return
    const minimum = parsedMinimum.minimum
    setSaving(true)
    try {
      if (draft.id) {
        // An update always sends the floor: all-null fields clear a stored one.
        await provisioning.updateTemplate(draft.id, {
          name: draft.name.trim(),
          description: draft.description.trim(),
          imageId: draft.imageId,
          deployTarget: deployTargetForEphemeral(draft.ephemeral),
          network: { mode: draft.networkMode, subnetId: draft.subnetId.trim() || undefined, defaultGateway: draft.defaultGateway },
          minimumHardware: minimum,
          ...(admin && draft.poolId !== draft.originalPoolId ? { poolId: draft.poolId } : {}),
        })
        showToast({ tone: 'success', title: 'Deployment template updated' })
      } else {
        await provisioning.createTemplate({
          integrationId: draft.integrationId,
          poolId: draft.poolId,
          name: draft.name.trim(),
          description: draft.description.trim(),
          imageId: draft.imageId,
          deployTarget: deployTargetForEphemeral(draft.ephemeral),
          network: { mode: draft.networkMode, subnetId: draft.subnetId.trim() || undefined, defaultGateway: draft.defaultGateway },
          ...(minimumHardwareIsEmpty(minimum) ? {} : { minimumHardware: minimum }),
        })
        showToast({ tone: 'success', title: 'Deployment template created' })
      }
      closeForm()
      await load()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not save deployment template', description: failureText(failureOf(error, 'Unknown error')) })
    } finally {
      setSaving(false)
    }
  }

  // A template a Platform node group installs from cannot be deleted; the API refuses it with a
  // `conflict` naming the Platforms, which the error toast shows. The page does not pre-empt it, so
  // the API stays the one place that decides.
  const removeTemplate = async (template: DeploymentTemplate) => {
    if (!window.confirm(`Delete deployment template "${template.name}"?`)) return
    try {
      await provisioning.deleteTemplate(template.id)
      showToast({ tone: 'success', title: 'Deployment template deleted' })
      await load()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not delete deployment template', description: failureText(failureOf(error, 'Unknown error')) })
    }
  }

  const saveSecret = async () => {
    if (!secretTemplate || !secretValue || secretSaving) return
    setSecretSaving(true)
    try {
      await provisioning.replaceTemplateUserData(secretTemplate.id, secretValue)
      setSecretTemplate(null)
      setSecretValue('')
      showToast({ tone: 'success', title: 'Cloud-init replaced' })
      await load()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not replace cloud-init', description: failureText(failureOf(error, 'Unknown error')) })
    } finally {
      setSecretSaving(false)
    }
  }

  const clearSecret = async (template: DeploymentTemplate) => {
    try {
      await provisioning.clearTemplateUserData(template.id)
      showToast({ tone: 'success', title: 'Cloud-init removed' })
      await load()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not remove cloud-init', description: failureText(failureOf(error, 'Unknown error')) })
    }
  }

  const openEdit = (template: DeploymentTemplate) => {
    pendingImageDefaultId.current = ''
    setDraft({
      id: template.id,
      integrationId: template.integrationId,
      poolId: template.poolId ?? '',
      originalPoolId: template.poolId ?? '',
      name: template.name,
      description: template.description,
      imageId: template.imageId,
      ephemeral: template.ephemeral,
      networkMode: template.network?.mode ?? 'automatic',
      subnetId: template.network?.subnetId ?? '',
      defaultGateway: template.network?.defaultGateway ?? false,
      minimum: minimumHardwareDraft(template.minimumHardware),
    })
    setFormOpen(true)
  }
  const integrationName = (id: string) => (state.status === 'ready' ? state.integrations.find((item) => item.id === id)?.name ?? id : id)
  const siteName = (id: string) => sites.find((item) => item.id === id)?.name ?? id
  // A template without a Pool predates Pool ownership; only admins see it, and it reads as such.
  const poolName = (id: string | null) => (id ? (state.status === 'ready' ? state.pools.find((pool) => pool.id === id)?.name ?? id : id) : 'No Pool')
  const canChange = (template: DeploymentTemplate) => canOperatePool(currentUser, template.poolId)
  // The Pool select offers where the draft may go: on create, the caller's owned Pools; on an edit,
  // every Pool for an admin and only the current one otherwise (the select is then read-only).
  const poolOptions = (draft.id && !admin
    ? state.status === 'ready' ? state.pools.filter((pool) => pool.id === draft.poolId) : []
    : draft.id ? (state.status === 'ready' ? state.pools : []) : ownedPools
  ).map((pool) => ({ value: pool.id, label: pool.name }))

  return (
    <div className="console-page">
      <PageHeader
        title="Deployment Templates"
        subtitle="Reusable deployment settings for an OS image: Disk or RAM, network, and optional cloud-init."
        stackActionsOnMobile
        actions={
          ownsAnyPool(currentUser) ? (
            <Button colorPalette="brand" onClick={() => openCreate()}>
              <Plus size={16} />
              Create template
            </Button>
          ) : undefined
        }
      />

      {formOpen && (
        <Card.Root>
          <Card.Body gap="4" className="sw-template-editor">
            <Heading size="sm">{draft.id ? 'Edit deployment template' : 'Create deployment template'}</Heading>
            <div className="sw-form-grid">
              <Field.Root required>
                <Field.Label>
                  Provisioner integration <Field.RequiredIndicator />
                </Field.Label>
                <Select
                  value={draft.integrationId}
                  disabled={Boolean(draft.id)}
                  aria-label="Provisioner integration"
                  placeholder="Select an integration"
                  onChange={(value) => {
                    pendingImageDefaultId.current = ''
                    setDraft((current) => ({
                      ...current,
                      integrationId: value,
                      imageId: '',
                      ephemeral: true,
                    }))
                  }}
                  options={state.status === 'ready' ? state.integrations.map((integration) => ({ value: integration.id, label: integration.name })) : []}
                />
              </Field.Root>
              <Field.Root required>
                <Field.Label>
                  Pool <Field.RequiredIndicator />
                </Field.Label>
                <Select
                  value={draft.poolId}
                  disabled={Boolean(draft.id) && !admin}
                  aria-label="Pool"
                  placeholder="Select a Pool"
                  onChange={(value) => setDraft((current) => ({ ...current, poolId: value }))}
                  options={poolOptions}
                />
                <Field.HelperText>
                  {draft.id && !admin
                    ? 'Only an admin moves a template to another Pool.'
                    : 'Members of this Pool can deploy with the template; its owners can change it.'}
                </Field.HelperText>
              </Field.Root>
              <Field.Root required>
                <Field.Label>
                  Name <Field.RequiredIndicator />
                </Field.Label>
                <Input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} />
              </Field.Root>
              <OSImagePicker
                images={images}
                value={draft.imageId}
                siteId={siteId}
                integrationId={draft.integrationId}
                required
                error={imageError}
                disabled={!draft.integrationId || Boolean(imageError)}
                onChange={(value) => {
                  pendingImageDefaultId.current = ''
                  const image = images.find((candidate) => candidate.id === value)
                  setDraft((current) => ({
                    ...current,
                    imageId: value,
                    ephemeral: defaultDeployTargetForImage(image) === 'ram',
                  }))
                }}
              />
              <Field.Root>
                <Field.Label>Description</Field.Label>
                <Input value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} />
              </Field.Root>
              <DeployTargetField
                value={deployTargetForEphemeral(draft.ephemeral)}
                onChange={(nextTarget) => setDraft((current) => ({ ...current, ephemeral: deployTargetIsEphemeral(nextTarget) }))}
              />
              <AddressingModeField
                label="Network mode"
                value={draft.networkMode}
                onChange={(mode) => setDraft((current) => ({
                  ...current,
                  networkMode: mode,
                  defaultGateway: mode === 'static' ? current.defaultGateway : false,
                }))}
              />
              {draft.networkMode === 'static' && (
                <Field.Root required>
                  <Field.Label>
                    Subnet ID <Field.RequiredIndicator />
                  </Field.Label>
                  <Input value={draft.subnetId} onChange={(event) => setDraft((current) => ({ ...current, subnetId: event.target.value }))} />
                </Field.Root>
              )}
              {draft.networkMode === 'static' && (
                <Field.Root>
                  <Checkbox id="template-default-gateway" checked={draft.defaultGateway} onCheckedChange={(checked) => setDraft((current) => ({ ...current, defaultGateway: checked }))}>
                    Use as default gateway
                  </Checkbox>
                </Field.Root>
              )}
            </div>
            <fieldset className="sw-template-minimum">
              <legend>
                <Heading as="span" size="xs">Minimum hardware</Heading>
              </legend>
              <Text color="fg.muted" fontSize="sm">
                Optional. A Server joining a Platform node group that installs from this template must meet every value you set; a Server whose inventory
                does not report a value does not meet it. Leave a field empty to set no minimum. Deploying single Servers ignores it.
              </Text>
              <div className="sw-form-grid">
                {MINIMUM_HARDWARE_FIELDS.map((field) => (
                  <Field.Root key={field.key}>
                    <Field.Label>{field.label}</Field.Label>
                    <Input
                      value={draft.minimum[field.key]}
                      inputMode={field.numeric ? 'decimal' : 'text'}
                      onChange={(event) => setDraft((current) => ({ ...current, minimum: { ...current.minimum, [field.key]: event.target.value } }))}
                    />
                    <Field.HelperText>{field.helper}</Field.HelperText>
                  </Field.Root>
                ))}
              </div>
              {minimumError && <Text color="fg.error" fontSize="sm" role="alert">{minimumError}</Text>}
            </fieldset>
            <div className="sw-form-actions">
              <Button variant="ghost" onClick={closeForm}>
                Cancel
              </Button>
              <Button
                colorPalette="brand"
                loading={saving}
                disabled={saving || !draft.name.trim() || !draft.integrationId || (!draft.id && !draft.poolId) || !draft.imageId || (draft.networkMode === 'static' && !draft.subnetId.trim()) || (Boolean(imageError) && !draft.id) || Boolean(minimumError)}
                onClick={() => void submit()}
              >
                Save
              </Button>
            </div>
          </Card.Body>
        </Card.Root>
      )}

      {secretTemplate && (
        <Card.Root>
          <Card.Body gap="4" className="sw-template-editor">
            <Heading size="sm">Replace cloud-init for {secretTemplate.name}</Heading>
            <Alert status="info" title="Existing cloud-init is write-only and cannot be displayed." />
            <Field.Root required>
              <Field.Label htmlFor="template-cloud-init">
                New cloud-init <Field.RequiredIndicator />
              </Field.Label>
              <Textarea id="template-cloud-init" value={secretValue} onChange={(event) => setSecretValue(event.target.value)} rows={10} autoComplete="off" />
            </Field.Root>
            <div className="sw-form-actions">
              <Button
                variant="ghost"
                onClick={() => {
                  setSecretTemplate(null)
                  setSecretValue('')
                }}
              >
                Cancel
              </Button>
              <Button colorPalette="brand" loading={secretSaving} disabled={!secretValue || secretSaving} onClick={() => void saveSecret()}>
                Replace cloud-init
              </Button>
            </div>
          </Card.Body>
        </Card.Root>
      )}

      <DataToolbar variant="plain">
        <SearchInput value={query} onChange={setQuery} placeholder="Search templates" aria-label="Search deployment templates" />
      </DataToolbar>

      {state.status === 'loading' && <LoadingState rows={6} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={() => void load()} />}
      {state.status === 'ready' && filtered.length === 0 && (
        <EmptyState title="No deployment templates" message="Create one to reuse deployment settings." />
      )}
      {state.status === 'ready' && filtered.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
          <Table.Root size="sm" aria-label="Deployment templates" className="sw-provisioning-table">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Name</Table.ColumnHeader>
                <Table.ColumnHeader>Site</Table.ColumnHeader>
                <Table.ColumnHeader>Integration</Table.ColumnHeader>
                <Table.ColumnHeader>Pool</Table.ColumnHeader>
                <Table.ColumnHeader>Image ID</Table.ColumnHeader>
                <Table.ColumnHeader>Ephemeral</Table.ColumnHeader>
                <Table.ColumnHeader>Network</Table.ColumnHeader>
                <Table.ColumnHeader>Cloud-init</Table.ColumnHeader>
                <Table.ColumnHeader>Minimum hardware</Table.ColumnHeader>
                <Table.ColumnHeader>Used by</Table.ColumnHeader>
                <Table.ColumnHeader>Updated</Table.ColumnHeader>
                <Table.ColumnHeader />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filtered.map((template) => (
                <Table.Row key={template.id}>
                  <Table.Cell>
                    <strong>{template.name}</strong>
                    <Text as="small" display="block" color="fg.muted">
                      {template.description || '-'}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>{siteName(template.siteId)}</Table.Cell>
                  <Table.Cell>{integrationName(template.integrationId)}</Table.Cell>
                  <Table.Cell>
                    {poolName(template.poolId)}
                    <Text as="small" display="block" color="fg.muted">
                      {template.createdBy ? `Created by ${template.createdBy}` : '-'}
                    </Text>
                  </Table.Cell>
                  <Table.Cell className="sw-mono">{template.imageId}</Table.Cell>
                  <Table.Cell>{template.ephemeral ? 'Yes' : 'No'}</Table.Cell>
                  <Table.Cell>{templateNetworkLabel(template, ' - ')}</Table.Cell>
                  <Table.Cell>{template.hasUserData ? 'Configured' : '-'}</Table.Cell>
                  <Table.Cell>{formatMinimumHardware(template.minimumHardware)}</Table.Cell>
                  <Table.Cell>{formatTemplateUses(template.usedBy)}</Table.Cell>
                  <Table.Cell>{formatDateTime(template.updatedAt)}</Table.Cell>
                  <Table.Cell textAlign="end">
                    {canChange(template) && (
                      <span className="sw-row-actions">
                        <Button
                          variant="plain"
                          size="sm"
                          px="1"
                          h="auto"
                          colorPalette="brand"
                          onClick={() => openEdit(template)}
                        >
                          Edit
                        </Button>
                        <Button
                          variant="plain"
                          size="sm"
                          px="1"
                          h="auto"
                          colorPalette="brand"
                          onClick={() => {
                            setSecretTemplate(template)
                            setSecretValue('')
                          }}
                        >
                          {template.hasUserData ? 'Replace cloud-init' : 'Add cloud-init'}
                        </Button>
                        {template.hasUserData && (
                          <Button variant="plain" size="sm" px="1" h="auto" colorPalette="brand" onClick={() => void clearSecret(template)}>
                            Remove cloud-init
                          </Button>
                        )}
                        <Button variant="plain" size="sm" px="1" h="auto" colorPalette="red" onClick={() => void removeTemplate(template)}>
                          Delete
                        </Button>
                      </span>
                    )}
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list">
              {filtered.map((template) => (
                <ResourceCard
                  key={template.id}
                  title={template.name}
                  description={template.description || template.imageId}
                  actions={
                    canChange(template) ? (
                      <>
                        <Button size="sm" variant="outline" onClick={() => openEdit(template)}>Edit template</Button>
                        <Button size="sm" variant="plain" onClick={() => { setSecretTemplate(template); setSecretValue('') }}>
                          {template.hasUserData ? 'Replace cloud-init' : 'Add cloud-init'}
                        </Button>
                        {template.hasUserData && <Button size="sm" variant="plain" onClick={() => void clearSecret(template)}>Remove cloud-init</Button>}
                        <Button size="sm" variant="plain" colorPalette="red" onClick={() => void removeTemplate(template)}>Delete template</Button>
                      </>
                    ) : undefined
                  }
                  details={<ResourceCardField label="Updated">{formatDateTime(template.updatedAt)}</ResourceCardField>}
                >
                  <ResourceCardField label="Site">{siteName(template.siteId)}</ResourceCardField>
                  <ResourceCardField label="Integration">{integrationName(template.integrationId)}</ResourceCardField>
                  <ResourceCardField label="Pool">{poolName(template.poolId)}</ResourceCardField>
                  <ResourceCardField label="Created by">{template.createdBy || '-'}</ResourceCardField>
                  <ResourceCardField label="Image ID"><span className="sw-mono">{template.imageId}</span></ResourceCardField>
                  <ResourceCardField label="Ephemeral">{template.ephemeral ? 'Yes' : 'No'}</ResourceCardField>
                  <ResourceCardField label="Network">{templateNetworkLabel(template, ' · ')}</ResourceCardField>
                  <ResourceCardField label="Cloud-init">{template.hasUserData ? 'Configured' : 'Not configured'}</ResourceCardField>
                  <ResourceCardField label="Minimum hardware">{formatMinimumHardware(template.minimumHardware)}</ResourceCardField>
                  <ResourceCardField label="Used by">{formatTemplateUses(template.usedBy)}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}
    </div>
  )
}
