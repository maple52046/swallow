import type { ReactNode } from 'react'
import type { HealthAxis, HealthState, ServerHealthCounts } from '@/domain/server/types'
import { formatDateTime } from '@/shared/utils/time'
import type { MetricItem } from './ConsolePrimitives'
import type { AxisColor } from './deploymentPhase'

/** How one health state reads on screen: its visible label, its supplementary colour, its meaning. */
export interface HealthPresentation {
  label: string
  color: AxisColor
  /** The sentence a tooltip uses to explain the value; Up and Down add the sample time. */
  meaning: string
}

/**
 * The one health vocabulary of the dashboard (glossary Server Status: "The Dashboard labels the
 * health values Up, Down, Unknown, and N/A"), shared by `HealthBadge`, the Servers health filter and
 * fleet summary, the Overview, and the Monitoring console so no surface words a state differently.
 *
 * Colour only supplements the label: Up green, Down red (the only failure), Unknown the orange
 * warning tone (swallow expects an answer it does not have), N/A neutral gray (swallow does not
 * monitor the Server, which says nothing about the machine).
 */
export const HEALTH_PRESENTATION: Readonly<Record<HealthState, HealthPresentation>> = {
  up: { label: 'Up', color: 'green', meaning: 'Its Node Exporter answered the latest scrape.' },
  down: { label: 'Down', color: 'red', meaning: 'Its Node Exporter did not answer the latest scrape.' },
  unknown: {
    label: 'Unknown',
    color: 'orange',
    meaning: 'Swallow expects to monitor this Server but has no answer from its Node Exporter.',
  },
  not_applicable: {
    label: 'N/A',
    color: 'gray',
    meaning: "Swallow does not monitor this Server (not deployed, locked, or its Site's automation is off).",
  },
}

/**
 * The presentation of `state`. A value a newer API might add is display-only, so it is shown as
 * sent in the neutral tone rather than as a failure or as healthy.
 */
export function healthPresentation(state: HealthState): HealthPresentation {
  return HEALTH_PRESENTATION[state] ?? {
    label: String(state),
    color: 'gray',
    meaning: 'Swallow reported a health state this dashboard does not recognise.',
  }
}

/** The tooltip text for a Server's health: the state's meaning, plus the sample time when one exists. */
export function healthTooltip(axis: HealthAxis): string {
  const { meaning } = healthPresentation(axis.state)
  return axis.observedAt ? `${meaning} Observed ${formatDateTime(axis.observedAt)}.` : meaning
}

/** Servers swallow monitors: every state but N/A. The denominator of "up of monitored". */
export function monitoredServerCount(counts: ServerHealthCounts): number {
  return counts.up + counts.down + counts.unknown
}

/**
 * The shared Fleet health metric card of the Overview (from the aggregate's `inventory.health`) and
 * the Monitoring console (counted from its loaded Servers). The value is Up of the monitored Servers;
 * the caption names the other three counts so the four always add up to the scope. Down raises the
 * critical tone and Unknown the warning tone; with no monitored Server the card stays neutral instead
 * of claiming success.
 */
export function fleetHealthMetric(counts: ServerHealthCounts, icon: ReactNode): MetricItem {
  const monitored = monitoredServerCount(counts)
  let tone: MetricItem['tone'] = 'neutral'
  if (counts.down > 0) tone = 'critical'
  else if (counts.unknown > 0) tone = 'warning'
  else if (monitored > 0) tone = 'success'
  // Non-breaking spaces keep each count with its label when a narrow card wraps the caption.
  return {
    label: 'Fleet health',
    value: `${counts.up} / ${monitored}`,
    detail: `${counts.down}\u00a0down · ${counts.unknown}\u00a0unknown · ${counts.notApplicable}\u00a0N/A`,
    tone,
    icon,
  }
}
