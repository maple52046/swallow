import { Field } from '@chakra-ui/react'
import type { DeploymentNetworkMode } from '@/domain/provisioning/types'
import { ChoiceCardGroup, type ChoiceCardOption } from '@/presentation/components/ChoiceCardGroup'

/**
 * The three deployment addressing intents in the order a user weighs them (glossary Network
 * Configuration). Descriptions state who assigns the address and where DNS comes from, because
 * that is what tells Automatic and DHCP apart on a site-DHCP network (decision 058).
 */
const ADDRESSING_MODE_OPTIONS: ReadonlyArray<ChoiceCardOption<DeploymentNetworkMode>> = [
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
 * context, such as a template owning the value. A risk of the selected mode, like Automatic on a
 * subnet without provisioner DHCP, is a separate `SiteDhcpAddressingWarning` rather than helper
 * text, so it reads as a warning and not as a note.
 */
export function AddressingModeField({
  value,
  onChange,
  disabled,
  label = 'Addressing mode',
  helperText,
}: AddressingModeFieldProps) {
  return (
    // Every consumer places the field in a two-column form grid; three cards need the whole row.
    <Field.Root required className="sw-addressing-mode-field">
      <Field.Label>{label}</Field.Label>
      <ChoiceCardGroup label={label} options={ADDRESSING_MODE_OPTIONS} value={value} onChange={onChange} disabled={disabled} columns={3} />
      {helperText && <Field.HelperText>{helperText}</Field.HelperText>}
    </Field.Root>
  )
}
