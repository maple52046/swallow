import { useState } from 'react'
import { Divider, NavLink, ScrollArea, Stack, Text, ThemeIcon, Collapse, UnstyledButton, Group, Badge } from '@mantine/core'
import { useLocation, useNavigate } from 'react-router-dom'
import {
  IconLayoutDashboard, IconRocket, IconPlayerPlay, IconCpu, IconAlertTriangle,
  IconChartBar, IconServer, IconBuildingWarehouse, IconTerminal2,
  IconNetwork, IconDatabase, IconServer2, IconSitemap, IconBrain,
  IconPuzzle, IconClipboardList, IconChevronDown, IconChevronRight,
  IconPlus, IconList, IconUsers, IconBuildingCommunity, IconArchive,
} from '@tabler/icons-react'
import { t } from '@/presentation/app/i18n'
import { useAuth, type UserRole } from '@/presentation/contexts/AuthContext'

interface NavItem {
  label: string
  path?: string
  icon?: React.ReactNode
  children?: NavItem[]
  badge?: string
  allowedRoles?: UserRole[]
  divider?: boolean
}

const NAV: NavItem[] = [
  { label: t('nav.overview'), path: '/', icon: <IconLayoutDashboard size={16} /> },
  { label: '__div1__', divider: true },
  { label: 'Servers', path: '/servers', icon: <IconServer size={16} /> },
  { label: 'Datacenter', path: '/datacenter', icon: <IconBuildingWarehouse size={16} /> },
  { label: t('nav.provisioning'), path: '/provisioning', icon: <IconBuildingWarehouse size={16} /> },
  { label: '__div2__', divider: true },
  { label: t('nav.alerts'), path: '/alerts', icon: <IconAlertTriangle size={16} />, badge: 'hot' },
  { label: t('nav.analysis'), path: '/analysis', icon: <IconChartBar size={16} /> },
  { label: '__div5__', divider: true },
  {
    label: t('nav.managementPlanes'), icon: <IconSitemap size={16} />,
    children: [
      { label: t('nav.addPlane'), path: '/planes/new', icon: <IconPlus size={14} /> },
      { label: t('nav.kubernetes'), path: '/planes/kubernetes', icon: <IconServer2 size={14} /> },
      { label: t('nav.slurm'), path: '/planes/slurm', icon: <IconServer2 size={14} /> },
    ],
  },
  { label: '__div3__', divider: true },
  { label: 'Teams', path: '/teams', icon: <IconBuildingCommunity size={16} />, allowedRoles: ['admin'] },
  { label: 'Users', path: '/users', icon: <IconUsers size={16} />, allowedRoles: ['admin'] },
  ...(import.meta.env.DEV ? [
    { label: '__div4__', divider: true },
    {
      label: 'Deprecated', icon: <IconArchive size={16} />,
      children: [
        {
          label: t('nav.datacenter'), icon: <IconBuildingWarehouse size={16} />,
          children: [
            { label: t('nav.inventory'), path: '/deprecated/datacenter/inventory', icon: <IconServer size={14} /> },
            { label: t('nav.ipmi'), path: '/deprecated/datacenter/ipmi', icon: <IconTerminal2 size={14} /> },
            { label: t('nav.networking'), path: '/deprecated/datacenter/networking', icon: <IconNetwork size={14} /> },
            { label: t('nav.storage'), path: '/deprecated/datacenter/storage', icon: <IconDatabase size={14} /> },
          ],
        },
        {
          label: t('nav.observability'), icon: <IconCpu size={16} />,
          children: [
            { label: t('nav.gpuMetrics'), path: '/deprecated/observability/gpu-metrics', icon: <IconCpu size={14} /> },
            { label: t('nav.gpuProfiling'), path: '/deprecated/observability/gpu-profiling', icon: <IconChartBar size={14} /> },
            { label: t('nav.dashboards'), path: '/deprecated/observability/dashboards', icon: <IconChartBar size={14} /> },
          ],
        },
        {
          label: t('nav.platform'), icon: <IconBrain size={16} />,
          children: [
            { label: t('nav.agents'), path: '/deprecated/platform/agents', icon: <IconBrain size={14} /> },
            { label: t('nav.models'), path: '/deprecated/platform/models', icon: <IconBrain size={14} /> },
            { label: t('nav.plugins'), path: '/deprecated/platform/plugins', icon: <IconPuzzle size={14} /> },
            { label: t('nav.auditLog'), path: '/deprecated/platform/audit', icon: <IconClipboardList size={14} /> },
          ],
        },
        {
          label: t('nav.missions'), icon: <IconRocket size={16} />,
          children: [
            { label: t('nav.allMissions'), path: '/deprecated/missions', icon: <IconList size={14} /> },
            { label: t('nav.newMission'), path: '/deprecated/missions/new', icon: <IconPlus size={14} /> },
          ],
        },
        { label: t('nav.runs'), path: '/deprecated/runs', icon: <IconPlayerPlay size={16} /> },
      ],
    },
  ] as NavItem[] : []),
]

