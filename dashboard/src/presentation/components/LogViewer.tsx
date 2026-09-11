import { Fragment, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Box, Flex, IconButton, Input, InputGroup, Text } from '@chakra-ui/react'
import { ChevronDown, ChevronUp, Copy, Download, RefreshCw, Search } from 'lucide-react'
import { copyText } from '@/presentation/utils/clipboard'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Tooltip } from '@/presentation/components/ui/tooltip'

interface LogViewerProps {
  /** The full retained output to display; newlines separate rendered lines. */
  text: string
  /** Accessible name for the log region and the noun used in action labels (e.g. "stdout"). */
  label: string
  /** Base filename (without extension) used for the download action. */
  downloadName: string
  /** Optional refresh handler; the refresh control is hidden when absent. */
  onRefresh?: () => void
  /** Fixed viewport height in pixels for the scrollable output. */
  height?: number
}

interface Segment {
  text: string
  matchIndex?: number
}

/** Counts case-insensitive occurrences of `query` in `line` (0 when the query is empty). */
function countMatches(line: string, query: string): number {
  if (!query) return 0
  const lower = line.toLowerCase()
  const needle = query.toLowerCase()
  let count = 0
  let found = lower.indexOf(needle)
  while (found !== -1) {
    count += 1
    found = lower.indexOf(needle, found + needle.length)
  }
  return count
}

/** Splits a line into plain/matched segments, tagging each match with its global ordinal. */
function segmentLine(line: string, query: string, startOrdinal: number): Segment[] {
  if (!query) return [{ text: line }]
  const segments: Segment[] = []
  const lower = line.toLowerCase()
  const needle = query.toLowerCase()
  let cursor = 0
  let ordinal = startOrdinal
  let found = lower.indexOf(needle)
  while (found !== -1) {
    if (found > cursor) segments.push({ text: line.slice(cursor, found) })
    segments.push({ text: line.slice(found, found + needle.length), matchIndex: ordinal })
    ordinal += 1
    cursor = found + needle.length
    found = lower.indexOf(needle, cursor)
  }
  if (cursor < line.length) segments.push({ text: line.slice(cursor) })
  return segments
}

/**
 * Retained-output viewer for a run or Step, replacing the previous third-party log
 * viewer with a themed, in-house one.
 *
 * Provides case-insensitive search with match navigation (highlighted in place and
 * scrolled into view), line numbers, and copy/download/refresh actions. Copy and
 * download are best-effort and report their outcome through the shared toast.
 * Rendering is line-based, suited to the bounded operation logs the console shows;
 * it is not virtualized.
 */
