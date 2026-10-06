/**
 * Builds the Redfish commands an operator runs from a workstation to mount a Boot ISO on a
 * Server's BMC and boot it once (Add servers, external-DHCP path; decision 053). swallow cannot do
 * this itself yet: the machine is not in the provisioner, so swallow knows neither its BMC nor
 * its credentials.
 *
 * The commands use the DMTF standard actions (`VirtualMedia.InsertMedia`, a one-time
 * `BootSourceOverrideTarget: Cd`, `ComputerSystem.Reset`). Resource IDs differ by vendor, so the
 * commands default to the common `Systems/1` and `Managers/1/VirtualMedia/CD1` and show how to
 * list the real ones. `curl -u <user>` prompts for the password, so none is ever embedded. Inputs
 * are shell-quoted; empty ones fall back to visible placeholders.
 */
export function redfishBootISOCommands({ bmcAddress, bmcUser, isoUrl }: { bmcAddress: string; bmcUser: string; isoUrl: string }): string {
  const address = bmcAddress.trim().replace(/\/+$/, '')
  const bmc = address ? (/^https?:\/\//i.test(address) ? address : `https://${address}`) : 'https://<bmc-address>'
  const user = shellQuote(bmcUser.trim() || 'admin')
  const insert = JSON.stringify({ Image: isoUrl, Inserted: true, WriteProtected: true })
  return [
    `BMC=${shellQuote(bmc)}`,
    'SYSTEM="$BMC/redfish/v1/Systems/1"',
    'CD="$BMC/redfish/v1/Managers/1/VirtualMedia/CD1"',
    '# IDs differ by vendor; list them with:',
    `#   curl -sk -u ${user} "$BMC/redfish/v1/Systems"`,
    `#   curl -sk -u ${user} "$BMC/redfish/v1/Managers/1/VirtualMedia"`,
    '',
    '# Mount the Boot ISO',
    `curl -sk -u ${user} -X POST -H 'Content-Type: application/json' \\`,
    `  -d ${shellQuote(insert)} "$CD/Actions/VirtualMedia.InsertMedia"`,
    '',
    '# Boot from it once',
    `curl -sk -u ${user} -X PATCH -H 'Content-Type: application/json' \\`,
    `  -d '{"Boot":{"BootSourceOverrideEnabled":"Once","BootSourceOverrideTarget":"Cd"}}' "$SYSTEM"`,
    '',
    '# Power on (ResetType ForceRestart if it is already on)',
    `curl -sk -u ${user} -X POST -H 'Content-Type: application/json' \\`,
    `  -d '{"ResetType":"On"}' "$SYSTEM/Actions/ComputerSystem.Reset"`,
  ].join('\n')
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", "'\\''")}'`
}