function isRoleAllowed(item: NavItem, role: UserRole | undefined) {
  if (!item.allowedRoles || item.allowedRoles.length === 0) return true
  if (!role) return false
  return item.allowedRoles.includes(role)
}

function filterNavByRole(items: NavItem[], role: UserRole | undefined): NavItem[] {
  return items
    .filter((item) => item.divider || isRoleAllowed(item, role))
    .map((item) => {
      if (!item.children) return item
      const filteredChildren = filterNavByRole(item.children, role)
      return { ...item, children: filteredChildren }
    })
    .filter((item) => !item.children || item.children.length > 0 || !!item.path || !!item.divider)
}

function NavSection({ item, depth = 0 }: { item: NavItem; depth?: number }) {
  const location = useLocation()
  const navigate = useNavigate()

  const isActive = item.path
    ? item.path === '/'
      ? location.pathname === '/'
      : location.pathname.startsWith(item.path)
    : false

  const isChildActive = item.children?.some((c) =>
    c.path ? (c.path === '/' ? location.pathname === '/' : location.pathname.startsWith(c.path)) : false,
  )

  const [open, setOpen] = useState(isChildActive ?? false)

  if (item.children) {
    return (
      <div>
        <UnstyledButton
          onClick={() => setOpen((o) => !o)}
          w="100%"
          px="sm"
          py={6}
          style={{ display: 'flex', alignItems: 'center', borderRadius: 6, gap: 8 }}
        >
          <Group gap={6} flex={1}>
            <ThemeIcon variant="transparent" size="sm" c={isChildActive ? 'blue' : 'dimmed'}>
              {item.icon}
            </ThemeIcon>
            <Text size="sm" fw={isChildActive ? 600 : 400} c={isChildActive ? undefined : 'dimmed'}>
              {item.label}
            </Text>
          </Group>
          <ThemeIcon variant="transparent" size="xs" c="dimmed">
            {open ? <IconChevronDown size={12} /> : <IconChevronRight size={12} />}
          </ThemeIcon>
        </UnstyledButton>
        <Collapse in={open}>
          <Stack gap={0} pl="sm">
            {item.children.map((child) => (
              <NavSection key={child.label} item={child} depth={depth + 1} />
            ))}
          </Stack>
        </Collapse>
      </div>
    )
  }

  return (
    <NavLink
      label={
        <Group gap={4}>
          <Text size="sm">{item.label}</Text>
          {item.badge && <Badge size="xs" color="red" variant="filled">!</Badge>}
        </Group>
      }
      leftSection={
        <ThemeIcon variant="transparent" size="sm" c={isActive ? 'blue' : 'dimmed'}>
          {item.icon}
        </ThemeIcon>
      }
      active={isActive}
      onClick={() => item.path && navigate(item.path)}
      styles={{ root: { borderRadius: 6 } }}
    />
  )
}

export function SideNav() {
  const { currentUser } = useAuth()
  const visibleNav = filterNavByRole(NAV, currentUser?.role)

  return (
    <ScrollArea h="100%" type="scroll">
      <Stack gap={2} p="xs">
        {visibleNav.map((item) =>
          item.divider ? (
            <Divider key={item.label} my={4} />
          ) : (
            <NavSection key={item.label} item={item} />
          ),
        )}
      </Stack>
    </ScrollArea>
  )
}
