/**
 * Saves text the console already holds as a file through the browser's download flow.
 *
 * Used for content fetched with the operator's session (a log, a generated private key, the
 * Server enrollment script), where a plain link cannot carry the bearer token. The object URL
 * is revoked right after the click, so the content stays only in the saved file and in the
 * caller's own state; callers decide whether that content may be written to disk at all.
 */
export function downloadTextFile(fileName: string, text: string, type = 'text/plain;charset=utf-8'): void {
  const url = URL.createObjectURL(new Blob([text], { type }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName
  anchor.click()
  URL.revokeObjectURL(url)
}
