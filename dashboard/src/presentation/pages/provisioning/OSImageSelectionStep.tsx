import { Button, Field, Heading, Text } from '@chakra-ui/react'
import { RefreshCw } from 'lucide-react'
import type { OSImage } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Select, type SelectOption } from '@/presentation/components/ui/select'
import { OSImagePicker } from './OSImagePicker'

export interface OSImageConfigurationSource {
  value: string
  options: readonly SelectOption[]
  onChange: (value: string) => void
  customization?: {
    customized: boolean
    onToggle: () => void
  }
}

export interface OSImageSelectionNotice {
  title: string
  message: string
}

interface OSImageSelectionStepProps {
  images: readonly OSImage[]
  value: string
  siteId?: string
  integrationId?: string
  loading?: boolean
  error?: string
  disabled?: boolean
  onChange: (imageId: string) => void
  onRefresh: () => void
  configurationSource?: OSImageConfigurationSource
  scopeNotice?: OSImageSelectionNotice
}

/**
 * The single composed OS-image decision step shared by standalone, contextual, and Platform flows.
 * Launch adapters provide capabilities and state; this component owns the visual hierarchy.
 */
export function OSImageSelectionStep({
  images,
  value,
  siteId,
  integrationId,
  loading,
  error,
  disabled,
  onChange,
  onRefresh,
  configurationSource,
  scopeNotice,
}: OSImageSelectionStepProps) {
  const customization = configurationSource?.value
    ? configurationSource.customization
    : undefined

  return (
    <section className="sw-os-image-selection-step">
      <div className="sw-os-image-selection-step__heading">
        <Heading as="h2" size="md">Operating system</Heading>
        <Button
          variant="plain"
          size="xs"
          loading={loading}
          disabled={loading}
          onClick={onRefresh}
        >
          <RefreshCw size={14} aria-hidden="true" />
          Refresh
        </Button>
      </div>

      {scopeNotice && (
        <Alert status="info" title={scopeNotice.title}>
          {scopeNotice.message}
        </Alert>
      )}

      <Text className="sw-os-image-selection-step__description" color="fg.muted" fontSize="sm">
        Choose an OS family, then compare the deployable images in that family. Architecture,
        size, tags, deploy-mode availability, and distinct provider IDs remain visible.
      </Text>

      {configurationSource && (
        <div className="sw-os-image-selection-step__configuration">
          <Field.Root required>
            <Field.Label>Configuration source</Field.Label>
            <Select
              value={configurationSource.value}
              aria-label="Configuration source"
              onChange={configurationSource.onChange}
              options={configurationSource.options}
            />
          </Field.Root>
          {customization && (
            <Field.Root>
              <Field.Label>Template control</Field.Label>
              <Button
                variant={customization.customized ? 'outline' : 'plain'}
                size="sm"
                alignSelf="flex-start"
                onClick={customization.onToggle}
              >
                {customization.customized ? 'Use template defaults' : 'Customize settings'}
              </Button>
              <Field.HelperText>
                {customization.customized
                  ? 'This deployment can override the selected template.'
                  : 'Image, install location, and network intent are owned by the template.'}
              </Field.HelperText>
            </Field.Root>
          )}
        </div>
      )}

      <OSImagePicker
        images={images}
        value={value}
        siteId={siteId}
        integrationId={integrationId}
        required
        loading={loading}
        error={error}
        disabled={disabled}
        presentation="plain"
        onChange={onChange}
      />
    </section>
  )
}
