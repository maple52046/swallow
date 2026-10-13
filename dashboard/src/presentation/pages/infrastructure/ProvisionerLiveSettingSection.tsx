import type { ReactNode } from 'react'
import type { Integration } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { IntegrationSettingsPanel } from './IntegrationSettingsPanel'

interface ProvisionerLiveSettingSectionProps {
  /** The stored MAAS provisioner Integration being edited. */
  integration: Pick<Integration, 'hasCredential'>
  /** Prefix of the heading's id, which labels the section for assistive technology. */
  id: string
  title: string
  /** What the setting does for deployed machines, and that MAAS, not this Integration, holds it. */
  description: ReactNode
  /** What the operator cannot do until the Integration has a credential, ending the shared notice. */
  credentialNeededFor: string
  /** The live editor, rendered only when the Integration has a credential to reach MAAS with. */
  children: ReactNode
}

/**
 * The panel of a provisioner-owned, live setting on the Integration settings page of a MAAS
 * provisioner — the HTTP Proxy (decision 056) and Subnet DNS (decision 058) tabs. Such a setting is
 * read from and applied to MAAS by its own button and is not part of the Integration record.
 * Without a stored credential the API could not reach MAAS, so instead of rendering `children` (and
 * issuing their request) it says what is missing and where to set it.
 */
export function ProvisionerLiveSettingSection({
  integration,
  id,
  title,
  description,
  credentialNeededFor,
  children,
}: ProvisionerLiveSettingSectionProps) {
  return (
    <IntegrationSettingsPanel id={id} title={title} description={description}>
      {integration.hasCredential ? (
        children
      ) : (
        <Alert status="info" title="No MAAS API key">
          Set the Integration&apos;s credential on the Credential tab first; swallow {credentialNeededFor} with it.
        </Alert>
      )}
    </IntegrationSettingsPanel>
  )
}
