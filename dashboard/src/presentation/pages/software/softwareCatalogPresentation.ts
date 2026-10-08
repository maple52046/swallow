import { Box, Container, FolderSync, type LucideIcon } from 'lucide-react'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/types'
import { softwareInstallBlocker } from '@/domain/software/targets'
import type {
  SoftwareAssignment,
  SoftwareAssignmentState,
  SoftwareCatalogEntry,
  SoftwareKind,
} from '@/domain/software/types'

/** Stable catalog section owned by Dashboard presentation, not by the software API. */
export type SoftwareCategory = 'container-runtimes' | 'storage-file-sharing'

/** Curated copy and visual identity for one fixed Managed Software kind. */
export interface SoftwareCatalogPresentation {
  category: SoftwareCategory
  description: string
  icon: LucideIcon
  order: number
  capabilities: readonly string[]
  hasSettings: boolean
}

/** A catalog section and its current API-backed entries. */
export interface SoftwareCatalogGroup {
  key: SoftwareCategory
  label: string
  description: string
  entries: SoftwareCatalogEntry[]
}

/** Site-scoped deployment footprint rendered as secondary context on one catalog card. */
export interface SoftwareFootprint {
  installed: number
  changing: number
  failed: number
}

/** URL-owned state lens used by the Software detail deployment inventory. */
export type SoftwareDeploymentLens = 'all' | 'installed' | 'changing' | 'failed'

/** A Server and its current assignment, joined for detail presentation. */
export interface SoftwareDeploymentRow {
  assignment: SoftwareAssignment
  server: Server
}

/** Why a Server cannot be offered for a new installation of one software kind. */
export type SoftwareTargetBlocker =
  | 'existing_assignment'
  | 'locked'
  | 'kubernetes_member'
  | 'mutually_exclusive'
  | 'server_unavailable'

const PRESENTATION: Record<SoftwareKind, SoftwareCatalogPresentation> = {
  'docker-ce': {
    category: 'container-runtimes',
    description: 'Run and manage Docker workloads on deployed Servers, with optional access from the Containers workspace.',
    icon: Container,
    order: 10,
    capabilities: ['Container runtime', 'Optional Docker Engine API', 'Version pinning'],
    hasSettings: true,
  },
  podman: {
    category: 'container-runtimes',
    description: 'Install a daemonless container runtime on deployed Servers without creating a Platform.',
    icon: Box,
    order: 20,
    capabilities: ['Container runtime', 'Daemonless operation', 'Version pinning'],
    hasSettings: false,
  },
  nfs: {
    category: 'storage-file-sharing',
    description: 'Configure shared storage by assigning NFS server and client roles across deployed Servers.',
    icon: FolderSync,
    order: 10,
    capabilities: ['Server and client roles', 'Shared export settings', 'Mount configuration'],
    hasSettings: false,
  },
}

const CATEGORIES: ReadonlyArray<Omit<SoftwareCatalogGroup, 'entries'>> = [
  {
    key: 'container-runtimes',
    label: 'Container runtimes',
    description: 'Host-level runtimes for building and running container workloads.',
  },
  {
    key: 'storage-file-sharing',
    label: 'Storage & file sharing',
    description: 'Software that provides or consumes shared storage across Servers.',
  },
]

/** Returns curated Dashboard presentation for a contract-owned software kind. */
export function softwareCatalogPresentation(kind: SoftwareKind): SoftwareCatalogPresentation {
  return PRESENTATION[kind]
}

/** Groups the small API catalog into stable app-catalog sections. */
export function groupSoftwareCatalog(catalog: readonly SoftwareCatalogEntry[]): SoftwareCatalogGroup[] {
  return CATEGORIES.map((category) => ({
    ...category,
    entries: catalog
      .filter((entry) => PRESENTATION[entry.kind].category === category.key)
      .sort((left, right) => PRESENTATION[left.kind].order - PRESENTATION[right.kind].order),
  })).filter((category) => category.entries.length > 0)
}

/** Joins non-absent assignments to Servers in the active Site scope. */
export function softwareDeploymentRows(
  assignments: readonly SoftwareAssignment[],
  servers: readonly Server[],
): SoftwareDeploymentRow[] {
  const serverIndex = new Map(servers.map((server) => [server.id, server]))
  return assignments
    .flatMap((assignment) => {
      const server = serverIndex.get(assignment.serverId)
      return server ? [{ assignment, server }] : []
    })
    .sort((left, right) => {
      const rank = (state: SoftwareAssignmentState) => {
        if (state === 'pending' || state === 'uninstalling') return 0
        if (state === 'failed') return 1
        if (state === 'installed') return 2
        return 3
      }
      return rank(left.assignment.state) - rank(right.assignment.state) ||
        serverDisplayName(left.server).localeCompare(serverDisplayName(right.server))
    })
}