export function LogViewer({ text, label, downloadName, onRefresh, height = 520 }: LogViewerProps) {
  const { showToast } = useToast()
  const [query, setQuery] = useState('')
  const [activeMatch, setActiveMatch] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)

  const lines = useMemo(() => text.split('\n'), [text])

  // Build highlighted line content and count total matches so the match counter and
  // navigation share a single source of truth. Per-line match offsets are precomputed as a
  // prefix sum, so the JSX-producing map stays pure (no running mutation during render).
  const { rendered, totalMatches } = useMemo(() => {
    const counts = lines.map((line) => countMatches(line, query))
    const offsets: number[] = []
    let accumulated = 0
    for (let index = 0; index < counts.length; index += 1) {
      offsets[index] = accumulated
      accumulated += counts[index]
    }
    const out: ReactNode[] = lines.map((line, lineIndex) => {
      const segments = segmentLine(line, query, offsets[lineIndex])
      return (
        <Box as="span" display="block" key={lineIndex} css={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
          {segments.map((segment, segmentIndex) =>
            segment.matchIndex === undefined ? (
              <Fragment key={segmentIndex}>{segment.text}</Fragment>
            ) : (
              <Box
                as="mark"
                key={segmentIndex}
                id={`sw-log-match-${segment.matchIndex}`}
                bg={segment.matchIndex === activeMatch ? 'yellow.400' : 'yellow.200'}
                color="black"
                rounded="xs"
              >
                {segment.text}
              </Box>
            ),
          )}
        </Box>
      )
    })
    return { rendered: out, totalMatches: accumulated }
  }, [lines, query, activeMatch])

  // Keep the active match in view whenever it or the query changes.
  useLayoutEffect(() => {
    if (totalMatches === 0) return
    document.getElementById(`sw-log-match-${activeMatch}`)?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }, [activeMatch, totalMatches, query])

  const gutterWidth = `${Math.max(2, String(lines.length).length)}ch`

  const step = (delta: number) => {
    if (totalMatches === 0) return
    setActiveMatch((current) => (current + delta + totalMatches) % totalMatches)
  }

  const copy = async () => {
    const ok = await copyText(text)
    showToast(
      ok
        ? { title: `${label} copied`, tone: 'success' }
        : { title: `Could not copy ${label}`, description: 'Clipboard access is unavailable in this browser.', tone: 'error' },
    )
  }

  const download = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `${downloadName}.log`
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return (
    <Box borderWidth="1px" borderColor="border" rounded="lg" overflow="hidden" bg="bg.panel">
      <Flex align="center" gap="2" p="2" borderBottomWidth="1px" borderColor="border" wrap="wrap">
        <InputGroup startElement={<Search size={16} />} flex="1" minW="12rem" maxW="26rem">
          <Input
            size="sm"
            placeholder={`Search ${label}`}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value)
              setActiveMatch(0)
            }}
          />
        </InputGroup>
        {query && (
          <Text fontSize="sm" color="fg.muted" minW="8ch" textAlign="center">
            {totalMatches === 0 ? 'No matches' : `${activeMatch + 1} / ${totalMatches}`}
          </Text>
        )}
        <IconButton size="sm" variant="ghost" aria-label="Previous match" disabled={totalMatches === 0} onClick={() => step(-1)}>
          <ChevronUp size={16} />
        </IconButton>
        <IconButton size="sm" variant="ghost" aria-label="Next match" disabled={totalMatches === 0} onClick={() => step(1)}>
          <ChevronDown size={16} />
        </IconButton>
        <Box flex="1" />
        <Tooltip content={`Copy ${label}`}>
          <IconButton size="sm" variant="ghost" aria-label={`Copy ${label}`} onClick={() => void copy()}>
            <Copy size={16} />
          </IconButton>
        </Tooltip>
        <Tooltip content={`Download ${label}`}>
          <IconButton size="sm" variant="ghost" aria-label={`Download ${label}`} onClick={download}>
            <Download size={16} />
          </IconButton>
        </Tooltip>
        {onRefresh && (
          <Tooltip content={`Refresh ${label}`}>
            <IconButton size="sm" variant="ghost" aria-label={`Refresh ${label}`} onClick={onRefresh}>
              <RefreshCw size={16} />
            </IconButton>
          </Tooltip>
        )}
      </Flex>
      <Box
        ref={scrollRef}
        role="log"
        aria-label={label}
        overflow="auto"
        height={`${height}px`}
        fontFamily="mono"
        fontSize="xs"
        lineHeight="1.6"
        bg="bg.subtle"
      >
        <Flex as="pre" m="0" minW="max-content">
          <Box
            as="span"
            flexShrink="0"
            width={gutterWidth}
            px="3"
            py="2"
            textAlign="end"
            color="fg.subtle"
            userSelect="none"
            borderInlineEndWidth="1px"
            borderColor="border"
            position="sticky"
            insetStart="0"
            bg="bg.subtle"
          >
            {lines.map((_, index) => (
              <Box as="span" display="block" key={index}>
                {index + 1}
              </Box>
            ))}
          </Box>
          <Box as="code" px="3" py="2" flex="1">
            {rendered}
          </Box>
        </Flex>
      </Box>
    </Box>
  )
}
