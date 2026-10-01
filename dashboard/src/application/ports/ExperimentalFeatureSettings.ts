/**
 * Dashboard capabilities that are still in development. They are presentation
 * switches, not swallow domain concepts: hiding one never changes API, CLI, or
 * backend behavior, only whether the dashboard shows the surface.
 *
 * - `monitoring`: the Monitoring page, the Server detail Monitoring tab, and the
 *   metrics-owned health shown on shared pages.
 * - `osImageUpload`: uploading a custom OS Image from OS Images.
 * - `deploymentTemplates`: managing and applying Deployment Templates.
 */
export type ExperimentalFeature = 'monitoring' | 'osImageUpload' | 'deploymentTemplates'

/** Every experimental feature, in the order the settings dialog lists them. */
export const EXPERIMENTAL_FEATURES: readonly ExperimentalFeature[] = [
  'monitoring',
  'osImageUpload',
  'deploymentTemplates',
]

/**
 * Where the dashboard learns whether each experimental feature is shown.
 *
 * The composition root binds exactly one implementation per build: development
 * builds persist an adjustable per-browser choice; release builds bind a fixed
 * implementation where every feature is off and nothing can turn it on, so an
 * unfinished surface never reaches operators through a stored preference.
 */
export interface ExperimentalFeatureSettings {
  /** False in release builds: `setEnabled` and `reset` must be no-ops there. */
  readonly adjustable: boolean
  /** Whether the feature is shown now. Never throws, even if storage is unavailable. */
  isEnabled(feature: ExperimentalFeature): boolean
  /** Records the choice for this browser; ignored when not `adjustable`. */
  setEnabled(feature: ExperimentalFeature, enabled: boolean): void
  /** Restores the build's default for every feature; ignored when not `adjustable`. */
  reset(): void
}
