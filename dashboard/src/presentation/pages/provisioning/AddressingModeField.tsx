import { useState } from 'react'
import { Field, RadioCard } from '@chakra-ui/react'
import type { DeploymentNetworkMode } from '@/domain/provisioning/types'

const ADDRESSING_MODE_OPTIONS: ReadonlyArray<{
  value: DeploymentNetworkMode
  label: string
  description: string
}> = [
  {
    value: 'automatic',
    label: 'Automatic',
    description: 'Let the provisioner assign addressing for each selected Server.',
  },
  {
    value: 'static',
    label: 'Static',
    description: 'Choose a NIC, subnet, and unique IPv4 address for every Server.',
  },
]

interface AddressingModeFieldProps {
  value: DeploymentNetworkMode
  onChange: (mode: DeploymentNetworkMode) => void
  disabled?: boolean
  label?: string
  helperText?: string
}

/**
 * Selects deployment-time network intent with equal-width radio cards. Supporting text makes the
 * operational consequence available before selection, and disabled template-owned values remain
 * readable instead of collapsing into a terse segmented control.
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
    <Field.Root required>
      <Field.Label>{label}</Field.Label>
      <RadioCard.Root
        aria-label={label}
        colorPalette="brand"
        value={value}
        disabled={disabled}
        className="sw-deployment-choice-grid"
        data-focus-modality={keyboardNavigation ? 'keyboard' : 'pointer'}
        onKeyDownCapture={() => setKeyboardNavigation(true)}
        onPointerDownCapture={() => setKeyboardNavigation(false)}
        onValueChange={(details) => {
          if (details.value === 'automatic' || details.value === 'static') {
            onChange(details.value)
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
