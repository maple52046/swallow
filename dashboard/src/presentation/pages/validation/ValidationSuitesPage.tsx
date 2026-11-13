import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Badge,
  Box,
  Button,
  Grid,
  Heading,
  HStack,
  Link as ChakraLink,
  Stack,
  Switch,
  Text,
} from '@chakra-ui/react'
import { Blocks, FileCode2, FilePlus2, FlaskConical, Server } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isAdmin } from '@/domain/user/access'
import type { ValidationSuite } from '@/domain/validation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { useToast } from '@/presentation/components/toast/toastContext'
import {
  IMPACT_COPY,
  offeredSuites,
  validationSuiteGroup,
  type ValidationSuiteGroup,
} from '@/presentation/components/validation/validationPresentation'
import { useAuth } from '@/presentation/contexts/AuthContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useShowExperimentalSuites } from '@/presentation/hooks/useShowExperimentalSuites'
import { UserSuiteDialog } from './UserSuiteDialog'
import { ValidationHeader } from './ValidationHeader'

type PageState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; suites: ValidationSuite[] }

const GROUPS: Array<{
  key: ValidationSuiteGroup
  title: string
  description: string
  icon: typeof Blocks
}> = [
  {
    key: 'platform',
    title: 'Platform validation',
    description: 'Assurance capabilities that understand a Platform runtime and its member topology.',
    icon: Blocks,
  },
  {
    key: 'servers',
    title: 'Server validation',
    description: 'Reusable checks that inspect one or more deployed Servers.',
    icon: Server,
  },
  {
    key: 'custom',
    title: 'Custom script suites',
    description: 'Site-independent scripts authored by administrators for organization-specific checks.',
    icon: FileCode2,
  },
]

function suiteTarget(suite: ValidationSuite): string {
  if (suite.target === 'servers') return 'Deployed Servers'
  return suite.platformType ? `${suite.platformType} Platform` : 'Platform'
}

/**
 * Capability catalog for Validation Suites. Grouping uses target and source domain facts rather
 * than module IDs, so new module suites enter the correct section without presentation branches.
 */
