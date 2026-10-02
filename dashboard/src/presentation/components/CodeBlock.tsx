import { Box } from '@chakra-ui/react'
import { useMemo } from 'react'
import { highlightCode, highlightThemeCss, type CodeLanguage } from './codeHighlight'

interface CodeBlockProps {
  /** Source text shown verbatim; it is escaped by highlight.js before injection. */
  code: string
  language: CodeLanguage
  /** Names the block for assistive technology when surrounding text does not, e.g. "CLI example". */
  'aria-label'?: string
}

/**
 * The console's read-only, syntax-colored code sample (highlight.js with the GitHub light and dark
 * themes, following the current color mode). Use it for commands an operator copies, such as CLI
 * examples, rather than hand-styled `<pre>` blocks, so every sample looks the same.
 *
 * The theme stylesheet is a React 19 style resource: React hoists it into `<head>` once, however many
 * blocks are mounted. Highlighting is memoized per code and language. Long lines scroll horizontally
 * instead of wrapping, so a command reads exactly as it should be typed.
 */
export function CodeBlock({ code, language, 'aria-label': ariaLabel }: CodeBlockProps) {
  const html = useMemo(() => highlightCode(code, language), [code, language])
  return (
    <>
      <style href="swallow-highlight-github" precedence="default">{highlightThemeCss()}</style>
      <Box
        as="pre"
        aria-label={ariaLabel}
        className="sw-code-block"
        m="0"
        fontSize="xs"
        lineHeight="1.6"
        rounded="md"
        borderWidth="1px"
        borderColor="border.muted"
        overflow="hidden"
      >
        {/* highlight.js output: escaped source wrapped in token spans (see highlightCode). */}
        <code className={`hljs language-${language} sw-mono`} dangerouslySetInnerHTML={{ __html: html }} />
      </Box>
    </>
  )
}
