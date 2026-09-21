import { Field, SegmentGroup } from '@chakra-ui/react'
import { DEPLOY_TARGET_LABELS, type DeployTarget } from '@/domain/provisioning/types'

const DEPLOY_TARGET_OPTIONS: { value: DeployTarget; label: string }[] = [
  { value: 'disk', label: DEPLOY_TARGET_LABELS.disk },
  { value: 'ram', label: DEPLOY_TARGET_LABELS.ram },
]

/**
 * A segmented Disk / RAM deploy-target selector, the single control that replaces the old
 * "Ephemeral deployment" checkbox across the deploy wizard, template editor, and platform wizard.
 * It emits the canonical `deployTarget` vocabulary; callers map it onto the wire (`deployTarget`,
 * with `ephemeral` still accepted for compatibility).
 */
export function DeployTargetField({
  value,
  onChange,
  disabled,
  label = 'Deploy target',
  helperText,
}: {
  value: DeployTarget
  onChange: (target: DeployTarget) => void
  disabled?: boolean
  label?: string
  helperText?: string
}) {
  return (
    <Field.Root>
      <Field.Label>{label}</Field.Label>
      <SegmentGroup.Root
        value={value}
        disabled={disabled}
        onValueChange={(details) => {
          if (details.value === 'disk' || details.value === 'ram') onChange(details.value)
        }}
      >
        <SegmentGroup.Indicator />
        {DEPLOY_TARGET_OPTIONS.map((option) => (
          <SegmentGroup.Item key={option.value} value={option.value}>
            <SegmentGroup.ItemText>{option.label}</SegmentGroup.ItemText>
            <SegmentGroup.ItemHiddenInput />
          </SegmentGroup.Item>
        ))}
      </SegmentGroup.Root>
      {helperText && <Field.HelperText>{helperText}</Field.HelperText>}
    </Field.Root>
  )
}
