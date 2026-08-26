import type {
  ListOperationsFilters,
  Operation,
  OperationEvents,
} from '@/domain/operation/types'

/** A page of results, matching the backend's pagination envelope. */
export interface Paginated<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

/**
 * Reads operations, their task-level progress, and their logs, and retries a finished one.
 * Operations are created by the actions that need them (a cluster deployment, for example),
 * so there is no generic create here; retry is the one write, and it always makes a new
 * operation rather than mutating the original.
 */
export interface OperationRepository {
  listOperations(filters?: ListOperationsFilters): Promise<Paginated<Operation>>
  getOperation(id: string): Promise<Operation | null>
  /** Task-level progress from the runner's retained events; empty until a run starts. */
  getEvents(id: string): Promise<OperationEvents>
  /** Retained runner output as plain text; empty for a pending run or one with no output. */
  getLogs(id: string): Promise<string>
  /** Create a new operation repeating a finished one, linked back to it. */
  retryOperation(id: string): Promise<Operation>
}