/** Counts installed, changing, and failed assignments for every catalog kind. */
export function softwareFootprints(
  catalog: readonly SoftwareCatalogEntry[],
  assignments: readonly SoftwareAssignment[],
  servers: readonly Server[],
): Map<SoftwareKind, SoftwareFootprint> {
  const scopedIds = new Set(servers.map((server) => server.id))
  const result = new Map<SoftwareKind, SoftwareFootprint>(
    catalog.map((entry) => [entry.kind, { installed: 0, changing: 0, failed: 0 }]),
  )
  for (const assignment of assignments) {
    if (!scopedIds.has(assignment.serverId)) continue
    const footprint = result.get(assignment.kind)
    if (!footprint) continue
    if (assignment.state === 'installed') footprint.installed += 1
    if (assignment.state === 'pending' || assignment.state === 'uninstalling') footprint.changing += 1
    if (assignment.state === 'failed') footprint.failed += 1
  }
  return result
}

/** Maps assignment lifecycle to the coarser detail filter chosen by the operator. */
export function assignmentMatchesLens(state: SoftwareAssignmentState, lens: SoftwareDeploymentLens): boolean {
  if (lens === 'all') return true
  if (lens === 'changing') return state === 'pending' || state === 'uninstalling'
  return state === lens
}

/** Filters deployment rows by visible Server identity and the selected state lens. */
export function filterSoftwareDeployments(
  rows: readonly SoftwareDeploymentRow[],
  query: string,
  lens: SoftwareDeploymentLens,
): SoftwareDeploymentRow[] {
  const needle = query.trim().toLowerCase()
  return rows.filter(({ assignment, server }) => {
    if (!assignmentMatchesLens(assignment.state, lens)) return false
    if (!needle) return true
    return [serverDisplayName(server), server.fqdn ?? '', server.id, ...server.addresses, ...server.tags]
      .some((value) => value.toLowerCase().includes(needle))
  })
}

/** True while an assignment is being installed or uninstalled and should trigger adaptive refresh. */
export function isSoftwareAssignmentChanging(assignment: SoftwareAssignment): boolean {
  return assignment.state === 'pending' || assignment.state === 'uninstalling'
}

/** Finds the first client-visible reason a Server cannot receive a new assignment. */
export function softwareTargetBlocker(
  server: Server,
  entry: SoftwareCatalogEntry,
  assignments: readonly SoftwareAssignment[],
): SoftwareTargetBlocker | null {
  if (assignments.some((assignment) => assignment.serverId === server.id && assignment.kind === entry.kind)) {
    return 'existing_assignment'
  }
  if (softwareInstallBlocker(server) !== null) return 'server_unavailable'
  if (server.provisioning?.locked) return 'locked'
  if (entry.refusedForKubernetesMembers && server.membership !== null) return 'kubernetes_member'
  if (assignments.some((assignment) =>
    assignment.serverId === server.id &&
    entry.mutuallyExclusiveWith.includes(assignment.kind) &&
    assignment.state !== 'absent'
  )) return 'mutually_exclusive'
  return null
}

/** Operator-facing reason rendered beside a disabled target or software choice. */
export function softwareTargetBlockerLabel(blocker: SoftwareTargetBlocker): string {
  switch (blocker) {
    case 'existing_assignment':
      return 'Already managed; use Reconfigure from its deployment.'
    case 'locked':
      return 'Unlock this Server before installing software.'
    case 'kubernetes_member':
      return 'This software cannot be installed on a Kubernetes member.'
    case 'mutually_exclusive':
      return 'A mutually exclusive software runtime is already assigned.'
    case 'server_unavailable':
      return 'The Server does not currently have an eligible deployed OS.'
  }
}

/** Searches candidate Servers by the identity and Tag fields exposed in the target picker. */
export function softwareTargetMatches(server: Server, query: string): boolean {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  return [serverDisplayName(server), server.fqdn ?? '', server.id, ...server.addresses, ...server.tags]
    .some((value) => value.toLowerCase().includes(needle))
}

/** Compact, truthful assignment configuration for deployment rows and mobile cards. */
export function softwareAssignmentSummary(assignment: SoftwareAssignment): string {
  const parts: string[] = []
  if (assignment.roles.length > 0) parts.push(assignment.roles.join(' + '))
  const spec = assignment.spec ?? {}
  if (typeof spec.version === 'string' && spec.version.trim()) parts.push(`Version ${spec.version}`)
  if (assignment.kind === 'docker-ce' && typeof spec.enableApi === 'boolean') {
    parts.push(spec.enableApi ? 'Docker API enabled' : 'Docker API disabled')
  }
  if (assignment.kind === 'nfs') {
    if (typeof spec.source === 'string' && spec.source.trim()) parts.push(spec.source)
    if (typeof spec.exportPath === 'string' && spec.exportPath.trim()) parts.push(spec.exportPath)
  }
  return parts.join(' · ') || 'Default configuration'
}
