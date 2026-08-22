/**
 * Computes the SHA-256 hex digest of a password string.
 *
 * Raw passwords must never be transmitted to the API.
 * Call this function before passing credentials to any auth service or API layer.
 */
export async function hashPassword(password: string): Promise<string> {
  const encoder = new TextEncoder()
  const data = encoder.encode(password)
  const buffer = await crypto.subtle.digest('SHA-256', data)
  return Array.from(new Uint8Array(buffer))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}
