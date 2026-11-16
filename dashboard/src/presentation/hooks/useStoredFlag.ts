import { useCallback, useState } from 'react'

function readFlag(key: string): boolean {
  try {
    return localStorage.getItem(key) === 'true'
  } catch {
    // Unavailable storage (private mode, a blocked origin) keeps the default: off.
    return false
  }
}

/**
 * An on/off UI preference kept in this browser's localStorage under `key`, off by default, with its
 * setter. Only presentation choices belong here (what a list shows), never data about swallow,
 * credentials, or anything the API decides: the value is readable by any script on the origin and
 * travels nowhere. Storage that is unavailable or full only makes the choice last until the page
 * reloads.
 *
 * Each component reads the stored value when it mounts; two components mounted at once do not sync
 * with each other, so a dialog that must follow a page's switch remounts on every opening.
 */
export function useStoredFlag(key: string): [boolean, (on: boolean) => void] {
  const [on, setOn] = useState(() => readFlag(key))
  const update = useCallback((next: boolean) => {
    setOn(next)
    try {
      localStorage.setItem(key, String(next))
    } catch {
      // A full or blocked storage only means the choice lasts until the page reloads.
    }
  }, [key])
  return [on, update]
}
