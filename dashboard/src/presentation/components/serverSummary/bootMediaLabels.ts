import type { BootMediaApplier, RedfishSupport } from '@/domain/server/types'

/**
 * Operator wording for a Redfish probe result (glossary BMC). The domain value stays the API's
 * `supported` / `unsupported` / `unreachable` / `no_bmc`; an unknown future value reads as not
 * supported rather than failing open.
 */
export function redfishSupportLabel(support: RedfishSupport | string): string {
  switch (support) {
    case 'supported':
      return 'Redfish Boot Media supported'
    case 'unsupported':
      return 'Redfish without Boot Media support'
    case 'unreachable':
      return 'Redfish unreachable'
    case 'no_bmc':
      return 'No BMC'
    default:
      return 'Not supported'
  }
}

/**
 * The shared StatusBadge status family for a probe result, so its colour follows the dashboard's
 * status palette; the badge text always comes from redfishSupportLabel.
 */
export function redfishSupportStatus(support: RedfishSupport | string): string {
  if (support === 'supported') return 'ok'
  if (support === 'unsupported') return 'warning'
  if (support === 'unreachable') return 'unreachable'
  return 'unknown'
}

/** Who last applied Boot Media, as a phrase completing "Last applied …". */
export function bootMediaApplierLabel(applier: BootMediaApplier | string | undefined): string {
  if (applier === 'preflight') return 'when it was enabled'
  if (applier === 'ensure') return 'before an OS deployment'
  return ''
}

/**
 * How persistently the BMC took Boot Media. `Once` is not a degraded state: every OS deployment
 * re-applies Boot Media before it powers the Server on.
 */
export function bootOverrideLabel(mode: string | undefined): string {
  if (mode === 'Continuous') return 'every boot'
  if (mode === 'Once') return 'the next boot (re-applied before each OS deployment)'
  return ''
}
