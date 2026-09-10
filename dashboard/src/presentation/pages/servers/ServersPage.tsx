import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  Alert,
  AlertActionLink,
  AlertVariant,
  Badge,
  Button,
  Checkbox,
  Dropdown,
  DropdownItem,
  DropdownList,
  FormSelect,
  FormSelectOption,
  Label,
  MenuToggle,
  Popover,
  SearchInput,
  ToolbarGroup,
  ToolbarItem,
  Tooltip,
} from "@patternfly/react-core";
import {
  AngleDownIcon,
  AngleRightIcon,
  ColumnsIcon,
  CloudUploadAltIcon,
  EllipsisVIcon,
  FilterIcon,
  SortAmountDownIcon,
  SortAmountUpIcon,
} from "@patternfly/react-icons";
import { Lock } from "lucide-react";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useNavigate } from "react-router-dom";
import { useApp } from "@/di/AppProvider";
import { refreshServerProjections } from "@/application/usecases/servers/refreshServerProjections";
import type { Integration } from "@/domain/site/types";
import type {
  ReleaseServerInput,
  Server,
} from "@/domain/server/types";
import {
  EMPTY_SERVER_FILTERS,
  compareServers,
  countByDimension,
  groupValueOf,
  matchesServerFilters,
  serverDisplayName,
  serverMacAddress,
  serverPrimaryAddress,
  type ServerDimension,
  type ServerFilters,
  type ServerGroupBy,
  type ServerSortKey,
  type SortDirection,
} from "@/domain/server/list";
import { PageHeader } from "@/presentation/components/PageHeader";
import {
  DataToolbar,
  StickyTableFrame,
} from "@/presentation/components/OperatorPrimitives";
import { GpuVendorLogo } from "@/presentation/components/GpuVendorLogo";
import { LoadingState } from "@/presentation/components/LoadingState";
import { EmptyState } from "@/presentation/components/EmptyState";
import { ErrorState } from "@/presentation/components/ErrorState";
import { Pagination } from "@/presentation/components/Pagination";
import {
  DeploymentBadge,
  HealthBadge,
  MembershipBadge,
  PowerBadge,
} from "@/presentation/components/AxisBadge";
import { powerStateLabel } from "@/presentation/components/axisBadgeUtils";
import { CopyButton } from "@/presentation/components/CopyButton";
import { useSiteScope } from "@/presentation/contexts/SiteScopeContext";
import {
  SERVER_ACTION_GROUPS,
  actionLabel,
  serverActionAvailability,
  type BulkAction,
  type ServerMenuAction,
} from "./serverActions";
import { ServerLockDialog } from "./ServerLockDialog";
import { ServerDeleteDialog } from "./ServerDeleteDialog";
import { ServerReleaseDialog } from "./ServerReleaseDialog";
import { ServerPowerDialog } from "./ServerPowerDialog";
import { useServerWorkingSet } from "./useServerWorkingSet";
import { useServerBulkActions } from "./useServerBulkActions";
import { ServerActionResultDialog } from "./ServerActionResultDialog";
import {
  failedServerActionOutcomes,
  type ServerActionRunResult,
  type ServerActionTarget,
} from "./serverActionResults";
/** Table row density for the server list; controls compact vs comfortable row spacing. */
type ServerDensity = "compact" | "comfortable";

interface ColumnToggle {
  key: string;
  label: string;
}
interface FilterOption {
  value: string;
  label: string;
  count: number;
}
interface ServerFilterOptions {
  provisioningState: FilterOption[];
  zone: FilterOption[];
  pool: FilterOption[];
  tag: FilterOption[];
}

/** Independent provider observations that replace the former composed Hardware cell. */
const HARDWARE_COLUMNS: ColumnToggle[] = [
  { key: "architecture", label: "Architecture" },
  { key: "cpuCores", label: "CPU cores" },
  { key: "cpuModel", label: "CPU model" },
  { key: "memory", label: "Memory" },
  { key: "storage", label: "Storage" },
  { key: "systemVendor", label: "System vendor" },
  { key: "systemProduct", label: "System product" },
];
const OPTIONAL_COLUMNS: ColumnToggle[] = [
  { key: "power", label: "Power" },
  { key: "status", label: "Deployment" },
  { key: "address", label: "Address" },
  { key: "mac", label: "MAC address" },
  { key: "zone", label: "Zone" },
  { key: "pool", label: "Pool" },
  { key: "tags", label: "Tags" },
  ...HARDWARE_COLUMNS,
  { key: "gpus", label: "GPUs" },
  { key: "platform", label: "Platform" },
  { key: "health", label: "Health" },
];
const DEFAULT_PAGE_SIZE = 50;
const EMPTY_SERVERS: Server[] = [];
const GROUP_KEY = "swallow.servers.group-by";
const COLUMNS_KEY = "swallow.servers.hidden-columns";
const PAGE_SIZE_KEY = "swallow.servers.page-size";
const DEPLOYMENT_POLL_INTERVAL_MS = 2_000;
const MAX_DEPLOYMENT_POLL_ATTEMPTS = 150;
// How long to keep polling a just-released Server that is not yet in an active provisioning
// axis, so the list reflects the release once the durable Operation dispatches.
const RELEASE_FOLLOW_WINDOW_MS = 180_000;

function readPreference<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw === null ? fallback : (JSON.parse(raw) as T);
  } catch {
    return fallback;
  }
}
function writePreference<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Browser policy can block persistence; the active view remains fully usable.
  }
}
function toFilterOptions(counts: Map<string, number>): FilterOption[] {
  return [...counts.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([value, count]) => ({ value, label: value, count }));
}
function filtersAreEmpty(filters: ServerFilters, keyword: string): boolean {
  return (
    keyword === "" &&
    filters.provisioningStates.length === 0 &&
    filters.zones.length === 0 &&
    filters.pools.length === 0 &&
    filters.tags.length === 0 &&
    filters.hasGpu === null &&
    filters.lockState === "any"
  );
}

