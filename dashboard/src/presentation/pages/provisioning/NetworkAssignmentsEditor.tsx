import { Input } from '@chakra-ui/react'
import type { DeploymentNetworkMode, NetworkTarget } from '@/domain/provisioning/types'
import { Select } from '@/presentation/components/ui/select'
import { formatSubnetOptionLabel } from '@/presentation/utils/network'

/** Editable per-Server network values owned by the shared deployment form. */
export interface NetworkAssignmentValue {
  readonly interfaceId: string
  readonly subnetId: string
  readonly ipAddress: string
}

interface NetworkAssignmentsEditorProps {
  readonly targets: readonly NetworkTarget[]
  readonly serverNames: Readonly<Record<string, string>>
  readonly assignments: Readonly<Record<string, NetworkAssignmentValue>>
  readonly mode: DeploymentNetworkMode
  readonly ariaLabel: string
  readonly showCurrentProviderMode?: boolean
  readonly onChange: (serverId: string, assignment: NetworkAssignmentValue) => void
}

const EMPTY_ASSIGNMENT: NetworkAssignmentValue = {
  interfaceId: '',
  subnetId: '',
  ipAddress: '',
}

function providerModeLabel(rawProviderMode: string | undefined): string {
  if (rawProviderMode === 'AUTO') return 'Provider-managed (MAAS AUTO)'
  return rawProviderMode || '-'
}

/**
 * Shared per-Server network assignment editor for every OS deployment surface.
 *
 * A single accessible table-shaped DOM owns every control. CSS presents the rows as a
 * comparison table on wide viewports and as labelled Server cards on mobile, avoiding
 * duplicated form controls, clipped selects, and horizontal form scrolling.
 */
export function NetworkAssignmentsEditor({
  targets,
  serverNames,
  assignments,
  mode,
  ariaLabel,
  showCurrentProviderMode = true,
  onChange,
}: NetworkAssignmentsEditorProps) {
  return (
    <div
      className="sw-network-assignment-editor"
      role="table"
      aria-label={ariaLabel}
      data-mode={mode}
      data-current-mode={showCurrentProviderMode ? 'visible' : 'hidden'}
    >
      <div className="sw-network-assignment-editor__header" role="row">
        <span role="columnheader">Server</span>
        <span role="columnheader">Interface</span>
        <span role="columnheader">Subnet</span>
        {mode === 'static' && <span role="columnheader">Static IPv4 address</span>}
        {showCurrentProviderMode && (
          <span role="columnheader" title="Current provider mode">Current mode</span>
        )}
      </div>
      <div className="sw-network-assignment-editor__body" role="rowgroup">
        {targets.map((target) => {
          const assignment = assignments[target.serverId] ?? EMPTY_ASSIGNMENT
          const iface = target.network.interfaces.find(
            (candidate) => candidate.id === assignment.interfaceId,
          )
          const name = serverNames[target.serverId] ?? target.serverId
          const currentMode = providerModeLabel(iface?.rawProviderMode)
          const changeInterface = (interfaceId: string) => {
            const nextInterface = target.network.interfaces.find(
              (candidate) => candidate.id === interfaceId,
            )
            const compatible = nextInterface?.availableSubnets.some(
              (subnet) => subnet.id === assignment.subnetId,
            )
            const subnetId = compatible
              ? assignment.subnetId
              : nextInterface?.availableSubnets.length === 1
                ? nextInterface.availableSubnets[0].id
                : ''
            onChange(target.serverId, { ...assignment, interfaceId, subnetId })
          }

          return (
            <div
              key={target.serverId}
              className="sw-network-assignment-editor__row"
              role="row"
              aria-label={`Network assignment for ${name}`}
            >
              <div className="sw-network-assignment-editor__cell sw-network-assignment-editor__server" role="cell">
                <span className="sw-network-assignment-editor__mobile-label">Server</span>
                <strong>{name}</strong>
              </div>
              <div className="sw-network-assignment-editor__cell" role="cell">
                <span className="sw-network-assignment-editor__mobile-label">Interface</span>
                <Select
                  aria-label={`Interface for ${name}`}
                  value={assignment.interfaceId}
                  size="sm"
                  placeholder="Select an interface"
                  onChange={changeInterface}
                  options={target.network.interfaces.map((candidate) => ({
                    value: candidate.id,
                    label: `${candidate.name} - ${candidate.macAddress}${candidate.boot ? ' (boot NIC)' : ''}`,
                  }))}
                />
              </div>
              <div className="sw-network-assignment-editor__cell" role="cell">
                <span className="sw-network-assignment-editor__mobile-label">Subnet</span>
                <Select
                  aria-label={`Subnet for ${name}`}
                  value={assignment.subnetId}
                  size="sm"
                  placeholder="Select a subnet"
                  onChange={(subnetId) =>
                    onChange(target.serverId, { ...assignment, subnetId })
                  }
                  options={(iface?.availableSubnets ?? []).map((subnet) => ({
                    value: subnet.id,
                    label: formatSubnetOptionLabel(subnet),
                  }))}
                />
              </div>
              {mode === 'static' && (
                <div className="sw-network-assignment-editor__cell" role="cell">
                  <span className="sw-network-assignment-editor__mobile-label">
                    Static IPv4 address
                  </span>
                  <Input
                    aria-label={`Static IPv4 address for ${name}`}
                    value={assignment.ipAddress}
                    placeholder="192.0.2.10"
                    onChange={(event) =>
                      onChange(target.serverId, {
                        ...assignment,
                        ipAddress: event.target.value,
                      })
                    }
                  />
                </div>
              )}
              {showCurrentProviderMode && (
                <div
                  className="sw-network-assignment-editor__cell sw-network-assignment-editor__current"
                  role="cell"
                  title={currentMode}
                >
                  <span className="sw-network-assignment-editor__mobile-label">Current mode</span>
                  <span>{currentMode}</span>
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
