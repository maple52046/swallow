import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type MouseEvent,
} from "react";
import { Button, Tooltip } from "@patternfly/react-core";
import { Check, Copy } from "lucide-react";

interface CopyButtonProps {
  /** The exact text placed on the clipboard (e.g. a hostname, MAC, or IP address). */
  value: string;
  /**
   * Accessible name and idle tooltip for the control, phrased as an action — for example
   * "Copy hostname". It is the only cue a screen-reader user gets, so it must name the field.
   */
  label: string;
  /** Optional extra class for cell- or context-specific sizing. */
  className?: string;
}

/**
 * `CopyButton` — an inline, icon-only button that copies a single field value to the
 * clipboard.
 *
 * Usage: rendered next to a displayed identifier (hostname, MAC, IP) in dense tables and
 * detail views so an operator can grab the value without selecting text. It is the shared
 * source for inline value copying; compose it rather than re-implementing `navigator.clipboard`.
 *
 * Behaviour and contracts:
 * - Renders nothing when `value` is empty, so callers may pass an optional/absent field
 *   directly without guarding (an unobserved address shows no dangling button).
 * - Isolates its click (`stopPropagation`) so it is safe inside a clickable table row and
 *   never triggers row navigation.
 * - Confirms success by swapping the icon and tooltip to a transient "Copied" state (status
 *   conveyed by icon + text, never colour alone); the state auto-resets and is cleaned up on
 *   unmount.
 * - Treats clipboard access as best-effort: a denied permission or insecure origin fails
 *   quietly and leaves the idle icon, because copying is a convenience, not a critical path.
 */
export function CopyButton({ value, label, className }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  // Hold the reset timer so a rapid second copy, or an unmount, cannot leave a pending
  // setState firing against a gone component.
  const resetTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(
    () => () => {
      if (resetTimer.current) clearTimeout(resetTimer.current);
    },
    [],
  );

  const copy = useCallback(
    async (event: MouseEvent<HTMLButtonElement>) => {
      event.stopPropagation();
      try {
        await navigator.clipboard.writeText(value);
        setCopied(true);
        if (resetTimer.current) clearTimeout(resetTimer.current);
        resetTimer.current = setTimeout(() => setCopied(false), 1500);
      } catch {
        // Clipboard access can be blocked (permissions or a non-secure origin); copying is
        // only a convenience here, so fail silently instead of surfacing an error.
      }
    },
    [value],
  );

  if (!value) return null;
  return (
    <Tooltip content={copied ? "Copied" : label}>
      <Button
        variant="plain"
        className={["sw-copy-button", className].filter(Boolean).join(" ")}
        aria-label={label}
        icon={copied ? <Check /> : <Copy />}
        onClick={copy}
      />
    </Tooltip>
  );
}
