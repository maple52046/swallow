/**
 * The rack address the Build ISO dialog pre-fills for a provisioner: the host of its endpoint
 * (`http://10.170.168.20:5240/MAAS` gives `10.170.168.20`). That host is the MAAS rack when region
 * and rack run on one machine, the common install; the operator edits it otherwise. The endpoint's
 * port is the region API's, never the rack's `ipxe.cfg` port, so it is dropped. Returns an empty
 * string when the endpoint cannot be parsed, leaving the field for the operator to fill.
 */
export function rackAddressFromEndpoint(endpoint: string): string {
  const value = endpoint.trim()
  if (!value) return ''
  try {
    return new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(value) ? value : `http://${value}`).hostname
  } catch {
    // An endpoint that is not a URL has no host to offer; the operator types the rack address.
    return ''
  }
}

/**
 * A suggested Boot ISO name for a provisioner, so the common one-ISO-per-provisioner case needs no
 * typing: the Integration name in lower case with anything outside letters, digits, `-`, `_` and
 * `.` replaced by `-`, plus `-ipxe`, kept within the API's 63-character limit.
 */
export function suggestedBootISOName(integrationName: string): string {
  const base = integrationName
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return `${base || 'boot'}-ipxe`.slice(0, 63)
}
