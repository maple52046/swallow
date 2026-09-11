import { createContext, useContext } from 'react'

/**
 * True for the subtree rendered inside a {@link Modal}/Dialog.
 *
 * Why this exists: Chakra's `Select` positioner does not compute a position when
 * it is portalled to `document.body` from inside a focus-trapped `Dialog` — it
 * stays parked at its off-screen placeholder (`translate3d(0, -100vh, 0)`) and the
 * list appears pinned to the top-left corner (this is documented Chakra behavior:
 * do not portal a select's positioner to the body within a dialog). {@link Select}
 * reads this flag and renders its positioner inline when inside a dialog, and
 * portals it to the body otherwise.
 */
export const InsideDialogContext = createContext(false)

/** Returns `true` when the caller is rendered inside a Modal/Dialog. */
export function useInsideDialog(): boolean {
  return useContext(InsideDialogContext)
}
