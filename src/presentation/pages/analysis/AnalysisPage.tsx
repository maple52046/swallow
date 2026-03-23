import { useEffect, useMemo, useState } from 'react'
import {
  Badge, Card, Group, Progress, SegmentedControl, Select, SimpleGrid,
  Tabs, Text, ThemeIcon, Table,
  useMantineColorScheme,
} from '@mantine/core'
import { AreaChart } from '@mantine/charts'
import {
  IconActivity, IconBrain, IconBuildingCommunity, IconDatabase,
  IconNetwork, IconServer,
} from '@tabler/icons-react'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import type { Team } from '@/domain/team/types'

// ─── Types ────────────────────────────────────────────────────────────────────

type TimeRange = '24h' | '7d' | '30d' | '90d'
type RankMetric = 'GPU%' | 'RAM%' | 'Network MB/s'

type TrendPoint = { t: string; v: number }

interface SummaryMetrics {
  avgGpuUtil: number
  peakGpuUtil: number
  avgRam: number
  peakRam: number
  avgNetwork: number
  activeServers: number
}

interface RankEntry {
  name: string
  gpu: number
  ram: number
  network: number
}

interface WorkloadEntry {
  name: string
  gpu: number
  ram: number
  node: string
}

// ─── Mock data ────────────────────────────────────────────────────────────────

const RANGE_SEED: Record<TimeRange, number> = { '24h': 0, '7d': 3, '30d': 9, '90d': 17 }

function hashSeed(s: string): number {
  return s.split('').reduce((acc, c) => (acc * 31 + c.charCodeAt(0)) & 0xffff, 0)
}

function pseudoRand(seed: number): number {
  const x = Math.sin(seed + 1) * 10000
  return x - Math.floor(x)
}

interface RangeConfig {
  points: number
  stepMs: number
  fmt: (d: Date) => string
}

const RANGE_CONFIG: Record<TimeRange, RangeConfig> = {
  '24h': {
    points: 144,
    stepMs: 10 * 60 * 1000,
    fmt: (d) => `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`,
  },
  '7d': {
    points: 168,
    stepMs: 60 * 60 * 1000,
    fmt: (d) => `${d.getMonth() + 1}/${d.getDate()} ${d.getHours()}h`,
  },
  '30d': {
    points: 120,
    stepMs: 6 * 60 * 60 * 1000,
    fmt: (d) => `${d.getMonth() + 1}/${d.getDate()}`,
  },
  '90d': {
    points: 90,
    stepMs: 24 * 60 * 60 * 1000,
    fmt: (d) => `${d.getMonth() + 1}/${d.getDate()}`,
  },
}

function genTrend(scope: string, range: TimeRange, base: number, amplitude: number): TrendPoint[] {
  const { points, stepMs, fmt } = RANGE_CONFIG[range]
  const seed = hashSeed(scope) + RANGE_SEED[range]
  const now = Date.now()
  return Array.from({ length: points }, (_, i) => {
    const d = new Date(now - (points - i) * stepMs)
    const wave = Math.sin((i + seed) * 0.18) * amplitude * 0.6
    const noise = pseudoRand(seed + i * 1.7) * amplitude * 0.4
    const v = Math.round(Math.max(0, Math.min(100, base + wave + noise - amplitude * 0.2)))
    return { t: fmt(d), v }
  })
}

function genSummary(scope: string, range: TimeRange): SummaryMetrics {
  const s = hashSeed(scope) + RANGE_SEED[range]
  const r = (off: number, min: number, max: number) => Math.round(min + pseudoRand(s + off) * (max - min))
  return {
    avgGpuUtil: r(1, 35, 78),
    peakGpuUtil: r(2, 75, 99),
    avgRam: r(3, 40, 72),
    peakRam: r(4, 70, 95),
    avgNetwork: r(5, 150, 850),
    activeServers: r(6, 4, 24),
  }
}

const SERVER_NAMES = ['gpu-node-01', 'gpu-node-02', 'gpu-node-03', 'compute-01', 'compute-02', 'gpu-node-04', 'gpu-node-05', 'compute-03']
const WORKLOAD_NAMES = ['llm-train-7b', 'inference-api', 'jupyter-lab', 'batch-eval', 'finetune-3b', 'embedding-svc', 'sdxl-render', 'test-runner']

