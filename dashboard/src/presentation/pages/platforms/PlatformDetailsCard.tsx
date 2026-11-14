import { useId, useState, type ReactNode } from 'react'
import { IconButton, Text } from '@chakra-ui/react'
import { PenLine } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Condition } from '@/domain/convergence/types'
import { formatGPUDriverModuleOptions } from '@/domain/platform/gpuDriver'
import { platformLifecycleLabel } from '@/domain/platform/lifecycle'
import { platformMemberCounts } from '@/domain/platform/membership'
import { platformCurrentSpec } from '@/domain/platform/spec'
import type { Platform } from '@/domain/platform/types'
import { DEPLOY_TARGET_LABELS } from '@/domain/provisioning/types'
import { isAdmin } from '@/domain/user/access'
import { SectionHeader } from '@/presentation/components/ConsolePrimitives'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { useIntegration } from '@/presentation/pages/infrastructure/useIntegrationSettings'
import type { PlatformFact, PlatformModuleUI } from '@/modules/registry'
import { formatDateTime } from '@/shared/utils/time'
import { platformTopologyLabel } from './platformDetailShared'
import { PlatformPoolDialog } from './PlatformPoolDialog'
import { describePlatformSpecState } from './platformSpecState'

interface PlatformDetailsCardProps {
  platform: Platform
  /** The module that owns the Platform's type, from the registry; undefined when none ships a UI. */
  module: PlatformModuleUI | undefined
  /** Called after an admin moved the Platform to another Pool, so the page re-reads it. */
  onPoolChanged: () => void
}

/**
 * The Platform's Pool: its name, linked to the Pool's page, or "N/A" with why when it has none. A
 * Platform belongs to the Pool every one of its Servers is in, so "none" covers Servers in no Pool and
 * Servers across Pools alike; such a Platform is the admins'. `onMove`, given to admins only because
 * the move is an admin route, adds the shortcut that moves the Platform with all its Servers.
 */
function PoolValue({ platform, poolName, href, onMove }: {
  platform: Platform
  poolName: string | null
  href: string | null
  onMove?: () => void
}) {
  const moveLabel = `Move ${platform.name} to another pool`
  const none = poolName === null || href === null
  return (
    <FactValue details={none ? ['Its servers are in no pool or in different pools; only admins manage it.'] : []}>
      <span className="sw-platform-pool">
        {none ? <Text as="span" color="fg.muted">N/A</Text> : <RouterLink to={href}>{poolName}</RouterLink>}
        {onMove && (
          <Tooltip content={moveLabel}>
            <IconButton variant="ghost" size="2xs" aria-label={moveLabel} onClick={onMove}>
              <PenLine size={13} aria-hidden />
            </IconButton>
          </Tooltip>
        )}
      </span>
    </FactValue>
  )
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

/** Human-readable Condition name without assuming the backend's open type vocabulary. */
function conditionLabel(value: string): string {
  const words = value.replaceAll('_', ' ').replaceAll('-', ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** One controller judgement with its generation, message, and transition time kept together. */
function conditionValue(condition: Condition): ReactNode {
  const reason = conditionLabel(condition.reason)
  let summary = reason
  if (condition.type === 'drifted') {
    if (condition.status === 'true') summary = reason === 'Drifted' ? 'Drift detected' : `Drift detected · ${reason}`
    else if (condition.status === 'false') summary = 'In sync'
    else summary = `Drift unknown · ${reason}`
  } else if (condition.status === 'unknown') {
    summary = `Unknown · ${reason}`
  } else if (condition.type !== 'realized') {
    summary = `${condition.status === 'true' ? 'True' : 'False'} · ${reason}`
  }
  const details: ReactNode[] = []
  if (condition.message) details.push(condition.message)
  details.push(<>Generation {condition.observedGeneration} · Updated <Timestamp value={condition.lastTransitionTime} /></>)
  return <FactValue details={details}>{summary}</FactValue>
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
 * - General: type, lifecycle, Site, Pool, creation time, and where its spec stands
 *   (`describePlatformSpecState`). Workflow history belongs to the route-owned Activity page. An admin
 *   moves the Platform to another Pool from the Pool fact (`PlatformPoolDialog`).
 * - Conditions: the controller's current generation-aware judgements, including drift.
 * - Deployment: topology, members, and OS preparation, from the applied spec before the desired one.
 * - The type's own facts, from the owning module's `facts` and titled with its label, so this shell
 *   never branches on a type name (architecture spec "Module UI"); a module without facts adds no group.
 * - Integration and GPU: the credential Integration by name ("Not connected" when the Platform has
 *   none), and the GPU stack, plus the GRES type and driver options only when the user set them.
 *
 * The page renders it once the Platform loaded, so it has no loading or error state of its own.
 * Everything comes from the Platform record, except the Site name (the Site scope's registry), the
 * Pool name (the Pool catalog, a reference read), and the Integration name, which an admin's page
 * reads once per Integration id. Each falls back to the id while loading or when unreadable, so a
 * failed lookup never blanks a fact. Each group is a named `group` labelled by its heading, and the
 * card is the "Platform details" region.
 */
export function PlatformDetailsCard({ platform, module, onPoolChanged }: PlatformDetailsCardProps) {
  const { infrastructure } = useApp()
  const { sites, scopedHref } = useSiteScope()
  const { currentUser } = useAuth()
  const admin = isAdmin(currentUser)
  const [movingPool, setMovingPool] = useState(false)
  const poolCatalog = useAsyncData(() => infrastructure.listPools(), [infrastructure])
  const poolId = platform.status.poolId ?? null
  const poolName = poolId === null
    ? null
    : (poolCatalog.status === 'ready' ? poolCatalog.data.find((pool) => pool.id === poolId)?.name : undefined) ?? poolId
  // Reading an Integration is an admin route; other users see the Integration's id.
  const { state: integrationState } = useIntegration(admin ? platform.integrationId ?? undefined : undefined)
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
        {
          label: 'Pool',
          value: (
            <PoolValue
              platform={platform}
              poolName={poolName}
              href={poolId === null ? null : scopedHref(`/pools/${encodeURIComponent(poolId)}`)}
              onMove={admin ? () => setMovingPool(true) : undefined}
            />
          ),
        },
        { label: 'Created', value: <Timestamp value={platform.createdAt} /> },
        { label: 'Spec state', value: <FactValue details={specState.details}>{specState.summary}</FactValue> },
      ],
    },
    ...(platform.status.conditions.length > 0 ? [{
      title: 'Conditions',
      facts: platform.status.conditions.map((condition) => ({
        label: conditionLabel(condition.type),
        value: conditionValue(condition),
      })),
    }] : []),
    {
      title: 'Deployment',
      facts: [
        { label: 'Topology', value: platformTopologyLabel(platform, module, 'Not recorded') },
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
      {movingPool && (
        <PlatformPoolDialog platform={platform} onClose={() => setMovingPool(false)} onMoved={onPoolChanged} />
      )}
    </section>
  )
}
