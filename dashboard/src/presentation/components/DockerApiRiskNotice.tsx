import { Text } from '@chakra-ui/react'
import { DOCKER_ENGINE_API_PORT } from '@/domain/software/types'
import { Alert } from '@/presentation/components/ui/alert'

interface DockerApiRiskNoticeProps {
  /**
   * `option` explains the install-time checkbox; `enable` frames the same risk where the
   * Containers tab asks to turn the API on for an installed Server. Both must state the exposure.
   */
  context: 'option' | 'enable'
}

/**
 * The single statement of what the Docker CE `enableApi` variant exposes (decision 043).
 *
 * Shown wherever an operator can turn the Docker Engine API on — the Install software dialog and the
 * Server Containers tab — so the warning cannot drift between the two. The listener is
 * unauthenticated plain HTTP on every interface, which is root-equivalent host access for anyone who
 * can reach the port; the copy therefore says so and recommends disabling it on Internet-reachable
 * Servers. It also states the daemon restart, because toggling stops containers without a restart
 * policy. Rendered as a `warning` alert so the meaning is carried by text and icon, not colour.
 */
export function DockerApiRiskNotice({ context }: DockerApiRiskNoticeProps) {
  return (
    <Alert
      status="warning"
      title={context === 'option' ? 'Enables the Docker Engine API' : 'Enabling the Docker Engine API has risks'}
    >
      <Text>
        The Docker daemon will also listen on TCP port {DOCKER_ENGINE_API_PORT} without authentication or TLS so swallow can
        manage its images, containers, volumes, and networks from the Server&apos;s Containers tab. Anyone who can reach
        that port controls the host as root. If the Server can be reached from the Internet, keep this disabled.
      </Text>
      <Text mt="1">
        Changing this setting restarts the Docker daemon; containers without a restart policy stop.
      </Text>
    </Alert>
  )
}
