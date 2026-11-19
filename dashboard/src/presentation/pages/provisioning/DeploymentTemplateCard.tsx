import { Badge, Box, Button, Card, Heading, HStack, Menu, Portal, Stack, Text } from '@chakra-ui/react'
import { ChevronRight, EllipsisVertical, FileStack, HardDrive, MemoryStick, PencilLine, Trash2 } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import type { DeploymentTemplate } from '@/domain/provisioning/types'
import { formatDateTime } from '@/shared/utils/time'
import { DEPLOYMENT_NETWORK_MODE_LABELS } from '@/domain/provisioning/types'
import { formatMinimumHardware } from './minimumHardwareForm'
import type { TemplateImageResolution } from './deploymentTemplatePresentation'

interface DeploymentTemplateCardProps {
  template: DeploymentTemplate
  image: TemplateImageResolution
  poolName: string
  integrationName: string
  siteName: string
  showSite: boolean
  canChange: boolean
  scopedHref: (path: string, siteId?: string) => string
  onEdit: () => void
  onDelete: () => void
}

/**
 * A Pool-owned deployment recipe card.
 *
 * Live image evidence stays separate from the swallow-owned Template: an unavailable provider
 * catalog is shown as unknown rather than as a missing image, while a successful catalog that no
 * longer contains the id receives an explicit warning. Nested actions remain buttons, so the card
 * deliberately is not a link; Platform references inside Technical details are real links.
 */
