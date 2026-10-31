package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// ServerWorkflowHolds reports whether a running Workflow holds a Server's resource lease, which
// holds the Server's convergence (glossary: Hold): the Workflow is changing the machine, and the
// controller must not change it underneath.
type ServerWorkflowHolds interface {
	HeldByWorkflow(ctx context.Context, serverID string) (bool, error)
}

// serverField is one provisioner-realized part of a Server's spec (decision 071). The controller
// asks each field how the observed machine departs from the spec, realizes the spec through the
// provider, and re-observes the field afterwards.
type serverField interface {
	// name is the field name used in drift messages and events.
	name() string
	// drift describes how the observed state departs from the spec, or "" when it matches.
	drift(server *serverdomain.Server) string
	// capable reports whether the provider can realize the field at all.
	capable(provider provisioningdomain.OSProvisioningProvider) bool
	// realize changes the provider to match the spec.
	realize(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error
	// observe re-reads the field from the provider and records it on server and in the store.
	observe(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error
}

// ServerController converges the provisioner-realized fields of every Server to its spec
// (glossary: Controller; desired-state-model.md §6, §7). It is the only writer of a Server's
// conditions and observedGeneration.
//
// For each Server it:
//   - adopts the first observation as the spec when the Server has none (§6.5), writing nothing to
//     the provisioner;
//   - reports held while the Server is locked, in Rescue Mode, or leased by a Workflow, and makes no
//     provider change then;
//   - reports provider_missing while the provisioner no longer reports the machine;
//   - converges a spec change on any trigger, and corrects drift (observed departed from a spec that
//     was realized) only on the periodic resync, as decision 071 requires;
//   - retries transient provider errors through the runtime's backoff, and stops on a rejection with
//     requires_attention until the spec changes.
//
// Status is written only when a condition or the observed generation changes, so routine
// observation does not cause writes, and therefore no change-feed loop.
type ServerController struct {
	servers   serverdomain.ServerRepository
	store     serverdomain.ServerConvergenceStore
	providers provisioningdomain.ProviderFactory
	holds     ServerWorkflowHolds
	events    convergence.EventRecorder
	fields    []serverField
	now       func() time.Time
	log       *slog.Logger
}

var _ convergence.Controller = (*ServerController)(nil)

// NewServerController wires the Server repository and its desired-state store, the provider
// factory, the Workflow lease reader, and the convergence timeline.
func NewServerController(
	servers serverdomain.ServerRepository,
	desired serverdomain.ServerConvergenceStore,
	providers provisioningdomain.ProviderFactory,
	holds ServerWorkflowHolds,
	events convergence.EventRecorder,
) *ServerController {
	return &ServerController{
		servers: servers, store: desired, providers: providers, holds: holds, events: events,
		fields: []serverField{serverTags{store: desired}},
		now:    func() time.Time { return time.Now().UTC() },
		log:    slog.Default().With("controller", "server"),
	}
}

// Name implements convergence.Controller.
func (c *ServerController) Name() string { return "server" }

// Collections implements convergence.Controller.
func (c *ServerController) Collections() []string { return []string{"servers"} }

// KeysForChange implements convergence.Controller: a changed Server is its own key; a deleted one
// needs nothing.
func (c *ServerController) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	if change.Operation == store.ChangeDelete {
		return nil, nil
	}
	return []string{change.Key}, nil
}

// Keys implements convergence.Controller with every Server, including absent ones (they report
// provider_missing).
func (c *ServerController) Keys(ctx context.Context) ([]string, error) {
	result, err := c.servers.List(ctx, serverdomain.ListFilter{IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(result.Servers))
	for i, server := range result.Servers {
		keys[i] = server.ID
	}
	return keys, nil
}

// Reconcile implements convergence.Controller.
func (c *ServerController) Reconcile(ctx context.Context, request convergence.Request) error {
	server, err := c.servers.FindByID(ctx, request.Key)
	if errors.Is(err, serverdomain.ErrServerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if server.Generation == 0 {
		if server, err = c.adopt(ctx, server); err != nil || server == nil {
			return err
		}
	}

	s := &serverStatus{conditions: server.Convergence.Conditions, observedGeneration: server.Convergence.ObservedGeneration, now: c.now()}
	generation := server.Generation
	held, holdReason, holdMessage, err := c.holdOf(ctx, server)
	if err != nil {
		return err
	}
	if held {
		s.set(convergence.ConditionHeld, convergence.ConditionTrue, holdReason, holdMessage, generation)
	} else {
		s.set(convergence.ConditionHeld, convergence.ConditionFalse, convergence.ReasonNotHeld, "", generation)
	}

	if server.Absent {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderMissing,
			"The provisioner no longer reports this machine.", generation)
		return c.write(ctx, server, s)
	}
	if attention, ok := convergence.FindCondition(s.conditions, convergence.ConditionRequiresAttention); ok &&
		attention.Status == convergence.ConditionTrue && attention.ObservedGeneration == generation {
		// Stopped after a rejection; only a spec change (a new generation) tries again.
		return c.write(ctx, server, s)
	}

	pending := s.observedGeneration < generation
	drifts := c.drifts(server)
	if len(drifts) == 0 {
		c.markRealized(ctx, server, s, pending, false)
		return c.write(ctx, server, s)
	}

	message := strings.Join(drifts, " ")
	if !pending {
		if !convergence.IsTrue(s.conditions, convergence.ConditionDrifted) {
			c.record(ctx, server, convergence.EventDriftDetected, c.driftedFields(server), message)
		}
		s.set(convergence.ConditionDrifted, convergence.ConditionTrue, convergence.ReasonDriftDetected, message, generation)
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging,
			"Drift is corrected at the next resync.", generation)
		if request.Trigger != convergence.TriggerResync {
			return c.write(ctx, server, s)
		}
	}
	if held {
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging,
			"Convergence waits for the hold to end.", generation)
		return c.write(ctx, server, s)
	}

	if err := c.realize(ctx, server); err != nil {
		var missing capabilityMissingError
		var rejected *provisioningdomain.ProviderError
		switch {
		case errors.As(err, &missing):
			s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonCapabilityMissing, err.Error(), generation)
			return c.write(ctx, server, s)
		case errors.As(err, &rejected) && rejected.Kind == provisioningdomain.ProviderErrorRejected:
			s.set(convergence.ConditionRequiresAttention, convergence.ConditionTrue, convergence.ReasonProviderRejected, rejected.Detail, generation)
			s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonFailed, rejected.Detail, generation)
			c.record(ctx, server, convergence.EventConvergenceFailed, c.driftedFields(server), rejected.Detail)
			return c.write(ctx, server, s)
		default:
			s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable,
				"The provisioner could not be reached; retrying.", generation)
			if werr := c.write(ctx, server, s); werr != nil {
				return werr
			}
			return err
		}
	}
	if drifts := c.drifts(server); len(drifts) > 0 {
		// The provider accepted the change but reports something else; observe again later.
		s.set(convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonConverging, strings.Join(drifts, " "), generation)
		if werr := c.write(ctx, server, s); werr != nil {
			return werr
		}
		return fmt.Errorf("server %s still differs from its spec after converging", server.ID)
	}
	c.markRealized(ctx, server, s, pending, !pending)
	return c.write(ctx, server, s)
}

