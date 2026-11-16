import { diffLines } from 'diff'

/**
 * Line diffs of plain text, the only module that uses the `diff` package (jsdiff): every diff the
 * console shows goes through {@link lineDiff} and is drawn by the shared `TextDiff` component, so a
 * change of library stays in this file.
 */

/** Whether a line is only in the old text, only in the new one, or in both. */
export type DiffLineKind = 'removed' | 'added' | 'unchanged'

/**
 * One line of a line diff. `oldNumber` and `newNumber` are 1-based line numbers in the old and new
 * text; a removed line has no new number and an added line no old number.
 */
export interface DiffLine {
  kind: DiffLineKind
  text: string
  oldNumber: number | null
  newNumber: number | null
}

/**
 * The line-by-line difference from `before` to `after`, in reading order (within a changed block,
 * removed lines come before the lines that replace them). Lines are compared exactly, whitespace
 * included, because a script's whitespace can change what it does. A missing newline at the end of
 * either text does not make its last line differ.
 */
export function lineDiff(before: string, after: string): DiffLine[] {
  const lines: DiffLine[] = []
  let oldNumber = 1
  let newNumber = 1
  for (const change of diffLines(before, after, { ignoreNewlineAtEof: true })) {
    const texts = change.value.split('\n')
    // A block ending in a newline splits into one empty string too many.
    if (texts.length > 1 && texts[texts.length - 1] === '') texts.pop()
    for (const text of texts) {
      if (change.added) lines.push({ kind: 'added', text, oldNumber: null, newNumber: newNumber++ })
      else if (change.removed) lines.push({ kind: 'removed', text, oldNumber: oldNumber++, newNumber: null })
      else lines.push({ kind: 'unchanged', text, oldNumber: oldNumber++, newNumber: newNumber++ })
    }
  }
  return lines
}
