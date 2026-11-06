import {
  hasDuplicateWords,
  isUserDeclared,
  isValidSourceName,
  parseWordList,
  type RepositorySource,
  type RepositorySourceSpec,
} from '@/domain/localRepository/types'

/**
 * Why `value` cannot be a Repository Source's upstream URL, or null when it can: the contract wants
 * an `http` or `https` URL with a host and without user credentials, because the spec is stored and
 * shown in plain text.
 */
function sourceUrlProblem(value: string): string | null {
  if (!value) return 'Enter the archive URL.'
  let url: URL
  try {
    url = new URL(value)
  } catch {
    // Not parseable at all, typically a missing scheme ("archive.ubuntu.com/ubuntu").
    return 'Use an http:// or https:// URL.'
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return 'Use an http:// or https:// URL.'
  if (!url.hostname) return 'Name the archive host.'
  if (url.username || url.password) return 'Leave the user and password out; the spec is shown in plain text.'
  return null
}

/** The Add source form of the Manage user sources dialog as typed; list fields are free text. */
export interface DebSourceDraft {
  name: string
  url: string
  /** Words separated by commas or spaces, e.g. "noble, noble-updates". */
  suites: string
  components: string
  architectures: string
}

/** The empty Add source form. */
export const EMPTY_DEB_SOURCE_DRAFT: DebSourceDraft = { name: '', url: '', suites: '', components: '', architectures: '' }

/** Why each field of a draft cannot be added, keyed by field; an absent key is a valid field. */
export type DebSourceProblems = Partial<Record<keyof DebSourceDraft, string>>

/** What a new source is checked against: the draft list it joins and the repository's effective sources. */
interface DebSourceContext {
  userSources: readonly RepositorySourceSpec[]
  effectiveSources: readonly RepositorySource[]
}

/**
 * Why a typed `deb` source cannot join the user's sources, per field.
 *
 * It mirrors the contract's rules (local-repository.md, Replace The User's Sources) only so the
 * dialog can flag a typo before the write: a valid name not already in the list, an `http`/`https`
 * URL with a host and without credentials (the spec is shown in plain text), at least one suite and
 * one component, and each word listed once. A name a module (or a pull-through source) already uses
 * is refused only when the format or URL differs, which is exactly when the API refuses it as a
 * contradiction; the same format and URL merge into the module's source. The API stays the
 * validator of record.
 */
export function debSourceProblems(draft: DebSourceDraft, context: DebSourceContext): DebSourceProblems {
  const problems: DebSourceProblems = {}
  const name = draft.name.trim()
  const url = draft.url.trim()
  if (!name) problems.name = 'Enter a name.'
  else if (!isValidSourceName(name)) {
    problems.name = 'Use 1 to 63 lower-case letters, digits, ".", "_", or "-", starting and ending with a letter or digit.'
  } else if (context.userSources.some((source) => source.name === name)) problems.name = `You already have a source named ${name}.`
  else {
    const declared = context.effectiveSources.find((source) => source.name === name && !isUserDeclared(source))
    if (declared && (declared.format !== 'deb' || declared.url !== url)) {
      problems.name = `A module already declares ${name} with another format or URL; choose another name.`
    }
  }

  const urlProblem = sourceUrlProblem(url)
  if (urlProblem) problems.url = urlProblem

  const suites = parseWordList(draft.suites)
  if (suites.length === 0) problems.suites = 'Enter at least one suite, for example noble.'
  else if (hasDuplicateWords(suites)) problems.suites = 'List each suite once.'

  const components = parseWordList(draft.components)
  if (components.length === 0) problems.components = 'Enter at least one component, for example main.'
  else if (hasDuplicateWords(components)) problems.components = 'List each component once.'

  if (hasDuplicateWords(parseWordList(draft.architectures))) problems.architectures = 'List each architecture once.'
  return problems
}

/** The spec source a valid draft stands for; call only when {@link debSourceProblems} found nothing. */
export function debSourceFromDraft(draft: DebSourceDraft): RepositorySourceSpec {
  return {
    name: draft.name.trim(),
    format: 'deb',
    url: draft.url.trim(),
    suites: parseWordList(draft.suites),
    components: parseWordList(draft.components),
    architectures: parseWordList(draft.architectures),
  }
}
