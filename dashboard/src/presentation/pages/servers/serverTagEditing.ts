import type { ServerTagOption } from "@/domain/provisioning/types";

/**
 * Pure, framework-free helpers for the tri-state tag editor.
 *
 * They are separated from the React component so the diff logic — which is what actually decides
 * the add/remove sent to the backend — can be reasoned about and reused without a DOM. swallow owns
 * every manual tag and the backend writes the edit into each Server's spec (decision 071); this
 * module only computes the user's intent from the checkbox states.
 */

/** How many of the edited Servers currently carry a tag: all, some (mixed), or none. */
export type TagCoverage = "all" | "some" | "none";

/**
 * The user's decision for a tag, overriding its current coverage:
 * - `"add"` — apply to every edited Server.
 * - `"remove"` — remove from every edited Server.
 * - `undefined` — leave each Server as it is (no change), which is the only way a mixed tag stays mixed.
 */
export type TagDecision = "add" | "remove" | undefined;

/** One row in the editor: a known or already-applied tag with its coverage across the selection. */
export interface TagRow {
  name: string;
  /** False for an automatic tag (the provisioner computes or maintains it); it can never be added. */
  editable: boolean;
  /**
   * Whether the tag can be taken off the selection. Always true for a manual tag; for an automatic
   * tag only when the Site's tag list says the provisioner allows it, so a tag computed from a rule,
   * or one whose kind could not be loaded, stays disabled.
   */
  removable: boolean;
  coverage: TagCoverage;
  /** How many of the edited Servers currently carry this tag, for a "N of M" hint on mixed tags. */
  count: number;
}

/**
 * The tag facts of one edited Server: the manual tags swallow owns for it (its spec, see
 * `serverManualTags`) and the automatic tags its provisioner reports, which can at most be removed.
 */
export interface TaggedServer {
  manualTags: readonly string[];
  automaticTags: readonly string[];
}

/**
 * Builds the editor rows from the edited Servers and the Site's known tags.
 *
 * The row set is the union of every tag known for the Site and every tag on a selected Server, so
 * the editor can both offer existing names and reflect tags a Server carries that the catalog lookup
 * did not return (or that failed to load). Coverage is computed from how many selected Servers carry
 * the tag. A tag is not editable when the known list says so or when it is automatic on any selected
 * Server, so the Servers' own facts still protect automatic tags without the catalog. An automatic
 * tag is removable only when the known list says so: without that list its kind is unknown, and the
 * editor takes the conservative side. Rows are sorted by name for a stable list.
 */
export function buildTagRows(
  servers: readonly TaggedServer[],
  known: readonly ServerTagOption[],
): TagRow[] {
  const editableByName = new Map<string, boolean>();
  const removableByName = new Map<string, boolean>();
  for (const option of known) {
    editableByName.set(option.name, option.editable);
    removableByName.set(option.name, option.removable);
  }

  const counts = new Map<string, number>();
  for (const server of servers) {
    for (const tag of new Set([...server.manualTags, ...server.automaticTags])) {
      counts.set(tag, (counts.get(tag) ?? 0) + 1);
    }
    for (const tag of server.automaticTags) {
      editableByName.set(tag, false);
    }
  }

  const names = new Set<string>([...editableByName.keys(), ...counts.keys()]);
  const total = servers.length;
  const rows: TagRow[] = [];
  for (const name of names) {
    const count = counts.get(name) ?? 0;
    const editable = editableByName.get(name) ?? true;
    rows.push({
      name,
      editable,
      removable: editable || (removableByName.get(name) ?? false),
      coverage: coverageOf(count, total),
      count,
    });
  }
  rows.sort((a, b) => a.name.localeCompare(b.name));
  return rows;
}

/** Maps a per-tag count against the selection size onto a coverage bucket. */
export function coverageOf(count: number, total: number): TagCoverage {
  if (total === 0 || count === 0) return "none";
  if (count >= total) return "all";
  return "some";
}

/**
 * The checkbox visual for a tag: a definite decision wins, otherwise the checkbox reflects the
 * current coverage (all → checked, some → indeterminate, none → unchecked).
 */
export function tagCheckboxState(
  coverage: TagCoverage,
  decision: TagDecision,
): boolean | "indeterminate" {
  if (decision === "add") return true;
  if (decision === "remove") return false;
  if (coverage === "all") return true;
  if (coverage === "some") return "indeterminate";
  return false;
}

/**
 * Whether the user can change a row at all: a manual tag always, an automatic tag only when it is
 * removable and some edited Server carries it, because an automatic tag can only be taken off.
 */
export function isTagRowChangeable(row: TagRow): boolean {
  return row.editable || (row.removable && row.coverage !== "none");
}

/**
 * The next decision when the user clicks a tag's checkbox, like an email label.
 *
 * A mixed (some) tag cycles indeterminate → add-to-all → remove-from-all → back to mixed, so the
 * user can either apply, clear, or leave it as it is. An all/none tag is a plain toggle between
 * its original state (no change) and the opposite definite decision. A removable automatic tag
 * (`canAdd` false) only toggles between no change and remove-from-all, since swallow never assigns
 * it.
 */
export function cycleTagDecision(
  coverage: TagCoverage,
  decision: TagDecision,
  canAdd = true,
): TagDecision {
  if (!canAdd) {
    return decision === undefined && coverage !== "none" ? "remove" : undefined;
  }
  if (coverage === "some") {
    if (decision === undefined) return "add";
    if (decision === "add") return "remove";
    return undefined;
  }
  if (decision !== undefined) return undefined;
  return coverage === "all" ? "remove" : "add";
}

/**
 * Computes the backend diff from the rows and the user's decisions, dropping no-ops so the
 * request only carries real changes: an `add` for a tag already on every Server, or a `remove` for a
 * tag on no Server, is omitted. An automatic tag can only carry a `remove` decision (see
 * `cycleTagDecision`), which the backend carries out on the machines rather than in the spec.
 */
export function computeTagDiff(
  rows: readonly TagRow[],
  decisions: ReadonlyMap<string, TagDecision>,
): { add: string[]; remove: string[] } {
  const add: string[] = [];
  const remove: string[] = [];
  for (const row of rows) {
    const decision = decisions.get(row.name);
    if (decision === "add" && row.coverage !== "all") {
      add.push(row.name);
    } else if (decision === "remove" && row.coverage !== "none") {
      remove.push(row.name);
    }
  }
  return { add, remove };
}
