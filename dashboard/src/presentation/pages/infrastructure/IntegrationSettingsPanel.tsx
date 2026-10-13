import type { ReactNode } from 'react'
import { Heading, Stack, Text } from '@chakra-ui/react'

interface IntegrationSettingsPanelProps {
  /** Prefix of the heading's id, which labels the panel for assistive technology. */
  id: string
  title: string
  /** What the panel's settings do and who holds them (swallow or the provider). */
  description: ReactNode
  children: ReactNode
}

/**
 * The labelled card one tab of the Integration settings page renders its settings in. The heading
 * is an h2 under the page's h1, so each tab reads as one section; the card keeps the tab's single
 * action (Save, Apply, or Set credential) next to the settings it affects.
 */
export function IntegrationSettingsPanel({ id, title, description, children }: IntegrationSettingsPanelProps) {
  const headingId = `${id}-title`
  return (
    <section className="sw-integration-settings sw-integration-settings--page" aria-labelledby={headingId}>
      <Stack gap="1">
        <Heading as="h2" size="sm" id={headingId}>
          {title}
        </Heading>
        <Text fontSize="sm" color="fg.muted">
          {description}
        </Text>
      </Stack>
      {children}
    </section>
  )
}