// adopt takes the first observation as the spec (desired-state-model.md §6.5). When the API wrote
// a spec first, adoption does nothing and the Server is re-read.
func (c *ServerController) adopt(ctx context.Context, server *serverdomain.Server) (*serverdomain.Server, error) {
	spec := serverdomain.ServerSpec{Tags: server.Observed.ManualTags()}
	adopted, err := c.store.AdoptSpec(ctx, server.ID, spec)
	if err != nil {
		return nil, err
	}
	if !adopted {
		server, err = c.servers.FindByID(ctx, server.ID)
		if errors.Is(err, serverdomain.ErrServerNotFound) {
			return nil, nil
		}
		return server, err
	}
	server.Generation, server.Spec = 1, spec
	// The adopted spec is the observed state, so it is realized by definition.
	server.Convergence.ObservedGeneration = 1
	c.record(ctx, server, convergence.EventAdopted, nil, "The observed state became the spec.")
	return server, nil
}

func (c *ServerController) markRealized(ctx context.Context, server *serverdomain.Server, s *serverStatus, pending, corrected bool) {
	generation := server.Generation
	wasDrifted := convergence.IsTrue(s.conditions, convergence.ConditionDrifted)
	s.set(convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged, "", generation)
	s.set(convergence.ConditionDrifted, convergence.ConditionFalse, convergence.ReasonInSync, "", generation)
	s.set(convergence.ConditionRequiresAttention, convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded, "", generation)
	s.observedGeneration = generation
	switch {
	case corrected:
		c.record(ctx, server, convergence.EventDriftCorrected, nil, "Drift was corrected to the spec.")
	case pending:
		c.record(ctx, server, convergence.EventConverged, nil, "The spec was realized.")
	case wasDrifted:
		c.record(ctx, server, convergence.EventDriftResolved, nil, "The observed state matches the spec again.")
	}
}

// holdOf reports whether the Server's convergence is held, and why.
func (c *ServerController) holdOf(ctx context.Context, server *serverdomain.Server) (bool, string, string, error) {
	if p := server.Provisioning; p != nil {
		if p.Locked {
			return true, convergence.ReasonServerLocked, "The Server is locked in its provisioner.", nil
		}
		if p.State == string(provisioningdomain.MachineStatusRescue) {
			return true, convergence.ReasonRescueMode, "The Server is in Rescue Mode.", nil
		}
	}
	held, err := c.holds.HeldByWorkflow(ctx, server.ID)
	if err != nil {
		return false, "", "", err
	}
	if held {
		return true, convergence.ReasonWorkflowLease, "A running Workflow holds this Server.", nil
	}
	return false, "", "", nil
}

