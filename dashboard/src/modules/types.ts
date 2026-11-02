import type { ComponentType, ReactNode } from 'react'
import type { DeployPlatformInput, Platform, PlatformType } from '@/domain/platform/types'
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
  /** Steps after OS preparation, for example Kubernetes networking. */
  extraSteps: (selected: Server[]) => DeployStep[]
  /** Review cards before the shared Features and OS cards; `shared` holds what the shell owns. */
  reviewGroups: (shared: { name: string; site: string; preparationSummary: string }) => DeployReviewGroup[]
  /** Review cards after the OS card. */
  reviewGroupsAfterOS: (selected: Server[]) => DeployReviewGroup[]
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
  deploy: PlatformDeployUI
}

/** The manifest of an Accelerator module (AMD): the Platform features it brings. */
export interface AcceleratorModuleUI {
  kind: 'accelerator'
  id: string
  label: string
  platformSections: PlatformSectionContribution[]
  deployFeatures: PlatformDeployFeatureContribution[]
}
