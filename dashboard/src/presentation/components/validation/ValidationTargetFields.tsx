import { useEffect, useMemo, useState } from 'react'
import { Box, Button, Fieldset, Field, HStack, Stack, Text, Wrap } from '@chakra-ui/react'
import { X } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { Platform } from '@/domain/platform/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { canOperatePlatform, canOperatePool } from '@/domain/user/access'
import type { ValidationSuite } from '@/domain/validation/types'
import { failureOf, failureText } from '@/presentation/components/failure'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { DebouncedSearchInput } from '@/presentation/components/ui/debounced-search-input'
import { Select } from '@/presentation/components/ui/select'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useServerNames } from '@/presentation/hooks/useServerNames'

/** How many matching deployed Servers the picker lists at once; a search narrows the rest. */
const SERVER_RESULTS = 50

/** Why a listed target cannot be chosen: running validation is Pool operate (validation.md). */
const POOL_OWNERS_ONLY = 'Pool owners only'

interface ValidationTargetFieldsProps {
  suite: ValidationSuite
  serverIds: readonly string[]
  onServerIdsChange: (serverIds: string[]) => void
  platformId: string
  onPlatformIdChange: (platformId: string) => void
  /** Keeps control ids unique where two dialogs could be mounted. */
  idPrefix: string
  disabled?: boolean
}

/**
 * Where a run or a schedule of `suite` validates: deployed Servers of the scoped Site for a
 * `servers` suite, or one Platform of the suite's Platform type for a `platform` suite. The Run and
 * Schedule dialogs compose it, so both choose targets the same way.
 *
 * The lists are hints, not the rule: the API checks the targets again when the run or schedule is
 * written (deployed, one Site, not locked or busy, and the caller owns their Pool) and its refusal is
 * what the dialog shows. A target in a Pool the user does not own is listed but cannot be chosen.
 */
export function ValidationTargetFields(props: ValidationTargetFieldsProps) {
  return props.suite.target === 'platform' ? <PlatformTargetField {...props} /> : <ServerTargetField {...props} />
}

type ServerResults =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; servers: Server[]; total: number }

/**
 * Picks deployed Servers of the scoped Site. It reads one page of matches (`listServers` with
 * `provisioningState=deployed`, at most {@link SERVER_RESULTS}) for the committed search, dropping
 * a read that a newer search replaced; chosen Servers stay chosen while the search changes and are
 * listed above it by name, each with a Remove button. A failed read is shown in place of the list.
 */
