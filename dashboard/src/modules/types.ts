import type { LucideIcon } from 'lucide-react'
import type { ComponentType, ReactNode } from 'react'
import type { DeployPlatformInput, KubernetesTopology, Platform, PlatformSpec, PlatformType } from '@/domain/platform/types'
import type { Server } from '@/domain/server/types'

/**
 * The UI contract between the dashboard's shared shells and its first-party modules (architecture
 * spec "Module UI"). A module lives in `src/modules/<kind>/<id>/` and exports one manifest; the
 * shells find it through `src/modules/registry.ts` by module id and never branch on a platform or
 * vendor name. Manifests are static and render-time only: they hold components, not state.
 */

/** What the Platform detail shell hands the module that owns a Platform's type. */
export interface PlatformAccessProps {
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
  /** Members loaded only while a native route is active; Kubernetes explorer routes ignore them. */
  members: Server[]
  /** Slurm submission hosts, which are not membership rows. */
  loginNodes: Server[]
  /** Opens a Server's detail page by id. */
  onOpenServer: (serverId: string) => void
}

/** A native workspace route a Platform module adds to the shared routes. */
export interface PlatformModuleTab {
  /** Stable tab value and route segment (`/platforms/:id/<value>`); the router must declare it. */
  value: string
  /** Visible tab text and the tab's accessible name. */
  label: string
  /** Decorative glyph before the label, in the style of the shell's own tabs. */
  icon: LucideIcon
  /** Whether the tab applies to this Platform, so a tab never makes a call that can only fail. */
  available: (platform: Platform) => boolean
  /** Loads Platform member and login-node projections only while this route is active. */
  requiresMembers?: boolean
  Content: ComponentType<PlatformTabProps>
}

/**
 * One labelled fact about a Platform in the shell's Platform details card (a `<dt>`/`<dd>` pair).
 * `label` is unique within the module's facts and names the fact for assistive technology; `value`
 * is short text, worded so it reads correctly without colour or an icon.
 */
export interface PlatformFact {
  label: string
  value: ReactNode
}

/** What a module-owned Configuration section receives from the shared workspace. */
export interface PlatformSectionProps {
  platform: Platform
  members: Server[]
  /** Reloads the Platform after the section changed it. */
  onChanged: () => void
  /** Opens a Workflow's detail page by id. */
  onOpenWorkflow: (workflowId: string) => void
}

/** A section another module adds to Configuration for one Platform type. */
export interface PlatformSectionContribution {
  platformType: PlatformType
  Section: ComponentType<PlatformSectionProps>
}

/** One labelled value in a deploy review card. */
export interface DeployReviewItem {
  label: string
  value: ReactNode
}

/** A deploy review card and the wizard step its Edit button returns to. */
export interface DeployReviewGroup {
  title: string
  stepId: string
  items: DeployReviewItem[]
}

/** A wizard step a module adds; the shell frames `content` in a section titled `title`. */
export interface DeployStep {
  id: string
  name: string
  title: string
  canProceed: boolean
  /** Why the step cannot proceed, read to the user next to the disabled Next button. */
  blockedReason?: string
  content: ReactNode
}

/**
 * The static IPv4 address, by Server id, that the shell's OS preparation gives each Server the
 * deploy provisions while the effective network mode (a selected Deployment Template's, otherwise
 * the user's choice) is `static`: what the request submits as that Server's
 * `machinePreparation.network.assignments[].ipAddress`. A Server is absent when it is reused, when
 * the mode is automatic or DHCP (the provisioner picks its address during the deploy), or while the
 * OS networking step holds no valid IPv4 address for it. A module reads it to show an address the
 * deploy will use before the deploy, for example the Kubernetes API endpoint.
 */
export type DeployStaticAddresses = Readonly<Record<string, string>>

