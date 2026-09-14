import { Stack, Text } from '@chakra-ui/react'
import { Alert } from '@/presentation/components/ui/alert'
import { credentialGuidance } from './credentialGuidance'

/**
 * Explains, in plain terms, what an Integration credential is for a given provider.
 *
 * Rendered wherever an operator sets or replaces a credential (create and replace dialogs), so
 * the abstract, write-only secret is never presented without saying what it maps to and what it
 * affects. The copy is provider-specific (keyed by `providerKind`) and always states the
 * write-only rule, because that surprises operators who expect to read the value back.
 */
export function CredentialGuidanceNote({ providerKind }: { providerKind: string }) {
  const guidance = credentialGuidance(providerKind)
  return (
    <Alert status="info" title={guidance.term}>
      <Stack gap="1">
        <Text>{guidance.purpose}</Text>
        <Text>{guidance.whatItIs}</Text>
        <Text>
          <Text as="span" fontWeight="semibold">
            Scope:
          </Text>{' '}
          {guidance.impact}
        </Text>
        <Text color="fg.muted">
          Write-only: Swallow stores it encrypted and never shows it again — you can only replace it.
          {guidance.optional ? ' Optional: leave empty for anonymous access.' : ''}
        </Text>
      </Stack>
    </Alert>
  )
}