func (c *ServerController) drifts(server *serverdomain.Server) []string {
	var out []string
	for _, field := range c.fields {
		if d := field.drift(server); d != "" {
			out = append(out, d)
		}
	}
	return out
}

func (c *ServerController) driftedFields(server *serverdomain.Server) []string {
	var out []string
	for _, field := range c.fields {
		if field.drift(server) != "" {
			out = append(out, field.name())
		}
	}
	return out
}

// capabilityMissingError means the provider cannot realize a field; the spec is kept.
type capabilityMissingError struct{ field string }

func (e capabilityMissingError) Error() string {
	return fmt.Sprintf("The provisioner cannot set %s.", e.field)
}

// realize changes every drifted field on the provider and re-observes it.
func (c *ServerController) realize(ctx context.Context, server *serverdomain.Server) error {
	provider, err := c.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return err
	}
	for _, field := range c.fields {
		if field.drift(server) == "" {
			continue
		}
		if !field.capable(provider) {
			return capabilityMissingError{field: field.name()}
		}
		if err := field.realize(ctx, provider, server); err != nil {
			return err
		}
		if err := field.observe(ctx, provider, server); err != nil {
			return err
		}
	}
	return nil
}

func (c *ServerController) write(ctx context.Context, server *serverdomain.Server, s *serverStatus) error {
	if !s.changed && s.observedGeneration == server.Convergence.ObservedGeneration {
		return nil
	}
	return c.store.SetConvergence(ctx, server.ID, serverdomain.ServerConvergence{
		ObservedGeneration: s.observedGeneration, Conditions: s.conditions,
	})
}

func (c *ServerController) record(ctx context.Context, server *serverdomain.Server, eventType convergence.EventType, fields []string, message string) {
	if c.events == nil {
		return
	}
	err := c.events.Record(ctx, convergence.Event{
		ResourceKind: "server", ResourceID: server.ID, Type: eventType, Fields: fields,
		Message: message, Generation: server.Generation, At: c.now(),
	})
	if err != nil {
		c.log.Warn("convergence event not recorded", "server", server.ID, "type", eventType, "error", err)
	}
}

// serverStatus accumulates condition changes during one Reconcile.
type serverStatus struct {
	conditions         []convergence.Condition
	observedGeneration int64
	changed            bool
	now                time.Time
}

func (s *serverStatus) set(t convergence.ConditionType, status convergence.ConditionStatus, reason, message string, generation int64) {
	var changed bool
	s.conditions, changed = convergence.SetCondition(s.conditions, convergence.Condition{
		Type: t, Status: status, Reason: reason, Message: message, ObservedGeneration: generation,
	}, s.now)
	s.changed = s.changed || changed
}

// serverTags is the manual-tag field: the spec is the complete set of manual tags, so a missing
// tag is added and an extra manual tag removed; automatic tags are never touched (decision 071).
type serverTags struct {
	store serverdomain.ServerConvergenceStore
}

func (serverTags) name() string { return "tags" }

func (serverTags) drift(server *serverdomain.Server) string {
	missing, extra := tagDifference(server.Spec.Tags, server.Observed.ManualTags())
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		parts = append(parts, "unexpected "+strings.Join(extra, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Manual tags differ from the spec (" + strings.Join(parts, "; ") + ")."
}

func (serverTags) capable(provider provisioningdomain.OSProvisioningProvider) bool {
	_, ok := provider.(provisioningdomain.MachineTagController)
	return ok && provider.Capabilities().Tagging
}

func (serverTags) realize(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error {
	controller := provider.(provisioningdomain.MachineTagController)
	missing, extra := tagDifference(server.Spec.Tags, server.Observed.ManualTags())
	machine := []string{server.Source.ProviderMachineID}
	for _, name := range missing {
		if err := controller.EnsureTag(ctx, name); err != nil {
			return err
		}
		if err := controller.AddTag(ctx, name, machine); err != nil {
			return err
		}
	}
	for _, name := range extra {
		if err := controller.RemoveTag(ctx, name, machine); err != nil {
			return err
		}
	}
	return nil
}

// observe re-reads the machine's tags. Tags the controller just added are manual by construction;
// the automatic classification of the rest is kept from the last reconcile pass.
func (f serverTags) observe(ctx context.Context, provider provisioningdomain.OSProvisioningProvider, server *serverdomain.Server) error {
	machine, err := provider.GetMachine(ctx, server.Source.ProviderMachineID)
	if err != nil {
		return err
	}
	automatic := intersectTags(machine.Tags, server.Observed.AutomaticTags)
	server.Observed.Tags = slices.Clone(machine.Tags)
	server.Observed.AutomaticTags = automatic
	return f.store.SetObservedTags(ctx, server.ID, server.Observed.Tags, automatic)
}

// tagDifference returns the tags of want missing from have, and those of have not in want.
func tagDifference(want, have []string) (missing, extra []string) {
	for _, tag := range want {
		if !slices.Contains(have, tag) {
			missing = append(missing, tag)
		}
	}
	for _, tag := range have {
		if !slices.Contains(want, tag) {
			extra = append(extra, tag)
		}
	}
	return missing, extra
}