/** Preserves visibility choices when a composed legacy column becomes independent fields. */
function normalizeHiddenColumns(columns: readonly string[]): string[] {
  const normalized = new Set(columns);
  if (normalized.delete("placement")) {
    normalized.add("zone");
    normalized.add("pool");
  }
  if (normalized.delete("hardware")) {
    HARDWARE_COLUMNS.forEach((column) => normalized.add(column.key));
  }
  return [...normalized];
}

/** Uses one quiet placeholder for provider text that has not been observed. */
function textOrDash(value: string | null | undefined): string {
  return value?.trim() || "-";
}

/** Formats positive hardware quantities while treating zero as an absent observation. */
function quantityOrDash(value: number, unit = "", divisor = 1): string {
  if (!Number.isFinite(value) || value <= 0) return "-";
  const quantity = Math.round(value / divisor);
  return unit ? `${quantity} ${unit}` : String(quantity);
}

interface RenderGroup {
  key: string;
  label: string;
  items: Server[];
}
function renderGroups(items: Server[], groupBy: ServerGroupBy): RenderGroup[] {
  if (groupBy === "none") return [{ key: "", label: "", items }];
  const groups = new Map<string, Server[]>();
  for (const server of items) {
    const key = groupValueOf(server, groupBy);
    groups.set(key, [...(groups.get(key) ?? []), server]);
  }
  return [...groups.entries()].map(([key, grouped]) => ({
    key,
    label: key,
    items: grouped,
  }));
}

/**
 * NetBox-style fleet inventory with MAAS lifecycle axes and selection-driven actions.
 * All API pages are loaded before client grouping/filtering, so counts, saved views, and
 * select-all operate over the complete scoped working set rather than the first 100 rows.
 */