function genRanking(
  scope: string,
  range: TimeRange,
  type: 'teams' | 'servers' | 'workloads',
  teamNames: string[],
): RankEntry[] {
  const names = type === 'teams' ? teamNames : type === 'servers' ? SERVER_NAMES : WORKLOAD_NAMES
  const s = hashSeed(scope) + RANGE_SEED[range]
  return names
    .map((name, i) => ({
      name,
      gpu: Math.round(pseudoRand(s + i * 3 + 1) * 90 + 5),
      ram: Math.round(pseudoRand(s + i * 3 + 2) * 80 + 10),
      network: Math.round(pseudoRand(s + i * 3 + 3) * 900 + 50),
    }))
    .sort((a, b) => b.gpu - a.gpu)
}

function genWorkloads(scope: string, range: TimeRange): WorkloadEntry[] {
  const s = hashSeed(scope) + RANGE_SEED[range]
  return WORKLOAD_NAMES.map((name, i) => ({
    name,
    gpu: Math.round(pseudoRand(s + i * 4 + 1) * 95 + 2),
    ram: Math.round(pseudoRand(s + i * 4 + 2) * 60 + 4),
    node: SERVER_NAMES[i % SERVER_NAMES.length],
  })).sort((a, b) => b.gpu - a.gpu)
}

// ─── Chart theme ──────────────────────────────────────────────────────────────

interface ChartColors {
  axisText: string
  gridColor: string
}

function useChartColors(): ChartColors {
  const { colorScheme } = useMantineColorScheme()
  const isDark = colorScheme === 'dark'
  return {
    axisText: isDark ? '#C1C2C5' : '#495057',
    gridColor: isDark ? '#373A40' : '#DEE2E6',
  }
}

// ─── Sub-components ───────────────────────────────────────────────────────────

function SummaryCard({ label, value, unit, icon, color }: {
  label: string
  value: string | number
  unit?: string
  icon: React.ReactNode
  color: string
}) {
  return (
    <Card withBorder p="md">
      <Group gap="xs" mb={4}>
        <ThemeIcon variant="light" color={color} size="sm">{icon}</ThemeIcon>
        <Text size="xs" c="dimmed">{label}</Text>
      </Group>
      <Group align="baseline" gap={4}>
        <Text size="xl" fw={700}>{value}</Text>
        {unit && <Text size="xs" c="dimmed">{unit}</Text>}
      </Group>
    </Card>
  )
}

const TICK_EVERY: Record<TimeRange, number> = { '24h': 12, '7d': 12, '30d': 10, '90d': 9 }

function TrendCard({ label, color, data, icon, range, chartColors }: {
  label: string
  color: string
  data: TrendPoint[]
  icon: React.ReactNode
  range: TimeRange
  chartColors: ChartColors
}) {
  const tick = TICK_EVERY[range]
  const tickData = data.filter((_, i) => i % tick === 0)
  return (
    <Card withBorder radius="md">
      <Group gap="xs" mb="xs">
        <ThemeIcon variant="light" color={color} size="sm">{icon}</ThemeIcon>
        <Text size="sm" fw={500}>{label}</Text>
      </Group>
      <AreaChart
        h={120}
        data={tickData}
        dataKey="t"
        series={[{ name: 'v', color }]}
        withDots={false}
        withXAxis
        withYAxis={false}
        gridAxis="x"
        fillOpacity={0.15}
        curveType="natural"
        xAxisProps={{
          tick: { fontSize: 10, fill: chartColors.axisText },
          axisLine: { stroke: chartColors.gridColor },
          tickLine: { stroke: chartColors.gridColor },
        }}
      />
    </Card>
  )
}

const RANK_METRIC_KEY: Record<RankMetric, keyof RankEntry> = {
  'GPU%': 'gpu',
  'RAM%': 'ram',
  'Network MB/s': 'network',
}

const RANK_BADGE_COLOR = ['yellow', 'gray', 'orange']

