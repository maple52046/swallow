import type { PowerControl, PowerFamily } from '@/domain/server/types'

/**
 * Operator wording for a provisioner power driver (glossary Power Configuration). The domain value
 * stays the provisioner's driver name, which is what the API reads and writes; an unknown driver is
 * shown verbatim because it is still the provisioner's own driver.
 */
export function powerDriverLabel(driver: string): string {
  switch (driver) {
    case '':
      return 'Not configured'
    case 'ipmi':
      return 'IPMI'
    case 'redfish':
      return 'Redfish'
    case 'virsh':
      return 'virsh (libvirt)'
    case 'manual':
      return 'Manual'
    default:
      return driver
  }
}

/** What a driver family reaches, for the Driver row and the driver choices of the edit form. */
export function powerFamilyLabel(family: PowerFamily | null): string {
  if (family === 'bmc') return 'BMC'
  if (family === 'virsh') return 'virtual machine'
  return ''
}

/**
 * Short badge text for a power control value. Status is carried by this text, never by the badge
 * colour alone; `unknown` reads as unknown rather than as working control.
 */
export function powerControlLabel(control: PowerControl): string {
  switch (control) {
    case 'automatic':
      return 'Automatic'
    case 'manual':
      return 'Manual'
    case 'none':
      return 'No power control'
    default:
      return 'Unknown'
  }
}

/** What a power control value means for the Server, shown beside its badge. */
export function powerControlDetail(control: PowerControl): string {
  switch (control) {
    case 'automatic':
      return 'The provisioner switches and reads power itself.'
    case 'manual':
      return 'A person switches power, and its state cannot be read, so swallow cannot see enrollment end.'
    case 'none':
      return 'The provisioner cannot power this Server, so it cannot inspect or deploy it.'
    default:
      return 'This control value is not known to the dashboard.'
  }
}

/** The shared StatusBadge status family for a control value; the text comes from powerControlLabel. */
export function powerControlStatus(control: PowerControl): string {
  if (control === 'automatic') return 'ok'
  if (control === 'manual') return 'warning'
  if (control === 'none') return 'failed'
  return 'unknown'
}
