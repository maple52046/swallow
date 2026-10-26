/**
 * GPU node settings an operator may set on a Platform deploy (platforms.md "GPU driver options"
 * and "GPU GRES"; decision 063). Neither names a GPU model or a virtualization mode: Swallow derives
 * the driver options a node's GPU topology needs and the GRES type from the GPU architecture, and
 * these settings only add or override. The grammar mirrors the API's, so the form can explain a
 * mistake before submission; the API re-validates and answers `validation_error` regardless.
 */

/** At most this many module options per Platform. */
export const MAX_GPU_DRIVER_MODULE_OPTIONS = 16

const OPTION_NAME = /^[a-z0-9_]{1,64}$/
const OPTION_VALUE = /^[A-Za-z0-9_.,:-]{1,128}$/
const GRES_TYPE = /^[A-Za-z][A-Za-z0-9_]{0,63}$/

/** Whether a Slurm GRES type name is acceptable (letters, digits, underscores; a letter first). */
export function isValidGPUGresType(name: string): boolean {
  return GRES_TYPE.test(name)
}

/** The result of reading the module-options text field. */
export interface ParsedGPUDriverModuleOptions {
  /** Valid options keyed by name; empty when the field is empty. */
  options: Record<string, string>
  /** One operator-facing message per rejected line, in line order. */
  errors: string[]
}

/**
 * Reads amdgpu module options written one `name=value` per line. Blank lines and lines starting
 * with `#` are ignored. Every other line must match the API grammar; a repeated name is an error
 * rather than a silent override, and more than {@link MAX_GPU_DRIVER_MODULE_OPTIONS} is an error.
 */
export function parseGPUDriverModuleOptions(text: string): ParsedGPUDriverModuleOptions {
  const options: Record<string, string> = {}
  const errors: string[] = []
  text.split('\n').forEach((raw, index) => {
    const line = raw.trim()
    if (!line || line.startsWith('#')) return
    const separator = line.indexOf('=')
    const name = separator > 0 ? line.slice(0, separator).trim() : ''
    const value = separator > 0 ? line.slice(separator + 1).trim() : ''
    if (!OPTION_NAME.test(name)) {
      errors.push(`Line ${index + 1}: use name=value with a lower-case name (letters, digits, underscores).`)
    } else if (!OPTION_VALUE.test(value)) {
      errors.push(`Line ${index + 1}: the value of ${name} may use only letters, digits, and _.,:-`)
    } else if (name in options) {
      errors.push(`Line ${index + 1}: ${name} is set twice.`)
    } else {
      options[name] = value
    }
  })
  if (Object.keys(options).length > MAX_GPU_DRIVER_MODULE_OPTIONS) {
    errors.push(`Set at most ${MAX_GPU_DRIVER_MODULE_OPTIONS} options.`)
  }
  return { options, errors }
}

/** Display text for module options, for example `pcie_p2p=0, vm_fragment_size=9`; '' when none. */
export function formatGPUDriverModuleOptions(options: Record<string, string> | null | undefined): string {
  return Object.entries(options ?? {})
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([name, value]) => `${name}=${value}`)
    .join(', ')
}
