import { useCallback, useEffect, useState } from "react";
import { useApp } from "@/di/AppProvider";
import type { SlurmDeploymentRequirement } from "@/domain/platform/types";
import { failureOf, type DisplayedFailure } from "@/presentation/components/failure";

/**
 * Fail-closed loading contract for the Slurm deployment policy used by the wizard; `error` carries
 * the failed read's message and request ID.
 */
export type SlurmRequirementState =
  | { status: "loading" }
  | ({ status: "error" } & DisplayedFailure)
  | { status: "ready"; requirement: SlurmDeploymentRequirement };

/**
 * Loads the global Slurm requirement and exposes an explicit reload for stale-policy
 * rejections. Until the provider answers, callers must not treat the policy as disabled.
 */
export function useSlurmDeploymentRequirement(): {
  state: SlurmRequirementState;
  reload: () => void;
} {
  const { platforms } = useApp();
  const [state, setState] = useState<SlurmRequirementState>({
    status: "loading",
  });
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => {
    setState({ status: "loading" });
    setRevision((current) => current + 1);
  }, []);

  useEffect(() => {
    let cancelled = false;
    platforms
      .getSlurmDeploymentRequirement()
      .then((requirement) => {
        if (!cancelled) setState({ status: "ready", requirement });
      })
      .catch((error: Error) => {
        if (!cancelled) setState({ status: "error", ...failureOf(error, "The Slurm deployment requirement could not be loaded.") });
      });
    return () => {
      cancelled = true;
    };
  }, [platforms, revision]);

  return { state, reload };
}
