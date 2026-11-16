import { useMemo } from 'react'
import { Box, HStack, Text, VisuallyHidden } from '@chakra-ui/react'
import { lineDiff, type DiffLineKind } from '@/shared/utils/textDiff'

interface TextDiffProps {
  /** The earlier text, as stored; compared line by line with whitespace kept. */
  before: string
  /** The later text. */
  after: string
  /** Names the earlier side in the legend, for example "Revision 2". */
  beforeLabel: string
  /** Names the later side in the legend, for example "Revision 3". */
  afterLabel: string
  /** Names the whole diff for assistive technology, for example "Script changes". */
  'aria-label': string
}

/** How each kind of line is marked: a visible sign and a spoken word, so colour is never the only cue. */
const LINE_MARK: Readonly<Record<DiffLineKind, { sign: string; spoken: string; bg?: string; fg?: string }>> = {
  removed: { sign: '−', spoken: 'Removed', bg: 'red.subtle', fg: 'red.fg' },
  added: { sign: '+', spoken: 'Added', bg: 'green.subtle', fg: 'green.fg' },
  unchanged: { sign: ' ', spoken: 'Unchanged' },
}

/**
 * The console's one read-only, unified line diff of two texts (built on `lineDiff`, the only user of
 * the `diff` package). Any screen that shows what changed in a text, such as the review before a
 * changed Validation Suite runs, composes it rather than drawing its own.
 *
 * Every line keeps its old and new line numbers and a `−`/`+` sign, and screen readers hear
 * "Removed"/"Added" before it, so the change does not depend on colour. A summary above the lines
 * counts what changed; identical texts say so instead of listing every line as unchanged. Long
 * lines wrap so nothing is hidden behind a horizontal scroll on a phone.
 */
export function TextDiff({ before, after, beforeLabel, afterLabel, 'aria-label': ariaLabel }: TextDiffProps) {
  const lines = useMemo(() => lineDiff(before, after), [before, after])
  const added = lines.filter((line) => line.kind === 'added').length
  const removed = lines.filter((line) => line.kind === 'removed').length
  const summary = added === 0 && removed === 0
    ? `No line changed between ${beforeLabel} and ${afterLabel}.`
    : `${added} ${added === 1 ? 'line' : 'lines'} added and ${removed} removed from ${beforeLabel} to ${afterLabel}.`

  return (
    <Box role="group" aria-label={ariaLabel}>
      <HStack gap="4" mb="2" wrap="wrap" fontSize="xs" color="fg.muted">
        <Text>{summary}</Text>
        <Text aria-hidden>− {beforeLabel} · + {afterLabel}</Text>
      </HStack>
      {(added > 0 || removed > 0) && (
        <Box as="pre" className="sw-mono" m="0" fontSize="xs" lineHeight="1.6" rounded="md" borderWidth="1px" borderColor="border.muted" overflow="hidden">
          {lines.map((line, index) => {
            const mark = LINE_MARK[line.kind]
            return (
              // Lines have no identity of their own and the list never reorders, so their position is their key.
              <Box key={index} display="flex" bg={mark.bg} color={mark.fg}>
                <Box as="span" aria-hidden flexShrink={0} w="3.5ch" textAlign="end" pe="1" color="fg.subtle" userSelect="none">{line.oldNumber ?? ''}</Box>
                <Box as="span" aria-hidden flexShrink={0} w="3.5ch" textAlign="end" pe="1" color="fg.subtle" userSelect="none">{line.newNumber ?? ''}</Box>
                <Box as="span" aria-hidden flexShrink={0} w="2ch" textAlign="center" userSelect="none">{mark.sign}</Box>
                <Box as="span" flex="1" minW="0" whiteSpace="pre-wrap" wordBreak="break-word" pe="2">
                  {line.kind !== 'unchanged' && <VisuallyHidden>{mark.spoken}: </VisuallyHidden>}
                  {line.text || ' '}
                </Box>
              </Box>
            )
          })}
        </Box>
      )}
    </Box>
  )
}
