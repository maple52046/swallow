package maas

import (
	"context"
	"net/http"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// Each capability action is a thin wrapper over a named MAAS operation. The test that
// matters is that the right operation name goes on the wire, since a wrong name would
// silently do nothing or the wrong thing.
func TestCapabilityActions_InvokeTheRightOperation(t *testing.T) {
	cases := []struct {
		name    string
		call    func(*Provider) error
		wantOp  string
		wantErr bool
	}{
		{"power on", func(p *Provider) error { _, err := p.PowerOn(context.Background(), "abc123"); return err }, "power_on", false},
		{"power off", func(p *Provider) error { _, err := p.PowerOff(context.Background(), "abc123"); return err }, "power_off", false},
		{"commission", func(p *Provider) error { _, err := p.Commission(context.Background(), "abc123"); return err }, "commission", false},
		{"test", func(p *Provider) error { _, err := p.Test(context.Background(), "abc123"); return err }, "test", false},
		{"abort", func(p *Provider) error { _, err := p.Abort(context.Background(), "abc123"); return err }, "abort", false},
		{"override", func(p *Provider) error { _, err := p.OverrideFailedTesting(context.Background(), "abc123"); return err }, "override_failed_testing", false},
		{"lock", func(p *Provider) error { _, err := p.Lock(context.Background(), "abc123"); return err }, "lock", false},
		{"unlock", func(p *Provider) error { _, err := p.Unlock(context.Background(), "abc123"); return err }, "unlock", false},
		{"mark broken", func(p *Provider) error { _, err := p.MarkBroken(context.Background(), "abc123"); return err }, "mark_broken", false},
		{"mark fixed", func(p *Provider) error { _, err := p.MarkFixed(context.Background(), "abc123"); return err }, "mark_fixed", false},
		{"rescue", func(p *Provider) error { _, err := p.EnterRescueMode(context.Background(), "abc123"); return err }, "rescue_mode", false},
		{"exit rescue", func(p *Provider) error { _, err := p.ExitRescueMode(context.Background(), "abc123"); return err }, "exit_rescue_mode", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeMAAS(t)
			fake.onMachineOperation(http.StatusOK, readyMachineJSON)
			provider := newTestProvider(t, fake)

			if err := tc.call(provider); (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if fake.lastOperation != tc.wantOp {
				t.Errorf("operation: got %q, want %q", fake.lastOperation, tc.wantOp)
			}
		})
	}
}

func TestQueryPowerState_ReadsLiveState(t *testing.T) {
	fake := newFakeMAAS(t)
	// query_power_state is a read: a GET on the machine path with an op parameter,
	// answered with the live state rather than a machine object.
	fake.onGetMachine(http.StatusOK, `{"state": "on"}`)
	provider := newTestProvider(t, fake)

	state, err := provider.QueryPowerState(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("QueryPowerState: %v", err)
	}
	if fake.lastOperation != "query_power_state" {
		t.Errorf("operation: got %q, want query_power_state", fake.lastOperation)
	}
	if state != provisioningdomain.PowerStateOn {
		t.Errorf("state: got %q, want on", state)
	}
}

func TestCapabilities_AdvertisesEverythingMAASOffers(t *testing.T) {
	provider := &Provider{}
	caps := provider.Capabilities()
	if !caps.Power || !caps.HardwareValidation || !caps.OperatorState || !caps.MachineDetail || !caps.HardwareInventory || !caps.EphemeralDeploy || !caps.MachineRemoval || !caps.ReleaseOptions {
		t.Errorf("MAAS should advertise every capability, got %+v", caps)
	}
}
