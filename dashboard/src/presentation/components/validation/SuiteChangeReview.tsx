import { Box, Heading, List, Stack, Text } from '@chakra-ui/react'
import type { SuiteChange } from '@/application/usecases/validation/reviewSuiteChange'
import type { SuiteSettingChange } from '@/domain/validation/revisions'
import { suiteSettingChanges } from '@/domain/validation/revisions'
import { CodeBlock } from '@/presentation/components/CodeBlock'
import { TextDiff } from '@/presentation/components/TextDiff'
import { IMPACT_COPY, STARTER_LEVEL_COPY } from './validationPresentation'

interface SuiteChangeReviewProps {
  change: SuiteChange
  /**
   * Why the review appears: the suite changed since the User's own last run of it, or it changed
   * while the run dialog was open (the API refused the start with `409`).
   */
  reason: 'last-run' | 'changed-while-open'
}

/** One changed setting in words, both values named, so the change reads without colour or icons. */
function settingChangeText(change: SuiteSettingChange): string {
  switch (change.field) {
    case 'sudo':
      return `Sudo: ${change.from ? 'yes' : 'no'} → ${change.to ? 'yes' : 'no'}`
    case 'starterLevel':
      return `Who can start: ${STARTER_LEVEL_COPY[change.from].label} → ${STARTER_LEVEL_COPY[change.to].label}`
    case 'timeoutSeconds':
      return `Timeout: ${change.from} → ${change.to} seconds`
    case 'impact':
      return `Impact: ${IMPACT_COPY[change.from].label} → ${IMPACT_COPY[change.to].label}`
  }
}

/**
 * The review a User passes before running a user suite that changed: the revisions it moves
 * between, every changed setting, and a diff of the scripts (`TextDiff`). The run dialog composes
 * it and offers "Use the new version" as the only way on, so a changed script never runs unseen.
 *
 * When the earlier revision was never kept (`previous` null, a revision from before revisions were
 * kept), it says so and shows the whole current script instead of a diff. When the two compared
 * revisions have the same script and settings (revisions in between changed them and changed them
 * back), it says exactly that rather than claiming the script changed. A change that touched
 * only the name or description makes no new revision, so it never reaches this review.
 */
export function SuiteChangeReview({ change, reason }: SuiteChangeReviewProps) {
  const { previous, current, fromRevision } = change
  const from = `revision ${fromRevision}`
  const to = `revision ${current.revision}`
  const heading = reason === 'last-run'
    ? `Changed since your last run: r${fromRevision} → r${current.revision}`
    : `Changed while you were reviewing it: r${fromRevision} → r${current.revision}`
  const settings = previous ? suiteSettingChanges(previous, current) : []
  // Revisions between the two may have changed things and changed them back, so the compared pair can be identical.
  const sameContent = previous !== null && settings.length === 0 && previous.script === current.script

  return (
    <Stack gap="4" role="region" aria-label="Suite changes">
      <Stack gap="1">
        <Heading as="h3" size="sm">{heading}</Heading>
        <Text fontSize="sm" color="fg.muted">
          Read what changed before you run it. Using the new version runs {to} and nothing else; {from} cannot be run any more.
        </Text>
      </Stack>
      {previous && settings.length > 0 && (
        <Box>
          <Text fontWeight="medium" fontSize="sm" mb="1">Changed settings</Text>
          <List.Root fontSize="sm" ps="5">
            {settings.map((setting) => <List.Item key={setting.field}>{settingChangeText(setting)}</List.Item>)}
          </List.Root>
        </Box>
      )}
      {previous && settings.length === 0 && (
        <Text fontSize="sm">
          {sameContent
            ? `Revision ${fromRevision} and ${to} run the same script with the same settings: the revisions in between changed them and changed them back.`
            : 'No setting changed; only the script did.'}
        </Text>
      )}
      {previous ? (
        <TextDiff before={previous.script} after={current.script} beforeLabel={from} afterLabel={to} aria-label="Script changes" />
      ) : (
        <Stack gap="2">
          <Text fontSize="sm">
            The earlier {from} is not available, so its script cannot be compared. Here is the whole script of {to}.
          </Text>
          <Box maxH="20rem" overflowY="auto" rounded="md">
            <CodeBlock code={current.script} language="bash" aria-label={`Script of ${to}`} />
          </Box>
        </Stack>
      )}
    </Stack>
  )
}
