import type { GPUDevice, GPUMetrics, GPUProfile } from '@/domain/gpu/types'

const daysAgo = (d: number) => new Date(Date.now() - d * 86400 * 1000).toISOString()

export const seedGPUDevices: GPUDevice[] = [
  // host-01: 8x NVIDIA A100 80GB
  ...Array.from({ length: 8 }, (_, i) => ({ id: `gpu-h01-${i}`, serverId: 'host-01', serverName: 'gpu-host-01', index: i, vendor: 'nvidia' as const, model: 'A100 80GB', serial: `A100-H01-${i}`, uuid: `GPU-a100-h01-${i}`, driverVersion: '535.161.08', cudaVersion: '12.2', status: 'healthy' as const, datacenter: 'dc-east', rack: 'R01', memoryGB: 80 })),
  // host-02: 8x NVIDIA A100 80GB
  ...Array.from({ length: 8 }, (_, i) => ({ id: `gpu-h02-${i}`, serverId: 'host-02', serverName: 'gpu-host-02', index: i, vendor: 'nvidia' as const, model: 'A100 80GB', serial: `A100-H02-${i}`, uuid: `GPU-a100-h02-${i}`, driverVersion: '535.161.08', cudaVersion: '12.2', status: 'healthy' as const, datacenter: 'dc-east', rack: 'R01', memoryGB: 80 })),
  // host-03: 4x NVIDIA H100 80GB
  ...Array.from({ length: 4 }, (_, i) => ({ id: `gpu-h03-${i}`, serverId: 'host-03', serverName: 'gpu-host-03', index: i, vendor: 'nvidia' as const, model: 'H100 80GB SXM5', serial: `H100-H03-${i}`, uuid: `GPU-h100-h03-${i}`, driverVersion: '550.54.15', cudaVersion: '12.4', status: 'healthy' as const, datacenter: 'dc-east', rack: 'R02', memoryGB: 80 })),
  // host-04: 4x NVIDIA H100 80GB (1 critical - ECC errors)
  { id: 'gpu-h04-0', serverId: 'host-04', serverName: 'gpu-host-04', index: 0, vendor: 'nvidia' as const, model: 'H100 80GB SXM5', serial: 'H100-H04-0', uuid: 'GPU-h100-h04-0', driverVersion: '550.54.15', cudaVersion: '12.4', status: 'critical' as const, datacenter: 'dc-east', rack: 'R02', memoryGB: 80 },
  ...Array.from({ length: 3 }, (_, i) => ({ id: `gpu-h04-${i + 1}`, serverId: 'host-04', serverName: 'gpu-host-04', index: i + 1, vendor: 'nvidia' as const, model: 'H100 80GB SXM5', serial: `H100-H04-${i + 1}`, uuid: `GPU-h100-h04-${i + 1}`, driverVersion: '550.54.15', cudaVersion: '12.4', status: 'healthy' as const, datacenter: 'dc-east', rack: 'R02', memoryGB: 80 })),
  // host-05: 8x AMD MI300X (1 degraded)
  { id: 'gpu-h05-0', serverId: 'host-05', serverName: 'gpu-host-05', index: 0, vendor: 'amd' as const, model: 'MI300X 192GB', serial: 'MI300X-H05-0', uuid: 'GPU-mi300x-h05-0', driverVersion: '', rocmVersion: '6.1.2', status: 'degraded' as const, datacenter: 'dc-west', rack: 'R10', memoryGB: 192 },
  ...Array.from({ length: 7 }, (_, i) => ({ id: `gpu-h05-${i + 1}`, serverId: 'host-05', serverName: 'gpu-host-05', index: i + 1, vendor: 'amd' as const, model: 'MI300X 192GB', serial: `MI300X-H05-${i + 1}`, uuid: `GPU-mi300x-h05-${i + 1}`, driverVersion: '', rocmVersion: '6.1.2', status: 'healthy' as const, datacenter: 'dc-west', rack: 'R10', memoryGB: 192 })),
  // host-06: 8x AMD MI300X
  ...Array.from({ length: 8 }, (_, i) => ({ id: `gpu-h06-${i}`, serverId: 'host-06', serverName: 'gpu-host-06', index: i, vendor: 'amd' as const, model: 'MI300X 192GB', serial: `MI300X-H06-${i}`, uuid: `GPU-mi300x-h06-${i}`, driverVersion: '', rocmVersion: '6.0.0', status: 'healthy' as const, datacenter: 'dc-west', rack: 'R10', memoryGB: 192 })),
  // host-07: 4x NVIDIA A100 40GB (degraded / throttling)
  ...Array.from({ length: 4 }, (_, i) => ({ id: `gpu-h07-${i}`, serverId: 'host-07', serverName: 'gpu-host-07', index: i, vendor: 'nvidia' as const, model: 'A100 40GB', serial: `A100-H07-${i}`, uuid: `GPU-a100-h07-${i}`, driverVersion: '535.104.05', cudaVersion: '12.2', status: 'degraded' as const, datacenter: 'dc-east', rack: 'R03', memoryGB: 40 })),
  // host-08: 4x NVIDIA A100 40GB (critical / overheating)
  ...Array.from({ length: 4 }, (_, i) => ({ id: `gpu-h08-${i}`, serverId: 'host-08', serverName: 'gpu-host-08', index: i, vendor: 'nvidia' as const, model: 'A100 40GB', serial: `A100-H08-${i}`, uuid: `GPU-a100-h08-${i}`, driverVersion: '535.104.05', cudaVersion: '12.2', status: 'critical' as const, datacenter: 'dc-east', rack: 'R03', memoryGB: 40 })),
]