export function DeploymentTemplateCard({
  template,
  image,
  poolName,
  integrationName,
  siteName,
  showSite,
  canChange,
  scopedHref,
  onEdit,
  onDelete,
}: DeploymentTemplateCardProps) {
  const networkMode = template.network?.mode ?? 'automatic'
  const inUse = template.usedBy.length > 0
  const deployMode = template.ephemeral ? 'RAM deploy' : 'Disk deploy'
  const ImageIcon = template.ephemeral ? MemoryStick : HardDrive

  return (
    <Card.Root as="article" className="sw-template-card" data-image-state={image.status}>
      <Card.Body gap="5">
        <div className="sw-template-card__topline">
          <span className="sw-template-card__icon" aria-hidden><FileStack size={21} /></span>
          <HStack gap="2" wrap="wrap" justify="flex-end">
            {showSite && <Badge variant="outline" colorPalette="gray">{siteName}</Badge>}
            <Badge variant="subtle" colorPalette="blue">
              <ImageIcon size={12} aria-hidden />
              {deployMode}
            </Badge>
            {inUse && <Badge variant="outline" colorPalette="gray">In use</Badge>}
          </HStack>
        </div>

        <div>
          <Heading as="h3" size="lg">{template.name}</Heading>
          <Text color="fg.muted" mt="1" minH={{ md: '2.75rem' }}>
            {template.description || 'Reusable OS deployment settings.'}
          </Text>
        </div>

        <dl className="sw-template-card__facts">
          <div className="sw-template-card__fact sw-template-card__fact--wide">
            <dt>OS image</dt>
            <dd>
              {image.status === 'available' ? (
                <Stack gap="0.5">
                  <HStack gap="2" wrap="wrap">
                    <Text as="span" fontWeight="semibold">{image.label}</Text>
                    {image.osFamilies.map((family) => <Badge key={family} variant="subtle" colorPalette="gray">{family}</Badge>)}
                  </HStack>
                  <Text as="span" className="sw-mono" color="fg.muted" fontSize="sm">{template.imageId}</Text>
                </Stack>
              ) : (
                <Stack gap="0.5">
                  <Text as="span" color={image.status === 'image-unavailable' ? 'fg.error' : 'fg.warning'} fontWeight="semibold">
                    {image.status === 'image-unavailable' ? 'OS image unavailable' : 'Image catalog unavailable'}
                  </Text>
                  <Text as="span" className="sw-mono" color="fg.muted" fontSize="sm">{template.imageId}</Text>
                </Stack>
              )}
            </dd>
          </div>
          <div className="sw-template-card__fact">
            <dt>Network</dt>
            <dd>{DEPLOYMENT_NETWORK_MODE_LABELS[networkMode]}</dd>
          </div>
          <div className="sw-template-card__fact">
            <dt>Cloud-init</dt>
            <dd>{template.hasUserData ? 'Configured' : 'Not configured'}</dd>
          </div>
          <div className="sw-template-card__fact sw-template-card__fact--wide">
            <dt>Minimum hardware</dt>
            <dd>{formatMinimumHardware(template.minimumHardware)}</dd>
          </div>
          <div className="sw-template-card__fact sw-template-card__fact--wide">
            <dt>Usage</dt>
            <dd>{inUse ? `Used by ${template.usedBy.length} ${template.usedBy.length === 1 ? 'node group' : 'node groups'}` : 'Not in use'}</dd>
          </div>
        </dl>

        {image.status !== 'available' && (
          <Box className="sw-template-card__notice" role="status">
            <Text fontSize="sm">
              {image.status === 'catalog-unavailable'
                ? image.message
                : 'The provisioner no longer reports this image. Update the Template before using it for a new deployment.'}
            </Text>
          </Box>
        )}

        <details className="sw-template-card__details">
          <summary><ChevronRight size={15} aria-hidden />Technical details</summary>
          <dl className="sw-template-card__details-grid">
            {showSite && <div><dt>Site</dt><dd>{siteName}</dd></div>}
            <div><dt>Integration</dt><dd>{integrationName}</dd></div>
            <div><dt>Pool</dt><dd>{poolName}</dd></div>
            <div><dt>Created by</dt><dd>{template.createdBy || 'Unknown'}</dd></div>
            <div><dt>Template ID</dt><dd className="sw-mono">{template.id}</dd></div>
            <div><dt>Updated</dt><dd>{formatDateTime(template.updatedAt)}</dd></div>
            <div><dt>Deploy mode</dt><dd>{deployMode}</dd></div>
            <div><dt>Network intent</dt><dd>{DEPLOYMENT_NETWORK_MODE_LABELS[networkMode]}{networkMode === 'static' && template.network.subnetId ? ` · ${template.network.subnetId}` : ''}{template.network.defaultGateway ? ' · Default gateway' : ''}</dd></div>
            <div><dt>Minimum hardware</dt><dd>{formatMinimumHardware(template.minimumHardware)}</dd></div>
            <div><dt>Cloud-init</dt><dd>{template.hasUserData ? 'Configured (write-only)' : 'Not configured'}</dd></div>
            {image.status === 'available' && <div><dt>Architectures</dt><dd>{image.architectures.join(', ')}</dd></div>}
            {image.status === 'available' && image.variants.length > 1 && (
              <div className="sw-template-card__details-wide">
                <dt>Image variants</dt>
                <dd>
                  <Stack as="ul" gap="1">
                    {image.variants.map((variant, index) => (
                      <li key={`${variant.architecture}-${index}`}>
                        <Text as="span" fontWeight="medium">{variant.architecture}</Text>
                        <Text as="span" color="fg.muted"> · {variant.status.name || template.imageId} · {variant.status.osSystem || 'OS not reported'}{variant.status.release ? ` ${variant.status.release}` : ''}</Text>
                      </li>
                    ))}
                  </Stack>
                </dd>
              </div>
            )}
          </dl>
          {inUse && (
            <div className="sw-template-card__uses">
              <Text fontSize="xs" color="fg.muted" fontWeight="semibold">PLATFORM NODE GROUPS</Text>
              <Stack as="ul" gap="1" mt="1">
                {template.usedBy.map((use) => (
                  <li key={`${use.platformId}-${use.group}`}>
                    <RouterLink to={scopedHref(`/platforms/${encodeURIComponent(use.platformId)}/overview`, template.siteId)}>
                      {use.platformName}
                    </RouterLink>
                    <Text as="span" color="fg.muted"> · {use.group}</Text>
                  </li>
                ))}
              </Stack>
            </div>
          )}
        </details>
      </Card.Body>

      {canChange && (
        <Card.Footer className="sw-template-card__actions">
          <Button variant="outline" size="sm" onClick={onEdit}>
            <PencilLine size={15} aria-hidden />
            Edit template
          </Button>
          <Menu.Root positioning={{ placement: 'bottom-end' }}>
            <Menu.Trigger asChild>
              <Button variant="ghost" size="sm" aria-label={`More actions for ${template.name}`}>
                <EllipsisVertical size={16} aria-hidden />
                More
              </Button>
            </Menu.Trigger>
            <Portal>
              <Menu.Positioner>
                <Menu.Content>
                  <Menu.Item value="delete" color="fg.error" disabled={inUse} onClick={onDelete}>
                    <Trash2 size={15} aria-hidden />
                    <Box>
                      <Text>Delete template</Text>
                      {inUse && <Text color="fg.muted" fontSize="xs">Remove it from every Platform node group first.</Text>}
                    </Box>
                  </Menu.Item>
                </Menu.Content>
              </Menu.Positioner>
            </Portal>
          </Menu.Root>
        </Card.Footer>
      )}
    </Card.Root>
  )
}
