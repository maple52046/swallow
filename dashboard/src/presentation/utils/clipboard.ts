/**
 * Copy `value` to the clipboard, resolving to whether it succeeded.
 *
 * The async Clipboard API (`navigator.clipboard`) only exists in a secure context — HTTPS or
 * localhost. Swallow's dashboard is frequently served over plain HTTP on a LAN, where
 * `window.isSecureContext` is false and `navigator.clipboard` is undefined, so a direct
 * `writeText` throws and copy silently does nothing. This helper falls back to a temporary
 * `<textarea>` plus the legacy `document.execCommand('copy')`, which works on insecure origins,
 * and returns `false` only when both paths fail so callers can show a failure state.
 *
 * It is the single source for clipboard writes in the dashboard; compose it rather than calling
 * `navigator.clipboard` directly.
 */
export async function copyText(value: string): Promise<boolean> {
  if (typeof navigator !== "undefined" && navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      // Permission denied or transient failure: fall through to the execCommand fallback.
    }
  }
  return copyViaTextarea(value);
}

/** Selector for an open modal surface; Chakra (Ark) dialogs mark their content with these. */
const OPEN_DIALOG_SELECTOR = '[role="dialog"][data-state="open"], [role="alertdialog"][data-state="open"]';

/**
 * Where the temporary textarea must live so it can take focus. An open modal traps focus inside
 * itself and immediately pulls focus back from anything outside, so a textarea appended to
 * `document.body` would never be selected and `execCommand('copy')` would copy nothing while still
 * reporting success. The textarea therefore joins the dialog that holds focus (or, when the click
 * did not focus the trigger — Safari does not focus buttons on click — the topmost open dialog);
 * with no dialog open, `document.body` is fine.
 */
function focusScopeHost(): HTMLElement {
  const fromFocus = document.activeElement?.closest<HTMLElement>(OPEN_DIALOG_SELECTOR);
  if (fromFocus) return fromFocus;
  const open = document.querySelectorAll<HTMLElement>(OPEN_DIALOG_SELECTOR);
  return open.length > 0 ? open[open.length - 1] : document.body;
}

/**
 * Insecure-origin fallback: place the text in an off-screen, read-only textarea inside the active
 * focus scope, focus and select it, and run the legacy copy command. Success is reported only when
 * the textarea really held focus and the whole value was selected, because `execCommand('copy')`
 * returns true even when the selection is empty. The operator's prior selection and focus are
 * restored so copying never disturbs what they had highlighted or where keyboard focus was.
 */
function copyViaTextarea(value: string): boolean {
  if (typeof document === "undefined") return false;
  const host = focusScopeHost();
  const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  const selection = document.getSelection();
  const previousRange =
    selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null;

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  // Transient and invisible: keep it out of the accessibility tree, the tab order, and the layout.
  textarea.setAttribute("aria-hidden", "true");
  textarea.tabIndex = -1;
  textarea.style.position = "fixed";
  textarea.style.top = "0";
  textarea.style.left = "-9999px";
  textarea.style.opacity = "0";
  textarea.style.pointerEvents = "none";
  host.appendChild(textarea);

  textarea.focus({ preventScroll: true });
  textarea.select();
  const selected =
    document.activeElement === textarea &&
    textarea.selectionStart === 0 &&
    textarea.selectionEnd === value.length;

  let copied = false;
  if (selected) {
    try {
      copied = document.execCommand("copy");
    } catch {
      copied = false;
    }
  }

  host.removeChild(textarea);
  previousFocus?.focus({ preventScroll: true });
  if (previousRange && selection) {
    selection.removeAllRanges();
    selection.addRange(previousRange);
  }
  return copied;
}
