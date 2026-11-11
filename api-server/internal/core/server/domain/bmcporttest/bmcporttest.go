// Package bmcporttest is the conformance suite of the domain.RedfishController port: what core
// expects of every adapter that drives a BMC for Boot Media, whatever the vendor. An adapter test
// calls Run with a Fixture that builds its fake BMC in each Scenario, so a change to an adapter's
// common code is checked against every fake BMC at once (core-and-extensions.md §3.7).
//
// It holds only vendor-neutral expectations stated in core vocabulary; what one BMC family does
// beyond them (which boot strategy, which requests) belongs in the adapter's own tests. Only tests
// import it: production code must not.
package bmcporttest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/server/domain"
)

// Scenario is a state of the fake BMC that the suite needs a Fixture to build.
type Scenario string

const (
	// Usable is a BMC Boot Media can be enabled on: Redfish answers, the host System has a virtual
	// CD, and the BMC lets its boot be directed at it. The fixture picks the starting state (a
	// mounted ISO, say) its BMC family needs to be usable.
	Usable Scenario = "usable"
	// RejectedAccount is a BMC that answers every authenticated request with 401 Unauthorized.
	RejectedAccount Scenario = "rejected-account"
	// Forbidden is a BMC whose account works but that answers a request Boot Media needs with 403
	// Forbidden and a Redfish error whose message contains ForbiddenMessage.
	Forbidden Scenario = "forbidden"
	// NoVirtualCD is a BMC whose host System offers no virtual CD.
	NoVirtualCD Scenario = "no-virtual-cd"
	// Busy is a Usable BMC that first answers a few requests with 503 Service Unavailable, as BMCs
	// do for minutes after the host posts.
	Busy Scenario = "busy"
)

// ForbiddenMessage is the message a Forbidden BMC puts in its 403 answer; the suite expects to
// find it in the reason the user is shown.
const ForbiddenMessage = "Not licensed to perform this request"

// Fixture builds a fresh fake BMC in scenario and returns the controller under test with an
// endpoint that reaches it; the fake must close with t. The controller's waits should be short
// enough for a unit test. The endpoint's password must be non-empty so the suite can check it
// never leaks.
type Fixture func(t *testing.T, scenario Scenario) (domain.RedfishController, domain.BMCEndpoint)

// sentinels are the errors a RedfishController failure must wrap exactly one of.
var sentinels = []error{domain.ErrBMCUnreachable, domain.ErrRedfishUnsupported, domain.ErrBootMediaRejected}