function RankingTable({ entries, metricLabel }: { entries: RankEntry[]; metricLabel: RankMetric }) {
  const key = RANK_METRIC_KEY[metricLabel]
  const sorted = [...entries].sort((a, b) => (b[key] as number) - (a[key] as number))
  return (
    <Table fz="sm" withRowBorders highlightOnHover>
      <Table.Thead>
        <Table.Tr>
          <Table.Th w={40}>#</Table.Th>
          <Table.Th>Name</Table.Th>
          <Table.Th>GPU%</Table.Th>
          <Table.Th>RAM%</Table.Th>
          <Table.Th w={120}>Network MB/s</Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {sorted.map((e, i) => (
          <Table.Tr key={e.name}>
            <Table.Td>
              <Badge size="xs" variant="outline" color={RANK_BADGE_COLOR[i] ?? 'default'}>
                {i + 1}
              </Badge>
            </Table.Td>
            <Table.Td fw={500}>{e.name}</Table.Td>
            <Table.Td>
              <Group gap={6} wrap="nowrap">
                <Progress value={e.gpu} color={e.gpu > 80 ? 'red' : e.gpu > 60 ? 'yellow' : 'blue'} size="xs" style={{ flex: 1 }} />
                <Text size="xs" w={30} ta="right">{e.gpu}%</Text>
              </Group>
            </Table.Td>
            <Table.Td>
              <Group gap={6} wrap="nowrap">
                <Progress value={e.ram} color={e.ram > 80 ? 'red' : 'teal'} size="xs" style={{ flex: 1 }} />
                <Text size="xs" w={30} ta="right">{e.ram}%</Text>
              </Group>
            </Table.Td>
            <Table.Td>{e.network}</Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  )
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export function AnalysisPage() {
  const { currentUser, users } = useAuth()
  const { teams: teamsUC } = useApp()
  const chartColors = useChartColors()

  const [teamList, setTeamList] = useState<Team[]>([])
  useEffect(() => {
    teamsUC.list.execute().then(setTeamList).catch(() => {})
  }, [teamsUC])

  const scopeOptions = useMemo(() => {
    const role = currentUser?.role
    if (role === 'admin') {
      return [
        { value: 'global', label: 'Global' },
        ...teamList.map((t) => ({ value: `team:${t.id}`, label: `Team: ${t.name}` })),
        ...users.map((u) => ({ value: `user:${u.id}`, label: `User: ${u.displayName}` })),
      ]
    }
    if (role === 'owner') {
      const myTeams = teamList.filter((t) => currentUser && t.ownerIds.includes(currentUser.id))
      return [
        ...myTeams.map((t) => ({ value: `team:${t.id}`, label: `Team: ${t.name}` })),
        { value: `user:${currentUser?.id ?? 'self'}`, label: 'My Usage' },
      ]
    }
    return [{ value: `user:${currentUser?.id ?? 'self'}`, label: 'My Usage' }]
  }, [currentUser, teamList, users])

  const [scope, setScope] = useState<string>('')
  const [range, setRange] = useState<TimeRange>('7d')
  const [rankingMetric, setRankingMetric] = useState<RankMetric>('GPU%')

  // Keep scope in sync when options load (e.g. after teams fetch)
  useEffect(() => {
    if (scopeOptions.length > 0 && (!scope || !scopeOptions.find((o) => o.value === scope))) {
      setScope(scopeOptions[0].value)
    }
  }, [scopeOptions, scope])

  const teamNames = useMemo(() => teamList.map((t) => t.name), [teamList])

  const summary = useMemo(() => genSummary(scope, range), [scope, range])
  const gpuTrend = useMemo(() => genTrend(scope, range, 62, 28), [scope, range])
  const gpuMemTrend = useMemo(() => genTrend(scope, range, 55, 22), [scope, range])
  const ramTrend = useMemo(() => genTrend(scope, range, 58, 18), [scope, range])
  const networkTrend = useMemo(() => genTrend(scope, range, 45, 35), [scope, range])
  const teamRanking = useMemo(() => genRanking(scope, range, 'teams', teamNames), [scope, range, teamNames])
  const serverRanking = useMemo(() => genRanking(scope, range, 'servers', teamNames), [scope, range, teamNames])
  const workloadRanking = useMemo(() => genRanking(scope, range, 'workloads', teamNames), [scope, range, teamNames])
  const workloads = useMemo(() => genWorkloads(scope, range), [scope, range])

  return (
    <>
      <PageHeader
        title="Analysis"
        subtitle="Resource usage across your infrastructure"
        actions={
          <Group gap="sm">
            <Select
              size="xs"
              data={scopeOptions}
              value={scope || null}
              onChange={(v) => v && setScope(v)}
              w={180}
              placeholder="Loading..."
            />
            <SegmentedControl
              size="xs"
              value={range}
              onChange={(v) => setRange(v as TimeRange)}
              data={[
                { label: '24h', value: '24h' },
                { label: '7d', value: '7d' },
                { label: '30d', value: '30d' },
                { label: '90d', value: '90d' },
              ]}
            />
          </Group>
        }
      />

      <SimpleGrid cols={{ base: 2, sm: 3, lg: 6 }} mb="lg">
        <SummaryCard label="Avg GPU Util" value={summary.avgGpuUtil} unit="%" icon={<IconBrain size={12} />} color="violet" />
        <SummaryCard label="Peak GPU Util" value={summary.peakGpuUtil} unit="%" icon={<IconBrain size={12} />} color="grape" />
        <SummaryCard label="Avg RAM" value={summary.avgRam} unit="%" icon={<IconDatabase size={12} />} color="blue" />
        <SummaryCard label="Peak RAM" value={summary.peakRam} unit="%" icon={<IconDatabase size={12} />} color="indigo" />
        <SummaryCard label="Avg Network" value={summary.avgNetwork} unit="MB/s" icon={<IconNetwork size={12} />} color="teal" />
        <SummaryCard label="Active Servers" value={summary.activeServers} icon={<IconServer size={12} />} color="green" />
      </SimpleGrid>

      <Text size="xs" fw={600} c="dimmed" tt="uppercase" mb="xs" style={{ letterSpacing: '0.06em' }}>
        Resource Trends
      </Text>
      <SimpleGrid cols={{ base: 1, sm: 2 }} mb="lg">
        <TrendCard label="GPU Utilization" color="violet" data={gpuTrend} icon={<IconBrain size={12} />} range={range} chartColors={chartColors} />
        <TrendCard label="GPU Memory" color="grape" data={gpuMemTrend} icon={<IconBrain size={12} />} range={range} chartColors={chartColors} />
        <TrendCard label="RAM Usage" color="blue" data={ramTrend} icon={<IconDatabase size={12} />} range={range} chartColors={chartColors} />
        <TrendCard label="Network" color="teal" data={networkTrend} icon={<IconNetwork size={12} />} range={range} chartColors={chartColors} />
      </SimpleGrid>

      <Text size="xs" fw={600} c="dimmed" tt="uppercase" mb="xs" style={{ letterSpacing: '0.06em' }}>
        Top Ranking
      </Text>
      <Card withBorder mb="lg" p="md">
        <Tabs defaultValue="servers">
          <Group justify="space-between" mb="md">
            <Tabs.List>
              <Tabs.Tab value="teams" leftSection={<IconBuildingCommunity size={14} />}>Teams</Tabs.Tab>
              <Tabs.Tab value="servers" leftSection={<IconServer size={14} />}>Servers</Tabs.Tab>
              <Tabs.Tab value="workloads" leftSection={<IconActivity size={14} />}>Workloads</Tabs.Tab>
            </Tabs.List>
            <Select
              size="xs"
              data={['GPU%', 'RAM%', 'Network MB/s']}
              value={rankingMetric}
              onChange={(v) => v && setRankingMetric(v as RankMetric)}
              w={130}
            />
          </Group>
          <Tabs.Panel value="teams">
            <RankingTable entries={teamRanking} metricLabel={rankingMetric} />
          </Tabs.Panel>
          <Tabs.Panel value="servers">
            <RankingTable entries={serverRanking} metricLabel={rankingMetric} />
          </Tabs.Panel>
          <Tabs.Panel value="workloads">
            <RankingTable entries={workloadRanking} metricLabel={rankingMetric} />
          </Tabs.Panel>
        </Tabs>
      </Card>

      <Text size="xs" fw={600} c="dimmed" tt="uppercase" mb="xs" style={{ letterSpacing: '0.06em' }}>
        Workloads
      </Text>
      <Card withBorder>
        <Table fz="sm" withRowBorders highlightOnHover>
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Container</Table.Th>
              <Table.Th w={160}>GPU Usage</Table.Th>
              <Table.Th w={160}>RAM Usage</Table.Th>
              <Table.Th>Node</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {workloads.map((w) => (
              <Table.Tr key={w.name}>
                <Table.Td fw={500} ff="mono" fz="xs">{w.name}</Table.Td>
                <Table.Td>
                  <Group gap={6} wrap="nowrap">
                    <Progress value={w.gpu} color={w.gpu > 80 ? 'red' : w.gpu > 60 ? 'yellow' : 'violet'} size="xs" style={{ flex: 1 }} />
                    <Text size="xs" w={32} ta="right">{w.gpu}%</Text>
                  </Group>
                </Table.Td>
                <Table.Td>
                  <Group gap={6} wrap="nowrap">
                    <Progress value={w.ram} color={w.ram > 80 ? 'red' : 'blue'} size="xs" style={{ flex: 1 }} />
                    <Text size="xs" w={32} ta="right">{w.ram}%</Text>
                  </Group>
                </Table.Td>
                <Table.Td c="dimmed" fz="xs" ff="mono">{w.node}</Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Card>
    </>
  )
}
