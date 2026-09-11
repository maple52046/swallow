import { Stack } from '@chakra-ui/react'
import type { ReleaseOptionsValue } from './releaseOptions'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'

interface ReleaseOptionsFieldsProps {
  /** Namespaces the control ids so two instances never collide when both are mounted. */
  idPrefix: string
  value: ReleaseOptionsValue
  onChange: (value: ReleaseOptionsValue) => void
  /** When false the provider exposes no configurable disk erasure and the group is hidden. */
  supportsReleaseOptions?: boolean
  /** When false static-IP cleanup is not offered and the unbind control is disabled. */
  supportsNetworkConfiguration?: boolean
}

/** Human explanation of the current erase choice; status is never conveyed by colour alone. */
function eraseExplanation(value: ReleaseOptionsValue): string {
  if (value.erase && value.secureErase && value.quickErase) {
    return 'Secure erase is preferred. MAAS will use quick erase only when secure erase is unavailable.'
  }
  if (value.erase && value.secureErase) {
    return 'MAAS will request hardware secure erase. A disk without secure erase support can cause the release to fail.'
  }
  if (value.erase && value.quickErase) {
    return 'Quick erase only wipes the beginning and end of each disk. It is faster, but it is not a secure wipe.'
  }
  if (value.erase) {
    return 'Full erase writes zeroes to every disk and can take a long time.'
  }
  return 'Disk erasure is off. The provisioner will release the machine without wiping its disks.'
}

/**
 * Renders the disk-erasure and static-IP cleanup choices for a release.
 *
 * Fully controlled: the caller owns the value and submits it, so the same UI serves
 * both the Server Release dialog and the platform uninstall dialog. Turning erase
 * off also clears the erase modes so a caller cannot submit an inconsistent
 * combination. Each choice is mirrored by an inline explanation, so the effect is
 * always described in text.
 */
export function ReleaseOptionsFields({
  idPrefix,
  value,
  onChange,
  supportsReleaseOptions = true,
  supportsNetworkConfiguration = true,
}: ReleaseOptionsFieldsProps) {
  const changeErase = (checked: boolean) =>
    onChange(
      checked
        ? { ...value, erase: true }
        : { ...value, erase: false, secureErase: false, quickErase: false },
    )

  return (
    <Stack gap="4">
      {supportsReleaseOptions ? (
        <>
          <Checkbox id={`${idPrefix}-erase-disks`} checked={value.erase} onCheckedChange={changeErase}>
            Erase disks before release
          </Checkbox>
          <Stack gap="3" ps="6">
            <Checkbox
              id={`${idPrefix}-secure-erase`}
              checked={value.secureErase}
              disabled={!value.erase}
              onCheckedChange={(checked) => onChange({ ...value, secureErase: checked })}
            >
              Use secure erase when supported
            </Checkbox>
            <Checkbox
              id={`${idPrefix}-quick-erase`}
              checked={value.quickErase}
              disabled={!value.erase}
              onCheckedChange={(checked) => onChange({ ...value, quickErase: checked })}
            >
              {value.secureErase ? 'Use quick erase if secure erase is unavailable' : 'Use quick erase'}
            </Checkbox>
          </Stack>
          <Alert
            status={value.erase ? 'warning' : 'info'}
            title={value.erase ? 'Disk erasure enabled' : 'Disk contents will be retained'}
          >
            {eraseExplanation(value)}
          </Alert>
        </>
      ) : (
        <Alert status="info" title="Disk erasure options unavailable">
          This provisioner can release the machine but does not expose configurable disk erasure through Swallow.
        </Alert>
      )}
      <Checkbox
        id={`${idPrefix}-unbind-static-ips`}
        checked={value.unbindStaticIPs}
        disabled={!supportsNetworkConfiguration}
        onCheckedChange={(checked) => onChange({ ...value, unbindStaticIPs: checked })}
      >
        Remove static IP bindings after release
      </Checkbox>
      <Alert
        status={value.unbindStaticIPs ? 'warning' : 'info'}
        title={value.unbindStaticIPs ? 'Static IP cleanup enabled' : 'Network configuration will be retained'}
      >
        {value.unbindStaticIPs
          ? 'Swallow will wait for Ready, then remove only unchanged Static links captured before Release. DHCP, provider-managed, Link only, and later changes are preserved.'
          : 'Release will leave all current network links in place.'}
      </Alert>
    </Stack>
  )
}
