import { useState } from 'react'
import { Field, RadioCard } from '@chakra-ui/react'
import type { DeploymentNetworkMode } from '@/domain/provisioning/types'

/**
 * The three deployment addressing intents in the order an operator weighs them (glossary Network
 * Configuration). Descriptions state who assigns the address and where DNS comes from, because
 * that is what tells Automatic and DHCP apart on a site-DHCP network (ADR 058).
 */
const ADDRESSING_MODE_OPTIONS: ReadonlyArray<{
  value: DeploymentNetworkMode
  label: string
  description: string
}> = [
  {
    value: 'automatic',
    label: 'Automatic',
    description: 'The provisioner assigns and records a fixed address, with the subnet DNS.',
  },
  {
    value: 'dhcp',
    label: 'DHCP',
    description: "The network's DHCP server assigns the address, gateway, and DNS.",
  },
  {
    value: 'static',
    label: 'Static',
    description: 'Choose a NIC, subnet, and unique IPv4 address for every Server.',
  },
]

const ADDRESSING_MODES: ReadonlySet<string> = new Set(ADDRESSING_MODE_OPTIONS.map((option) => option.value))

interface AddressingModeFieldProps {
  value: DeploymentNetworkMode
  onChange: (mode: DeploymentNetworkMode) => void
  disabled?: boolean
  label?: string
  helperText?: string
}

/**
 * Selects deployment-time network intent with equal-width radio cards, shared by the Deploy OS
 * dialog, Deployment Templates, and the platform deploy wizard. Supporting text makes the
 * operational consequence available before selection, and disabled template-owned values remain
 * readable instead of collapsing into a terse segmented control. `helperText` carries the caller's
 * context, such as why a mode was suggested.
 */
export function AddressingModeField({
  value,
  onChange,
  disabled,
  label = 'Addressing mode',
  helperText,
}: AddressingModeFieldProps) {
  const [keyboardNavigation, setKeyboardNavigation] = useState(false)

  return (
    // Every consumer places the field in a two-column form grid; three cards need the whole row.
    <Field.Root required className="sw-addressing-mode-field">
      <Field.Label>{label}</Field.Label>
      <RadioCard.Root
        aria-label={label}
        colorPalette="brand"
        value={value}
        disabled={disabled}
        className="sw-deployment-choice-grid sw-deployment-choice-grid--three"
        data-focus-modality={keyboardNavigation ? 'keyboard' : 'pointer'}
        onKeyDownCapture={() => setKeyboardNavigation(true)}
        onPointerDownCapture={() => setKeyboardNavigation(false)}
        onValueChange={(details) => {
          // The radio group only emits the values rendered below; the set check makes the
          // assertion safe even if Chakra emits null on a cleared selection.
          if (details.value && ADDRESSING_MODES.has(details.value)) {
            onChange(details.value as DeploymentNetworkMode)
          }
        }}
      >
        {ADDRESSING_MODE_OPTIONS.map((option) => (
          <RadioCard.Item key={option.value} value={option.value}>
            <RadioCard.ItemHiddenInput />
            <RadioCard.ItemControl className="sw-deployment-choice-card">
              <RadioCard.ItemContent className="sw-deployment-choice-card__content">
                <RadioCard.ItemText className="sw-deployment-choice-card__title">{option.label}</RadioCard.ItemText>
                <RadioCard.ItemDescription className="sw-deployment-choice-card__description">{option.description}</RadioCard.ItemDescription>
              </RadioCard.ItemContent>
              <RadioCard.ItemIndicator />
            </RadioCard.ItemControl>
          </RadioCard.Item>
        ))}
      </RadioCard.Root>
      {helperText && <Field.HelperText>{helperText}</Field.HelperText>}
    </Field.Root>
  )
}
