import { platformCurrentSpec, settingNumber, settingString } from '@/domain/platform/spec'
import type { KubernetesTopology, Platform, PlatformSpec } from '@/domain/platform/types'
import type { PlatformFact } from '@/modules/types'
import { DEFAULT_POD_CIDR, DEFAULT_SERVICE_CIDR } from './kubernetesDefaults'

/**
 * The topology a Kubernetes spec's member roles describe (platforms.md "Create A Platform":
 * `control-plane` or `worker`, plus `workloads` on a control plane that runs them), derived the way
 * the api-server derives `deployment.topology`: three or more control planes are high availability;
 * one control plane is standalone when it is the only member and runs workloads, multi-node
 * otherwise. Undefined when no member runs workloads or the control-plane count is no supported
 * shape, so a reader shows no guessed topology.
 */
export function kubernetesSpecTopology(spec: PlatformSpec): KubernetesTopology | undefined {
  const controllers = spec.members.filter((member) => member.roles.includes('control-plane'))
  const workloads = spec.members.filter((member) =>
    member.roles.includes('worker') || (member.roles.includes('control-plane') && member.roles.includes('workloads')))
  if (workloads.length === 0) return undefined
  if (controllers.length >= 3) return 'high-availability'
  if (controllers.length !== 1) return undefined
  return spec.members.length === 1 ? 'standalone' : 'multi-node'
}

/**
 * The Kubernetes facts of the Platform details card, from the spec's k0s settings
 * (platforms.md "Create A Platform"): the k0s version, the Pod and Service ranges, and the API
 * endpoint.
 *
 * - A range the spec omits is the API's default, labelled as such.
 * - Only high availability has an API virtual IP; the other topologies reach the API directly on
 *   the initial control-plane Server, which the card says in words instead of leaving a blank.
 * - A Platform not adopted yet (no spec) has no recorded settings, so it gets no facts.
 */
export function kubernetesPlatformFacts(platform: Platform): PlatformFact[] {
  const settings = platformCurrentSpec(platform)?.settings
  if (!settings) return []
  const apiVip = settingString(settings, 'apiVip')
  const apiVipPrefix = settingNumber(settings, 'apiVipPrefix')
  const podCidr = settingString(settings, 'podCidr')
  const serviceCidr = settingString(settings, 'serviceCidr')
  return [
    { label: 'k0s version', value: settingString(settings, 'k0sVersion') ?? 'Not recorded' },
    { label: 'Pod CIDR', value: podCidr ?? `${DEFAULT_POD_CIDR} (default)` },
    { label: 'Service CIDR', value: serviceCidr ?? `${DEFAULT_SERVICE_CIDR} (default)` },
    {
      label: 'API endpoint',
      value: apiVip
        ? `${apiVip}${apiVipPrefix === undefined ? '' : `/${apiVipPrefix}`} (virtual IP)`
        : 'Direct control-plane endpoint',
    },
  ]
}
