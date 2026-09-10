import { useCallback, useEffect, useState } from "react";
import {
  Alert,
  AlertVariant,
  Button,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
} from "@patternfly/react-core";
import { DownloadIcon, SyncAltIcon } from "@patternfly/react-icons";
import { Copy } from "lucide-react";
import { LogViewer, LogViewerSearch } from "@patternfly/react-log-viewer";
import { useApp } from "@/di/AppProvider";
import { copyText } from "@/presentation/utils/clipboard";
import { useAppearance } from "@/presentation/app/theme/appearanceContext";
import { EmptyState } from "@/presentation/components/EmptyState";
import { ErrorState } from "@/presentation/components/ErrorState";
import { LoadingState } from "@/presentation/components/LoadingState";

type LogsState =
  | { status: "loading" }
  | { status: "error"; message: string }
  | { status: "ready"; text: string };
type CopyFeedback = { title: string; variant: AlertVariant } | null;

/**
 * PatternFly retained-output workspace for a legacy run or one durable Step. `variant` selects
 * which stream to show: "stdout" is the full runner output; "stderr" is the focused error-only
 * report (failed/unreachable tasks and their stderr/stdout), so a failing Step's cause is
 * readable without scrolling the whole play. The stderr stream is per-Step only.
 */
export function OperationLogWorkspace({
  operationId,
  stepId,
  variant = "stdout",
}: {
  operationId: string;
  stepId?: string;
  variant?: "stdout" | "stderr";
}) {
  const { operations } = useApp();
  const { resolved } = useAppearance();
  const [state, setState] = useState<LogsState>({ status: "loading" });
  const [copyFeedback, setCopyFeedback] = useState<CopyFeedback>(null);
  const [nonce, setNonce] = useState(0);
  const reload = useCallback(() => setNonce((value) => value + 1), []);
  const noun = variant === "stderr" ? "stderr" : "stdout";

  useEffect(() => {
    let cancelled = false;
    const request =
      variant === "stderr" && stepId
        ? operations.getStepStderr(operationId, stepId)
        : stepId
          ? operations.getStepLogs(operationId, stepId)
          : operations.getLogs(operationId);
    request
      .then((text) => {
        if (!cancelled) setState({ status: "ready", text });
      })
      .catch((error: Error) => {
        if (!cancelled) setState({ status: "error", message: error.message });
      });
    return () => {
      cancelled = true;
    };
  }, [nonce, operationId, operations, stepId, variant]);

  if (state.status === "loading") return <LoadingState rows={5} />;
  if (state.status === "error")
    return <ErrorState message={state.message} onRetry={reload} />;
  if (!state.text.trim()) {
    return variant === "stderr" ? (
      <EmptyState
        title="No errors"
        message="This Step recorded no failed or unreachable tasks."
      />
    ) : (
      <EmptyState
        title="No stdout yet"
        message="Output appears after the executor starts producing stdout."
      />
    );
  }

  const copy = async () => {
    // copyText falls back to a legacy copy on insecure origins (LAN HTTP); report the real
    // outcome instead of assuming success up front.
    const ok = await copyText(state.text);
    setCopyFeedback(
      ok
        ? { title: `${noun} copied`, variant: AlertVariant.success }
        : {
            title: `Could not copy ${noun}. Clipboard access is unavailable in this browser.`,
            variant: AlertVariant.danger,
          },
    );
  };
  const download = () => {
    const url = URL.createObjectURL(
      new Blob([state.text], { type: "text/plain;charset=utf-8" }),
    );
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `swallow-operation-${operationId}${stepId ? `-${stepId}` : ""}-${noun}.log`;
    anchor.click();
    URL.revokeObjectURL(url);
  };
  const toolbar = (
    <Toolbar>
      <ToolbarContent>
        <ToolbarItem variant="label">
          <LogViewerSearch placeholder={`Search ${noun}`} minSearchChars={1} />
        </ToolbarItem>
        <ToolbarItem align={{ default: "alignEnd" }}>
          <Button
            variant="plain"
            icon={<Copy />}
            aria-label={`Copy ${noun}`}
            onClick={() => void copy()}
          />
        </ToolbarItem>
        <ToolbarItem>
          <Button
            variant="plain"
            icon={<DownloadIcon />}
            aria-label={`Download ${noun}`}
            onClick={download}
          />
        </ToolbarItem>
        <ToolbarItem>
          <Button
            variant="plain"
            icon={<SyncAltIcon />}
            aria-label={`Refresh ${noun}`}
            onClick={reload}
          />
        </ToolbarItem>
      </ToolbarContent>
    </Toolbar>
  );
  return (
    <>
      {copyFeedback && (
        <Alert
          variant={copyFeedback.variant}
          title={copyFeedback.title}
          isInline
        />
      )}
      <LogViewer
        data={state.text}
        hasLineNumbers
        height={520}
        theme={resolved}
        toolbar={toolbar}
        aria-label={`Operation ${noun}`}
      />
    </>
  );
}