function makeBaseMetrics(gpu: GPUDevice): Omit<GPUMetrics, 'timestamp'> {
  const isCritical = gpu.status === 'critical'
  const isDegraded = gpu.status === 'degraded'
  const isNvidia = gpu.vendor === 'nvidia'
  const isH100 = gpu.model.includes('H100')
  const isMI300X = gpu.model.includes('MI300X')
  return {
    gpuId: gpu.id,
    utilization: isCritical ? 96 : isDegraded ? 72 : 55 + Math.floor(Math.random() * 35),
    memoryUsedMB: Math.floor(gpu.memoryGB * 1024 * (isCritical ? 0.94 : isDegraded ? 0.78 : 0.6 + Math.random() * 0.2)),
    memoryTotalMB: gpu.memoryGB * 1024,
    temperatureC: isCritical ? 92 : isDegraded ? 83 : 50 + Math.floor(Math.random() * 25),
    powerDrawW: isNvidia ? (isH100 ? (isCritical ? 680 : 480) : (isCritical ? 390 : 280)) : isMI300X ? (isCritical ? 690 : 420) : 300,
    powerLimitW: isNvidia ? (isH100 ? 700 : 400) : isMI300X ? 750 : 400,
    fanSpeedPct: isNvidia ? (isCritical ? 95 : isDegraded ? 85 : 60 + Math.floor(Math.random() * 20)) : undefined,
    eccErrors: gpu.id === 'gpu-h04-0' ? 14 : 0,
    xidErrors: gpu.id === 'gpu-h04-0' ? 3 : 0,
    throttling: isCritical || isDegraded,
  }
}

export const seedBaseGPUMetrics: Map<string, Omit<GPUMetrics, 'timestamp'>> = new Map(
  seedGPUDevices.map((gpu) => [gpu.id, makeBaseMetrics(gpu)]),
)

export const seedGPUProfiles: GPUProfile[] = [
  {
    id: 'profile-001',
    gpuId: 'gpu-h03-0',
    gpuModel: 'H100 80GB SXM5',
    serverId: 'host-03',
    serverName: 'gpu-host-03',
    runId: 'run-004',
    missionId: 'mission-004',
    missionName: 'GPU Profiling: Training Job Analysis',
    status: 'completed',
    startedAt: daysAgo(7),
    completedAt: daysAgo(6.99),
    durationMs: 165000,
    summary: {
      topKernels: [
        { name: 'ampere_h16816gemm_256x64_ldg8_relu', durationPct: 38.2, callCount: 14520 },
        { name: 'vectorized_elementwise_kernel', durationPct: 18.7, callCount: 89000 },
        { name: 'void at::native::reduce_kernel', durationPct: 12.1, callCount: 4820 },
        { name: 'nccl_AllReduce', durationPct: 9.4, callCount: 1200 },
        { name: 'cudnn_infer_volta_s884gemm_fp16', durationPct: 7.8, callCount: 6100 },
      ],
      memoryBandwidthGBs: 3174,
      computeUtilizationPct: 87.4,
      memoryUtilizationPct: 91.2,
      rooflineEfficiency: 0.84,
    },
    reportUrl: '/artifacts/h100-profiling-report.html',
  },
  {
    id: 'profile-002',
    gpuId: 'gpu-h05-1',
    gpuModel: 'MI300X 192GB',
    serverId: 'host-05',
    serverName: 'gpu-host-05',
    status: 'completed',
    startedAt: daysAgo(3),
    completedAt: daysAgo(2.99),
    durationMs: 120000,
    summary: {
      topKernels: [
        { name: 'gemm_rocblas_bf16', durationPct: 42.1, callCount: 18200 },
        { name: 'naive_fwd_attention_kernel', durationPct: 15.3, callCount: 2400 },
        { name: 'ScaleKernel', durationPct: 10.8, callCount: 45000 },
        { name: 'AllReduce_rccl', durationPct: 8.2, callCount: 980 },
      ],
      memoryBandwidthGBs: 5427,
      computeUtilizationPct: 78.9,
      memoryUtilizationPct: 83.5,
      rooflineEfficiency: 0.79,
    },
    reportUrl: '/artifacts/mi300x-profiling-report.html',
  },
]
