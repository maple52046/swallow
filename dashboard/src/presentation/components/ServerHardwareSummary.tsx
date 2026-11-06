import { Cpu, MemoryStick, Microchip, type LucideIcon } from 'lucide-react'
import { gpuInventoryCount, serverGPUsByKind } from '@/domain/server/gpu'
import type { Server } from '@/domain/server/types'
import { formatQuantity } from '@/shared/utils/quantity'
import { gpuInventoryProfile } from '@/presentation/components/serverGpuPresentation'
import { hardwareBrand } from '@/presentation/components/serverHardwarePresentation'
import { Tooltip } from '@/presentation/components/ui/tooltip'

/**
 * One independently reported resource in the hardware summary. Keeping the icon, short fact,
 * accessible name, and optional exact-value tooltip together makes another resource an additive
 * entry instead of a new component layout.
 */
interface HardwareSummaryEntry {
  key: 'cpu' | 'memory' | 'gpu'
  Icon: LucideIcon
  valueLines: readonly string[]
  accessibleName: string
  tooltip?: string
}

/**
 * Presents the Server inventory's CPU, memory, and compute-GPU capacity in dense tables and cards.
 *
 * Every reported resource keeps a visible short fact beside a decorative icon and an accessible
 * name that identifies the resource. CPU and GPU entries expose exact provider strings through a
 * keyboard-focusable tooltip. Display controllers are deliberately excluded: the API classifies
 * them separately from workload accelerator capacity, matching the Server list's GPU filters.
 * A partially observed Server renders only known entries; no known entry renders “Not reported”.
 */
export function ServerHardwareSummary({ server }: { server: Server }) {
  const cpuModel = server.status.cpuModel.trim()
  const cpuCores = server.status.cpuCores > 0 ? server.status.cpuCores.toLocaleString() : ''
  const memory = server.status.memoryMiB > 0 ? formatQuantity(server.status.memoryMiB, 'GiB', 1024) : ''
  const computeGPUs = serverGPUsByKind(server, 'compute')
  const gpuCount = gpuInventoryCount(computeGPUs)
  const entries: HardwareSummaryEntry[] = []

  if (cpuModel || cpuCores) {
    const cpuBrand = cpuModel ? hardwareBrand(cpuModel) ?? 'CPU' : ''
    const valueLines = [cpuBrand, cpuCores ? `${cpuCores} cores` : ''].filter(Boolean)
    entries.push({
      key: 'cpu',
      Icon: Cpu,
      valueLines,
      accessibleName: `CPU: ${[cpuBrand, cpuCores ? `${cpuCores} cores` : ''].filter(Boolean).join(', ')}`,
      ...(cpuModel ? { tooltip: cpuModel } : {}),
    })
  }

  if (memory) {
    entries.push({
      key: 'memory',
      Icon: MemoryStick,
      valueLines: [memory],
      accessibleName: `RAM: ${memory}`,
    })
  }

  if (computeGPUs.length > 0) {
    const vendors = [...new Set(computeGPUs
      .map((gpu) => gpu.vendor.trim())
      .filter(Boolean)
      .map((vendor) => hardwareBrand(vendor) ?? vendor))].sort()
    const vendorLabel = vendors.length > 0 ? vendors.join(' + ') : 'Unknown vendor'
    const countLabel = `${gpuCount.toLocaleString()} ${gpuCount === 1 ? 'GPU' : 'GPUs'}`
    entries.push({
      key: 'gpu',
      Icon: Microchip,
      valueLines: [vendorLabel, countLabel],
      accessibleName: `GPU: ${vendorLabel}, ${countLabel}`,
      tooltip: gpuInventoryProfile(computeGPUs),
    })
  }

  if (entries.length === 0) {
    return (
      <div className="sw-server-hardware-summary">
        <strong className="sw-server-hardware-empty">Not reported</strong>
      </div>
    )
  }

  return (
    <div className="sw-server-hardware-summary">
      {entries.map(({ key, Icon, valueLines, accessibleName, tooltip }) => {
        const entry = (
          <span
            key={key}
            className={`sw-server-hardware-entry sw-server-hardware-${key}`}
            role="group"
            aria-label={accessibleName}
            tabIndex={tooltip ? 0 : undefined}
            data-tooltip={tooltip ? 'true' : undefined}
          >
            <Icon className="sw-server-hardware-icon" aria-hidden />
            <span className="sw-server-hardware-value">
              {valueLines.map((line) => <span key={line}>{line}</span>)}
            </span>
          </span>
        )
        return tooltip ? (
          <Tooltip key={key} content={tooltip}>{entry}</Tooltip>
        ) : (
          entry
        )
      })}
    </div>
  )
}
