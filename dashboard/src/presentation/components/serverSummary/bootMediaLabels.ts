import type { BootMediaApplier, BootMediaMethod, LibvirtSupport, RedfishSupport, ServerBootMedia } from '@/domain/server/types'

/**
 * The method a Boot Media read is presented under: the API's `method`, or `libvirt` when it has
 * none but a libvirt probe exists — a virtual machine whose virsh address names no swallow Server
 * (`no_hypervisor`) is still explained through its Hypervisor rather than as a Server without a BMC.
 */
export function shownBootMediaMethod(media: Pick<ServerBootMedia, 'method' | 'libvirt'>): BootMediaMethod | null {
  return media.method ?? (media.libvirt ? 'libvirt' : null)
}

/**
 * What a Boot Media method drives, for sentences such as "Check BMC" or "the hypervisor refused":
 * the Server's BMC for `redfish`, its Hypervisor for `libvirt` (decision 055). Anything else reads
 * as the BMC, the only method that existed before libvirt.
 */
export function bootMediaTargetLabel(method: BootMediaMethod | string | null | undefined): 'BMC' | 'hypervisor' {
  return method === 'libvirt' ? 'hypervisor' : 'BMC'
}

/**
 * Operator wording for a libvirt probe result. The domain value stays the API's `supported` /
 * `unsupported` / `unreachable` / `no_hypervisor`; an unknown future value reads as not supported.
 */
export function libvirtSupportLabel(support: LibvirtSupport | string): string {
  switch (support) {
    case 'supported':
      return 'Hypervisor ready for Boot Media'
    case 'unsupported':
      return 'Domain not found on the hypervisor'
    case 'unreachable':
      return 'Hypervisor unreachable'
    case 'no_hypervisor':
      return 'No swallow hypervisor'
    default:
      return 'Not supported'
  }
}

/** The StatusBadge status family for a libvirt probe result; its text comes from libvirtSupportLabel. */
export function libvirtSupportStatus(support: LibvirtSupport | string): string {
  if (support === 'supported') return 'ok'
  if (support === 'unsupported') return 'warning'
  if (support === 'unreachable') return 'unreachable'
  return 'unknown'
}

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
  if (applier === 'ensure') return 'before an inspection or OS deployment'
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
