import { useCallback, useEffect, useRef, useState } from "react";
import { useApp } from "@/di/AppProvider";
import {
  isOrchestrationOperation,
  isTerminalStatus,
  type Operation,
  type OperationEvents,
  type OperationStatus,
} from "@/domain/operation/types";

/**
 * How often a running operation is re-read. An HA platform deployment runs for minutes
 * across several phases, so a few seconds between reads keeps progress live without
 * hammering the API; polling stops as soon as the run reaches a terminal state.
 */
const POLL_INTERVAL_MS = 5000;

export interface OperationDetailData {
  operation: Operation;
  /** Task-level progress; null when the events read failed but the operation loaded. */
  events: OperationEvents | null;
  /** Manual refetch, e.g. after a retry navigates back to this view. */
  reload: () => void;
  /**
   * Set when a background poll failed while the last-good operation is still shown. The view
   * keeps the previous data and keeps polling, so this is a non-blocking "live updates
   * interrupted, retrying" notice rather than a page-replacing error. Cleared on the next
   * successful poll.
   */
  refreshError?: string;
}

export type OperationDetailState =
  | { status: "loading" }
  | { status: "not-found" }
  | { status: "error"; message: string }
  | { status: "ready"; data: OperationDetailData };

/**
 * Loads one operation and its task events, polling while the run is active.
 *
 * The operation projection is authoritative for status. Only legacy Operations use the
 * top-level runner events endpoint; schema-v3 diagnostics load events per Step, avoiding a
 * compatibility 404. A legacy events failure degrades to null rather than failing the page.
 * Polling is bounded: it runs
 * only while the status is non-terminal and is always cleared on unmount or completion, so
 * it cannot outlive the screen. `reload` forces an immediate refetch.
 */
export function useOperation(id: string | undefined): OperationDetailState {
  const { operations } = useApp();
  const [state, setState] = useState<OperationDetailState>(
    id ? { status: "loading" } : { status: "not-found" },
  );
  const [nonce, setNonce] = useState(0);
  const reload = useCallback(() => setNonce((value) => value + 1), []);

  // Kept in a ref so the polling effect can read the latest status without re-subscribing.
  const statusRef = useRef<OperationStatus>("pending");
  // The operation id we currently hold good data for. A poll failure only "keeps the last-good
  // view" when it is for the operation already on screen; navigating to a different operation
  // falls back to first-load semantics (loading, then error/not-found).
  const loadedIdRef = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!id) return;

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    // Reschedule the next poll while the run can still change, so a live view keeps updating and,
    // after a failed poll, keeps trying until it recovers. A terminal run stops polling on its own.
    const scheduleNext = () => {
      if (!cancelled && !isTerminalStatus(statusRef.current)) {
        timer = setTimeout(load, POLL_INTERVAL_MS);
      }
    };

    const load = () => {
      operations
        .getOperation(id)
        .then(async (operation) => {
          const events = operation && !isOrchestrationOperation(operation)
            ? await operations.getEvents(id).catch(() => null)
            : null
          return { operation, events }
        })
        .then(({ operation, events }) => {
          if (cancelled) return;
          if (operation === null) {
            // A 404 for an operation we are already showing is treated as a transient read gap:
            // keep the last-good view and keep polling. Only an initial 404 is "not found".
            if (loadedIdRef.current === id) {
              scheduleNext();
              return;
            }
            setState({ status: "not-found" });
            return;
          }
          statusRef.current = operation.status ?? operation.execution.status;
          loadedIdRef.current = id;
          // A successful read replaces the data and clears any prior transient refresh error.
          setState({ status: "ready", data: { operation, events, reload } });
          scheduleNext();
        })
        .catch((err: Error) => {
          if (cancelled) return;
          // Once the operation has loaded, a failed poll (an expiring token, a backend restart, a
          // network hiccup) must not collapse the live view into an error screen that only a manual
          // refresh escapes. Keep the last-good data, surface a non-blocking notice, and keep
          // polling so the page recovers on its own. Only a failed first load fails hard.
          if (loadedIdRef.current === id) {
            setState((current) =>
              current.status === "ready"
                ? { status: "ready", data: { ...current.data, refreshError: err.message } }
                : current,
            );
            scheduleNext();
            return;
          }
          setState({ status: "error", message: err.message });
        });
    };

    load();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [operations, id, nonce, reload]);

  return state;
}
