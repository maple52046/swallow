/**
 * Local storage helpers for UI preferences only.
 *
 * Nothing about swallow is stored client-side. This exists for things like the
 * colour scheme, where the browser is the right owner.
 */

export function lsGet<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    if (raw === null) return fallback
    return JSON.parse(raw) as T
  } catch {
    // Corrupt or unavailable storage is not worth failing a render over.
    return fallback
  }
}

export function lsSet<T>(key: string, value: T): void {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Storage being full or blocked must not break the app.
  }
}
