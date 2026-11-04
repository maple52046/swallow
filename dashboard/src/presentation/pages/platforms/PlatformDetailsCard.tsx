import { useId, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { Operation } from '@/domain/operation/types'
import { formatGPUDriverModuleOptions } from '@/domain/platform/gpuDriver'
import { latestSucceededDeployment, platformLifecycleLabel, workflowFinishedAt } from '@/domain/platform/lifecycle'
import { platformMemberCounts } from '@/domain/platform/membership'
import { platformCurrentSpec } from '@/domain/platform/spec'
import type { Platform } from '@/domain/platform/types'
import { DEPLOY_TARGET_LABELS } from '@/domain/provisioning/types'
import { SectionHeader } from '@/presentation/components/ConsolePrimitives'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useIntegration } from '@/presentation/pages/infrastructure/useIntegrationSettings'
import type { PlatformFact, PlatformModuleUI } from '@/modules/registry'
import { formatDateTime } from '@/shared/utils/time'
import { topologyLabel } from './platformDetailShared'
import { describePlatformSpecState } from './platformSpecState'

interface PlatformDetailsCardProps {
  platform: Platform
  /**
   * The Platform's related Workflows as the page loaded them (one page, newest requested first);
   * the last deployment is looked up among them, never fetched separately.
   */
  operations: readonly Operation[]
  /** The module that owns the Platform's type, from the registry; undefined when none ships a UI. */
  module: PlatformModuleUI | undefined
}

/** One titled group of facts, rendered as an `<h3>` and the `<dl>` it labels. */
interface FactGroup {
  title: string
  facts: PlatformFact[]
}

/** A fact's main value with explanation lines under it, in the muted secondary style. */
function FactValue({ children, details }: { children: ReactNode; details: ReactNode[] }) {
  return (
    <>
      {children}
      {details.map((detail, index) => (
        // The lines are fixed, ordered parts of one fact that never reorder, so their index is a stable key.
        <span key={index} className="sw-platform-facts__detail">{detail}</span>
      ))}
    </>
  )
}

/** A timestamp shown in the viewer's locale, keeping the API's ISO value machine-readable. */
function Timestamp({ value }: { value: string }) {
  return <time dateTime={value}>{formatDateTime(value)}</time>
}

/**
 * The Platform's last deployment: when the newest succeeded deployment Workflow finished, with a
 * link to that Workflow. Without one among the loaded Workflows the words follow the lifecycle: a
 * Platform that swallow has not finished deploying is "Not deployed yet", a registered one was
 * never deployed by swallow, and one whose lifecycle proves a past deployment (active or being
 * uninstalled) deployed before the loaded history, which must not read as never deployed.
 */
function lastDeployedValue(platform: Platform, operations: readonly Operation[], workflowHref: (id: string) => string): ReactNode {
  const deployment = latestSucceededDeployment(operations)
  if (deployment) {
    const finishedAt = workflowFinishedAt(deployment)
    return (
      <FactValue details={[<Link to={workflowHref(deployment.id)}>View deployment workflow</Link>]}>
        {finishedAt ? <Timestamp value={finishedAt} /> : 'Finished'}
      </FactValue>
    )
  }
  if (platform.origin === 'registered') return 'Not deployed by Swallow'
  switch (platform.lifecycleState) {
    case 'active':
    case 'uninstalling':
    case 'uninstall_failed':
    case 'uninstalled':
      return 'Not in the recent Workflow history'
    default:
      return 'Not deployed yet'
  }
}

/**
 * The Platform's member count: the Servers of its applied (else desired) spec, which are its
 * Platform Claims and include Slurm login hosts. A Platform not adopted yet has no spec, so it
 * falls back to the membership the Platform's API reports, counted as the Platforms list does.
 */
function membersValue(platform: Platform): string {
  const spec = platformCurrentSpec(platform)
  if (spec && spec.members.length > 0) {
    return spec.members.length === 1 ? '1 Server' : `${spec.members.length} Servers`
  }
  const { total } = platformMemberCounts(platform)
  return total === 1 ? '1 reported member' : `${total} reported members`
}

/**
 * How the deployment prepared its members' OS (platforms.md "Create A Platform"): Provision OS
 * names the image and Deploy Target (omitted means disk); Existing OS reuses each Server's deployed
 * OS, so there is no image or target to show. An unknown Deploy Target is shown as sent.
 */
