import { Badge, Box, Button, HStack, Link as ChakraLink, List, Stack, Table, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { changePlanApprovalBlocker, isChangePlanUnfinished, type ChangePlan } from '@/domain/convergence/changePlan'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { formatDateTime } from '@/shared/utils/time'
import { CHANGE_PLAN_STATE_LABELS, changePlanResourceLabel, plannedActionChange } from './changePlanPresentation'

interface ChangePlanCardProps {
  plan: ChangePlan
  /** Time of the last read, so "expired" is judged without reading the clock during render. */
  now: number
  onApprove: (plan: ChangePlan) => void
  onReject: (plan: ChangePlan) => void
}

/**
 * One Change Plan on the Change Plans page: its resource and state, every planned action with
 * destructive ones marked in words, the expected effects, the blockers, and the decision buttons.
 *
 * All content is the API's; the card derives nothing about destructiveness. Approve is disabled
 * with its reason spelled out when the plan has blockers, is past `expiresAt`, or is not waiting
 * for approval; the buttons only open a confirmation, never decide directly. A decided or applied
 * plan shows who decided it and links its Workflow.
 */
export function ChangePlanCard({ plan, now, onApprove, onReject }: ChangePlanCardProps) {
  const resource = changePlanResourceLabel(plan.resource)
  const approvalBlocker = changePlanApprovalBlocker(plan, now)
  const unfinished = isChangePlanUnfinished(plan)
  return (
    <Box as="article" className="sw-change-plan" aria-label={`Change Plan for ${resource}`} borderWidth="1px" borderRadius="md" p="4">
      <Stack gap="4">
        <HStack justify="space-between" align="start" wrap="wrap" gap="3">
          <Stack gap="1">
            <Text fontWeight="semibold">{resource}</Text>
            <Text fontSize="sm" color="fg.muted">
              Spec generation {plan.generation} · computed {formatDateTime(plan.createdAt)} · expires {formatDateTime(plan.expiresAt)}
            </Text>
          </Stack>
          <StatusBadge status={plan.state} label={CHANGE_PLAN_STATE_LABELS[plan.state]} />
        </HStack>

        <PlannedActions plan={plan} />

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
 * The plan's actions as a table on desktop and cards on mobile. Destructive actions carry a
 * "Destructive" badge with text, so the marking does not depend on colour.
 */
function PlannedActions({ plan }: { plan: ChangePlan }) {
  const label = `Planned actions for ${changePlanResourceLabel(plan.resource)}`
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
                  <Table.Cell>{changePlanResourceLabel(action.target)}</Table.Cell>
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
              title={changePlanResourceLabel(action.target)}
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