function ServerTargetField({ serverIds, onServerIdsChange, idPrefix, disabled = false }: ValidationTargetFieldsProps) {
  const { servers } = useApp()
  const { siteId, sites } = useSiteScope()
  const { currentUser } = useAuth()
  const [keyword, setKeyword] = useState('')
  const searchKey = `${siteId ?? ''}|${keyword}`
  // The answer is kept with the search it answers, so a new search reads as loading until it lands.
  const [stored, setStored] = useState<{ key: string; results: ServerResults }>({ key: '', results: { status: 'loading' } })
  const results: ServerResults = stored.key === searchKey ? stored.results : { status: 'loading' }
  const names = useServerNames(serverIds)
  const [selectionSiteId, setSelectionSiteId] = useState<string>()
  const siteName = new Map(sites.map((site) => [site.id, site.name]))

  // One read per committed search; `cancelled` drops the answer of a search that was replaced.
  useEffect(() => {
    let cancelled = false
    servers
      .listServers({ siteId, provisioningState: 'deployed', keyword: keyword || undefined, pageSize: SERVER_RESULTS })
      .then((page) => {
        if (!cancelled) setStored({ key: searchKey, results: { status: 'ready', servers: page.items, total: page.total } })
      })
      .catch((caught: unknown) => {
        if (!cancelled) setStored({ key: searchKey, results: { status: 'error', message: failureText(failureOf(caught, 'Could not load the deployed Servers.')) } })
      })
    return () => {
      cancelled = true
    }
  }, [servers, siteId, keyword, searchKey])

  const chosen = useMemo(() => new Set(serverIds), [serverIds])
  const listed = results.status === 'ready' ? results.servers : []
  const nameOf = (id: string) => names.get(id) ?? listed.find((server) => server.id === id)?.status.hostname ?? id
  const effectiveSelectionSiteId = selectionSiteId ?? listed.find((server) => serverIds.includes(server.id))?.status.source.siteId
  const toggle = (id: string, on: boolean, targetSiteId?: string) => {
    if (on) {
      if (!siteId && !effectiveSelectionSiteId && targetSiteId) setSelectionSiteId(targetSiteId)
      onServerIdsChange([...serverIds, id])
      return
    }
    const next = serverIds.filter((other) => other !== id)
    if (next.length === 0) setSelectionSiteId(undefined)
    onServerIdsChange(next)
  }

  return (
    <Fieldset.Root disabled={disabled}>
      <Fieldset.Legend fontWeight="medium" fontSize="sm">Servers</Fieldset.Legend>
      <Fieldset.HelperText>Deployed Servers of this Site. The run checks each one again when it is started.</Fieldset.HelperText>
      <Stack gap="3" mt="2">
        {serverIds.length > 0 ? (
          <Wrap gap="2" aria-label="Chosen Servers" role="list">
            {serverIds.map((id) => (
              <HStack key={id} role="listitem" gap="1" borderWidth="1px" rounded="md" ps="2" pe="1" py="0.5">
                <Text fontSize="sm">{nameOf(id)}</Text>
                <Button size="2xs" variant="ghost" aria-label={`Remove ${nameOf(id)}`} onClick={() => toggle(id, false)} disabled={disabled}>
                  <X size={12} aria-hidden />
                </Button>
              </HStack>
            ))}
          </Wrap>
        ) : (
          <Text fontSize="sm" color="fg.muted">No Server chosen yet.</Text>
        )}
        <DebouncedSearchInput value={keyword} onCommit={setKeyword} placeholder="Search deployed Servers" aria-label="Search deployed Servers" />
        <Box maxH="56" overflowY="auto" borderWidth="1px" rounded="md" p="2" id={`${idPrefix}-servers`}>
          {results.status === 'loading' && <Text fontSize="sm" color="fg.muted" role="status">Loading deployed Servers…</Text>}
          {results.status === 'error' && <Text fontSize="sm" color="fg.error" role="alert">{results.message}</Text>}
          {results.status === 'ready' && listed.length === 0 && <Text fontSize="sm" color="fg.muted">No deployed Server matches.</Text>}
          {results.status === 'ready' && (
            <Stack gap="1">
              {listed.map((server) => {
                const operable = canOperatePool(currentUser, server.spec.poolId)
                return (
                  <Checkbox
                    key={server.id}
                    checked={chosen.has(server.id)}
                    onCheckedChange={(on) => toggle(server.id, on, server.status.source.siteId)}
                    disabled={disabled || !operable || Boolean(!siteId && effectiveSelectionSiteId && server.status.source.siteId !== effectiveSelectionSiteId)}
                  >
                    {serverDisplayName(server)}
                    {!siteId && server.status.source.siteId ? ` · ${siteName.get(server.status.source.siteId) ?? server.status.source.siteId}` : ''}
                    {!operable ? ` · ${POOL_OWNERS_ONLY}` : ''}
                  </Checkbox>
                )
              })}
              {results.total > listed.length && (
                <Text fontSize="xs" color="fg.muted">Showing {listed.length} of {results.total}; search to narrow the list.</Text>
              )}
            </Stack>
          )}
        </Box>
      </Stack>
    </Fieldset.Root>
  )
}

/**
 * Picks one Platform of the suite's Platform type in the scoped Site. A Platform that is not
 * `active` is listed but cannot be chosen, since the API validates only active Platforms; its state
 * is named in the option. The Platform list is read once per Site.
 */
function PlatformTargetField({ suite, platformId, onPlatformIdChange, idPrefix, disabled = false }: ValidationTargetFieldsProps) {
  const { platforms } = useApp()
  const { siteId } = useSiteScope()
  const { currentUser } = useAuth()
  const [list, setList] = useState<Platform[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Read once per Site; `cancelled` drops the answer for a Site the user left.
  useEffect(() => {
    let cancelled = false
    platforms
      .listPlatforms(siteId)
      .then((items) => {
        if (!cancelled) setList(items)
      })
      .catch((caught: unknown) => {
        if (!cancelled) setError(failureText(failureOf(caught, 'Could not load the Platforms.')))
      })
    return () => {
      cancelled = true
    }
  }, [platforms, siteId])

  const options = (list ?? [])
    .filter((platform) => platform.type === suite.platformType)
    .map((platform) => {
      const operable = canOperatePlatform(currentUser, platform)
      return {
        value: platform.id,
        label: !operable
          ? `${platform.name} (${POOL_OWNERS_ONLY})`
          : platform.lifecycleState === 'active' ? platform.name : `${platform.name} (${platform.lifecycleState.replaceAll('_', ' ')})`,
        disabled: !operable || platform.lifecycleState !== 'active',
      }
    })
  const id = `${idPrefix}-platform`
  return (
    <Field.Root required disabled={disabled} invalid={error !== null}>
      <Field.Label htmlFor={id}>Platform <Field.RequiredIndicator /></Field.Label>
      <Select
        id={id}
        aria-label="Platform"
        value={platformId}
        onChange={onPlatformIdChange}
        options={options}
        placeholder={list === null && !error ? 'Loading Platforms…' : options.length === 0 ? `No ${suite.platformType} Platform in this Site` : 'Choose a Platform'}
        disabled={disabled || options.length === 0}
      />
      {error ? <Field.ErrorText>{error}</Field.ErrorText> : <Field.HelperText>Only an active {suite.platformType} Platform can be validated.</Field.HelperText>}
    </Field.Root>
  )
}
