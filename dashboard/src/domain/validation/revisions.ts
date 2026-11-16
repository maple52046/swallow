import { suiteStarterLevel } from './access'
import type { ValidationStarterLevel, ValidationSuite, ValidationSuiteRevision } from './types'

/**
 * Comparing two revisions of a user suite, for the review a User passes before running a suite
 * that changed since their last run (validation contract, Revisions). Only the dashboard compares;
 * the API pins what runs through `suiteRevision`.
 */

/** The fields of a user suite revision that decide how and where its script runs, and what it may disturb. */
export type SuiteRevisionContent = Pick<ValidationSuiteRevision, 'revision' | 'script' | 'sudo' | 'starterLevel' | 'timeoutSeconds' | 'impact'>

/**
 * The current revision of a user suite as the catalog returns it, so a review compares it without
 * another read; null for a module suite (or a user suite missing a field the contract requires).
 */
export function currentSuiteRevision(suite: ValidationSuite): SuiteRevisionContent | null {
  if (suite.source !== 'user' || suite.revision === null || suite.script === null || suite.timeoutSeconds === null) return null
  return {
    revision: suite.revision,
    script: suite.script,
    sudo: suite.sudo === true,
    starterLevel: suiteStarterLevel(suite),
    timeoutSeconds: suite.timeoutSeconds,
    impact: suite.impact,
  }
}

/** One changed setting between two revisions, with both values. */
export type SuiteSettingChange =
  | { field: 'sudo'; from: boolean; to: boolean }
  | { field: 'starterLevel'; from: ValidationStarterLevel; to: ValidationStarterLevel }
  | { field: 'timeoutSeconds'; from: number; to: number }
  | { field: 'impact'; from: ValidationSuiteRevision['impact']; to: ValidationSuiteRevision['impact'] }

/** The settings (all but the script, which is diffed) that differ from `previous` to `current`, in a fixed order. */
export function suiteSettingChanges(previous: SuiteRevisionContent, current: SuiteRevisionContent): SuiteSettingChange[] {
  const changes: SuiteSettingChange[] = []
  if (previous.sudo !== current.sudo) changes.push({ field: 'sudo', from: previous.sudo, to: current.sudo })
  if (previous.starterLevel !== current.starterLevel) changes.push({ field: 'starterLevel', from: previous.starterLevel, to: current.starterLevel })
  if (previous.timeoutSeconds !== current.timeoutSeconds) changes.push({ field: 'timeoutSeconds', from: previous.timeoutSeconds, to: current.timeoutSeconds })
  if (previous.impact !== current.impact) changes.push({ field: 'impact', from: previous.impact, to: current.impact })
  return changes
}
