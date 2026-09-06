import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  SearchInput,
  TextArea,
  TextInput,
  ToggleGroup,
  ToggleGroupItem,
  ToolbarItem,
} from '@patternfly/react-core'
import { PlusCircleIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { DeploymentTemplate } from '@/domain/provisioning/types'
import type { Integration, OSImage } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
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
      setState({
        status: 'error',
        message: error instanceof Error ? error.message : 'Could not load deployment templates.',
      })
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
    provisioning.listOSImages(draft.integrationId)
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
    return state.templates.filter((template) => [
      template.name,
      template.description,
      template.imageId,
      state.integrations.find((item) => item.id === template.integrationId)?.name ?? '',
    ].some((value) => value.toLowerCase().includes(needle)))
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
    if (
      saving ||
      !draft.name.trim() ||
      !draft.integrationId ||
      !draft.imageId ||
      (Boolean(imageError) && !draft.id)
    ) return
    setSaving(true)
    try {
      if (draft.id) {
        await provisioning.updateTemplate(draft.id, {
          name: draft.name.trim(),
          description: draft.description.trim(),
          imageId: draft.imageId,
          ephemeral: draft.ephemeral,
          network: { mode: draft.networkMode, subnetId: draft.subnetId.trim() || undefined, defaultGateway: draft.defaultGateway },
        })
        showToast({ tone: 'success', title: 'Deployment template updated' })
      } else {
        await provisioning.createTemplate({
          integrationId: draft.integrationId,
          name: draft.name.trim(),
          description: draft.description.trim(),
          imageId: draft.imageId,
          ephemeral: draft.ephemeral,
          network: { mode: draft.networkMode, subnetId: draft.subnetId.trim() || undefined, defaultGateway: draft.defaultGateway },
        })
        showToast({ tone: 'success', title: 'Deployment template created' })
      }
      closeForm()
      await load()
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Could not save deployment template',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
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
      showToast({
        tone: 'error',
        title: 'Could not delete deployment template',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
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
      showToast({
        tone: 'error',
        title: 'Could not replace cloud-init',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
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
      showToast({
        tone: 'error',
        title: 'Could not remove cloud-init',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
    }
  }

  const integrationName = (id: string) => state.status === 'ready'
    ? state.integrations.find((item) => item.id === id)?.name ?? id
    : id
  const siteName = (id: string) => sites.find((item) => item.id === id)?.name ?? id

  return <div className="operator-page">
    <PageHeader
      title="Deployment templates"
      subtitle="Reusable OS deployment intent scoped to one provisioner integration."
      breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/deploy') }, { label: 'Templates' }]}
      actions={<Button variant="primary" icon={<PlusCircleIcon />} onClick={() => openCreate()}>Create template</Button>}
    />
    <ProvisioningTabs />

    {formOpen && (
      <Card>
        <CardTitle>{draft.id ? 'Edit deployment template' : 'Create deployment template'}</CardTitle>
        <CardBody className="sw-template-editor">
          <Form className="sw-form-grid">
            <FormGroup label="Provisioner integration" isRequired fieldId="template-integration">
              <FormSelect
                id="template-integration"
                value={draft.integrationId}
                isDisabled={Boolean(draft.id)}
                onChange={(_event, value) => setDraft((current) => ({
                  ...current,
                  integrationId: value,
                  imageId: '',
                }))}
              >
                <FormSelectOption value="" label="Select an integration" isDisabled isPlaceholder />
                {state.status === 'ready' && state.integrations.map((integration) => (
                  <FormSelectOption key={integration.id} value={integration.id} label={integration.name} />
                ))}
              </FormSelect>
            </FormGroup>
            <FormGroup label="Name" isRequired fieldId="template-name">
              <TextInput id="template-name" value={draft.name} onChange={(_event, value) => setDraft((current) => ({ ...current, name: value }))} />
            </FormGroup>
            <FormGroup label="OS image" isRequired fieldId="template-image">
              <FormSelect
                id="template-image"
                value={draft.imageId}
                isDisabled={!draft.integrationId || Boolean(imageError)}
                onChange={(_event, value) => setDraft((current) => ({ ...current, imageId: value }))}
              >
                <FormSelectOption value="" label="Select an image" isDisabled />
                {draft.imageId && !images.some((item) => item.id === draft.imageId) && (
                  <FormSelectOption value={draft.imageId} label={draft.imageId} />
                )}
                {images.map((image) => (
                  <FormSelectOption key={`${image.id}:${image.architecture}`} value={image.id} label={`${image.name} (${image.architecture})`} />
                ))}
              </FormSelect>
            </FormGroup>
            <FormGroup label="Description" fieldId="template-description">
              <TextInput id="template-description" value={draft.description} onChange={(_event, value) => setDraft((current) => ({ ...current, description: value }))} />
            </FormGroup>
            <FormGroup fieldId="template-ephemeral">
              <Checkbox
                id="template-ephemeral"
                label="Ephemeral deployment"
                isChecked={draft.ephemeral}
                onChange={(_event, checked) => setDraft((current) => ({ ...current, ephemeral: checked }))}
              />
            </FormGroup>
            <FormGroup label="Network mode" isRequired fieldId="template-network-mode">
              <ToggleGroup aria-label="Template network mode">
                <ToggleGroupItem text="Automatic" buttonId="template-network-automatic" isSelected={draft.networkMode === 'automatic'} onChange={() => setDraft((current) => ({ ...current, networkMode: 'automatic', defaultGateway: false }))} />
                <ToggleGroupItem text="Static" buttonId="template-network-static" isSelected={draft.networkMode === 'static'} onChange={() => setDraft((current) => ({ ...current, networkMode: 'static' }))} />
              </ToggleGroup>
            </FormGroup>
            {draft.networkMode === 'static' && <FormGroup label="Subnet ID" isRequired fieldId="template-subnet">
              <TextInput id="template-subnet" value={draft.subnetId} onChange={(_event, value) => setDraft((current) => ({ ...current, subnetId: value }))} />
            </FormGroup>}
            {draft.networkMode === 'static' && <FormGroup fieldId="template-default-gateway">
              <Checkbox id="template-default-gateway" label="Use as default gateway" isChecked={draft.defaultGateway} onChange={(_event, checked) => setDraft((current) => ({ ...current, defaultGateway: checked }))} />
            </FormGroup>}
          </Form>
          {imageError && <Alert variant={AlertVariant.warning} title="Image catalog unavailable" isInline>{imageError} Image-changing actions are disabled.</Alert>}
          <div className="sw-form-actions">
            <Button variant="primary" isLoading={saving} isDisabled={saving || !draft.name.trim() || !draft.integrationId || !draft.imageId || (draft.networkMode === 'static' && !draft.subnetId.trim()) || (Boolean(imageError) && !draft.id)} onClick={() => void submit()}>Save</Button>
            <Button variant="link" onClick={closeForm}>Cancel</Button>
          </div>
        </CardBody>
      </Card>
    )}

    {secretTemplate && (
      <Card>
        <CardTitle>Replace cloud-init for {secretTemplate.name}</CardTitle>
        <CardBody className="sw-template-editor">
          <Alert variant={AlertVariant.info} title="Existing cloud-init is write-only and cannot be displayed." isInline />
          <FormGroup label="New cloud-init" isRequired fieldId="template-user-data">
            <TextArea
              id="template-user-data"
              value={secretValue}
              onChange={(_event, value) => setSecretValue(value)}
              rows={10}
              autoComplete="off"
            />
          </FormGroup>
          <div className="sw-form-actions">
            <Button variant="primary" isLoading={secretSaving} isDisabled={!secretValue || secretSaving} onClick={() => void saveSecret()}>Replace cloud-init</Button>
            <Button variant="link" onClick={() => { setSecretTemplate(null); setSecretValue('') }}>Cancel</Button>
          </div>
        </CardBody>
      </Card>
    )}

    <DataToolbar variant="plain">
      <ToolbarItem>
        <SearchInput
          value={query}
          onChange={(_event, value) => setQuery(value)}
          onClear={() => setQuery('')}
          placeholder="Search templates"
          aria-label="Search deployment templates"
        />
      </ToolbarItem>
    </DataToolbar>

    {state.status === 'loading' && <LoadingState rows={6} />}
    {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
    {state.status === 'ready' && filtered.length === 0 && (
      <EmptyState title="No deployment templates" message="Create reusable OS deployment intent for future scale-out." />
    )}
    {state.status === 'ready' && filtered.length > 0 && (
      <StickyTableFrame>
        <Table aria-label="Deployment templates" variant="compact" className="sw-provisioning-table">
          <Thead><Tr>
            <Th>Name</Th><Th>Site</Th><Th>Integration</Th><Th>Image ID</Th>
            <Th>Ephemeral</Th><Th>Network</Th><Th>Cloud-init</Th><Th>Updated</Th><Th screenReaderText="Actions" />
          </Tr></Thead>
          <Tbody>{filtered.map((template) => (
            <Tr key={template.id}>
              <Td dataLabel="Name"><strong>{template.name}</strong><small>{template.description || '-'}</small></Td>
              <Td dataLabel="Site">{siteName(template.siteId)}</Td>
              <Td dataLabel="Integration">{integrationName(template.integrationId)}</Td>
              <Td dataLabel="Image ID" className="sw-mono">{template.imageId}</Td>
              <Td dataLabel="Ephemeral">{template.ephemeral ? 'Yes' : 'No'}</Td>
              <Td dataLabel="Network">{template.network?.mode === 'static' ? `Static - ${template.network.subnetId || '-'}` : 'Automatic'}</Td>
              <Td dataLabel="Cloud-init">{template.hasUserData ? 'Configured' : '-'}</Td>
              <Td dataLabel="Updated">{formatDateTime(template.updatedAt)}</Td>
              <Td isActionCell>
                <span className="sw-row-actions">
                  <Button variant="link" isInline onClick={() => {
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
                  }}>Edit</Button>
                  <Button variant="link" isInline onClick={() => navigate(`${scopedHref('/provisioning/deploy')}&templateId=${encodeURIComponent(template.id)}`.replace('?&', '?'))}>Deploy</Button>
                  <Button variant="link" isInline onClick={() => { setSecretTemplate(template); setSecretValue('') }}>{template.hasUserData ? 'Replace cloud-init' : 'Add cloud-init'}</Button>
                  {template.hasUserData && <Button variant="link" isInline onClick={() => void clearSecret(template)}>Remove cloud-init</Button>}
                  <Button variant="link" isDanger isInline onClick={() => void removeTemplate(template)}>Delete</Button>
                </span>
              </Td>
            </Tr>
          ))}</Tbody>
        </Table>
      </StickyTableFrame>
    )}
  </div>
}
