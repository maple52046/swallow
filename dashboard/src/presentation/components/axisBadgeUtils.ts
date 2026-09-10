import { Power, PowerOff, type LucideIcon } from 'lucide-react'
import type { ProvisioningAxis } from '@/domain/server/types'

/**
 * Icon, accessible label, and colour modifier for each provisioner power state, shared by the
 * display badge (`PowerBadge`) and any interactive power control so the mapping lives in one
 * place.
 *
 * Only `on` is tinted (success green) as a healthy running-machine cue; `error` uses the
 * danger colour and the rest stay neutral. The icon shape plus the label are what carry the
 * state, so colour never becomes the sole signal. `modifier` selects the status-colour class
 * defined in `src/index.css`; an empty string means the neutral default.
 *
 * Kept in this `.ts` module (not in `AxisBadge.tsx`) so the badge file only exports React
 * components and Fast Refresh keeps working.
 */
export const POWER_PRESENTATION: Record<
  ProvisioningAxis['powerState'],
  { Icon: LucideIcon; label: string; modifier: string }
> = {
  on: { Icon: Power, label: 'Powered on', modifier: 'sw-power-on' },
  off: { Icon: PowerOff, label: 'Powered off', modifier: '' },
  error: { Icon: Power, label: 'Power state error', modifier: 'sw-power-error' },
  unknown: { Icon: Power, label: 'Power state unknown', modifier: '' },
}

/**
 * Human-readable power-state label, shared by `PowerBadge` and any interactive control (e.g. a
 * power-action button/dialog) so the wording stays in one place. `null` means the provisioner
 * reported no power fact at all, which reads differently from a real "off".
 */
export function powerStateLabel(powerState: ProvisioningAxis['powerState'] | null): string {
  return powerState === null ? 'No power state reported' : POWER_PRESENTATION[powerState].label
}
