import { Button, Stack, Switch, Text } from '@chakra-ui/react'
import {
  EXPERIMENTAL_FEATURES,
  type ExperimentalFeature,
} from '@/application/ports/ExperimentalFeatureSettings'
import { Modal } from '@/presentation/components/ui/modal'
import { useExperimentalFeatures } from '@/presentation/contexts/ExperimentalFeaturesContext'

/** What each switch shows, written for a developer deciding what to preview. */
const FEATURE_COPY: Readonly<Record<ExperimentalFeature, { label: string; description: string }>> = {
  monitoring: {
    label: 'Monitoring',
    description: 'Monitoring page, the Server Monitoring tab, and Server health on Overview and the Server list.',
  },
  osImageUpload: {
    label: 'OS image upload',
    description: 'Uploading a custom OS image from OS images.',
  },
  deploymentTemplates: {
    label: 'Deployment Templates',
    description: 'The Templates workspace, Create template on OS images, and choosing or saving a template while deploying.',
  },
}

interface ExperimentalFeaturesDialogProps {
  open: boolean
  onClose: () => void
}

/**
 * Development-build switches for in-development dashboard features, in the spirit of
 * chrome://flags.
 *
 * Each switch writes through the experimental feature settings port immediately, so the
 * console behind the dialog re-renders as it would in a release build with that feature
 * hidden. Choices persist per browser. The header mounts this only when the settings are
 * adjustable, which is never the case in release builds. Each switch is a native checkbox
 * named by its visible label and described by its scope text, so it stays keyboard-operable
 * and announces both what it is and what it hides.
 */
export function ExperimentalFeaturesDialog({ open, onClose }: ExperimentalFeaturesDialogProps) {
  const { enabled, setEnabled, reset } = useExperimentalFeatures()
  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Experimental features"
      description="Development builds only. Release builds hide these features and offer no switches."
      footer={
        <>
          <Button variant="ghost" onClick={reset}>Reset to defaults</Button>
          <Button colorPalette="brand" onClick={onClose}>Done</Button>
        </>
      }
    >
      <Stack gap="4">
        {EXPERIMENTAL_FEATURES.map((feature) => {
          const copy = FEATURE_COPY[feature]
          const descriptionId = `experimental-feature-${feature}-description`
          return (
            <Stack key={feature} gap="1">
              <Switch.Root
                checked={enabled[feature]}
                onCheckedChange={(details) => setEnabled(feature, details.checked)}
                colorPalette="brand"
              >
                <Switch.HiddenInput aria-describedby={descriptionId} />
                <Switch.Control>
                  <Switch.Thumb />
                </Switch.Control>
                <Switch.Label fontWeight="medium">{copy.label}</Switch.Label>
              </Switch.Root>
              <Text id={descriptionId} fontSize="sm" color="fg.muted" ps="11">
                {copy.description}
              </Text>
            </Stack>
          )
        })}
      </Stack>
    </Modal>
  )
}
