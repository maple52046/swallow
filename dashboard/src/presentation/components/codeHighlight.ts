import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import githubLightCss from 'highlight.js/styles/github.css?inline'
import githubDarkCss from 'highlight.js/styles/github-dark.css?inline'

/** Languages {@link highlightCode} can color; add one by registering its highlight.js grammar below. */
export type CodeLanguage = 'bash'

/**
 * A private highlight.js instance with only the grammars the console shows, so the bundle does not
 * carry every language and no other module can change what this one registers.
 */
const highlighter = hljs.newInstance()
highlighter.registerLanguage('bash', bash)

/**
 * Returns `code` as highlight.js HTML. highlight.js escapes the source text, so the result is safe
 * to inject even for operator-supplied input; it contains only `<span class="hljs-…">` markup.
 */
export function highlightCode(code: string, language: CodeLanguage): string {
  return highlighter.highlight(code, { language, ignoreIllegals: true }).value
}

/**
 * Prefixes every selector of a highlight.js theme with `scope`, so the light and dark themes can
 * be loaded together and each applies only under its color mode. Theme files are flat rule lists
 * (no at-rules), which is all this handles; comments are dropped first so a comma in one cannot
 * split a selector.
 */
function scopeTheme(css: string, scope: string): string {
  return css
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/([^{}]+)\{/g, (_match, prelude: string) =>
      `${prelude.split(',').map((selector) => `${scope} ${selector.trim()}`).join(', ')} {`,
    )
}

let themeCss: string | undefined

/**
 * The GitHub light and dark highlight themes, scoped to the color-mode class next-themes puts on
 * `<html>` (`light` / `dark`), so code blocks follow the console's appearance — including a manual
 * override — without re-rendering. Computed on first use and then reused.
 */
export function highlightThemeCss(): string {
  themeCss ??= `${scopeTheme(githubLightCss, ':root.light')}\n${scopeTheme(githubDarkCss, ':root.dark')}`
  return themeCss
}
