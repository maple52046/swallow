import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { Link } from 'react-router-dom'
import type { PlatformAccessProps } from '@/modules/types'
import { serverDisplayName, serverPrimaryAddress } from '@/domain/server/types'
import { CopyButton } from '@/presentation/components/CopyButton'
import { SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

/** Compact Slurm submission access for the summary-first workspace Overview. */
export function SlurmAccess({ platform, loginNodes }: PlatformAccessProps) {
  const { scopedHref } = useSiteScope()
  return (
    <SectionSurface
      title="Submission access"
      description="SSH to a login node to submit jobs with sbatch or srun."
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to={scopedHref(`/platforms/${encodeURIComponent(platform.id)}/nodes`, platform.siteId)}>Open nodes</Link>
        </Button>
      }
    >
      {loginNodes.length === 0 ? (
        <Text color="fg.muted">No login node is recorded. Controllers remain available on the Nodes route.</Text>
      ) : (
        <Stack gap="2">
          {loginNodes.map((server) => {
            const address = serverPrimaryAddress(server)
            return (
              <HStack key={server.id} gap="2" wrap="wrap">
                <Link to={scopedHref(`/servers/${encodeURIComponent(server.id)}`, platform.siteId)}>{serverDisplayName(server)}</Link>
                <Text className="mono" color="fg.muted">{address ?? 'Address unavailable'}</Text>
                {address && <CopyButton value={address} label={`Copy ${serverDisplayName(server)} address`} />}
              </HStack>
            )
          })}
        </Stack>
      )}
    </SectionSurface>
  )
}
