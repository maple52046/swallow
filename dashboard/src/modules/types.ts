import type { ComponentType } from 'react'
import type { Platform, PlatformType } from '@/domain/platform/types'
import type { Server } from '@/domain/server/types'

/**
 * The UI contract between the dashboard's shared shells and its first-party modules (architecture
 * spec "Module UI"). A module lives in `src/modules/<kind>/<id>/` and exports one manifest; the
 * shells find it through `src/modules/registry.ts` by module id and never branch on a platform or
 * vendor name. Manifests are static and render-time only: they hold components, not state.
 */

/** What the Platform detail shell hands the module that owns a Platform's type. */
export interface PlatformOverviewProps {
  platform: Platform
  /** Servers that report membership in the Platform. */
  members: Server[]
  /** Servers the deployment intent names as login hosts (Slurm); empty for other types. */
  loginNodes: Server[]
  /** Opens a Server's detail page; the shell owns navigation and Site scope. */
  onSelectServer: (server: Server) => void
}

/** What a module tab receives from the Platform detail shell. */
export interface PlatformTabProps {
  platform: Platform
  /** Opens a Server's detail page by id. */
  onOpenServer: (serverId: string) => void
}

/** A tab a Platform module adds next to the shell's Overview and Activity tabs. */
export interface PlatformModuleTab {
  /** Stable tab value; also the tab's accessible identity within the tab list. */
  value: string
  label: string
  /** Whether the tab applies to this Platform, so a tab never makes a call that can only fail. */
  available: (platform: Platform) => boolean
  Content: ComponentType<PlatformTabProps>
}

/** What a section another module contributes to a Platform's Overview receives. */
export interface PlatformSectionProps {
  platform: Platform
  members: Server[]
  /** Reloads the Platform after the section changed it. */
  onChanged: () => void
  /** Opens a Workflow's detail page by id. */
  onOpenWorkflow: (workflowId: string) => void
}

/** A section a module adds to the Overview of Platforms of one type, for example a GPU feature. */
export interface PlatformSectionContribution {
  platformType: PlatformType
  Section: ComponentType<PlatformSectionProps>
}

/** The manifest of a Platform module (Kubernetes, Slurm). */
export interface PlatformModuleUI {
  kind: 'platform'
  /** The Platform type the module owns; equal to the api-server module id. */
  id: PlatformType
  label: string
  /** Chakra colour palette of the type badge; the badge text carries the type itself. */
  badgePalette: string
  /** The type's Overview body: its summary, members, and live reads. */
  Overview: ComponentType<PlatformOverviewProps>
  tabs: PlatformModuleTab[]
}

/** The manifest of an Accelerator module (AMD): the Platform features it brings. */
export interface AcceleratorModuleUI {
  kind: 'accelerator'
  id: string
  label: string
  platformSections: PlatformSectionContribution[]
}
