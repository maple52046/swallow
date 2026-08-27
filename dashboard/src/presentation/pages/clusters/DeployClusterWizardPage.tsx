import { useMemo, useState, type ReactNode } from 'react'
import {
  Alert, AlertVariant, Card, CardBody, CardTitle, DescriptionList, DescriptionListDescription,
  DescriptionListGroup, DescriptionListTerm, Form, FormGroup, FormHelperText, FormSelect,
  FormSelectOption, HelperText, HelperTextItem, Label, LabelGroup, TextInput, Title, Wizard,
  WizardStep,
} from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { GPUStackOwner, NodeRole, RoleAssignment } from '@/domain/cluster/types'
import { serverDisplayName, serverPrimaryAddress } from '@/domain/server/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useDeployableServers } from './useDeployableServers'

const DEFAULT_K0S_VERSION = 'v1.36.3+k0s.2'
type RoleChoice = 'none' | NodeRole

function validAddress(value: string): boolean {
  const parts = value.split('.')
  return parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
}

/** PatternFly deployment wizard that preserves input and creates an Operation on save. */
export function DeployClusterWizardPage() {
  const navigate = useNavigate()
  const { clusters } = useApp()
  const { siteId: scopedSiteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [siteId, setSiteId] = useState<string | undefined>(scopedSiteId)
  const [name, setName] = useState('')
  const [gpuStackOwner, setGpuStackOwner] = useState<GPUStackOwner>('provisioning')
  const [k0sVersion, setK0sVersion] = useState(DEFAULT_K0S_VERSION)
  const [apiVip, setApiVip] = useState('')
  const [apiVipPrefix, setApiVipPrefix] = useState('24')
  const [podCidr, setPodCidr] = useState('')
  const [serviceCidr, setServiceCidr] = useState('')
  const [roles, setRoles] = useState<Record<string, RoleChoice>>({})
  const [submitting, setSubmitting] = useState(false)
  const effectiveSiteId = siteId ?? scopedSiteId
  const state = useDeployableServers(effectiveSiteId)

  const assignments = useMemo<RoleAssignment[]>(() => Object.entries(roles)
    .filter(([, role]) => role !== 'none')
    .map(([serverId, role]) => ({ serverId, role: role as NodeRole })), [roles])
  const controllers = assignments.filter((item) => item.role === 'control-plane').length
  const workers = assignments.filter((item) => item.role === 'worker').length
  const basicsValid = Boolean(effectiveSiteId && name.trim() && k0sVersion.trim())
  const networkingValid = validAddress(apiVip.trim()) && Number(apiVipPrefix) >= 1 && Number(apiVipPrefix) <= 32
  const rolesValid = controllers >= 3 && controllers % 2 === 1 && workers >= 1

  const deploy = async () => {
    if (!effectiveSiteId || !basicsValid || !networkingValid || !rolesValid || submitting) return
    setSubmitting(true)
    try {
      const result = await clusters.deployCluster({
        siteId: effectiveSiteId,
        name: name.trim(),
        gpuStackOwner,
        k0sVersion: k0sVersion.trim(),
        apiVip: apiVip.trim(),
        apiVipPrefix: Number(apiVipPrefix),
        podCidr: podCidr.trim() || undefined,
        serviceCidr: serviceCidr.trim() || undefined,
        roleAssignments: assignments,
      })
      showToast({ title: 'Deployment started', description: 'Follow the Operation for live progress and retained output.', tone: 'success' })
      navigate(`/operations/${result.operationId}?site=${encodeURIComponent(effectiveSiteId)}`)
    } catch (error) {
      showToast({ title: 'Deployment failed', description: error instanceof Error ? error.message : 'Could not start the deployment.', tone: 'error' })
      setSubmitting(false)
    }
  }

  return (
    <div className="operator-page">
      <PageHeader title="Deploy cluster" breadcrumbs={[{ label: 'Clusters', href: scopedHref('/clusters') }, { label: 'Deploy' }]} subtitle="Build a highly available k0s cluster on deployed Servers." />
      {state.status === 'loading' && <LoadingState rows={7} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && (
        <Wizard
          aria-label="Deploy cluster"
          className="sw-deploy-wizard"
          height="min(670px, calc(100vh - 220px))"
          isVisitRequired
          shouldFocusContent
          onClose={() => navigate(scopedHref('/clusters'))}
          onSave={() => void deploy()}
        >
          <WizardStep name="Basics" id="deploy-basics" status={basicsValid ? 'success' : 'default'} footer={{ isNextDisabled: !basicsValid }}>
            <WizardSection title="Cluster identity">
              <Form className="sw-form-grid">
                <FormGroup label="Site" isRequired fieldId="cluster-site">
                  <FormSelect id="cluster-site" value={effectiveSiteId ?? ''} onChange={(_event, value) => { setSiteId(value); setRoles({}) }}>
                    <FormSelectOption value="" label="Select a Site" isDisabled />
                    {state.data.sites.map((site) => <FormSelectOption key={site.id} value={site.id} label={site.name} />)}
                  </FormSelect>
                </FormGroup>
                <FormGroup label="Cluster name" isRequired fieldId="cluster-name">
                  <TextInput id="cluster-name" value={name} onChange={(_event, value) => setName(value)} placeholder="lab-k0s" />
                </FormGroup>
                <FormGroup label="GPU stack owner" isRequired fieldId="cluster-gpu-owner">
                  <FormSelect id="cluster-gpu-owner" value={gpuStackOwner} onChange={(_event, value) => setGpuStackOwner(value as GPUStackOwner)}>
                    <FormSelectOption value="provisioning" label="Provisioning" />
                    <FormSelectOption value="gpu-operator" label="GPU Operator" />
                  </FormSelect>
                </FormGroup>
                <FormGroup label="k0s version" isRequired fieldId="cluster-version">
                  <TextInput id="cluster-version" value={k0sVersion} onChange={(_event, value) => setK0sVersion(value)} />
                </FormGroup>
              </Form>
            </WizardSection>
          </WizardStep>
          <WizardStep name="Networking" id="deploy-networking" status={networkingValid ? 'success' : 'default'} footer={{ isNextDisabled: !networkingValid }}>
            <WizardSection title="Control-plane network">
              <Alert variant={AlertVariant.info} title="The API virtual IP must be routable on the node network and outside DHCP ranges." isInline />
              <Form className="sw-form-grid">
                <FormGroup label="API virtual IP" isRequired fieldId="cluster-api-vip">
                  <TextInput id="cluster-api-vip" value={apiVip} onChange={(_event, value) => setApiVip(value)} placeholder="192.168.100.200" validated={!apiVip || validAddress(apiVip) ? 'default' : 'error'} />
                  <FormHelperText><HelperText><HelperTextItem variant={!apiVip || validAddress(apiVip) ? 'default' : 'error'}>Enter an IPv4 address.</HelperTextItem></HelperText></FormHelperText>
                </FormGroup>
                <FormGroup label="VIP prefix length" isRequired fieldId="cluster-api-prefix">
                  <TextInput id="cluster-api-prefix" type="number" value={apiVipPrefix} onChange={(_event, value) => setApiVipPrefix(value)} min={1} max={32} />
                </FormGroup>
                <FormGroup label="Pod CIDR" fieldId="cluster-pod-cidr"><TextInput id="cluster-pod-cidr" value={podCidr} onChange={(_event, value) => setPodCidr(value)} placeholder="Backend default" /></FormGroup>
                <FormGroup label="Service CIDR" fieldId="cluster-service-cidr"><TextInput id="cluster-service-cidr" value={serviceCidr} onChange={(_event, value) => setServiceCidr(value)} placeholder="Backend default" /></FormGroup>
              </Form>
            </WizardSection>
          </WizardStep>
          <WizardStep name="Roles" id="deploy-roles" status={rolesValid ? 'success' : 'default'} footer={{ isNextDisabled: !rolesValid }}>
            <WizardSection title="Member roles">
              <LabelGroup aria-label="Topology status">
                <Label color={controllers >= 3 && controllers % 2 === 1 ? 'green' : 'orange'}>{controllers} control-plane</Label>
                <Label color={workers >= 1 ? 'green' : 'orange'}>{workers} workers</Label>
              </LabelGroup>
              <p className="sw-muted">An odd control-plane quorum of at least three and one worker are required.</p>
              {state.data.servers.length === 0 ? <EmptyState title="No deployed Servers" message="Deploy an OS in this Site before building a Cluster." /> : (
                <StickyTableFrame><Table aria-label="Deployable Servers" variant="compact"><Thead><Tr><Th>Server</Th><Th>Address</Th><Th>Role</Th></Tr></Thead><Tbody>{state.data.servers.map((server) => <Tr key={server.id}><Td dataLabel="Server"><strong>{serverDisplayName(server)}</strong></Td><Td dataLabel="Address" className="sw-mono">{serverPrimaryAddress(server) ?? 'No data'}</Td><Td dataLabel="Role"><FormSelect aria-label={`Role for ${serverDisplayName(server)}`} value={roles[server.id] ?? 'none'} onChange={(_event, value) => setRoles((current) => ({ ...current, [server.id]: value as RoleChoice }))}><FormSelectOption value="none" label="Not included" /><FormSelectOption value="control-plane" label="Control-plane" /><FormSelectOption value="worker" label="Worker" /></FormSelect></Td></Tr>)}</Tbody></Table></StickyTableFrame>
              )}
            </WizardSection>
          </WizardStep>
          <WizardStep name="Review" id="deploy-review" status={basicsValid && networkingValid && rolesValid ? 'success' : 'warning'} footer={{ nextButtonText: 'Deploy cluster', isNextDisabled: submitting || !basicsValid || !networkingValid || !rolesValid, nextButtonProps: { isLoading: submitting } }}>
            <WizardSection title="Review deployment">
              <Card isCompact><CardTitle>{name}</CardTitle><CardBody><DescriptionList isHorizontal isCompact>{[
                ['Site', state.data.sites.find((site) => site.id === effectiveSiteId)?.name ?? effectiveSiteId],
                ['k0s version', k0sVersion], ['GPU stack owner', gpuStackOwner], ['API virtual IP', `${apiVip}/${apiVipPrefix}`],
                ['Pod CIDR', podCidr || 'Backend default'], ['Service CIDR', serviceCidr || 'Backend default'],
                ['Topology', `${controllers} control-plane, ${workers} workers`],
              ].map(([label, value]) => <DescriptionListGroup key={label}><DescriptionListTerm>{label}</DescriptionListTerm><DescriptionListDescription>{value}</DescriptionListDescription></DescriptionListGroup>)}</DescriptionList></CardBody></Card>
              <Alert variant={AlertVariant.info} title="Submitting creates a Cluster and an Operation." isInline />
            </WizardSection>
          </WizardStep>
        </Wizard>
      )}
    </div>
  )
}

function WizardSection({ title, children }: { title: string; children: ReactNode }) {
  return <section className="sw-wizard-section"><Title headingLevel="h2" size="lg">{title}</Title>{children}</section>
}
