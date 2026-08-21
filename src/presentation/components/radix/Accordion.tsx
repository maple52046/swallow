import * as RadixAccordion from '@radix-ui/react-accordion'
import { Box, Flex, Text } from '@radix-ui/themes'
import { ChevronDownIcon } from '@radix-ui/react-icons'
import type { ReactNode } from 'react'

/** One collapsible section. `count` renders a muted badge beside the label when set. */
export interface AccordionSection {
  value: string
  label: string
  count?: number
  content: ReactNode
}

interface AccordionProps {
  sections: AccordionSection[]
  /** Values open by default. Multiple may be open at once (type="multiple"). */
  defaultOpen?: string[]
}

/**
 * Multi-open accordion, replacing the accordion the filter panel needs (Radix Themes
 * ships none). Built on `@radix-ui/react-accordion` and styled with theme tokens.
 *
 * Triggers are real buttons with a rotating chevron, so sections are keyboard-operable
 * and their expanded state is exposed to assistive tech by the underlying primitive.
 */
export function Accordion({ sections, defaultOpen = [] }: AccordionProps) {
  return (
    <RadixAccordion.Root type="multiple" defaultValue={defaultOpen}>
      {sections.map((section) => (
        <RadixAccordion.Item key={section.value} value={section.value}>
          <RadixAccordion.Header>
            <RadixAccordion.Trigger asChild>
              <button type="button" style={triggerStyle}>
                <Flex align="center" justify="between" width="100%">
                  <Flex align="center" gap="2">
                    <Text size="2" weight="medium">
                      {section.label}
                    </Text>
                    {section.count !== undefined && (
                      <Text size="1" color="gray">
                        {section.count}
                      </Text>
                    )}
                  </Flex>
                  <ChevronDownIcon className="accordion-chevron" />
                </Flex>
              </button>
            </RadixAccordion.Trigger>
          </RadixAccordion.Header>
          <RadixAccordion.Content>
            <Box py="2">{section.content}</Box>
          </RadixAccordion.Content>
        </RadixAccordion.Item>
      ))}
    </RadixAccordion.Root>
  )
}

const triggerStyle: React.CSSProperties = {
  width: '100%',
  padding: 'var(--space-2) 0',
  background: 'transparent',
  border: 'none',
  borderBottom: '1px solid var(--gray-a4)',
  cursor: 'pointer',
  textAlign: 'left',
}
