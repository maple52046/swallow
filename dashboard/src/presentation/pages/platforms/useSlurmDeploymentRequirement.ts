import { useCallback, useEffect, useState } from "react";
import { useApp } from "@/di/AppProvider";
import type { SlurmDeploymentRequirement } from "@/domain/platform/types";

/** Fail-closed loading contract for the Slurm deployment policy used by the wizard. */
export type SlurmRequirementState =
  | { status: "loading" }
  | { status: "error"; message: string }
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
        if (!cancelled) setState({ status: "error", message: error.message });
      });
    return () => {
      cancelled = true;
    };
  }, [platforms, revision]);

  return { state, reload };
}
