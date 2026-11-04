import { Badge, Box, Button, HStack, Link as ChakraLink, List, Stack, Table, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { changePlanApprovalBlocker, isChangePlanUnfinished, type ChangePlan, type ChangePlanResource } from '@/domain/convergence/changePlan'
import { StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { CopyButton } from '@/presentation/components/CopyButton'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatDateTime } from '@/shared/utils/time'
import {
  CHANGE_PLAN_STATE_LABELS,
  changePlanResourceKind,
  changePlanResourceLabel,
  changePlanResourceName,
  plannedActionChange,
  type ChangePlanNames,
} from './changePlanPresentation'

interface ChangePlanCardProps {
  plan: ChangePlan
  /** Names of the Platforms and Servers the plan refers to; a resource without one shows its id. */
  names: ChangePlanNames
  /** Time of the last read, so "expired" is judged without reading the clock during render. */
  now: number
  onApprove: (plan: ChangePlan) => void
  onReject: (plan: ChangePlan) => void
}

/**
 * One Change Plan on the Change Plans page: its resource and state, every planned action with
 * destructive ones marked in words, the expected effects, the blockers, and the decision buttons.
 *
 * All plan content is the API's; the card derives nothing about destructiveness. The resource and
 * action targets are shown by name (`names`), with their ids kept beside them as selectable text,
 * and the plan id is shown with a copy button so a plan can be found again in the CLI
 * (`swallow plans`) or the API. Approve is disabled with its reason spelled out when the plan has
 * blockers, is past `expiresAt`, or is not waiting for approval; the buttons only open a
 * confirmation, never decide directly. A decided or applied plan shows who decided it and links
 * its Workflow.
 */
export function ChangePlanCard({ plan, names, now, onApprove, onReject }: ChangePlanCardProps) {
  const resource = changePlanResourceLabel(plan.resource, names)
  const resourceNamed = changePlanResourceName(plan.resource, names) !== null
  const approvalBlocker = changePlanApprovalBlocker(plan, now)
  const unfinished = isChangePlanUnfinished(plan)
  return (
    <Box as="article" className="sw-change-plan" aria-label={`Change Plan for ${resource}`} borderWidth="1px" borderRadius="md" p="4">
      <Stack gap="4">
        <HStack justify="space-between" align="start" wrap="wrap" gap="3">
          <Stack gap="1" minW="0">
            <Text fontWeight="semibold">{resource}</Text>
            <HStack gap="4" rowGap="0" wrap="wrap">
              {resourceNamed && (
                <PlanIdentifier
                  label={`${changePlanResourceKind(plan.resource)} ID`}
                  value={plan.resource.id}
                  copyLabel={`Copy ${changePlanResourceKind(plan.resource).toLowerCase()} ID`}
                />
              )}
              <PlanIdentifier label="Plan ID" value={plan.id} copyLabel="Copy Change Plan ID" />
            </HStack>
            <Text fontSize="sm" color="fg.muted">
              Spec generation {plan.generation} · computed {formatDateTime(plan.createdAt)} · expires {formatDateTime(plan.expiresAt)}
            </Text>
          </Stack>
          <StatusBadge status={plan.state} label={CHANGE_PLAN_STATE_LABELS[plan.state]} />
        </HStack>

        <PlannedActions plan={plan} names={names} />

        {plan.effects.length > 0 && (
          <Box>
            <Text fontWeight="medium" mb="1">Expected effects</Text>
            <List.Root ps="5">
              {plan.effects.map((effect) => <List.Item key={effect}>{effect}</List.Item>)}
            </List.Root>
          </Box>
        )}

        {plan.blockers.length > 0 && (
          <Box role="group" aria-label="Blockers">
            <Text fontWeight="medium" mb="1" color="fg.error">Blockers — this plan cannot be approved</Text>
            <List.Root ps="5">
              {plan.blockers.map((blocker) => <List.Item key={blocker}>{blocker}</List.Item>)}
            </List.Root>
          </Box>
        )}

        {plan.stateReason && <Text fontSize="sm">{plan.stateReason}</Text>}
        {plan.decidedBy && (
          <Text fontSize="sm" color="fg.muted">
            {plan.state === 'rejected' ? 'Rejected' : 'Approved'} by {plan.decidedBy}
            {plan.decidedAt ? ` at ${formatDateTime(plan.decidedAt)}` : ''}
          </Text>
        )}
        {plan.workflowId && (
          <Text fontSize="sm">
            Applied as Workflow{' '}
            <ChakraLink asChild color="brand.fg" textDecoration="underline" textUnderlineOffset="3px">
              <RouterLink to={`/workflows/${encodeURIComponent(plan.workflowId)}`}>{plan.workflowId}</RouterLink>
            </ChakraLink>
          </Text>
        )}

        {unfinished && (
          <Stack gap="2">
            <HStack gap="2" wrap="wrap">
              <Button
                colorPalette="red"
                size="sm"
                onClick={() => onApprove(plan)}
                disabled={approvalBlocker !== null}
                aria-describedby={approvalBlocker ? `plan-${plan.id}-approval` : undefined}
              >
                Approve plan
              </Button>
              <Button variant="outline" size="sm" onClick={() => onReject(plan)}>
                Reject plan
              </Button>
            </HStack>
            {approvalBlocker && plan.state === 'pending_approval' && (
              <Text id={`plan-${plan.id}-approval`} fontSize="sm" color="fg.muted">{approvalBlocker}</Text>
            )}
          </Stack>
        )}
      </Stack>
    </Box>
  )
}

/**
 * A labelled identifier in the card header ("Plan ID plan-1") with the shared copy button. The id
 * stays plain, wrapping text so it can also be selected by hand on a narrow screen; the id and its
 * copy button wrap as one unit, so the button never ends up alone on a line.
 */
function PlanIdentifier({ label, value, copyLabel }: { label: string; value: string; copyLabel: string }) {
  return (
    <Text as="span" className="sw-cell-inline" fontSize="sm" color="fg.muted">
      {/* The space keeps the label and the id apart in the text read aloud and copied; the flex gap spaces them visually. */}
      {label}{' '}
      <Box as="span" display="inline-flex" alignItems="center" gap="1" minW="0">
        <Text as="span" className="sw-mono" wordBreak="break-all">{value}</Text>
        <CopyButton value={value} label={copyLabel} />
      </Box>
    </Text>
  )
}

/**
 * An action target by name ("Server gpu-node-03") with its id underneath as selectable muted text;
 * while the name is unknown the label already shows the id, so it is not repeated.
 */
function PlanTarget({ target, names }: { target: ChangePlanResource; names: ChangePlanNames }) {
  const named = changePlanResourceName(target, names) !== null
  return (
    <Stack gap="0" minW="0">
      <Text as="span">{changePlanResourceLabel(target, names)}</Text>
      {named && (
        <Text as="span" className="sw-mono" fontSize="xs" fontWeight="normal" color="fg.muted" wordBreak="break-all">
          {target.id}
        </Text>
      )}
    </Stack>
  )
}

/**
 * The plan's actions as a table on desktop and cards on mobile. Destructive actions carry a
 * "Destructive" badge with text, so the marking does not depend on colour.
 */
function PlannedActions({ plan, names }: { plan: ChangePlan; names: ChangePlanNames }) {
  const label = `Planned actions for ${changePlanResourceLabel(plan.resource, names)}`
  return (
    <ResponsiveDataView
      desktop={
        <StickyTableFrame>
          <Table.Root size="sm" aria-label={label}>
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Target</Table.ColumnHeader>
                <Table.ColumnHeader>Action</Table.ColumnHeader>
                <Table.ColumnHeader>Change</Table.ColumnHeader>
                <Table.ColumnHeader>Reason</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {plan.actions.map((action) => (
                <Table.Row key={`${action.target.kind}/${action.target.id}/${action.action}`}>
                  <Table.Cell><PlanTarget target={action.target} names={names} /></Table.Cell>
                  <Table.Cell>
                    <HStack gap="2">
                      <Text as="span">{action.action}</Text>
                      {action.destructive && <Badge colorPalette="red" variant="subtle">Destructive</Badge>}
                    </HStack>
                  </Table.Cell>
                  <Table.Cell>{plannedActionChange(action)}</Table.Cell>
                  <Table.Cell>{action.reason}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      }
      mobile={
        <div className="sw-resource-card-list" aria-label={label}>
          {plan.actions.map((action) => (
            <ResourceCard
              key={`${action.target.kind}/${action.target.id}/${action.action}`}
              title={<PlanTarget target={action.target} names={names} />}
              description={action.action}
              status={action.destructive ? <Badge colorPalette="red" variant="subtle">Destructive</Badge> : undefined}
            >
              <ResourceCardField label="Change">{plannedActionChange(action)}</ResourceCardField>
              <ResourceCardField label="Reason">{action.reason}</ResourceCardField>
            </ResourceCard>
          ))}
        </div>
      }
    />
  )
}
