import { Alert, AlertVariant, Checkbox, Stack, StackItem } from '@patternfly/react-core'
import type { ReleaseOptionsValue } from './releaseOptions'

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
 * ReleaseOptionsFields renders the disk-erasure and static-IP cleanup choices for a release.
 *
 * It is fully controlled: the caller owns the value and submits it, so the same UI serves
 * both the Server Release dialog and the platform uninstall dialog. Turning erase off also
 * clears the erase modes so a caller cannot submit an inconsistent combination.
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
    <Stack hasGutter>
      {supportsReleaseOptions ? (
        <>
          <StackItem>
            <Checkbox
              id={`${idPrefix}-erase-disks`}
              label="Erase disks before release"
              isChecked={value.erase}
              onChange={(_event, checked) => changeErase(checked)}
            />
          </StackItem>
          <StackItem className="sw-release-suboptions">
            <Stack hasGutter>
              <StackItem>
                <Checkbox
                  id={`${idPrefix}-secure-erase`}
                  label="Use secure erase when supported"
                  isChecked={value.secureErase}
                  isDisabled={!value.erase}
                  onChange={(_event, checked) => onChange({ ...value, secureErase: checked })}
                />
              </StackItem>
              <StackItem>
                <Checkbox
                  id={`${idPrefix}-quick-erase`}
                  label={value.secureErase ? 'Use quick erase if secure erase is unavailable' : 'Use quick erase'}
                  isChecked={value.quickErase}
                  isDisabled={!value.erase}
                  onChange={(_event, checked) => onChange({ ...value, quickErase: checked })}
                />
              </StackItem>
            </Stack>
          </StackItem>
          <StackItem>
            <Alert
              variant={value.erase ? AlertVariant.warning : AlertVariant.info}
              title={value.erase ? 'Disk erasure enabled' : 'Disk contents will be retained'}
              isInline
            >
              {eraseExplanation(value)}
            </Alert>
          </StackItem>
        </>
      ) : (
        <StackItem>
          <Alert variant={AlertVariant.info} title="Disk erasure options unavailable" isInline>
            This provisioner can release the machine but does not expose configurable disk erasure through Swallow.
          </Alert>
        </StackItem>
      )}
      <StackItem>
        <Checkbox
          id={`${idPrefix}-unbind-static-ips`}
          label="Remove static IP bindings after release"
          isChecked={value.unbindStaticIPs}
          isDisabled={!supportsNetworkConfiguration}
          onChange={(_event, checked) => onChange({ ...value, unbindStaticIPs: checked })}
        />
      </StackItem>
      <StackItem>
        <Alert
          variant={value.unbindStaticIPs ? AlertVariant.warning : AlertVariant.info}
          title={value.unbindStaticIPs ? 'Static IP cleanup enabled' : 'Network configuration will be retained'}
          isInline
        >
          {value.unbindStaticIPs
            ? 'Swallow will wait for Ready, then remove only unchanged Static links captured before Release. DHCP, provider-managed, Link only, and later changes are preserved.'
            : 'Release will leave all current network links in place.'}
        </Alert>
      </StackItem>
    </Stack>
  )
}
