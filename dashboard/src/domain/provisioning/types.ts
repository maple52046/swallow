import type { ProvisioningActionResult } from '@/domain/server/types'

/** Reusable, integration-owned deployment intent. Cloud-init is never readable. */
export interface DeploymentTemplate {
  id: string
  siteId: string
  integrationId: string
  name: string
  description: string
  imageId: string
  ephemeral: boolean
  hasUserData: boolean
  createdAt: string
  updatedAt: string
}

/** Create input is the only template mutation that may carry initial write-only user data. */
export interface CreateDeploymentTemplateInput {
  integrationId: string
  name: string
  description?: string
  imageId: string
  ephemeral: boolean
  userData?: string
}

/** Templates never move integration and cloud-init has dedicated write-only methods. */
export interface UpdateDeploymentTemplateInput {
  name?: string
  description?: string
  imageId?: string
  ephemeral?: boolean
}

/**
 * Cloud-init handling for one deployment request.
 * Inherited content stays behind the API boundary; only 'replace' carries a value.
 */
export type DeploymentUserDataMode = 'inherit' | 'replace' | 'omit'

/**
 * One batch deployment intent. Every Server must use the same provisioner Integration,
 * while settings may inherit from a template or be supplied inline.
 */
export interface DeployServersInput {
  serverIds: string[]
  templateId?: string
  settings?: {
    imageId?: string
    ephemeral?: boolean
  }
  userData?: {
    mode: DeploymentUserDataMode
    value?: string
  }
}

/** A post-preflight provider refusal; accepted peers are not rolled back. */
export interface DeploymentFailure {
  serverId: string
  code: string
  message: string
}

/**
 * One local-state or provider-owned prerequisite that currently blocks a target.
 * The message is safe operator guidance from the active API contract, never secret data.
 */
export interface DeploymentTargetIssue {
  serverId: string
  code: string
  message: string
}

/**
 * Side-effect-free readiness report. A false valid flag is a successful inspection whose
 * complete blocking reasons are carried in issues, rather than a transport failure.
 */
export interface DeploymentTargetPreflightResult {
  valid: boolean
  integrationId: string
  issues: DeploymentTargetIssue[]
}

/** A dispatch summary, not a durable job. Progress is read from each Server projection. */
export interface DeployServersResult {
  requested: number
  accepted: ProvisioningActionResult[]
  failed: DeploymentFailure[]
}

/** Session-safe result shape. It intentionally has nowhere to store cloud-init. */
export interface StoredDeploymentResult extends DeployServersResult {
  serverIds: string[]
  integrationId: string
  savedAt: string
}
