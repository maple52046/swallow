import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Card, Field, Heading, Input, SegmentGroup, Table, Text, Textarea } from '@chakra-ui/react'
import { Plus } from 'lucide-react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { DeploymentTemplate } from '@/domain/provisioning/types'
import type { Integration, OSImage } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Select } from '@/presentation/components/ui/select'
import { deployTargetForEphemeral, deployTargetIsEphemeral } from '@/domain/provisioning/types'
import { DeployTargetField } from './DeployTargetField'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { ProvisioningTabs } from './ProvisioningTabs'

type TemplatesState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; templates: DeploymentTemplate[]; integrations: Integration[] }

interface TemplateDraft {
  id?: string
  integrationId: string
  name: string
  description: string
  imageId: string
  ephemeral: boolean
  networkMode: 'automatic' | 'static'
  subnetId: string
  defaultGateway: boolean
}

const EMPTY_DRAFT: TemplateDraft = {
  integrationId: '',
  name: '',
  description: '',
  imageId: '',
  ephemeral: false,
  networkMode: 'automatic',
  subnetId: '',
  defaultGateway: false,
}

const NETWORK_MODES = [
  { value: 'automatic', label: 'Automatic' },
  { value: 'static', label: 'Static' },
]

/** CRUD workspace for Swallow-owned deployment intent and write-only cloud-init. */
export function DeploymentTemplatesPage() {
  const { provisioning, sites: siteRepository } = useApp()
  const { siteId, sites, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<TemplatesState>({ status: 'loading' })
  const [query, setQuery] = useState('')
  const [draft, setDraft] = useState<TemplateDraft>(EMPTY_DRAFT)
  const [formOpen, setFormOpen] = useState(false)
  const [images, setImages] = useState<OSImage[]>([])
  const [imageError, setImageError] = useState('')
  const [saving, setSaving] = useState(false)
  const [secretTemplate, setSecretTemplate] = useState<DeploymentTemplate | null>(null)
  const [secretValue, setSecretValue] = useState('')
  const [secretSaving, setSecretSaving] = useState(false)

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      const [templates, integrations] = await Promise.all([
        provisioning.listTemplates({ siteId }),
        siteRepository.listIntegrations({ siteId, kind: 'provisioner' }),
      ])
      setState({ status: 'ready', templates, integrations })
    } catch (error) {
      setState({ status: 'error', message: error instanceof Error ? error.message : 'Could not load deployment templates.' })
    }
  }, [provisioning, siteId, siteRepository])

  useEffect(() => {
    void load()
  }, [load])

  const openCreate = useCallback((integrationId = '', imageId = '') => {
    setDraft({ ...EMPTY_DRAFT, integrationId, imageId })
    setFormOpen(true)
  }, [])

  useEffect(() => {
    if (searchParams.get('create') !== '1') return
    openCreate(searchParams.get('integrationId') ?? '', searchParams.get('imageId') ?? '')
  }, [openCreate, searchParams])

  useEffect(() => {
    if (!formOpen || !draft.integrationId) {
      setImages([])
      setImageError('')
      return
    }
    let cancelled = false
    setImageError('')
    provisioning
      .listOSImages(draft.integrationId)
      .then((items) => {
        if (!cancelled) setImages(items)
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setImages([])
          setImageError(error.message)
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

  const submit = async () => {
    if (saving || !draft.name.trim() || !draft.integrationId || !draft.imageId || (Boolean(imageError) && !draft.id)) return
    setSaving(true)
    try {
      if (draft.id) {
        await provisioning.updateTemplate(draft.id, {
          name: draft.name.trim(),
          description: draft.description.trim(),
          imageId: draft.imageId,
          deployTarget: deployTargetForEphemeral(draft.ephemeral),
          network: { mode: draft.networkMode, subnetId: draft.subnetId.trim() || undefined, defaultGateway: draft.defaultGateway },
        })
        showToast({ tone: 'success', title: 'Deployment template updated' })
      } else {
        await provisioning.createTemplate({
          integrationId: draft.integrationId,
          name: draft.name.trim(),
          description: draft.description.trim(),
          imageId: draft.imageId,
          deployTarget: deployTargetForEphemeral(draft.ephemeral),
          network: { mode: draft.networkMode, subnetId: draft.subnetId.trim() || undefined, defaultGateway: draft.defaultGateway },
        })
        showToast({ tone: 'success', title: 'Deployment template created' })
      }
      closeForm()
      await load()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not save deployment template', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setSaving(false)
    }
  }

  const removeTemplate = async (template: DeploymentTemplate) => {
    if (!window.confirm(`Delete deployment template "${template.name}"?`)) return
    try {
      await provisioning.deleteTemplate(template.id)
      showToast({ tone: 'success', title: 'Deployment template deleted' })
      await load()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not delete deployment template', description: error instanceof Error ? error.message : 'Unknown error' })
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
      showToast({ tone: 'error', title: 'Could not replace cloud-init', description: error instanceof Error ? error.message : 'Unknown error' })
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
      showToast({ tone: 'error', title: 'Could not remove cloud-init', description: error instanceof Error ? error.message : 'Unknown error' })
    }
  }

  const openEdit = (template: DeploymentTemplate) => {
    setDraft({
      id: template.id,
      integrationId: template.integrationId,
      name: template.name,
      description: template.description,
      imageId: template.imageId,
      ephemeral: template.ephemeral,
      networkMode: template.network?.mode ?? 'automatic',
      subnetId: template.network?.subnetId ?? '',
      defaultGateway: template.network?.defaultGateway ?? false,
    })
    setFormOpen(true)
  }
  const deployTemplate = (template: DeploymentTemplate) => {
    navigate(`${scopedHref('/provisioning/deploy')}&templateId=${encodeURIComponent(template.id)}`.replace('?&', '?'))
  }

  const integrationName = (id: string) => (state.status === 'ready' ? state.integrations.find((item) => item.id === id)?.name ?? id : id)
  const siteName = (id: string) => sites.find((item) => item.id === id)?.name ?? id

  return (
    <div className="operator-page">
      <PageHeader
        title="Deployment templates"
        breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/deploy') }, { label: 'Templates' }]}
        actions={
          <Button colorPalette="brand" onClick={() => openCreate()}>
            <Plus size={16} />
            Create template
          </Button>
        }
      />
      <ProvisioningTabs />

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
                  onChange={(value) => setDraft((current) => ({ ...current, integrationId: value, imageId: '' }))}
                  options={state.status === 'ready' ? state.integrations.map((integration) => ({ value: integration.id, label: integration.name })) : []}
                />
              </Field.Root>
              <Field.Root required>
                <Field.Label>
                  Name <Field.RequiredIndicator />
                </Field.Label>
                <Input value={draft.name} onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))} />
              </Field.Root>
              <Field.Root required>
                <Field.Label>
                  OS image <Field.RequiredIndicator />
                </Field.Label>
                <Select
                  value={draft.imageId}
                  disabled={!draft.integrationId || Boolean(imageError)}
                  aria-label="OS image"
                  placeholder="Select an image"
                  onChange={(value) => setDraft((current) => ({ ...current, imageId: value }))}
                  options={[
                    // Keep the current selection visible even if it is no longer in the fetched list.
                    ...(draft.imageId && !images.some((item) => item.id === draft.imageId) ? [{ value: draft.imageId, label: draft.imageId }] : []),
                    ...images.map((image) => ({ value: image.id, label: `${image.name} (${image.architecture})` })),
                  ]}
                />
              </Field.Root>
              <Field.Root>
                <Field.Label>Description</Field.Label>
                <Input value={draft.description} onChange={(event) => setDraft((current) => ({ ...current, description: event.target.value }))} />
              </Field.Root>
              <DeployTargetField
                value={deployTargetForEphemeral(draft.ephemeral)}
                onChange={(nextTarget) => setDraft((current) => ({ ...current, ephemeral: deployTargetIsEphemeral(nextTarget) }))}
                helperText="Disk installs the OS to the machine's disk; RAM runs it from memory (ephemeral)."
              />
              <Field.Root required>
                <Field.Label>Network mode</Field.Label>
                <SegmentGroup.Root
                  value={draft.networkMode}
                  onValueChange={(details) => {
                    if (!details.value) return
                    const mode = details.value as 'automatic' | 'static'
                    setDraft((current) => ({ ...current, networkMode: mode, defaultGateway: mode === 'automatic' ? false : current.defaultGateway }))
                  }}
                >
                  <SegmentGroup.Indicator />
                  {NETWORK_MODES.map((option) => (
                    <SegmentGroup.Item key={option.value} value={option.value}>
                      <SegmentGroup.ItemText>{option.label}</SegmentGroup.ItemText>
                      <SegmentGroup.ItemHiddenInput />
                    </SegmentGroup.Item>
                  ))}
                </SegmentGroup.Root>
              </Field.Root>
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
            {imageError && (
              <Alert status="warning" title="Image catalog unavailable">
                {imageError} Image-changing actions are disabled.
              </Alert>
            )}
            <div className="sw-form-actions">
              <Button variant="ghost" onClick={closeForm}>
                Cancel
              </Button>
              <Button
                colorPalette="brand"
                loading={saving}
                disabled={saving || !draft.name.trim() || !draft.integrationId || !draft.imageId || (draft.networkMode === 'static' && !draft.subnetId.trim()) || (Boolean(imageError) && !draft.id)}
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
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
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
                <Table.ColumnHeader>Image ID</Table.ColumnHeader>
                <Table.ColumnHeader>Ephemeral</Table.ColumnHeader>
                <Table.ColumnHeader>Network</Table.ColumnHeader>
                <Table.ColumnHeader>Cloud-init</Table.ColumnHeader>
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
                  <Table.Cell className="sw-mono">{template.imageId}</Table.Cell>
                  <Table.Cell>{template.ephemeral ? 'Yes' : 'No'}</Table.Cell>
                  <Table.Cell>{template.network?.mode === 'static' ? `Static - ${template.network.subnetId || '-'}` : 'Automatic'}</Table.Cell>
                  <Table.Cell>{template.hasUserData ? 'Configured' : '-'}</Table.Cell>
                  <Table.Cell>{formatDateTime(template.updatedAt)}</Table.Cell>
                  <Table.Cell textAlign="end">
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
                        onClick={() => deployTemplate(template)}
                      >
                        Deploy
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
                    <>
                      <Button size="sm" colorPalette="brand" onClick={() => deployTemplate(template)}>Deploy template</Button>
                      <Button size="sm" variant="outline" onClick={() => openEdit(template)}>Edit template</Button>
                      <Button size="sm" variant="plain" onClick={() => { setSecretTemplate(template); setSecretValue('') }}>
                        {template.hasUserData ? 'Replace cloud-init' : 'Add cloud-init'}
                      </Button>
                      {template.hasUserData && <Button size="sm" variant="plain" onClick={() => void clearSecret(template)}>Remove cloud-init</Button>}
                      <Button size="sm" variant="plain" colorPalette="red" onClick={() => void removeTemplate(template)}>Delete template</Button>
                    </>
                  }
                  details={<ResourceCardField label="Updated">{formatDateTime(template.updatedAt)}</ResourceCardField>}
                >
                  <ResourceCardField label="Site">{siteName(template.siteId)}</ResourceCardField>
                  <ResourceCardField label="Integration">{integrationName(template.integrationId)}</ResourceCardField>
                  <ResourceCardField label="Image ID"><span className="sw-mono">{template.imageId}</span></ResourceCardField>
                  <ResourceCardField label="Ephemeral">{template.ephemeral ? 'Yes' : 'No'}</ResourceCardField>
                  <ResourceCardField label="Network">{template.network?.mode === 'static' ? `Static · ${template.network.subnetId || '-'}` : 'Automatic'}</ResourceCardField>
                  <ResourceCardField label="Cloud-init">{template.hasUserData ? 'Configured' : 'Not configured'}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}
    </div>
  )
}
