import type { ServerTagOption } from "@/domain/provisioning/types";

/**
 * Pure, framework-free helpers for the tri-state tag editor.
 *
 * They are separated from the React component so the diff logic — which is what actually decides
 * the add/remove sent to the backend — can be reasoned about and reused without a DOM. Ownership of
 * the tags themselves is capability-first (decision 031); this module only computes the operator's
 * intent from the checkbox states.
 */

/** How many of the edited Servers currently carry a tag: all, some (mixed), or none. */
export type TagCoverage = "all" | "some" | "none";

/**
 * The operator's decision for a tag, overriding its current coverage:
 * - `"add"` — apply to every edited Server.
 * - `"remove"` — remove from every edited Server.
 * - `undefined` — leave each Server as it is (no change), which is the only way a mixed tag stays mixed.
 */
export type TagDecision = "add" | "remove" | undefined;

/** One row in the editor: a known or already-applied tag with its coverage across the selection. */
export interface TagRow {
  name: string;
  /** False for a provider-computed automatic tag; the editor shows it disabled. */
  editable: boolean;
  coverage: TagCoverage;
  /** How many of the edited Servers currently carry this tag, for a "N of M" hint on mixed tags. */
  count: number;
}

/** A minimal Server shape for building rows; only the tag names are needed. */
interface TaggedServer {
  tags: string[];
}

/**
 * Builds the editor rows from the edited Servers and the Site's known tags.
 *
 * The row set is the union of every tag known for the Site and every tag already on a selected
 * Server, so the editor can both offer existing names and reflect tags a Server carries that the
 * catalog lookup did not return. Coverage is computed from how many selected Servers carry the tag.
 * A tag's editability comes from the known list; a tag seen only on Servers is assumed editable
 * (swallow would not have applied a non-editable one). Rows are sorted by name for a stable list.
 */
export function buildTagRows(
  servers: readonly TaggedServer[],
  known: readonly ServerTagOption[],
): TagRow[] {
  const editableByName = new Map<string, boolean>();
  for (const option of known) {
    editableByName.set(option.name, option.editable);
  }

  const counts = new Map<string, number>();
  for (const server of servers) {
    for (const tag of new Set(server.tags)) {
      counts.set(tag, (counts.get(tag) ?? 0) + 1);
    }
  }

  const names = new Set<string>([...editableByName.keys(), ...counts.keys()]);
  const total = servers.length;
  const rows: TagRow[] = [];
  for (const name of names) {
    const count = counts.get(name) ?? 0;
    rows.push({
      name,
      editable: editableByName.get(name) ?? true,
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
 * The next decision when the operator clicks a tag's checkbox, like an email label.
 *
 * A mixed (some) tag cycles indeterminate → add-to-all → remove-from-all → back to mixed, so the
 * operator can either apply, clear, or leave it as it is. An all/none tag is a plain toggle between
 * its original state (no change) and the opposite definite decision.
 */
export function cycleTagDecision(
  coverage: TagCoverage,
  decision: TagDecision,
): TagDecision {
  if (coverage === "some") {
    if (decision === undefined) return "add";
    if (decision === "add") return "remove";
    return undefined;
  }
  if (decision !== undefined) return undefined;
  return coverage === "all" ? "remove" : "add";
}

/**
 * Computes the backend diff from the rows and the operator's decisions, dropping no-ops so the
 * request only carries real changes: an `add` for a tag already on every Server, or a `remove` for a
 * tag on no Server, is omitted. Non-editable tags never carry a decision, so they never appear here.
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
