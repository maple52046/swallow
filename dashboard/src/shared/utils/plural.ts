/**
 * `word` pluralised with a trailing "s" for any count other than one, for console copy such as
 * "1 destructive action" / "2 destructive actions" / "0 records".
 *
 * Shared by every feature that states a count in English sentences (Platform removal dialogs and
 * toasts, the Change Plan approval statement), so they never disagree on singular wording. It only
 * handles regular nouns; a word with an irregular plural must be written out by the caller.
 */
export function plural(count: number, word: string): string {
  return count === 1 ? word : `${word}s`
}
