import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Label,
  TextArea,
  TextInput,
  Title,
  Wizard,
  WizardFooter,
  WizardStep,
} from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type {
  DeploymentTemplate,
  DeploymentTargetIssue,
  DeploymentUserDataMode,
  DeployServersResult,
  StoredDeploymentResult,
} from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import type { Integration, OSImage } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ProvisioningBadge } from '@/presentation/components/AxisBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useServerWorkingSet } from '@/presentation/pages/servers/useServerWorkingSet'
import { ProvisioningTabs } from './ProvisioningTabs'

const RESULT_STORAGE_KEY = 'swallow.provisioning.last-result'
const MAX_TARGETS = 100

/**
 * Restores only an acceptance report for the exact URL-owned target set.
 * Cloud-init is absent from StoredDeploymentResult and is never read from browser storage.
 */
function readStoredResult(serverIds: string[]): StoredDeploymentResult | null {
  if (serverIds.length === 0) return null
  try {
    const raw = sessionStorage.getItem(RESULT_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as StoredDeploymentResult
    if (!Array.isArray(parsed.serverIds)) return null
    const expected = [...serverIds].sort().join(',')
    return [...parsed.serverIds].sort().join(',') === expected ? parsed : null
  } catch {
    return null
  }
}

/**
 * Persists non-secret dispatch outcomes for reload recovery in this browser tab.
 * A storage failure is non-fatal because the active render retains the same result.
 */
function storeResult(result: StoredDeploymentResult): void {
  try {
    sessionStorage.setItem(RESULT_STORAGE_KEY, JSON.stringify(result))
  } catch {
    // Results remain visible for the active render when session storage is unavailable.
  }
}

/** Rewrites repeated serverId parameters while preserving unrelated URL filters. */
function updateTargetParams(
  params: URLSearchParams,
  serverIds: readonly string[],
  integrationId?: string,
): URLSearchParams {
  const next = new URLSearchParams(params)
  next.delete('serverId')
  serverIds.forEach((id) => next.append('serverId', id))
  if (integrationId) next.set('integrationId', integrationId)
  else next.delete('integrationId')
  return next
}

function serverIsDeployable(server: Server): boolean {
  return !server.absent && server.provisioning?.state === 'ready'
}

function targetIssueName(issue: DeploymentTargetIssue, servers: Server[]): string {
  const server = servers.find((item) => item.id === issue.serverId)
  return server ? serverDisplayName(server) : issue.serverId
}

/**
 * PatternFly OS provisioning workflow for one to one hundred Servers.
 *
 * Targets remain deep-linkable in the URL, cloud-init remains memory-only, and the
 * Results step stores only non-secret acceptance data in sessionStorage. The Targets
 * step performs provider-owned readiness inspection before configuration, while final
 * deployment repeats backend preflight to protect against changed provider state.
 */
export function DeployOSWizardPage() {
  const { provisioning, sites: siteRepository, servers: serverRepository } = useApp()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { siteId, loading: siteScopeLoading, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const workingSet = useServerWorkingSet({ siteId, includeAbsent: true })
  const initialTargetIds = useMemo(() => searchParams.getAll('serverId'), [searchParams])
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set(initialTargetIds))
  const [integrationId, setIntegrationId] = useState(searchParams.get('integrationId') ?? '')
  const [integrations, setIntegrations] = useState<Integration[]>([])
  const [templates, setTemplates] = useState<DeploymentTemplate[]>([])
  const [images, setImages] = useState<OSImage[]>([])
  const [catalogError, setCatalogError] = useState('')
  const [resourcesLoading, setResourcesLoading] = useState(true)
  const [templateId, setTemplateId] = useState(searchParams.get('templateId') ?? '')
  const [customized, setCustomized] = useState(false)
  const [imageId, setImageId] = useState(searchParams.get('imageId') ?? '')
  const [ephemeral, setEphemeral] = useState(false)
  const [userDataMode, setUserDataMode] = useState<DeploymentUserDataMode>('omit')
  const [userData, setUserData] = useState('')
  const [saveTemplate, setSaveTemplate] = useState(false)
  const [templateName, setTemplateName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [checkingTargets, setCheckingTargets] = useState(false)
  const [targetIssues, setTargetIssues] = useState<DeploymentTargetIssue[]>([])
  const [result, setResult] = useState<StoredDeploymentResult | null>(() => readStoredResult(initialTargetIds))
  const [liveServers, setLiveServers] = useState<Record<string, Server>>({})
  const previousSite = useRef<{ initialized: boolean; value?: string }>({
    initialized: false,
    value: siteId,
  })

  // Site-scoped integrations and templates load together; cleanup prevents an older
  // request from overwriting state after a scope change or unmount.
  useEffect(() => {
    let cancelled = false
    setResourcesLoading(true)
    Promise.all([
      siteRepository.listIntegrations({ siteId, kind: 'provisioner' }),
      provisioning.listTemplates({ siteId }),
    ]).then(([nextIntegrations, nextTemplates]) => {
      if (cancelled) return
      setIntegrations(nextIntegrations)
      setTemplates(nextTemplates)
    }).catch((error: Error) => {
      if (!cancelled) {
        showToast({ tone: 'error', title: 'Provisioning resources unavailable', description: error.message })
      }
    }).finally(() => {
      if (!cancelled) setResourcesLoading(false)
    })
    return () => {
      cancelled = true
    }
  }, [provisioning, showToast, siteId, siteRepository])

  // The image catalog is provider-owned live data. A changed integration cancels
  // presentation updates from the previous provider request.
  useEffect(() => {
    if (!integrationId) {
      setImages([])
      setCatalogError('')
      return
    }
    let cancelled = false
    setCatalogError('')
    provisioning.listOSImages(integrationId)
      .then((items) => {
        if (!cancelled) setImages(items)
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setImages([])
          setCatalogError(error.message)
        }
      })
    return () => {
      cancelled = true
    }
  }, [integrationId, provisioning])

  useEffect(() => {
    if (siteScopeLoading) return
    if (!previousSite.current.initialized) {
      previousSite.current = { initialized: true, value: siteId }
      return
    }
    if (previousSite.current.value === siteId) return
    previousSite.current = { initialized: true, value: siteId }
    setSelected(new Set())
    setTargetIssues([])
    setIntegrationId('')
    setTemplateId('')
    setCustomized(false)
    setImageId('')
    setEphemeral(false)
    setUserDataMode('omit')
    setUserData('')
    setSaveTemplate(false)
    setTemplateName('')
    setResult(null)
    try {
      sessionStorage.removeItem(RESULT_STORAGE_KEY)
    } catch {
      // The workflow still resets in memory.
    }
    const next = new URLSearchParams(searchParams)
    ;['serverId', 'integrationId', 'templateId', 'imageId'].forEach((key) => next.delete(key))
    setSearchParams(next, { replace: true })
    showToast({
      tone: 'warning',
      title: 'Provisioning draft cleared',
      description: 'Targets and configuration were cleared because the Site scope changed.',
    })
  }, [searchParams, setSearchParams, showToast, siteId, siteScopeLoading])

  const servers = useMemo(
    () => workingSet.state.status === 'ready' ? workingSet.state.data.servers : [],
    [workingSet.state],
  )
  const selectedServers = useMemo(
    () => servers.filter((server) => selected.has(server.id)),
    [selected, servers],
  )

  useEffect(() => {
    if (workingSet.state.status !== 'ready' || initialTargetIds.length === 0) return
    const compatible = initialTargetIds
      .map((id) => servers.find((server) => server.id === id))
      .filter((server): server is Server => Boolean(server && serverIsDeployable(server)))
    const derivedIntegration = compatible[0]?.source.integrationId ?? ''
    const sameIntegration = compatible.filter(
      (server) => server.source.integrationId === derivedIntegration,
    )
    if (sameIntegration.length !== initialTargetIds.length) {
      setSelected(new Set(sameIntegration.map((server) => server.id)))
      setTargetIssues([])
      setIntegrationId(derivedIntegration)
      setSearchParams(
        updateTargetParams(searchParams, sameIntegration.map((server) => server.id), derivedIntegration),
        { replace: true },
      )
      showToast({
        tone: 'warning',
        title: 'Incompatible targets removed',
        description: 'Only ready, present Servers from one provisioner can be deployed together.',
      })
    } else if (!integrationId && derivedIntegration) {
      setIntegrationId(derivedIntegration)
      setSearchParams(updateTargetParams(searchParams, initialTargetIds, derivedIntegration), { replace: true })
    }
  }, [
    initialTargetIds,
    integrationId,
    searchParams,
    servers,
    setSearchParams,
    showToast,
    workingSet.state.status,
  ])

  useEffect(() => {
    if (!templateId || templates.length === 0) return
    const template = templates.find((item) => item.id === templateId)
    if (!template) {
      setTemplateId('')
      showToast({ tone: 'warning', title: 'Deployment template not found' })
      return
    }
    setIntegrationId(template.integrationId)
    setImageId(template.imageId)
    setEphemeral(template.ephemeral)
    setUserDataMode('inherit')
    setCustomized(false)
  }, [showToast, templateId, templates])

  // Accepted deployments are projections rather than durable jobs, so poll each Server
  // every five seconds only while at least one remains deploying. Unmount and result
  // replacement clear the timer and suppress stale responses.
  useEffect(() => {
    if (!result || result.accepted.length === 0) return
    let cancelled = false
    let failureReported = false
    const refresh = async () => {
      try {
        const entries = await Promise.all(result.accepted.map(async (accepted) => {
          const server = await serverRepository.getServer(accepted.serverId)
          return [accepted.serverId, server] as const
        }))
        if (cancelled) return
        const next: Record<string, Server> = {}
        let stillRunning = false
        for (const [id, server] of entries) {
          if (!server) continue
          next[id] = server
          if (server.provisioning?.state === 'deploying') stillRunning = true
        }
        failureReported = false
        setLiveServers(next)
        if (stillRunning) timer = window.setTimeout(() => void refresh(), 5000)
      } catch (error) {
        if (cancelled) return
        if (!failureReported) {
          showToast({
            tone: 'warning',
            title: 'Deployment status refresh failed',
            description: error instanceof Error ? error.message : 'Unknown error',
          })
        }
        failureReported = true
        timer = window.setTimeout(() => void refresh(), 5000)
      }
    }
    let timer = window.setTimeout(() => void refresh(), 5000)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [result, serverRepository, showToast])

  const availableServers = useMemo(
    () => servers.filter((server) => (
      serverIsDeployable(server) &&
      (!integrationId || server.source.integrationId === integrationId)
    )),
    [integrationId, servers],
  )
  const selectedTemplate = templates.find((item) => item.id === templateId)
  const effectiveImageId = selectedTemplate && !customized ? selectedTemplate.imageId : imageId
  const effectiveEphemeral = selectedTemplate && !customized ? selectedTemplate.ephemeral : ephemeral
  const targetsValid = selected.size > 0 && selected.size <= MAX_TARGETS &&
    selectedServers.length === selected.size &&
    selectedServers.every((server) => (
      serverIsDeployable(server) && server.source.integrationId === integrationId
    ))
  const configurationValid = Boolean(
    integrationId &&
    effectiveImageId &&
    !catalogError &&
    (userDataMode !== 'replace' || userData),
  )
  const inheritedSecretCannotBeSaved = Boolean(
    saveTemplate &&
    selectedTemplate?.hasUserData &&
    customized &&
    userDataMode === 'inherit',
  )
  const reviewValid = targetsValid && configurationValid &&
    (!saveTemplate || Boolean(templateName.trim())) &&
    !inheritedSecretCannotBeSaved

  const checkTargets = async (onNext: () => void) => {
    if (!targetsValid || checkingTargets) return
    setCheckingTargets(true)
    setTargetIssues([])
    try {
      const preflight = await provisioning.preflightDeploymentTargets([...selected])
      if (!preflight.valid) {
        setTargetIssues(preflight.issues)
        showToast({
          tone: 'warning',
          title: 'Deployment targets need attention',
          description: `${preflight.issues.length} Server${preflight.issues.length === 1 ? '' : 's'} cannot be deployed yet.`,
        })
        return
      }
      onNext()
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Could not check deployment readiness',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
    } finally {
      setCheckingTargets(false)
    }
  }

  const selectIntegration = (nextIntegrationId: string) => {
    setIntegrationId(nextIntegrationId)
    setSelected(new Set())
    setTemplateId('')
    setTargetIssues([])
    setCustomized(false)
    setImageId('')
    setEphemeral(false)
    setUserDataMode('omit')
    setUserData('')
    setResult(null)
    setSearchParams(updateTargetParams(searchParams, [], nextIntegrationId), { replace: true })
  }

  const toggleServer = (serverId: string) => {
    setTargetIssues([])
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(serverId)) next.delete(serverId)
      else if (next.size < MAX_TARGETS) next.add(serverId)
      setSearchParams(updateTargetParams(searchParams, [...next], integrationId), { replace: true })
      return next
    })
  }

  const selectTemplate = (id: string) => {
    setTemplateId(id)
    setCustomized(false)
    setUserData('')
    if (!id) {
      setImageId('')
      setEphemeral(false)
      setUserDataMode('omit')
      const next = new URLSearchParams(searchParams)
      next.delete('templateId')
      setSearchParams(next, { replace: true })
      return
    }
    const template = templates.find((item) => item.id === id)
    if (!template) return
    setIntegrationId(template.integrationId)
    setImageId(template.imageId)
    setEphemeral(template.ephemeral)
    setUserDataMode('inherit')
    const next = new URLSearchParams(searchParams)
    next.set('templateId', id)
    next.set('integrationId', template.integrationId)
    setSearchParams(next, { replace: true })
  }

  const deploy = async () => {
    if (!reviewValid || submitting) return
    setSubmitting(true)
    try {
      if (saveTemplate) {
        await provisioning.createTemplate({
          integrationId,
          name: templateName.trim(),
          imageId: effectiveImageId,
          ephemeral: effectiveEphemeral,
          userData: userDataMode === 'replace' ? userData : undefined,
        })
        showToast({ tone: 'success', title: 'Deployment template saved' })
      }
      const response: DeployServersResult = await provisioning.deployServers({
        serverIds: [...selected],
        templateId: selectedTemplate?.id,
        settings: !selectedTemplate || customized
          ? { imageId: effectiveImageId, ephemeral: effectiveEphemeral }
          : undefined,
        userData: {
          mode: selectedTemplate ? userDataMode : userDataMode === 'replace' ? 'replace' : 'omit',
          value: userDataMode === 'replace' ? userData : undefined,
        },
      })
      const stored: StoredDeploymentResult = {
        ...response,
        serverIds: [...selected],
        integrationId,
        savedAt: new Date().toISOString(),
      }
      storeResult(stored)
      setResult(stored)
      setUserData('')
      showToast({
        tone: response.failed.length ? 'warning' : 'success',
        title: response.failed.length ? 'Deployment partially accepted' : 'Deployment accepted',
        description: `${response.accepted.length} accepted, ${response.failed.length} failed.`,
      })
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Deployment preflight failed',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
    } finally {
      setSubmitting(false)
    }
  }

  const retryDeployment = () => {
    setResult(null)
    setLiveServers({})
    setTargetIssues([])
    try {
      sessionStorage.removeItem(RESULT_STORAGE_KEY)
    } catch {
      // The in-memory workflow still restarts when session storage is unavailable.
    }
  }


  const cancel = () => navigate(scopedHref('/servers'))
  const startIndex = result ? 4 : 1

  return <div className="operator-page">
    <PageHeader
      title="Deploy OS"
      subtitle="Apply one operating system configuration to as many as 100 ready Servers."
      breadcrumbs={[{ label: 'Provisioning' }, { label: 'Deploy OS' }]}
    />
    <ProvisioningTabs />
    {workingSet.state.status === 'loading' || resourcesLoading ? <LoadingState rows={7} /> : null}
    {workingSet.state.status === 'error' && <ErrorState message={workingSet.state.message} onRetry={workingSet.reload} />}
    {workingSet.state.status === 'ready' && !resourcesLoading && (
      <Wizard
        key={result ? 'provisioning-result' : 'provisioning-form'}
        aria-label="Deploy operating system"
        className="sw-deploy-wizard"
        height="min(700px, calc(100vh - 220px))"
        startIndex={startIndex}
        isVisitRequired
        shouldFocusContent
        onClose={cancel}
        onSave={cancel}
        footer={(activeStep, onNext, onBack, onClose) => (
          <WizardFooter
            activeStep={activeStep}
            onNext={
              activeStep.id === 'os-targets' ? (event) => void checkTargets(() => onNext(event)) :
              activeStep.id === 'os-review' ? () => void deploy() : onNext
            }
            onBack={onBack}
            onClose={onClose}
            nextButtonText={
              activeStep.id === 'os-review' ? 'Deploy OS' :
              activeStep.id === 'os-results' ? 'Done' :
              activeStep.id === 'os-targets' && targetIssues.length > 0 ? 'Check again' : undefined
            }
            isNextDisabled={
              activeStep.id === 'os-targets' ? !targetsValid || checkingTargets :
              activeStep.id === 'os-configuration' ? !configurationValid :
              activeStep.id === 'os-review' ? !reviewValid || submitting :
              false
            }
            isBackHidden={activeStep.id === 'os-results'}
            nextButtonProps={activeStep.id === 'os-targets'
              ? { isLoading: checkingTargets }
              : activeStep.id === 'os-review' ? { isLoading: submitting } : undefined}
          />
        )}
      >
        <WizardStep name="Targets" id="os-targets" status={targetIssues.length ? 'warning' : targetsValid ? 'success' : 'default'}>
          <WizardSection title="Deployment targets">
            <FormGroup label="Provisioner integration" isRequired fieldId="deploy-integration">
              <FormSelect id="deploy-integration" value={integrationId} onChange={(_event, value) => selectIntegration(value)}>
                <FormSelectOption value="" label="Select an integration" isDisabled />
                {integrations.map((integration) => <FormSelectOption key={integration.id} value={integration.id} label={integration.name} />)}
              </FormSelect>
            </FormGroup>
            <div className="sw-target-summary">
              <strong>{selected.size} of {MAX_TARGETS} selected</strong>
              {selected.size > 0 && <Button variant="link" isInline onClick={() => {
                setTargetIssues([])
                setSelected(new Set())
                setSearchParams(updateTargetParams(searchParams, [], integrationId), { replace: true })
              }}>Clear</Button>}
            </div>
            {targetIssues.length > 0 && <div className="sw-target-issues" aria-live="polite">
              {targetIssues.map((issue) => <Alert key={`${issue.serverId}:${issue.code}`} variant={AlertVariant.warning} title={`${targetIssueName(issue, servers)}: Deployment blocked`} isInline>
                <div>{issue.message}</div>
                <Button variant="link" isInline onClick={() => navigate(scopedHref(`/servers/${issue.serverId}/network`))}>Review Server Network</Button>
              </Alert>)}
            </div>}
            {!integrationId && <EmptyState title="Select a provisioner" message="Targets in a batch must come from the same integration." />}
            {integrationId && availableServers.length === 0 && <EmptyState title="No ready Servers" message="This provisioner has no present Server in the ready state." />}
            {integrationId && availableServers.length > 0 && (
              <StickyTableFrame>
                <Table aria-label="Deployment targets" variant="compact">
                  <Thead><Tr><Th /><Th>Server</Th><Th>Address</Th><Th>Power</Th><Th>State</Th></Tr></Thead>
                  <Tbody>{availableServers.map((server) => (
                    <Tr key={server.id}>
                      <Td className="sw-cell-center">
                        <Checkbox id={`deploy-target-${server.id}`} aria-label={`Select ${serverDisplayName(server)}`} isChecked={selected.has(server.id)} onChange={() => toggleServer(server.id)} />
                      </Td>
                      <Td dataLabel="Server"><strong>{serverDisplayName(server)}</strong></Td>
                      <Td dataLabel="Address" className="sw-mono">{server.addresses[0] ?? '-'}</Td>
                      <Td dataLabel="Power">{server.provisioning?.powerState ?? '-'}</Td>
                      <Td dataLabel="State"><ProvisioningBadge axis={server.provisioning} /></Td>
                    </Tr>
                  ))}</Tbody>
                </Table>
              </StickyTableFrame>
            )}
          </WizardSection>
        </WizardStep>

        <WizardStep name="Configuration" id="os-configuration" status={configurationValid ? 'success' : 'default'}>
          <WizardSection title="Operating system configuration">
            <Form className="sw-form-grid">
              <FormGroup label="Configuration source" isRequired fieldId="deploy-template">
                <FormSelect id="deploy-template" value={templateId} onChange={(_event, value) => selectTemplate(value)}>
                  <FormSelectOption value="" label="Custom configuration" />
                  {templates.filter((template) => template.integrationId === integrationId).map((template) => (
                    <FormSelectOption key={template.id} value={template.id} label={template.name} />
                  ))}
                </FormSelect>
              </FormGroup>
              {selectedTemplate && <FormGroup fieldId="customize-template">
                <Button variant={customized ? 'secondary' : 'link'} onClick={() => setCustomized((value) => !value)}>
                  {customized ? 'Use template defaults' : 'Customize'}
                </Button>
              </FormGroup>}
              <FormGroup label="OS image" isRequired fieldId="deploy-image">
                <FormSelect
                  id="deploy-image"
                  value={effectiveImageId}
                  isDisabled={Boolean(selectedTemplate && !customized) || Boolean(catalogError)}
                  onChange={(_event, value) => setImageId(value)}
                >
                  <FormSelectOption value="" label="Select an image" isDisabled />
                  {effectiveImageId && !images.some((image) => image.id === effectiveImageId) && <FormSelectOption value={effectiveImageId} label={effectiveImageId} />}
                  {images.map((image) => <FormSelectOption key={`${image.id}:${image.architecture}`} value={image.id} label={`${image.name} (${image.architecture})`} />)}
                </FormSelect>
              </FormGroup>
              <FormGroup fieldId="deploy-ephemeral">
                <Checkbox id="deploy-ephemeral" label="Ephemeral deployment" isChecked={effectiveEphemeral} isDisabled={Boolean(selectedTemplate && !customized)} onChange={(_event, checked) => setEphemeral(checked)} />
              </FormGroup>
              <FormGroup label="Cloud-init" isRequired fieldId="deploy-user-data-mode">
                <FormSelect id="deploy-user-data-mode" value={userDataMode} onChange={(_event, value) => { setUserDataMode(value as DeploymentUserDataMode); if (value !== 'replace') setUserData('') }}>
                  {selectedTemplate && <FormSelectOption value="inherit" label={selectedTemplate.hasUserData ? 'Inherit template cloud-init' : 'Inherit (template has none)'} />}
                  <FormSelectOption value="replace" label="Replace for this deployment" />
                  <FormSelectOption value="omit" label="Omit cloud-init" />
                </FormSelect>
              </FormGroup>
              {userDataMode === 'replace' && <FormGroup label="Cloud-init" isRequired fieldId="deploy-user-data">
                <TextArea id="deploy-user-data" value={userData} onChange={(_event, value) => setUserData(value)} rows={10} autoComplete="off" />
              </FormGroup>}
            </Form>
            {catalogError && <Alert variant={AlertVariant.warning} title="Image catalog unavailable" isInline>{catalogError}</Alert>}
            {selectedTemplate && !customized && <Alert variant={AlertVariant.info} title="Template settings are locked" isInline>Choose Customize to override the image or ephemeral setting.</Alert>}
          </WizardSection>
        </WizardStep>

        <WizardStep name="Review" id="os-review" status={reviewValid ? 'success' : 'warning'}>
          <WizardSection title="Review deployment">
            <Card isCompact>
              <CardTitle>{selected.size} Server{selected.size === 1 ? '' : 's'}</CardTitle>
              <CardBody>
                <DescriptionList isHorizontal isCompact>
                  <ReviewItem label="Integration" value={integrations.find((item) => item.id === integrationId)?.name ?? integrationId} />
                  <ReviewItem label="Configuration" value={selectedTemplate ? `${selectedTemplate.name}${customized ? ' (customized)' : ''}` : 'Custom'} />
                  <ReviewItem label="Image" value={effectiveImageId} />
                  <ReviewItem label="Ephemeral" value={effectiveEphemeral ? 'Yes' : 'No'} />
                  <ReviewItem label="Cloud-init" value={userDataMode === 'inherit' ? 'Inherit from template' : userDataMode === 'replace' ? 'Replace for this deployment' : 'Omit'} />
                </DescriptionList>
              </CardBody>
            </Card>
            <StickyTableFrame>
              <Table aria-label="Deployment review targets" variant="compact">
                <Thead><Tr><Th>Server</Th><Th>Provider machine ID</Th><Th>Power</Th></Tr></Thead>
                <Tbody>{selectedServers.map((server) => <Tr key={server.id}>
                  <Td>{serverDisplayName(server)}</Td>
                  <Td className="sw-mono">{server.source.providerMachineId}</Td>
                  <Td>{server.provisioning?.powerState ?? '-'}</Td>
                </Tr>)}</Tbody>
              </Table>
            </StickyTableFrame>
            {(!selectedTemplate || customized) && <Card isCompact>
              <CardTitle>Reuse this configuration</CardTitle>
              <CardBody className="sw-template-editor">
                <Checkbox id="save-deployment-template" label="Save as a deployment template" isChecked={saveTemplate} onChange={(_event, checked) => setSaveTemplate(checked)} />
                {saveTemplate && <TextInput aria-label="New deployment template name" value={templateName} onChange={(_event, value) => setTemplateName(value)} placeholder="Template name" />}
                {inheritedSecretCannotBeSaved && <Alert variant={AlertVariant.warning} title="Inherited cloud-init cannot be copied" isInline>Select Replace or Omit before saving a customized template.</Alert>}
              </CardBody>
            </Card>}
            <Alert variant={AlertVariant.warning} title="Deployment starts immediately after preflight" isInline>Accepted provider requests cannot be rolled back as a batch.</Alert>
          </WizardSection>
        </WizardStep>

        <WizardStep name="Results" id="os-results" isDisabled={!result} status={result?.failed.length ? 'warning' : result ? 'success' : 'default'}>
          <WizardSection title="Deployment results">
            {result && <>
              <div className="sw-result-summary">
                <Label color="green">{result.accepted.length} accepted</Label>
                <Label color={result.failed.length ? 'red' : 'grey'}>{result.failed.length} failed</Label>
                <Button variant="secondary" onClick={retryDeployment}>Configure and try again</Button>
              </div>
              {result.accepted.length > 0 && <StickyTableFrame>
                <Table aria-label="Accepted deployments" variant="compact">
                  <Thead><Tr><Th>Server</Th><Th>Current state</Th><Th>Observed</Th></Tr></Thead>
                  <Tbody>{result.accepted.map((accepted) => {
                    const live = liveServers[accepted.serverId]
                    return <Tr key={accepted.serverId}>
                      <Td><Button variant="link" isInline onClick={() => navigate(scopedHref(`/servers/${accepted.serverId}/summary`))}>{live ? serverDisplayName(live) : accepted.serverId}</Button></Td>
                      <Td><ProvisioningBadge axis={live?.provisioning ?? { ...accepted, integrationId: result.integrationId }} /></Td>
                      <Td>{live?.provisioning?.observedAt ?? accepted.observedAt}</Td>
                    </Tr>
                  })}</Tbody>
                </Table>
              </StickyTableFrame>}
              {result.failed.map((failure) => <Alert key={failure.serverId} variant={AlertVariant.danger} title={`${failure.serverId}: ${failure.code}`} isInline>{failure.message}</Alert>)}
            </>}
          </WizardSection>
        </WizardStep>
      </Wizard>
    )}
  </div>
}

function WizardSection({ title, children }: { title: string; children: ReactNode }) {
  return <section className="sw-wizard-section"><Title headingLevel="h2" size="lg">{title}</Title>{children}</section>
}

function ReviewItem({ label, value }: { label: string; value: ReactNode }) {
  return <DescriptionListGroup><DescriptionListTerm>{label}</DescriptionListTerm><DescriptionListDescription>{value || '-'}</DescriptionListDescription></DescriptionListGroup>
}
