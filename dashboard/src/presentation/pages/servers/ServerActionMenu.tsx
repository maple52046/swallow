import { useState } from 'react'
import { Dropdown, DropdownItem, DropdownList, MenuToggle } from '@patternfly/react-core'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import type { ProvisionerCapabilities } from '@/domain/server/types'
import { SERVER_ACTION_GROUPS, actionLabel, type BulkAction } from './serverActions'

/**
 * Capability-gated MAAS-style machine action menu. Each accepted write reports its
 * snapshot and then reloads projection/detail; query-power remains explicitly read-only.
 */
export function ServerActionMenu({ serverId, serverName, capabilities, deployDisabledReason, onActed }: { serverId: string; serverName: string; capabilities: ProvisionerCapabilities | null; deployDisabledReason?: string; onActed: () => void }) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const [busy, setBusy] = useState(false)
  const [open, setOpen] = useState(false)
  const run = async (action: BulkAction) => {
    setBusy(true); setOpen(false)
    try {
      const result = action === 'release' ? await servers.releaseServer(serverId) : await servers.runServerAction(serverId, action)
      showToast({ tone: 'success', title: `${actionLabel(action)} accepted`, description: `${serverName} reports "${result.state}"; reconciliation will follow it.` }); onActed()
    } catch (error) { showToast({ tone: 'error', title: `${actionLabel(action)} failed`, description: error instanceof Error ? error.message : 'Unknown error' }) }
    finally { setBusy(false) }
  }
  const queryPower = async () => {
    setOpen(false)
    try { const result = await servers.queryPowerState(serverId); showToast({ tone: 'info', title: `Live power state: ${result.powerState}` }) }
    catch (error) { showToast({ tone: 'error', title: 'Could not read power state', description: error instanceof Error ? error.message : 'Unknown error' }) }
  }
  const groups = SERVER_ACTION_GROUPS.filter((group) => group.capability === null || capabilities?.[group.capability])
  const deployHref = () => {
    const target = new URL(scopedHref('/provisioning/deploy'), window.location.origin)
    target.searchParams.append('serverId', serverId)
    navigate(`${target.pathname}${target.search}`)
  }
  return <Dropdown isOpen={open} onOpenChange={setOpen} toggle={(ref) => <MenuToggle ref={ref} variant="primary" isExpanded={open} isDisabled={busy} onClick={() => setOpen((value) => !value)}>{busy ? 'Working...' : 'Take action'}</MenuToggle>}><DropdownList><DropdownItem isDisabled={Boolean(deployDisabledReason)} onClick={deployHref}>{deployDisabledReason ? `Deploy OS - ${deployDisabledReason}` : 'Deploy OS'}</DropdownItem>{groups.flatMap((group) => [<DropdownItem key={`${group.label}-label`} isDisabled>{group.label}</DropdownItem>, ...group.actions.map((entry) => <DropdownItem key={entry.action} onClick={() => void run(entry.action)}>{entry.label}</DropdownItem>)])}{capabilities?.power && <DropdownItem onClick={() => void queryPower()}>Query power state</DropdownItem>}</DropdownList></Dropdown>
}
