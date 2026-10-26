import { Box, Switch, Text } from '@chakra-ui/react'
import { useId, type ReactNode } from 'react'

interface FeatureToggleCardProps {
  title: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  children?: ReactNode
  disabled?: boolean
  disabledReason?: string
  switchLabel?: string
  status?: ReactNode
}
/**
 * Presents one optional deployment capability as a semantic on/off decision.
 *
 * The Switch remains associated with the visible title and descriptive copy, while expanded
 * configuration is rendered only when the feature is enabled. A disabled feature always carries
 * a textual reason so availability is never communicated through control styling alone.
 */
export function FeatureToggleCard({
  title,
  description,
  checked,
  onCheckedChange,
  children,
  disabled = false,
  disabledReason,
  switchLabel = `Enable ${title}`,
  status,
}: FeatureToggleCardProps) {
  const id = useId()
  const descriptionId = `${id}-description`
  const unavailableId = `${id}-unavailable`

  return (
    <Box
      as="section"
      className="sw-platform-feature-card"
      data-enabled={checked || undefined}
      data-disabled={disabled || undefined}
      aria-labelledby={`${id}-title`}
    >
      <div className="sw-platform-feature-card__header">
        <div className="sw-platform-feature-card__copy">
          <div className="sw-platform-feature-card__title-row">
            <Text id={`${id}-title`} fontWeight="semibold">{title}</Text>
            {status}
          </div>
          <Text id={descriptionId} color="fg.muted" fontSize="sm">{description}</Text>
          {disabledReason && (
            <Text id={unavailableId} color="orange.fg" fontSize="sm">{disabledReason}</Text>
          )}
        </div>
        <Switch.Root
          checked={checked}
          disabled={disabled}
          colorPalette="brand"
          onCheckedChange={(details) => onCheckedChange(details.checked === true)}
        >
          <Switch.HiddenInput
            role="switch"
            aria-label={switchLabel}
            aria-describedby={[descriptionId, disabledReason ? unavailableId : ''].filter(Boolean).join(' ')}
          />
          <Switch.Control><Switch.Thumb /></Switch.Control>
        </Switch.Root>
      </div>
      {checked && children && <div className="sw-platform-feature-card__body">{children}</div>}
    </Box>
  )
}