// Run checks the port's contract against the fake BMC fixture builds, mounting isoURL.
func Run(t *testing.T, fixture Fixture, isoURL string) {
	t.Helper()
	t.Run("probe answers unless ctx ended", func(t *testing.T) {
		controller, endpoint := fixture(t, Usable)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := controller.Probe(ctx, endpoint); !errors.Is(err, context.Canceled) {
			t.Errorf("Probe(ended ctx) error = %v, want context.Canceled", err)
		}
	})

	t.Run("usable BMC", func(t *testing.T) {
		controller, endpoint := fixture(t, Usable)
		ctx := testContext(t)
		capability := probe(t, controller, endpoint)
		if capability.Support != domain.RedfishSupported || !capability.VirtualMedia || capability.Profile == "" {
			t.Fatalf("Probe = %+v, want supported with virtual media and a BMC Profile", capability)
		}
		mode, applied, err := controller.ApplyBootMedia(ctx, endpoint, isoURL)
		if err != nil {
			t.Fatalf("ApplyBootMedia error = %v", err)
		}
		if mode != "Once" && mode != "Continuous" {
			t.Errorf("ApplyBootMedia mode = %q, want Once or Continuous", mode)
		}
		if !applied.Ready() {
			t.Errorf("ApplyBootMedia state = %+v, want ready", applied)
		}
		read, err := controller.ReadBootMedia(ctx, endpoint, isoURL)
		if err != nil {
			t.Fatalf("ReadBootMedia after apply error = %v", err)
		}
		if read.MediaInserted != applied.MediaInserted || read.OverrideReady != applied.OverrideReady {
			t.Errorf("ReadBootMedia after apply = %+v, want what ApplyBootMedia verified (%+v)", read, applied)
		}
		if _, again, err := controller.ApplyBootMedia(ctx, endpoint, isoURL); err != nil || !again.Ready() {
			t.Errorf("second ApplyBootMedia = %+v, %v; want ready (apply is idempotent)", again, err)
		}
		if err := controller.ClearBootMedia(ctx, endpoint, isoURL); err != nil {
			t.Fatalf("ClearBootMedia error = %v", err)
		}
		if cleared, err := controller.ReadBootMedia(ctx, endpoint, isoURL); err != nil || cleared.MediaInserted {
			t.Errorf("ReadBootMedia after clear = %+v, %v; want nothing mounted", cleared, err)
		}
		// Enabling again after a disable mounts on an empty virtual CD, whatever state the BMC
		// started in.
		if _, remounted, err := controller.ApplyBootMedia(ctx, endpoint, isoURL); err != nil || !remounted.Ready() {
			t.Errorf("ApplyBootMedia after clear = %+v, %v; want ready", remounted, err)
		}
	})

	t.Run("rejected account is unreachable", func(t *testing.T) {
		controller, endpoint := fixture(t, RejectedAccount)
		capability := probe(t, controller, endpoint)
		if capability.Support != domain.RedfishUnreachable || capability.Reason == "" {
			t.Errorf("Probe = %+v, want unreachable with a reason", capability)
		}
		noLeak(t, endpoint, capability.Reason)
		_, _, err := controller.ApplyBootMedia(testContext(t), endpoint, isoURL)
		wantFailure(t, endpoint, err, domain.ErrBMCUnreachable)
	})

	t.Run("refused request is unsupported with the BMC's words", func(t *testing.T) {
		controller, endpoint := fixture(t, Forbidden)
		capability := probe(t, controller, endpoint)
		if capability.Support != domain.RedfishUnsupported || !strings.Contains(capability.Reason, ForbiddenMessage) {
			t.Errorf("Probe = %+v, want unsupported with the BMC's message %q", capability, ForbiddenMessage)
		}
		noLeak(t, endpoint, capability.Reason)
		_, _, err := controller.ApplyBootMedia(testContext(t), endpoint, isoURL)
		wantFailure(t, endpoint, err, domain.ErrRedfishUnsupported)
		if err != nil && !strings.Contains(err.Error(), ForbiddenMessage) {
			t.Errorf("ApplyBootMedia error = %v, want the BMC's message %q", err, ForbiddenMessage)
		}
	})

	t.Run("no virtual CD is unsupported", func(t *testing.T) {
		controller, endpoint := fixture(t, NoVirtualCD)
		capability := probe(t, controller, endpoint)
		if capability.Support != domain.RedfishUnsupported || capability.VirtualMedia {
			t.Errorf("Probe = %+v, want unsupported without virtual media", capability)
		}
		_, _, err := controller.ApplyBootMedia(testContext(t), endpoint, isoURL)
		wantFailure(t, endpoint, err, domain.ErrRedfishUnsupported)
	})

	t.Run("busy BMC is retried", func(t *testing.T) {
		controller, endpoint := fixture(t, Busy)
		if capability := probe(t, controller, endpoint); capability.Support != domain.RedfishSupported {
			t.Errorf("Probe through busy answers = %+v, want supported", capability)
		}
	})
}

// testContext bounds one scenario so a fake that never answers fails the test instead of hanging.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// probe runs Probe with a live context, which must not fail.
func probe(t *testing.T, controller domain.RedfishController, endpoint domain.BMCEndpoint) domain.RedfishCapability {
	t.Helper()
	capability, err := controller.Probe(testContext(t), endpoint)
	if err != nil {
		t.Fatalf("Probe error = %v, want a result (only an ended ctx is an error)", err)
	}
	return capability
}

// wantFailure checks that err wraps want and no other sentinel, and does not leak the password.
func wantFailure(t *testing.T, endpoint domain.BMCEndpoint, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("error = %v, want %v", err, want)
		return
	}
	matched := 0
	for _, sentinel := range sentinels {
		if errors.Is(err, sentinel) {
			matched++
		}
	}
	if matched != 1 {
		t.Errorf("error %v wraps %d of the Boot Media sentinels, want exactly one", err, matched)
	}
	noLeak(t, endpoint, err.Error())
}

// noLeak fails when text carries the endpoint's password.
func noLeak(t *testing.T, endpoint domain.BMCEndpoint, text string) {
	t.Helper()
	if endpoint.Password == "" {
		t.Fatal("the fixture's endpoint has no password, so a leak cannot be checked")
	}
	if strings.Contains(text, endpoint.Password) {
		t.Errorf("%q carries the BMC password", text)
	}
}