function machinePreparationFacts(platform: Platform): PlatformFact[] {
  const spec = platformCurrentSpec(platform)
  if (!spec) return [{ label: 'OS preparation', value: 'Not recorded' }]
  const preparation = spec.machinePreparation
  if (preparation?.mode !== 'provision_os') return [{ label: 'OS preparation', value: 'Existing OS' }]
  const target = preparation.settings?.deployTarget ?? 'disk'
  return [
    { label: 'OS preparation', value: 'Provision OS' },
    { label: 'Image', value: preparation.settings?.imageId ?? 'Not recorded' },
    { label: 'Deploy target', value: target === 'disk' || target === 'ram' ? DEPLOY_TARGET_LABELS[target] : target },
  ]
}

/**
 * Platform details: the facts a user looks for first on a Platform, at the top of the Overview tab
 * below its lifecycle notices, in titled `<dl>` groups.
 *
 * - General: type, lifecycle, Site, when it was created and last deployed (linked to that
 *   Workflow), and where its spec stands (`describePlatformSpecState`).
 * - Deployment: topology, members, and OS preparation, from the applied spec before the desired one.
 * - The type's own facts, from the owning module's `facts` and titled with its label, so this shell
 *   never branches on a type name (architecture spec "Module UI"); a module without facts adds no group.
 * - Integration and GPU: the credential Integration by name ("Not connected" when the Platform has
 *   none), and the GPU stack, plus the GRES type and driver options only when the user set them.
 *
 * The page renders it once the Platform loaded, so it has no loading or error state of its own.
 * Everything comes from the Platform record and the page's Workflows, except the Site name (the Site
 * scope's registry) and the Integration name, read once per Integration id. Both fall back to the id
 * while loading or when unreadable, so a failed lookup never blanks a fact. Each group is a named
 * `group` labelled by its heading, and the card is the "Platform details" region.
 */
export function PlatformDetailsCard({ platform, operations, module }: PlatformDetailsCardProps) {
  const { sites, scopedHref } = useSiteScope()
  const { state: integrationState } = useIntegration(platform.integrationId ?? undefined)
  const headingPrefix = useId()
  const specState = describePlatformSpecState(platform)
  const siteName = sites.find((site) => site.id === platform.siteId)?.name ?? platform.siteId
  // The hook keeps its last answer while a new id loads, so a name is used only for the current id.
  const integrationName = integrationState.status === 'ready' && integrationState.integration.id === platform.integrationId
    ? integrationState.integration.name
    : platform.integrationId
  const driverOptions = formatGPUDriverModuleOptions(platform.gpuDriver?.moduleOptions)
  const moduleFacts = module?.facts?.(platform) ?? []

  const groups: FactGroup[] = [
    {
      title: 'General',
      facts: [
        { label: 'Type', value: module?.label ?? platform.type },
        { label: 'Lifecycle', value: platformLifecycleLabel(platform.lifecycleState) },
        { label: 'Site', value: siteName },
        { label: 'Created', value: <Timestamp value={platform.createdAt} /> },
        { label: 'Last deployed', value: lastDeployedValue(platform, operations, (id) => scopedHref(`/workflows/${id}`)) },
        { label: 'Spec state', value: <FactValue details={specState.details}>{specState.summary}</FactValue> },
      ],
    },
    {
      title: 'Deployment',
      facts: [
        { label: 'Topology', value: platform.deployment ? topologyLabel(platform.deployment.topology) : 'Not recorded' },
        { label: 'Members', value: membersValue(platform) },
        ...machinePreparationFacts(platform),
      ],
    },
    ...(module && moduleFacts.length > 0 ? [{ title: module.label, facts: moduleFacts }] : []),
    {
      title: 'Integration and GPU',
      facts: [
        { label: 'Integration', value: integrationName ?? 'Not connected' },
        { label: 'GPU stack', value: platform.gpuStackOwner },
        ...(platform.gpuGresType ? [{ label: 'GPU GRES type', value: platform.gpuGresType }] : []),
        ...(driverOptions ? [{ label: 'GPU driver options', value: driverOptions }] : []),
      ],
    },
  ]

  return (
    <section className="sw-section" aria-label="Platform details">
      <SectionHeader title="Platform details" />
      <div className="sw-platform-details">
        {groups.map((group, index) => {
          const headingId = `${headingPrefix}-${index}`
          return (
            <div key={group.title} role="group" aria-labelledby={headingId}>
              <h3 id={headingId} className="sw-platform-details__heading">{group.title}</h3>
              <dl className="sw-platform-facts">
                {group.facts.map((fact) => (
                  <div key={fact.label}>
                    <dt>{fact.label}</dt>
                    <dd>{fact.value}</dd>
                  </div>
                ))}
              </dl>
            </div>
          )
        })}
      </div>
    </section>
  )
}