export function ServersPage() {
  const { sites, servers } = useApp();
  const navigate = useNavigate();
  const { siteId, scopedHref } = useSiteScope();
  const bulk = useServerBulkActions();
  const [searchInput, setSearchInput] = useState("");
  const [coarseKeyword, setCoarseKeyword] = useState("");
  const [includeAbsent, setIncludeAbsent] = useState(false);
  const [filters, setFilters] = useState<ServerFilters>(EMPTY_SERVER_FILTERS);
  const [groupBy, setGroupBy] = useState<ServerGroupBy>(() =>
    readPreference(GROUP_KEY, "none"),
  );
  const [sortKey, setSortKey] = useState<ServerSortKey>("name");
  const [sortDir, setSortDir] = useState<SortDirection>("asc");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(() =>
    readPreference(PAGE_SIZE_KEY, DEFAULT_PAGE_SIZE),
  );
  const [density, setDensity] = useState<ServerDensity>("compact");
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [collapsedGroups, setCollapsedGroups] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const [hiddenColumns, setHiddenColumns] = useState<ReadonlySet<string>>(
    () =>
      new Set(
        normalizeHiddenColumns(readPreference<string[]>(COLUMNS_KEY, [])),
      ),
  );
  const [provisioners, setProvisioners] = useState<Integration[]>([]);
  const [deleteTarget, setDeleteTarget] = useState<Server | null>(null);
  const [releaseTargets, setReleaseTargets] = useState<
    ServerActionTarget[] | null
  >(null);
  const [lastActionResult, setLastActionResult] =
    useState<ServerActionRunResult | null>(null);
  const [resultDialogOpen, setResultDialogOpen] = useState(false);
  const [pendingLockAction, setPendingLockAction] = useState<{
    action: "lock" | "unlock";
    targets: readonly Server[];
    skipped: readonly Server[];
  } | null>(null);

  useEffect(() => {
    const id = setTimeout(() => {
      setCoarseKeyword(searchInput);
      setPage(1);
    }, 300);
    return () => clearTimeout(id);
  }, [searchInput]);
  const query = useMemo(
    () => ({ siteId, keyword: coarseKeyword || undefined, includeAbsent }),
    [coarseKeyword, includeAbsent, siteId],
  );
  const { state, reload } = useServerWorkingSet(query);
  // Servers whose release we just accepted. A durable release Operation runs
  // asynchronously, so the Server is not yet in an active provisioning axis and the
  // active-projection poll below will not pick it up. Follow these Servers for a bounded
  // window so the list reflects the release (deployed -> releasing -> ready) in place,
  // without the operator manually refreshing.
  const [followedServerIds, setFollowedServerIds] = useState<readonly string[]>([]);
  useEffect(() => {
    let cancelled = false;
    sites
      .listIntegrations({ siteId, kind: "provisioner" })
      .then((items) => {
        if (!cancelled) setProvisioners(items);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [siteId, sites]);

  const workingSet =
    state.status === "ready" ? state.data.servers : EMPTY_SERVERS;
  const activeProjectionTargetKey = useMemo(
    () =>
      workingSet
        .filter((server) =>
          ["deploying", "releasing", "commissioning", "testing"].includes(
            server.provisioning?.state ?? "",
          ) || ["deploying", "verifying"].includes(server.deployment?.state ?? ""),
        )
        .map((server) => server.id)
        .sort()
        .join(","),
    [workingSet],
  );
  // The set of Servers to poll: those already in an active axis, plus recently released
  // Servers we are following until the durable Operation moves them into one. Deduped so a
  // Server that becomes active while followed is polled once, not twice.
  const pollTargetKey = useMemo(() => {
    const active = activeProjectionTargetKey
      ? activeProjectionTargetKey.split(",")
      : [];
    return Array.from(new Set([...active, ...followedServerIds]))
      .sort()
      .join(",");
  }, [activeProjectionTargetKey, followedServerIds]);
  useEffect(() => {
    if (!pollTargetKey) return;
    const targetIds = pollTargetKey.split(",");
    let cancelled = false;
    let attempts = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
      // Nudge the backend to re-observe each active Server live; the resulting projection
      // write is delivered back through the SSE stream and patched into the list by
      // useServerWorkingSet, so this no longer refetches the whole list.
      await refreshServerProjections(servers, targetIds);
      if (cancelled) return;
      attempts += 1;
      if (attempts < MAX_DEPLOYMENT_POLL_ATTEMPTS) {
        timer = setTimeout(() => void tick(), DEPLOYMENT_POLL_INTERVAL_MS);
      }
    };
    void tick();
    return () => {
      cancelled = true;
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [pollTargetKey, servers]);
  // Stop following released Servers after a bounded window. A followed Server keeps being
  // polled (above) even while it is not in an active axis; once the window elapses it drops
  // out of the poll set whether or not it ever transitioned (e.g. a release that never
  // dispatched), so the list does not poll forever.
  useEffect(() => {
    if (followedServerIds.length === 0) return;
    const handle = setTimeout(
      () => setFollowedServerIds([]),
      RELEASE_FOLLOW_WINDOW_MS,
    );
    return () => clearTimeout(handle);
  }, [followedServerIds]);
  const filtered = useMemo(
    () => workingSet.filter((server) => matchesServerFilters(server, filters)),
    [filters, workingSet],
  );
  const sorted = useMemo(
    () => [...filtered].sort((a, b) => compareServers(a, b, sortKey, sortDir)),
    [filtered, sortDir, sortKey],
  );
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize));
  const safePage = Math.min(page, totalPages);
  const pageItems = useMemo(
    () => sorted.slice((safePage - 1) * pageSize, safePage * pageSize),
    [pageSize, safePage, sorted],
  );
  const filterOptions = useMemo<ServerFilterOptions>(
    () => ({
      provisioningState: toFilterOptions(
        countByDimension(workingSet, "provisioningState"),
      ),
      zone: toFilterOptions(countByDimension(workingSet, "zone")),
      pool: toFilterOptions(countByDimension(workingSet, "pool")),
      tag: toFilterOptions(
        countByDimension(workingSet, "tag" as ServerDimension),
      ),
    }),
    [workingSet],
  );

  const toggleOne = useCallback(
    (id: string) =>
      setSelected((current) => {
        const next = new Set(current);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      }),
    [],
  );
  const setMany = useCallback(
    (ids: string[], checked: boolean) =>
      setSelected((current) => {
        const next = new Set(current);
        ids.forEach((id) => {
          if (checked) next.add(id);
          else next.delete(id);
        });
        return next;
      }),
    [],
  );
  const clearSelection = useCallback(() => setSelected(new Set()), []);
  const pageIds = pageItems.map((server) => server.id);
  const allPageSelected =
    pageIds.length > 0 && pageIds.every((id) => selected.has(id));
  const somePageSelected = pageIds.some((id) => selected.has(id));
  const changeFilters = useCallback((next: ServerFilters) => {
    setFilters(next);
    setPage(1);
    setSelected(new Set());
  }, []);
  const toggleColumn = useCallback(
    (key: string) =>
      setHiddenColumns((current) => {
        const next = new Set(current);
        if (next.has(key)) next.delete(key);
        else next.add(key);
        writePreference(COLUMNS_KEY, [...next]);
        return next;
      }),
    [],
  );
  const runAction = useCallback(
    async (action: ServerMenuAction, ids: string[], confirmed = false) => {
      if (!ids.length) return;
      const selectedServers = ids
        .map((id) => workingSet.find((server) => server.id === id))
        .filter((server): server is Server => Boolean(server));
      const availability = serverActionAvailability(action, selectedServers);
      if (availability.disabledReason) return;
      if ((action === "lock" || action === "unlock") && !confirmed) {
        setPendingLockAction({
          action,
          targets: availability.eligible,
          skipped: availability.skipped,
        });
        return;
      }
      ids = availability.eligible.map((server) => server.id);
      if (action === "delete") {
        const target = workingSet.find((server) => server.id === ids[0]);
        if (target) setDeleteTarget(target);
        return;
      }
      const targets = ids.map((id) => {
        const server = workingSet.find((item) => item.id === id);
        return {
          serverId: id,
          serverName: server ? serverDisplayName(server) : id,
        };
      });
      if (action === "release") {
        setReleaseTargets(targets);
        return;
      }
      const result = await bulk.run(action, targets);
      setLastActionResult(result);
      setResultDialogOpen(failedServerActionOutcomes(result).length > 0);
      clearSelection();
      reload();
    },
    [bulk, clearSelection, reload, workingSet],
  );
  const confirmRelease = useCallback(
    async (input: ReleaseServerInput) => {
      if (!releaseTargets?.length) return;
      const releasedIds = releaseTargets.map((target) => target.serverId);
      // Stay on the Server list after accepting the release; the toast confirms the durable
      // Operation. Follow the released Servers so the list converges in place
      // (deployed -> releasing -> ready) rather than yanking the operator to the Operation
      // page or leaving a stale row until the next manual refresh.
      await bulk.release(releaseTargets, input);
      clearSelection();
      setFollowedServerIds(releasedIds);
      reload();
    },
    [bulk, clearSelection, releaseTargets, reload],
  );
  const visible = useCallback(
    (key: string) => !hiddenColumns.has(key),
    [hiddenColumns],
  );
  const columnSpan =
    3 + OPTIONAL_COLUMNS.filter((column) => visible(column.key)).length;
  const staleProvisioners = provisioners.filter(
    (item) => item.sync.lastError !== null,
  );
  const lastActionFailures = lastActionResult
    ? failedServerActionOutcomes(lastActionResult)
    : [];
  const actionTargets = workingSet.filter((server) => selected.has(server.id));
  const targetIntegrations = new Set(
    actionTargets.map((server) => server.source.integrationId),
  );
  const deployDisabledReason =
    selected.size > 100
      ? "Deploy OS supports at most 100 Servers."
      : actionTargets.some((server) => server.absent)
        ? "Absent Servers cannot be deployed."
        : actionTargets.some((server) => server.provisioning?.locked)
          ? "Unlock every selected Server before deployment."
          : actionTargets.some(
                (server) => server.provisioning?.state !== "ready",
              )
            ? "Every selected Server must be ready."
            : targetIntegrations.size > 1
              ? "Selected Servers must use the same provisioner integration."
              : undefined;
  const deploySelected = () => {
    const target = new URL(
      scopedHref("/provisioning/deploy"),
      window.location.origin,
    );
    [...selected].forEach((id) => target.searchParams.append("serverId", id));
    navigate(`${target.pathname}${target.search}`);
  };

  const sort = (key: ServerSortKey) => {
    if (key === sortKey)
      setSortDir((direction) => (direction === "asc" ? "desc" : "asc"));
    else {
      setSortKey(key);
      setSortDir("asc");
    }
  };

  if (state.status === "error")
    return (
      <>
        <PageHeader
          title="Servers"
          subtitle="Projected from each Site's provisioner."
        />
        <ErrorState message={state.message} onRetry={reload} />
      </>
    );
  return (
    <div className="operator-page">
      <PageHeader
        title="Servers"
        subtitle="Fleet inventory projected from Site provisioners; machines are not created here."
        metadata={
          activeProjectionTargetKey ? (
            <Label color="blue">Updating active Servers...</Label>
          ) : undefined
        }
      />
      {staleProvisioners.map((item) => (
        <div key={item.id} className="sw-inline-warning">
          <strong>{item.name} sync failed</strong>
          <span>{item.sync.lastError}</span>
        </div>
      ))}
      {lastActionResult && lastActionFailures.length > 0 && (
        <Alert
          variant={
            lastActionResult.succeeded > 0
              ? AlertVariant.warning
              : AlertVariant.danger
          }
          title={
            actionLabel(lastActionResult.action) +
            (lastActionResult.succeeded > 0 ? " partially accepted" : " failed")
          }
          isInline
          actionLinks={
            <>
              <AlertActionLink onClick={() => setResultDialogOpen(true)}>
                View details
              </AlertActionLink>
              <AlertActionLink onClick={() => setLastActionResult(null)}>
                Dismiss
              </AlertActionLink>
            </>
          }
        >
          {lastActionFailures.length === 1
            ? lastActionFailures[0].serverName +
              ": " +
              lastActionFailures[0].message
            : lastActionResult.succeeded +
              " accepted; " +
              lastActionFailures.length +
              " failed."}
        </Alert>
      )}
      <DataToolbar variant="plain">
        <ToolbarItem>
          <SearchInput
            value={searchInput}
            onChange={(_event, value) => setSearchInput(value)}
            onClear={() => setSearchInput("")}
            placeholder="Search hostname, serial, address, or ID"
            aria-label="Search Servers"
          />
        </ToolbarItem>
        <ToolbarItem>
          <Checkbox
            id="include-absent"
            label="Include absent"
            isChecked={includeAbsent}
            onChange={(_event, checked) => {
              setIncludeAbsent(checked);
              setPage(1);
              setSelected(new Set());
            }}
          />
        </ToolbarItem>
        <ToolbarItem>
          <FormSelect
            value={groupBy}
            onChange={(_event, value) => {
              setGroupBy(value as ServerGroupBy);
              writePreference(GROUP_KEY, value);
              setCollapsedGroups(new Set());
            }}
            aria-label="Group Servers"
          >
            <FormSelectOption value="none" label="No grouping" />
            <FormSelectOption value="provisioning" label="Provisioning" />
            <FormSelectOption value="zone" label="Zone" />
            <FormSelectOption value="pool" label="Pool" />
            <FormSelectOption value="architecture" label="Architecture" />
            <FormSelectOption value="power" label="Power" />
          </FormSelect>
        </ToolbarItem>
        <ToolbarItem>
          <Popover
            headerContent="Filters"
            bodyContent={
              <FilterPanel
                filters={filters}
                options={filterOptions}
                onChange={changeFilters}
              />
            }
          >
            <Button variant="secondary" icon={<FilterIcon />}>
              Filters{" "}
              {!filtersAreEmpty(filters, "") && <Badge isRead>Active</Badge>}
            </Button>
          </Popover>
        </ToolbarItem>
        <ToolbarItem>
          <Popover
            headerContent="Visible columns"
            bodyContent={
              <ColumnPanel
                columns={OPTIONAL_COLUMNS}
                hidden={hiddenColumns}
                onToggle={toggleColumn}
              />
            }
          >
            <Button
              variant="secondary"
              icon={<ColumnsIcon />}
              aria-label="Configure columns"
            />
          </Popover>
        </ToolbarItem>
        <ToolbarItem>
          <FormSelect
            value={density}
            onChange={(_event, value) => setDensity(value as ServerDensity)}
            aria-label="Table density"
          >
            <FormSelectOption value="compact" label="Compact" />
            <FormSelectOption value="comfortable" label="Comfortable" />
          </FormSelect>
        </ToolbarItem>
        <ToolbarItem>
          <FormSelect
            value={String(pageSize)}
            onChange={(_event, value) => {
              const size = Number(value);
              setPageSize(size);
              writePreference(PAGE_SIZE_KEY, size);
              setPage(1);
            }}
            aria-label="Rows per page"
          >
            {[25, 50, 100].map((size) => (
              <FormSelectOption
                key={size}
                value={String(size)}
                label={`${size} rows`}
              />
            ))}
          </FormSelect>
        </ToolbarItem>
        {selected.size > 0 && (
          <ToolbarGroup variant="action-group">
            <ToolbarItem>
              <strong>{selected.size} selected</strong>
            </ToolbarItem>
            <ToolbarItem>
              <Tooltip
                content={
                  deployDisabledReason ??
                  "Deploy one OS configuration to the selected Servers"
                }
              >
                <span>
                  <Button
                    variant="primary"
                    icon={<CloudUploadAltIcon />}
                    isDisabled={Boolean(deployDisabledReason)}
                    onClick={deploySelected}
                  >
                    Deploy OS
                  </Button>
                </span>
              </Tooltip>
            </ToolbarItem>
            <ToolbarItem>
              <BulkActionMenu
                targets={actionTargets}
                running={bulk.running}
                onAction={(action) => void runAction(action, [...selected])}
              />
            </ToolbarItem>
            {deployDisabledReason && (
              <ToolbarItem>
                <span className="sw-action-reason">{deployDisabledReason}</span>
              </ToolbarItem>
            )}
            <ToolbarItem>
              <Button variant="link" onClick={clearSelection}>
                Clear
              </Button>
            </ToolbarItem>
          </ToolbarGroup>
        )}
      </DataToolbar>
      {state.status === "loading" && <LoadingState rows={8} />}
      {state.status === "ready" && sorted.length === 0 && (
        <EmptyState
          title="No Servers"
          message="Nothing matches this working view."
          action={
            !filtersAreEmpty(filters, coarseKeyword)
              ? {
                  label: "Clear filters",
                  onClick: () => {
                    changeFilters(EMPTY_SERVER_FILTERS);
                    setSearchInput("");
                  },
                }
              : undefined
          }
        />
      )}
      {state.status === "ready" && sorted.length > 0 && (
        <>
          {selected.size > 0 && selected.size < filtered.length && (
            <Button
              variant="link"
              onClick={() =>
                setMany(
                  filtered.map((server) => server.id),
                  true,
                )
              }
            >
              Select all {filtered.length} matching Servers
            </Button>
          )}
          <StickyTableFrame>
            <Table
              aria-label="Servers"
              variant={density === "compact" ? "compact" : undefined}
              isStriped
              className="sw-server-table"
              gridBreakPoint=""
            >
              <Thead>
                <Tr>
                  <Th className="sw-sticky-selection sw-cell-center">
                    <Checkbox
                      id="select-page"
                      aria-label="Select all on this page"
                      isChecked={
                        allPageSelected ? true : somePageSelected ? null : false
                      }
                      onChange={() => setMany(pageIds, !allPageSelected)}
                    />
                  </Th>
                  <Th className="sw-sticky-name">
                    <SortableHeader
                      label="Machine"
                      active={sortKey === "name"}
                      direction={sortDir}
                      onClick={() => sort("name")}
                    />
                  </Th>
                  {visible("power") && (
                    <Th className="sw-cell-center sw-power-cell">
                      <SortableHeader
                        label="Power"
                        active={sortKey === "power"}
                        direction={sortDir}
                        onClick={() => sort("power")}
                      />
                    </Th>
                  )}
                  {visible("status") && (
                    <Th>
                      <SortableHeader
                        label="Deployment"
                        active={sortKey === "provisioning"}
                        direction={sortDir}
                        onClick={() => sort("provisioning")}
                      />
                    </Th>
                  )}
                  {visible("address") && <Th>Address</Th>}
                  {visible("mac") && <Th>MAC address</Th>}
                  {visible("zone") && <Th>Zone</Th>}
                  {visible("pool") && <Th>Pool</Th>}
                  {visible("tags") && <Th className="sw-column-tags">Tags</Th>}
                  {visible("architecture") && (
                    <Th className="sw-hardware-column sw-column-architecture">
                      Architecture
                    </Th>
                  )}
                  {visible("cpuCores") && (
                    <Th className="sw-hardware-column sw-column-cpu-cores sw-cell-center">
                      CPU cores
                    </Th>
                  )}
                  {visible("cpuModel") && (
                    <Th className="sw-hardware-column sw-column-cpu-model">
                      CPU model
                    </Th>
                  )}
                  {visible("memory") && (
                    <Th className="sw-hardware-column sw-column-memory">
                      Memory
                    </Th>
                  )}
                  {visible("storage") && (
                    <Th className="sw-hardware-column sw-column-storage">
                      Storage
                    </Th>
                  )}
                  {visible("systemVendor") && (
                    <Th className="sw-hardware-column sw-column-system-vendor">
                      System vendor
                    </Th>
                  )}
                  {visible("systemProduct") && (
                    <Th className="sw-hardware-column sw-column-system-product">
                      System product
                    </Th>
                  )}
                  {visible("gpus") && <Th>GPUs</Th>}
                  {visible("platform") && <Th>Platform</Th>}
                  {visible("health") && <Th>Health</Th>}
                  <Th
                    className="sw-sticky-actions"
                    screenReaderText="Actions"
                  />
                </Tr>
              </Thead>
              <Tbody>
                {renderGroups(pageItems, groupBy).map((group) => (
                  <GroupRows
                    key={group.key || "all"}
                    group={group}
                    grouped={groupBy !== "none"}
                    columnSpan={columnSpan}
                    collapsed={collapsedGroups.has(group.key)}
                    onCollapse={() =>
                      setCollapsedGroups((current) => {
                        const next = new Set(current);
                        if (next.has(group.key)) next.delete(group.key);
                        else next.add(group.key);
                        return next;
                      })
                    }
                    selected={selected}
                    onToggleOne={toggleOne}
                    onToggleGroup={setMany}
                    onNavigate={(id) => navigate(scopedHref(`/servers/${id}`))}
                    onAction={(action, id) => void runAction(action, [id])}
                    visible={visible}
                  />
                ))}
              </Tbody>
            </Table>
          </StickyTableFrame>
          <div className="sw-pagination">
            <Pagination
              total={totalPages}
              value={safePage}
              onChange={setPage}
            />
          </div>
        </>
      )}
      {releaseTargets && (
        <ServerReleaseDialog
          targets={releaseTargets}
          onClose={() => setReleaseTargets(null)}
          onRelease={confirmRelease}
        />
      )}
      {lastActionResult && resultDialogOpen && (
        <ServerActionResultDialog
          result={lastActionResult}
          onClose={() => setResultDialogOpen(false)}
        />
      )}
      {deleteTarget && (
        <ServerDeleteDialog
          serverId={deleteTarget.id}
          serverName={serverDisplayName(deleteTarget)}
          onClose={() => setDeleteTarget(null)}
          onDeleted={() => {
            setDeleteTarget(null);
            clearSelection();
            reload();
          }}
        />
      )}
      {pendingLockAction && (
        <ServerLockDialog
          action={pendingLockAction.action}
          targets={pendingLockAction.targets}
          skipped={pendingLockAction.skipped}
          busy={bulk.running}
          onClose={() => setPendingLockAction(null)}
          onConfirm={() => {
            const pending = pendingLockAction;
            setPendingLockAction(null);
            void runAction(
              pending.action,
              pending.targets.map((server) => server.id),
              true,
            );
          }}
        />
      )}
    </div>
  );
}

function SortableHeader({
  label,
  active,
  direction,
  onClick,
}: {
  label: string;
  active: boolean;
  direction: SortDirection;
  onClick: () => void;
}) {
  return (
    <Button
      variant="plain"
      className="sw-sort-button"
      icon={
        active ? (
          direction === "asc" ? (
            <SortAmountUpIcon />
          ) : (
            <SortAmountDownIcon />
          )
        ) : undefined
      }
      iconPosition="end"
      onClick={onClick}
      aria-label={`Sort by ${label}`}
    >
      {label}
    </Button>
  );
}

function updateValues(
  values: readonly string[],
  value: string,
  checked: boolean,
): string[] {
  return checked ? [...values, value] : values.filter((item) => item !== value);
}

function FilterOptions({
  title,
  values,
  selected,
  onChange,
}: {
  title: string;
  values: FilterOption[];
  selected: readonly string[];
  onChange: (next: string[]) => void;
}) {
  return (
    <fieldset className="sw-filter-group">
      <legend>{title}</legend>
      {values.map((option) => (
        <Checkbox
          key={option.value}
          id={`filter-${title}-${option.value}`}
          label={`${option.label} (${option.count})`}
          isChecked={selected.includes(option.value)}
          onChange={(_event, checked) =>
            onChange(updateValues(selected, option.value, checked))
          }
        />
      ))}
    </fieldset>
  );
}

function FilterPanel({
  filters,
  options,
  onChange,
}: {
  filters: ServerFilters;
  options: ServerFilterOptions;
  onChange: (next: ServerFilters) => void;
}) {
  return (
    <div className="sw-filter-panel">
      <fieldset className="sw-filter-group">
        <legend>Protection</legend>
        <FormSelect
          value={filters.lockState}
          onChange={(_event, value) =>
            onChange({
              ...filters,
              lockState: value as ServerFilters["lockState"],
            })
          }
          aria-label="Filter Server lock"
        >
          <FormSelectOption value="any" label="Any" />
          <FormSelectOption value="locked" label="Locked" />
          <FormSelectOption value="unlocked" label="Unlocked" />
        </FormSelect>
      </fieldset>
      <FilterOptions
        title="Provisioning"
        values={options.provisioningState}
        selected={filters.provisioningStates}
        onChange={(values) =>
          onChange({ ...filters, provisioningStates: values })
        }
      />
      <FilterOptions
        title="Zone"
        values={options.zone}
        selected={filters.zones}
        onChange={(values) => onChange({ ...filters, zones: values })}
      />
      <FilterOptions
        title="Pool"
        values={options.pool}
        selected={filters.pools}
        onChange={(values) => onChange({ ...filters, pools: values })}
      />
      <FilterOptions
        title="Tags"
        values={options.tag}
        selected={filters.tags}
        onChange={(values) => onChange({ ...filters, tags: values })}
      />
      <fieldset className="sw-filter-group">
        <legend>GPU</legend>
        <FormSelect
          value={
            filters.hasGpu === null ? "any" : filters.hasGpu ? "yes" : "no"
          }
          onChange={(_event, value) =>
            onChange({
              ...filters,
              hasGpu: value === "any" ? null : value === "yes",
            })
          }
          aria-label="Filter GPU presence"
        >
          <FormSelectOption value="any" label="Any" />
          <FormSelectOption value="yes" label="Has GPU" />
          <FormSelectOption value="no" label="No GPU" />
        </FormSelect>
      </fieldset>
      <Button variant="link" onClick={() => onChange(EMPTY_SERVER_FILTERS)}>
        Clear filters
      </Button>
    </div>
  );
}

function ColumnPanel({
  columns,
  hidden,
  onToggle,
}: {
  columns: ColumnToggle[];
  hidden: ReadonlySet<string>;
  onToggle: (key: string) => void;
}) {
  return (
    <div className="sw-column-panel">
      {columns.map((column) => (
        <Checkbox
          key={column.key}
          id={`column-${column.key}`}
          label={column.label}
          isChecked={!hidden.has(column.key)}
          onChange={() => onToggle(column.key)}
        />
      ))}
    </div>
  );
}

function ActionDropdown({
  label,
  icon,
  targets,
  running,
  includeSingleOnly = true,
  onAction,
}: {
  label: string;
  icon?: ReactNode;
  targets: readonly Server[];
  running?: boolean;
  includeSingleOnly?: boolean;
  onAction: (action: ServerMenuAction) => void;
}) {
  const [open, setOpen] = useState(false);
  const groups = SERVER_ACTION_GROUPS.map((group) => ({
    ...group,
    actions: group.actions.filter(
      (entry) => includeSingleOnly || entry.bulk !== false,
    ),
  })).filter((group) => group.actions.length > 0);
  return (
    <Dropdown
      isOpen={open}
      onOpenChange={setOpen}
      toggle={(ref) => (
        <MenuToggle
          ref={ref}
          icon={icon}
          variant={label ? "default" : "plain"}
          aria-label={label || "Actions"}
          isExpanded={open}
          isDisabled={running}
          onClick={() => setOpen((value) => !value)}
        >
          {label || null}
        </MenuToggle>
      )}
    >
      <DropdownList>
        {groups.flatMap((group) => [
          <DropdownItem key={`${group.label}-label`} isDisabled>
            {group.label}
          </DropdownItem>,
          ...group.actions.map((entry) => {
            const availability = serverActionAvailability(
              entry.action,
              targets,
            );
            return (
              <DropdownItem
                key={entry.action}
                value={entry.action}
                isDanger={entry.destructive}
                isDisabled={Boolean(availability.disabledReason)}
                description={availability.disabledReason}
                onClick={() => {
                  setOpen(false);
                  onAction(entry.action);
                }}
              >
                {entry.label}
              </DropdownItem>
            );
          }),
        ])}
      </DropdownList>
    </Dropdown>
  );
}
function BulkActionMenu({
  targets,
  running,
  onAction,
}: {
  targets: readonly Server[];
  running: boolean;
  onAction: (action: BulkAction) => void;
}) {
  return (
    <ActionDropdown
      label={running ? "Working..." : "Take action"}
      targets={targets}
      running={running}
      includeSingleOnly={false}
      onAction={(action) => {
        if (action !== "delete") onAction(action);
      }}
    />
  );
}

function GroupRows({
  group,
  grouped,
  columnSpan,
  collapsed,
  onCollapse,
  selected,
  onToggleOne,
  onToggleGroup,
  onNavigate,
  onAction,
  visible,
}: {
  group: RenderGroup;
  grouped: boolean;
  columnSpan: number;
  collapsed: boolean;
  onCollapse: () => void;
  selected: ReadonlySet<string>;
  onToggleOne: (id: string) => void;
  onToggleGroup: (ids: string[], checked: boolean) => void;
  onNavigate: (id: string) => void;
  onAction: (action: ServerMenuAction, id: string) => void;
  visible: (key: string) => boolean;
}) {
  const ids = group.items.map((item) => item.id);
  const all = ids.length > 0 && ids.every((id) => selected.has(id));
  const some = ids.some((id) => selected.has(id));
  return (
    <>
      {grouped && (
        <Tr className="sw-group-row">
          <Td colSpan={columnSpan}>
            <span>
              <Button
                variant="plain"
                icon={collapsed ? <AngleRightIcon /> : <AngleDownIcon />}
                aria-label={`${collapsed ? "Expand" : "Collapse"} ${group.label}`}
                onClick={onCollapse}
              />
              <Checkbox
                id={`group-${group.key}`}
                aria-label={`Select all in ${group.label}`}
                isChecked={all ? true : some ? null : false}
                onChange={() => onToggleGroup(ids, !all)}
              />
              <strong>{group.label}</strong>
              <Badge isRead>{group.items.length}</Badge>
            </span>
          </Td>
        </Tr>
      )}
      {!collapsed &&
        group.items.map((server) => (
          <ServerRow
            key={server.id}
            server={server}
            checked={selected.has(server.id)}
            onToggle={() => onToggleOne(server.id)}
            onNavigate={() => onNavigate(server.id)}
            onAction={(action) => onAction(action, server.id)}
            visible={visible}
          />
        ))}
    </>
  );
}

function ServerRow({
  server,
  checked,
  onToggle,
  onNavigate,
  onAction,
  visible,
}: {
  server: Server;
  checked: boolean;
  onToggle: () => void;
  onNavigate: () => void;
  onAction: (action: ServerMenuAction) => void;
  visible: (key: string) => boolean;
}) {
  // The power cell doubles as a shortcut to power actions; the dialog lives on the row so each
  // row owns its own open state without lifting it into the (already large) page component.
  const [powerDialogOpen, setPowerDialogOpen] = useState(false);
  return (
    <>
    <Tr isClickable onRowClick={onNavigate}>
      <Td
        dataLabel="Selection"
        className="sw-sticky-selection sw-cell-center"
        onClick={(event) => event.stopPropagation()}
      >
        <Checkbox
          id={"server-" + server.id}
          aria-label={"Select " + serverDisplayName(server)}
          isChecked={checked}
          onChange={onToggle}
        />
      </Td>
      <Td dataLabel="Machine" className="sw-sticky-name">
        <span className="sw-machine-name">
          {(server.provisioning?.locked ?? false) && (
            <Tooltip content="This Server is protected. Unlock it before making changes.">
              <span className="sw-lock-indicator" role="img" aria-label="Locked">
                <Lock />
              </span>
            </Tooltip>
          )}
          <strong>{serverDisplayName(server)}</strong>
          <CopyButton value={serverDisplayName(server)} label="Copy hostname" />
          {server.absent && <Label color="grey">absent</Label>}
        </span>
      </Td>
      {visible("power") && (
        <Td
          dataLabel="Power"
          className="sw-cell-center sw-power-cell"
          onClick={(event) => event.stopPropagation()}
        >
          {server.provisioning ? (
            <Tooltip
              content={`Power actions (${powerStateLabel(
                server.provisioning.powerState,
              )})`}
            >
              <Button
                variant="plain"
                className="sw-power-button"
                aria-label={`Power actions for ${serverDisplayName(server)}`}
                onClick={() => setPowerDialogOpen(true)}
              >
                <PowerBadge powerState={server.provisioning.powerState} decorative />
              </Button>
            </Tooltip>
          ) : (
            <PowerBadge powerState={null} />
          )}
        </Td>
      )}
      {visible("status") && (
        <Td dataLabel="Deployment">
          <DeploymentBadge axis={server.deployment} provider={server.provisioning} />
        </Td>
      )}
      {visible("address") && (
        <Td dataLabel="Address" className="mono">
          <span className="sw-copyable">
            {textOrDash(serverPrimaryAddress(server))}
            <CopyButton
              value={serverPrimaryAddress(server) ?? ""}
              label="Copy IP address"
            />
          </span>
        </Td>
      )}
      {visible("mac") && (
        <Td dataLabel="MAC address" className="mono">
          <span className="sw-copyable">
            {textOrDash(serverMacAddress(server))}
            <CopyButton
              value={serverMacAddress(server) ?? ""}
              label="Copy MAC address"
            />
          </span>
        </Td>
      )}
      {visible("zone") && (
        <Td dataLabel="Zone">{textOrDash(server.providerZone)}</Td>
      )}
      {visible("pool") && (
        <Td dataLabel="Pool">{textOrDash(server.providerResourcePool)}</Td>
      )}
      {visible("tags") && (
        <Td dataLabel="Tags" className="sw-column-tags">
          {server.tags.length ? (
            <span className="sw-tag-list">
              {server.tags.slice(0, 3).map((tag) => (
                <Label key={tag} color="blue" isCompact>
                  {tag}
                </Label>
              ))}
            </span>
          ) : (
            "-"
          )}
        </Td>
      )}
      {visible("architecture") && (
        <Td
          dataLabel="Architecture"
          className="sw-hardware-column sw-column-architecture"
        >
          {textOrDash(server.architecture)}
        </Td>
      )}
      {visible("cpuCores") && (
        <Td
          dataLabel="CPU cores"
          className="sw-hardware-column sw-column-cpu-cores sw-cell-center"
        >
          {quantityOrDash(server.cpuCores)}
        </Td>
      )}
      {visible("cpuModel") && (
        <Td
          dataLabel="CPU model"
          className="sw-hardware-column sw-column-cpu-model"
        >
          {textOrDash(server.cpuModel)}
        </Td>
      )}
      {visible("memory") && (
        <Td dataLabel="Memory" className="sw-hardware-column sw-column-memory">
          {quantityOrDash(server.memoryMiB, "GiB", 1024)}
        </Td>
      )}
      {visible("storage") && (
        <Td
          dataLabel="Storage"
          className="sw-hardware-column sw-column-storage"
        >
          {quantityOrDash(server.storageGB, "GB")}
        </Td>
      )}
      {visible("systemVendor") && (
        <Td
          dataLabel="System vendor"
          className="sw-hardware-column sw-column-system-vendor"
        >
          {textOrDash(server.systemVendor)}
        </Td>
      )}
      {visible("systemProduct") && (
        <Td
          dataLabel="System product"
          className="sw-hardware-column sw-column-system-product"
        >
          {textOrDash(server.systemProduct)}
        </Td>
      )}
      {visible("gpus") && (
        <Td dataLabel="GPUs">
          <GpuInventory server={server} />
        </Td>
      )}
      {visible("platform") && (
        <Td dataLabel="Platform">
          {server.membership ? (
            <MembershipBadge axis={server.membership} />
          ) : (
            "-"
          )}
        </Td>
      )}
      {visible("health") && (
        <Td dataLabel="Health">
          {server.health ? <HealthBadge axis={server.health} /> : "-"}
        </Td>
      )}
      <Td
        isActionCell
        className="sw-sticky-actions"
        onClick={(event) => event.stopPropagation()}
      >
        <ActionDropdown
          label=""
          icon={<EllipsisVIcon />}
          targets={[server]}
          onAction={onAction}
        />
      </Td>
    </Tr>
    {powerDialogOpen && (
      <ServerPowerDialog
        server={server}
        onClose={() => setPowerDialogOpen(false)}
        onSelect={(action) => {
          setPowerDialogOpen(false);
          onAction(action);
        }}
      />
    )}
    </>
  );
}

/**
 * Presents physical GPU inventory by vendor without implying utilization or health.
 * Model details remain available through each accessible vendor-mark tooltip.
 */
function GpuInventory({ server }: { server: Server }) {
  if (server.gpus.length === 0) return <>-</>;
  return (
    <span className="sw-gpu-inventory">
      {server.gpus.map((gpu, index) => (
        <span key={[gpu.vendor, gpu.model, index].join("-")}>
          <span className="sw-gpu-count">{gpu.count} x</span>
          <GpuVendorLogo vendor={gpu.vendor} model={gpu.model} />
        </span>
      ))}
    </span>
  );
}
