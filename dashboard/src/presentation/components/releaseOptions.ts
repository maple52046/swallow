/**
 * ReleaseOptionsValue is the provider-neutral set of release choices shared by the
 * standalone Server Release action and the platform uninstall dialog when it also releases
 * member servers. `erase` gates the two erase modes; `secureErase` and `quickErase` are
 * hints MAAS reconciles (secure preferred, quick as fallback).
 *
 * These live in their own module (not the component file) so the component module stays
 * component-only for React Fast Refresh.
 */
export interface ReleaseOptionsValue {
  erase: boolean
  secureErase: boolean
  quickErase: boolean
  unbindStaticIPs: boolean
}

/** A zeroed ReleaseOptionsValue for initializing caller state. */
export const emptyReleaseOptions: ReleaseOptionsValue = {
  erase: false,
  secureErase: false,
  quickErase: false,
  unbindStaticIPs: false,
}
