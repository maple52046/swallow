import type { K8sCluster, SlurmCluster } from '@/domain/plane/types'

const d = (days: number) => new Date(Date.now() - days * 86400000).toISOString()
const h = (hrs: number) => new Date(Date.now() - hrs * 3600000).toISOString()

export const seedK8sClusters: K8sCluster[] = [
  {
    id: 'plane-k8s-prod', name: 'k8s-prod', type: 'kubernetes', status: 'connected',
    endpointRef: 'vault:kv/planes/k8s-prod/endpoint', authRef: 'vault:kv/planes/k8s-prod/kubeconfig',
    labels: ['env:prod', 'region:east'], version: '1.29.2', registeredAt: d(90), lastSyncAt: h(0.1),
    nodeCount: 16, gpuNodeCount: 10,
    nodes: [
      { name: 'control-plane-01', status: 'ready', role: 'control-plane', gpuCount: 0, cpuCount: 64, memoryGB: 256, osImage: 'Rocky Linux 9.3', kubeletVersion: 'v1.29.2' },
      { name: 'control-plane-02', status: 'ready', role: 'control-plane', gpuCount: 0, cpuCount: 64, memoryGB: 256, osImage: 'Rocky Linux 9.3', kubeletVersion: 'v1.29.2' },
      ...Array.from({ length: 8 }, (_, i) => ({ name: `worker-0${i + 1}`, status: 'ready' as const, role: 'worker' as const, gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.29.2' })),
      { name: 'worker-09', status: 'not-ready' as const, role: 'worker' as const, gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.29.2' },
      { name: 'worker-10', status: 'ready' as const, role: 'worker' as const, gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.29.2' },
      { name: 'worker-11', status: 'cordoned' as const, role: 'worker' as const, gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.29.2' },
      { name: 'cpu-worker-01', status: 'ready' as const, role: 'worker' as const, gpuCount: 0, cpuCount: 64, memoryGB: 256, osImage: 'Rocky Linux 9.3', kubeletVersion: 'v1.29.2' },
      { name: 'cpu-worker-02', status: 'ready' as const, role: 'worker' as const, gpuCount: 0, cpuCount: 64, memoryGB: 256, osImage: 'Rocky Linux 9.3', kubeletVersion: 'v1.29.2' },
    ],
    addons: [
      { name: 'NVIDIA Device Plugin', version: '0.14.5', status: 'healthy', latestVersion: '0.16.0' },
      { name: 'GPU Feature Discovery', version: '0.8.2', status: 'healthy' },
      { name: 'Prometheus', version: '25.11.0', status: 'healthy' },
      { name: 'Karpenter', version: '0.33.1', status: 'degraded' },
      { name: 'cert-manager', version: '1.14.2', status: 'healthy' },
    ],
  },
  {
    id: 'plane-k8s-staging', name: 'k8s-staging', type: 'kubernetes', status: 'connected',
    endpointRef: 'vault:kv/planes/k8s-staging/endpoint', authRef: 'vault:kv/planes/k8s-staging/kubeconfig',
    labels: ['env:staging', 'region:west'], version: '1.30.0', registeredAt: d(60), lastSyncAt: h(0.5),
    nodeCount: 6, gpuNodeCount: 4,
    nodes: [
      { name: 'stg-control-01', status: 'ready', role: 'control-plane', gpuCount: 0, cpuCount: 32, memoryGB: 128, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.30.0' },
      { name: 'stg-worker-01', status: 'ready', role: 'worker', gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.30.0' },
      { name: 'stg-worker-02', status: 'ready', role: 'worker', gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.30.0' },
      { name: 'stg-worker-03', status: 'ready', role: 'worker', gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.30.0' },
      { name: 'stg-worker-04', status: 'ready', role: 'worker', gpuCount: 8, cpuCount: 96, memoryGB: 512, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.30.0' },
      { name: 'stg-cpu-01', status: 'ready', role: 'worker', gpuCount: 0, cpuCount: 32, memoryGB: 128, osImage: 'Ubuntu 22.04 LTS', kubeletVersion: 'v1.30.0' },
    ],
    addons: [
      { name: 'NVIDIA Device Plugin', version: '0.16.0', status: 'healthy' },
      { name: 'Prometheus', version: '25.11.0', status: 'healthy' },
      { name: 'cert-manager', version: '1.14.2', status: 'healthy' },
    ],
  },
]

export const seedSlurmClusters: SlurmCluster[] = [
  {
    id: 'plane-slurm-hpc01', name: 'hpc-cluster-01', type: 'slurm', status: 'connected',
    endpointRef: 'vault:kv/planes/slurm-hpc01/endpoint', authRef: 'vault:kv/planes/slurm-hpc01/sshkey',
    labels: ['env:prod', 'type:hpc'], version: '23.11.4', registeredAt: d(120), lastSyncAt: h(0.2),
    totalNodes: 48, idleNodes: 10, allocNodes: 38,
    partitions: [
      { name: 'gpu', state: 'up', nodeCount: 32, idleNodes: 4, allocNodes: 28, totalCPUs: 3072, totalGPUs: 256 },
      { name: 'highmem-gpu', state: 'up', nodeCount: 8, idleNodes: 2, allocNodes: 6, totalCPUs: 768, totalGPUs: 64 },
      { name: 'cpu', state: 'up', nodeCount: 8, idleNodes: 4, allocNodes: 4, totalCPUs: 512, totalGPUs: 0 },
    ],
    recentJobs: [
      { id: '482910', name: 'llm-training-v3', user: 'alice', partition: 'gpu', status: 'running', cpus: 192, gpus: 16, submittedAt: h(6), startedAt: h(5.9) },
      { id: '482909', name: 'model-eval-batch', user: 'bob', partition: 'gpu', status: 'running', cpus: 96, gpus: 8, submittedAt: h(3), startedAt: h(2.8) },
      { id: '482908', name: 'data-preprocess', user: 'charlie', partition: 'cpu', status: 'completed', cpus: 64, gpus: 0, submittedAt: h(8), startedAt: h(7.9), completedAt: h(4) },
      { id: '482907', name: 'inference-bench', user: 'alice', partition: 'highmem-gpu', status: 'pending', cpus: 96, gpus: 8, submittedAt: h(1) },
    ],
  },
]