export function ValidationSuitesPage() {
  const { validation } = useApp()
  const { currentUser } = useAuth()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [showExperimental, setShowExperimental] = useShowExperimentalSuites()
  const [state, setState] = useState<PageState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const [editor, setEditor] = useState<{ key: number } | null>(null)
  const ticket = useRef(0)

  const load = useCallback(() => {
    const mine = ++ticket.current
    validation.listSuites().then(
      (suites) => {
        if (mine === ticket.current) setState({ status: 'ready', suites })
      },
      (caught: unknown) => {
        if (mine === ticket.current) {
          setState({ status: 'error', ...failureOf(caught, 'Could not load the Validation Suites.') })
        }
      },
    )
  }, [validation])

  useEffect(() => {
    load()
    return () => {
      ticket.current += 1
    }
  }, [load, nonce])

  const catalog = state.status === 'ready' ? state.suites : []
  const { offered: suites } = offeredSuites(catalog, showExperimental)
  const experimentalCount = catalog.filter((suite) => suite.experimental && !suite.retired).length

  return (
    <div className="console-page">
      <ValidationHeader
        subtitle="Choose an assurance capability, understand what it checks, and run it against the right targets."
        actions={
          // Writing a script suite is an admin route (validation.md): a script runs on its targets.
          isAdmin(currentUser) ? (
            <Button colorPalette="brand" onClick={() => setEditor({ key: Date.now() })}>
              <FilePlus2 size={16} aria-hidden /> New script suite
            </Button>
          ) : undefined
        }
      />
      <Box className="sw-section" p={{ base: '4', md: '6' }}>
        <HStack justify="space-between" align="flex-start" gap="4" mb="6" wrap="wrap">
          <Stack gap="1">
            <Text color="brand.fg" fontSize="xs" fontWeight="bold" letterSpacing="0.12em" textTransform="uppercase">
              Assurance capabilities
            </Text>
            <Heading as="h2" size="lg">Validation suites</Heading>
            <Text color="fg.muted">Module capabilities and administrator-authored scripts available in this release.</Text>
          </Stack>
          <Switch.Root checked={showExperimental} onCheckedChange={(details) => setShowExperimental(details.checked === true)}>
            <Switch.HiddenInput />
            <Switch.Control><Switch.Thumb /></Switch.Control>
            <Switch.Label>
              Show experimental suites{state.status === 'ready' && experimentalCount > 0 ? ` (${experimentalCount})` : ''}
            </Switch.Label>
          </Switch.Root>
        </HStack>

        {state.status === 'loading' && <LoadingState rows={6} />}
        {state.status === 'error' && (
          <ErrorState message={state.message} requestId={state.requestId} onRetry={() => setNonce((value) => value + 1)} />
        )}
        {state.status === 'ready' && suites.length === 0 && (
          <EmptyState
            title="No Validation Suites"
            message={experimentalCount > 0 && !showExperimental
              ? 'Only experimental suites are hidden. Turn on Show experimental suites or write a script suite.'
              : 'This release declares no suite. Write a script suite for organization-specific Server checks.'}
          />
        )}
        {state.status === 'ready' && suites.length > 0 && (
          <Stack gap="8">
            {GROUPS.map((group) => {
              const items = suites.filter((suite) => validationSuiteGroup(suite) === group.key)
              if (items.length === 0) return null
              const Icon = group.icon
              return (
                <section key={group.key} aria-labelledby={`validation-group-${group.key}`}>
                  <HStack gap="3" mb="4" align="flex-start">
                    <Box color="brand.fg" bg="brand.subtle" rounded="md" p="2" aria-hidden><Icon size={18} /></Box>
                    <Stack gap="0">
                      <Heading id={`validation-group-${group.key}`} as="h3" size="md">{group.title}</Heading>
                      <Text color="fg.muted" fontSize="sm">{group.description}</Text>
                    </Stack>
                  </HStack>
                  <Grid templateColumns={{ base: '1fr', md: 'repeat(2, minmax(0, 1fr))', xl: 'repeat(3, minmax(0, 1fr))' }} gap="4">
                    {items.map((suite) => (
                      <ChakraLink
                        key={suite.id}
                        asChild
                        display="block"
                        height="full"
                        color="inherit"
                        _hover={{ textDecoration: 'none' }}
                        _focusVisible={{ outline: '2px solid', outlineColor: 'brand.focusRing', outlineOffset: '3px' }}
                      >
                        <RouterLink to={scopedHref(`/validation/suites/${encodeURIComponent(suite.id)}`)}>
                          <Box
                            height="full"
                            minH="15rem"
                            borderWidth="1px"
                            borderColor="border"
                            bg="bg.panel"
                            rounded="xl"
                            p="5"
                            display="flex"
                            flexDirection="column"
                            gap="4"
                            transition="transform 170ms ease, box-shadow 170ms ease, border-color 170ms ease"
                            _hover={{ transform: 'translateY(-2px)', shadow: 'md', borderColor: 'brand.muted' }}
                          >
                            <HStack justify="space-between" align="flex-start" gap="3">
                              <Box color="brand.fg" bg="brand.subtle" rounded="lg" p="2.5" aria-hidden>
                                {suite.source === 'user' ? <FileCode2 size={20} /> : <FlaskConical size={20} />}
                              </Box>
                              {suite.experimental && <Badge colorPalette="orange" variant="subtle">Experimental</Badge>}
                            </HStack>
                            <Stack gap="1">
                              <Heading as="h4" size="md">{suite.name}</Heading>
                              <Text color="fg.muted" fontSize="sm">{suite.description || 'No description provided.'}</Text>
                            </Stack>
                            <HStack gap="2" wrap="wrap">
                              <Badge variant="subtle">{suiteTarget(suite)}</Badge>
                              <Badge variant="outline">{IMPACT_COPY[suite.impact].label}</Badge>
                              <Badge variant="outline">{suite.checks.length} {suite.checks.length === 1 ? 'check' : 'checks'}</Badge>
                            </HStack>
                            <Stack gap="1" mt="auto">
                              {suite.checks.slice(0, 3).map((check) => (
                                <Text key={check.id} fontSize="xs" color="fg.muted">• {check.title}</Text>
                              ))}
                              {suite.checks.length > 3 && <Text fontSize="xs" color="fg.muted">+ {suite.checks.length - 3} more checks</Text>}
                            </Stack>
                            <Text color="brand.fg" fontWeight="semibold" fontSize="sm">View suite →</Text>
                          </Box>
                        </RouterLink>
                      </ChakraLink>
                    ))}
                  </Grid>
                </section>
              )
            })}
          </Stack>
        )}
      </Box>
      <UserSuiteDialog
        key={editor?.key ?? 0}
        open={editor !== null}
        onClose={() => setEditor(null)}
        onSaved={(saved) => {
          showToast({ tone: 'success', title: 'Suite created', description: saved.name })
          setNonce((value) => value + 1)
        }}
      />
    </div>
  )
}