/** What the deploy wizard shell hands a Platform module's deploy form. */
export interface PlatformDeployContext {
  platformName: string
  /** The Site's deployable candidates: Ready or Deployed and unlocked. */
  servers: Server[]
  /** Why another Platform holds a Server, for example "assigned to lab-k0s"; null when it is free. */
  heldBy: (server: Server) => string | null
}

/** What a deploy feature another module contributes (for example the AMD GPU Operator) reads. */
export interface PlatformDeployFeatureContext {
  servers: Server[]
  /** The selected Servers that run workloads, whose hardware a feature may need. */
  workloadServerIds: string[]
  /** The platform version the feature must be compatible with, null when not chosen yet. */
  platformVersion: string | null
}

/** A deploy feature another module contributes to a Platform type's Features step. */
export interface PlatformDeployFeature {
  content: ReactNode
  valid: boolean
  blockedReason?: string
  reviewItems: DeployReviewItem[]
  /** The request fields the feature sets, merged into the deploy request. */
  input: Partial<DeployPlatformInput>
  /** Clears the feature's choices, for example when the Site changes. */
  reset: () => void
}

/** A deploy feature contribution; `useFeature` is a hook the shell calls on every render. */
export interface PlatformDeployFeatureContribution {
  platformType: PlatformType
  useFeature: (context: PlatformDeployFeatureContext) => PlatformDeployFeature
}

/**
 * A Platform module's deploy form: its state, the type-specific parts of the shared wizard steps,
 * the steps only it has, its review cards, and its fields of the deploy request. The shell owns the
 * Site, name, candidate list, OS preparation, clock check, and submission.
 */
export interface PlatformDeployForm {
  /** Fields after Site and name in the identity grid. */
  basicsFields: ReactNode
  /** Content after the identity grid: advanced settings and notices. */
  basicsExtras: ReactNode
  basicsValid: boolean
  basicsBlockedReason: string
  machines: {
    name: string
    title: string
    /** Topology choice, role counts, and guidance above the candidate list. */
    intro: ReactNode
    /** Column headers of the module's role controls, after the shared columns. */
    roleHeaders: string[]
    /** One control per role header for a candidate; `surface` keeps desktop and mobile ids apart. */
    roleCells: (server: Server, surface: 'table' | 'card') => ReactNode[]
    /** The module's own eligibility for a candidate (Slurm minimum resources); undefined when none. */
    eligibility?: (server: Server) => { eligible: boolean; explanation: string } | undefined
    valid: boolean
    blockedReason: string
  }
  /** The selected Servers, in the order the request names them. */
  selectedServerIds: string[]
  featureContext: PlatformDeployFeatureContext
  features: { title: string; content: ReactNode; valid: boolean; blockedReason?: string }
  /**
   * Steps after OS preparation, for example Kubernetes networking. They come after the OS step, so
   * `staticAddresses` already holds what that step assigns.
   */
  extraSteps: (selected: Server[], staticAddresses: DeployStaticAddresses) => DeployStep[]
  /** Review cards before the shared Features and OS cards; `shared` holds what the shell owns. */
  reviewGroups: (shared: { name: string; site: string; preparationSummary: string }) => DeployReviewGroup[]
  /** Review cards after the OS card; `staticAddresses` as for `extraSteps`. */
  reviewGroupsAfterOS: (selected: Server[], staticAddresses: DeployStaticAddresses) => DeployReviewGroup[]
  /** A warning about the resolved OS deploy mode, shown at OS installation and again at review. */
  deployModeNotice: (ephemeral: boolean) => ReactNode
  /** The type's fields of the deploy request: settings and the member assignments. */
  input: () => Partial<DeployPlatformInput>
  /** Clears the form's Site-specific choices when the Site changes. */
  reset: () => void
  /** Reloads facts the form owns after the backend refuses a submission, for example a policy. */
  refresh?: () => void
}

