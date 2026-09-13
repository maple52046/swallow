import type { Server } from "@/domain/server/types";
import type { MinimumResources } from "./types";

/** One resource deficit, retaining machine-readable dimension data and an operator label. */
export interface ResourceShortfall {
  dimension: "cpu" | "memory" | "storage";
  actual: number;
  minimum: number;
  message: string;
}

/** The single eligibility result consumed by Slurm selection controls and payload creation. */
export interface ResourceEligibility {
  eligible: boolean;
  shortfalls: ResourceShortfall[];
  explanation: string;
}

/**
 * Evaluates observed Server inventory against the active Slurm deployment floor.
 * A null floor is the explicit disabled state. Zero or unknown observations naturally fail
 * any enabled positive floor and are reported by dimension rather than hidden.
 */
export function evaluateMinimumResourceEligibility(
  server: Pick<Server, "cpuCores" | "memoryMiB" | "storageGB">,
  minimum: MinimumResources | null,
): ResourceEligibility {
  if (!minimum) {
    return {
      eligible: true,
      shortfalls: [],
      explanation: "Minimum resource requirement is disabled.",
    };
  }

  const shortfalls: ResourceShortfall[] = [];
  if (server.cpuCores < minimum.cpuCores) {
    shortfalls.push({
      dimension: "cpu",
      actual: server.cpuCores,
      minimum: minimum.cpuCores,
      message: `CPU ${server.cpuCores} cores; minimum ${minimum.cpuCores}`,
    });
  }
  if (server.memoryMiB < minimum.memoryMiB) {
    shortfalls.push({
      dimension: "memory",
      actual: server.memoryMiB,
      minimum: minimum.memoryMiB,
      message: `Memory ${formatGiB(server.memoryMiB)} GiB; minimum ${formatGiB(minimum.memoryMiB)} GiB`,
    });
  }
  if (server.storageGB < minimum.storageGB) {
    shortfalls.push({
      dimension: "storage",
      actual: server.storageGB,
      minimum: minimum.storageGB,
      message: `Storage ${formatNumber(server.storageGB)} GB; minimum ${formatNumber(minimum.storageGB)} GB`,
    });
  }
  return shortfalls.length === 0
    ? {
        eligible: true,
        shortfalls,
        explanation: "Meets the Slurm minimum resource requirement.",
      }
    : {
        eligible: false,
        shortfalls,
        explanation: shortfalls.map((item) => item.message).join("; "),
      };
}

/** Formats a MiB contract value for the GiB-facing operator UI. */
export function formatGiB(memoryMiB: number): string {
  return formatNumber(memoryMiB / 1024);
}

/** Formats the active policy consistently in settings, selection, and review. */
export function formatMinimumResources(
  minimum: MinimumResources | null,
): string {
  if (!minimum) return "Disabled";
  return `${minimum.cpuCores} cores / ${formatGiB(minimum.memoryMiB)} GiB / ${formatNumber(minimum.storageGB)} GB`;
}

function formatNumber(value: number): string {
  return Number.isInteger(value)
    ? String(value)
    : value.toFixed(2).replace(/0+$/, "").replace(/\.$/, "");
}
