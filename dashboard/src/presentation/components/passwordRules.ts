/**
 * The password rule of the users.md and auth-me.md contracts: at least 8 characters and at most 72
 * bytes (UTF-8), the bcrypt limit. Forms check it before sending so a person learns the rule while
 * typing; the API checks it again and stays the authority.
 *
 * Returns the problem in words, or `null` when the password satisfies the rule.
 */
export function passwordProblem(password: string): string | null {
  if ([...password].length < 8) return 'Use at least 8 characters.'
  if (new TextEncoder().encode(password).length > 72) return 'Use at most 72 bytes; shorten the password.'
  return null
}
