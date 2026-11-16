import { Box, Code, Stack } from '@chakra-ui/react'
import type { ValidationSuite } from '@/domain/validation/types'
import { suiteStarterLevel } from '@/domain/validation/access'
import { CodeBlock } from '@/presentation/components/CodeBlock'
import { KeyValueGrid } from '@/presentation/components/ConsolePrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { STARTER_LEVEL_COPY, SUDO_COPY, suiteAuthorName } from './validationPresentation'

interface UserSuiteSummaryProps {
  /** A user suite (`source: user`); the fields this shows are null on a module suite. */
  suite: ValidationSuite
  /**
   * The signed-in User's username when the summary previews a run they are about to start: it then
   * says the script runs as their account. Omitted on the suite's own page, where any User may run it.
   */
  starterUsername?: string
  /** Shows the SHA-256 of the script, which the suite page keeps for audit. */
  showSha?: boolean
}

/**
 * What a user suite is and how it runs, shared by the suite's page and the run dialog's review so
 * both say the same: its author, revision, who may start it, whether it uses sudo, its timeout, and
 * the script itself, read-only and as stored (every User reads it, which is why it must hold no
 * secret). In the run dialog it also states the account the run uses on every target, and both
 * places say that whoever can read a run reads its output.
 */
export function UserSuiteSummary({ suite, starterUsername, showSha = false }: UserSuiteSummaryProps) {
  const level = suiteStarterLevel(suite)
  const sudo = suite.sudo === true
  // A sudo suite is worded as a requirement, not a promise: the dashboard does not know the
  // starter's sudo on each target, and the API refuses the run where it is missing or needs a password.
  const runsAs = starterUsername
    ? `Your account ${starterUsername}${sudo ? ', through sudo: this needs your passwordless sudo on every target, under the Pool’s Sudo Policy' : ''}`
    : `The account of the User who starts it${sudo ? ', through sudo: this needs their passwordless sudo on every target, under the Pool’s Sudo Policy' : ''}`
  return (
    <Stack gap="4">
      <KeyValueGrid items={[
        { label: 'Created by', value: suiteAuthorName(suite) },
        { label: 'Revision', value: suite.revision ?? '—' },
        { label: 'Runs as', value: runsAs },
        { label: 'Sudo', value: `${sudo ? 'Yes' : 'No'} — ${SUDO_COPY[sudo ? 'on' : 'off']}` },
        { label: 'Who can start', value: `${STARTER_LEVEL_COPY[level].label} — ${STARTER_LEVEL_COPY[level].description}` },
        { label: 'Timeout', value: suite.timeoutSeconds ? `${suite.timeoutSeconds} seconds` : '—' },
        ...(showSha ? [{ label: 'SHA-256', value: <Code fontSize="xs" wordBreak="break-all">{suite.scriptSha256 ?? '—'}</Code> }] : []),
      ]} />
      <Box maxH="20rem" overflowY="auto" rounded="md">
        <CodeBlock code={suite.script ?? ''} language="bash" aria-label={`Script of ${suite.name}, revision ${suite.revision ?? ''}`} />
      </Box>
      <Alert status="info" title="Everyone who can read a run reads its output">
        The tail of what the script prints is kept as the run’s evidence, redacted, and shown to every User who may read the run’s targets.
      </Alert>
    </Stack>
  )
}