/** How a Platform module joins the deploy wizard. */
export interface PlatformDeployUI {
  icon: ComponentType<{ size?: number }>
  namePlaceholder: string
  /** The deploy form hook; the shell calls it on every render of its type's wizard. */
  useForm: (context: PlatformDeployContext) => PlatformDeployForm
}

/** One monitoring component rendered by the module-neutral Monitoring route. */
export interface PlatformMonitoringEntry {
  name: string
  state: string
  /** Human-readable state; the raw state remains available for colour semantics. */
  statusLabel?: string
  detail: string
  workflowId: string | null
  /** A Server that hosts this component, when the module records one. */
  serverId?: string
  /** Diagnostic warning that remains attached to this component, not the whole Platform. */
  warning?: { title: string; message: string }
  /** A per-Server breakdown of one part of this component, for example its hardware counters. */
  serverStates?: PlatformMonitoringServerStates
}

/**
 * A part of a monitoring component that each Server reports on its own (Omnistat's hardware
 * counters on each compute node). The shared Monitoring route renders it under the component with a
 * link to each Server; `summary` is shown even while `servers` is empty (for example before the
 * install recorded them). `state` drives only the badge colour; `statusLabel` and `reason` carry the
 * meaning in text.
 */
export interface PlatformMonitoringServerStates {
  title: string
  summary: string
  servers: ReadonlyArray<{ serverId: string; state: string; statusLabel: string; reason?: string }>
}

/** Monitoring entries another module contributes to one Platform type. */
export interface PlatformMonitoringContribution {
  platformType: PlatformType
  /** Pure presentation projection from the canonical Platform record. */
  entries: (platform: Platform) => PlatformMonitoringEntry[]
}

/** The manifest of a Platform module (Kubernetes, Slurm). */
export interface PlatformModuleUI {
  kind: 'platform'
  /** The Platform type the module owns; equal to the api-server module id. */
  id: PlatformType
  label: string
  /** Monoline identity used by the shared workspace hero and deployment catalog. */
  icon: ComponentType<{ size?: number }>
  capabilities: readonly string[]
  /** Chakra colour palette of the type badge; the badge text carries the type itself. */
  badgePalette: string
  /** Compact type-specific access contribution used by the shared summary-first Overview. */
  Access: ComponentType<PlatformAccessProps>
  /** Native routes owned by this Platform module. */
  tabs: PlatformModuleTab[]
  /** Type-specific sections appended to the shared Configuration route. */
  configurationSections?: ComponentType<PlatformSectionProps>[]
  /** Type-specific component state exposed by the shared Monitoring route. */
  monitoringEntries?: (platform: Platform) => PlatformMonitoringEntry[]
  deploy: PlatformDeployUI
  /** Optional global settings surface owned by this Platform module. */
  Settings?: ComponentType
  /**
   * The type's own facts for the Platform details card, which the shell shows in a group titled
   * with `label`, after its type-neutral General and Deployment facts (for example the k0s version
   * and CIDRs, or the Slurm cluster name and container runtime). They are read from the Platform
   * record alone, the applied spec before the desired one (`platformCurrentSpec`), so they never
   * fetch. A plain function, not a hook: the shell calls it while rendering, so it must be pure.
   * An empty list, or no function, leaves the group out.
   */
  facts?: (platform: Platform) => PlatformFact[]
  /**
   * The topology a spec's member roles describe, in the type's own role vocabulary, for a Platform
   * that has no deployment record yet (a `planned` Platform before its install runs). Pure, like
   * `facts`; undefined when the roles form no supported shape.
   */
  specTopology?: (spec: PlatformSpec) => KubernetesTopology | undefined
}

/** The manifest of an Accelerator module (AMD): the Platform features it brings. */
export interface AcceleratorModuleUI {
  kind: 'accelerator'
  id: string
  label: string
  configurationSections: PlatformSectionContribution[]
  monitoringEntries: PlatformMonitoringContribution[]
  deployFeatures: PlatformDeployFeatureContribution[]
}
