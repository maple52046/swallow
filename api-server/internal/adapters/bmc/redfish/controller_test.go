package redfish

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

const testISO = "http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso"

func newTestController(t *testing.T, bmc http.Handler) (*Controller, serverdomain.BMCEndpoint) {
	t.Helper()
	server := httptest.NewTLSServer(bmc)
	t.Cleanup(server.Close)
	c := NewController()
	c.poll, c.settleWait, c.taskWait, c.mountSettle = time.Millisecond, time.Second, time.Second, time.Millisecond
	return c, serverdomain.BMCEndpoint{
		Address: strings.TrimPrefix(server.URL, "https://"), Username: "maas", Password: "secret",
		HostUUID: "7dd64000d6c111ef800074563cbcfd7d",
	}
}

func TestProbeIdentifiesHostSystemAndSupport(t *testing.T) {
	c, endpoint := newTestController(t, newFakeAMI())
	capability, err := c.Probe(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Probe error = %v", err)
	}
	if capability.Support != serverdomain.RedfishSupported || capability.SystemID != "Self" || !capability.VirtualMedia {
		t.Errorf("Probe = %+v, want supported on System Self with virtual media", capability)
	}
	if capability.Vendor != "AMI" || capability.FirmwareVersion != "13.06.10" || capability.RedfishVersion != "1.15.1" {
		t.Errorf("Probe identity = %+v", capability)
	}
	if strings.Contains(capability.ServiceRoot, "secret") || !strings.HasSuffix(capability.ServiceRoot, "/redfish/v1") {
		t.Errorf("ServiceRoot = %q, want the root URL without credentials", capability.ServiceRoot)
	}
}

func TestProbeReportsRejectedAccountAsUnreachable(t *testing.T) {
	c, endpoint := newTestController(t, newFakeAMI())
	endpoint.Password = "wrong"
	capability, err := c.Probe(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Probe error = %v", err)
	}
	if capability.Support != serverdomain.RedfishUnreachable || !strings.Contains(capability.Reason, "rejected") {
		t.Errorf("Probe = %+v, want unreachable because the account was rejected", capability)
	}
}

// The full AMI/Aptio apply: enable remote media, mount over plain HTTP without credentials, move
// the USB group to the front of the BIOS fixed boot order (pending for the next POST), and set a
// one-time boot to the virtual CD.
func TestApplyBootMediaOnAMIAptio(t *testing.T) {
	bmc := newFakeAMI()
	c, endpoint := newTestController(t, bmc)
	mode, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO)
	if err != nil {
		t.Fatalf("ApplyBootMedia error = %v", err)
	}
	if mode != "Continuous" || !state.Ready() {
		t.Errorf("ApplyBootMedia = %q, %+v; want Continuous and ready", mode, state)
	}
	if bmc.rmedia != "Enabled" {
		t.Error("remote media was not enabled before mounting")
	}
	if len(bmc.insertBodies) != 1 {
		t.Fatalf("InsertMedia calls = %d, want 1", len(bmc.insertBodies))
	}
	body := bmc.insertBodies[0]
	if body["TransferProtocolType"] != "HTTP" || body["Image"] != testISO {
		t.Errorf("InsertMedia body = %v, want HTTP and the ISO URL", body)
	}
	if _, ok := body["UserName"]; ok {
		t.Error("InsertMedia sent UserName, which AMI rejects for HTTP")
	}
	if got := bmc.biosPending["FBO201"]; got != "UEFI USB Device" {
		t.Errorf("pending FBO201 = %v, want the USB group first", got)
	}
	if bmc.boot["BootSourceOverrideTarget"] != "UefiBootNext" || bmc.boot["BootNext"] != "Boot0009" || bmc.boot["BootSourceOverrideEnabled"] != "Once" {
		t.Errorf("boot = %v, want a one-time boot to the virtual CD option", bmc.boot)
	}
	if bmc.boot["BootSourceOverrideTarget"] == "Cd" {
		t.Error("used the Cd class target, which hangs an Aptio host")
	}

	// A second apply mounts nothing new and keeps the BMC ready.
	if _, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err != nil || !state.Ready() {
		t.Fatalf("second ApplyBootMedia = %+v, %v; want ready", state, err)
	}
	if len(bmc.insertBodies) != 1 {
		t.Errorf("InsertMedia calls after re-apply = %d, want still 1", len(bmc.insertBodies))
	}
}

// An apply reports its phases through the context's reporter: the mount and its settle (with the
// settle's end) when it mounts, then the boot direction and the read-back; a re-apply of a mounted
// ISO skips straight to directing.
func TestApplyBootMediaReportsPhases(t *testing.T) {
	bmc := newFakeAMI()
	c, endpoint := newTestController(t, bmc)
	var phases []serverdomain.BootMediaPhase
	var settleEnd *time.Time
	ctx := serverdomain.WithBootMediaPhaseReporter(context.Background(), func(phase serverdomain.BootMediaPhase, endsAt *time.Time) {
		phases = append(phases, phase)
		if phase == serverdomain.BootMediaPhaseSettling {
			settleEnd = endsAt
		}
	})
	before := time.Now()
	if _, _, err := c.ApplyBootMedia(ctx, endpoint, testISO); err != nil {
		t.Fatalf("ApplyBootMedia error = %v", err)
	}
	want := []serverdomain.BootMediaPhase{
		serverdomain.BootMediaPhaseMounting, serverdomain.BootMediaPhaseSettling,
		serverdomain.BootMediaPhaseDirecting, serverdomain.BootMediaPhaseVerifying,
	}
	if fmt.Sprint(phases) != fmt.Sprint(want) {
		t.Errorf("phases = %v, want %v", phases, want)
	}
	if settleEnd == nil || settleEnd.Before(before.Add(c.mountSettle).Add(-time.Second)) {
		t.Errorf("settle end = %v, want about mountSettle after the mount", settleEnd)
	}

	phases = nil
	if _, _, err := c.ApplyBootMedia(ctx, endpoint, testISO); err != nil {
		t.Fatalf("second ApplyBootMedia error = %v", err)
	}
	if want := []serverdomain.BootMediaPhase{serverdomain.BootMediaPhaseDirecting, serverdomain.BootMediaPhaseVerifying}; fmt.Sprint(phases) != fmt.Sprint(want) {
		t.Errorf("re-apply phases = %v, want %v", phases, want)
	}
}

// After a boot without the CD the BIOS drops the virtual CD's UEFI option; with the Aptio fixed
// boot order the USB group alone directs the boot, so the BMC is ready with the override Disabled.
func TestApplyBootMediaOnAptioWithoutAVirtualCDOption(t *testing.T) {
	bmc := newFakeAMI()
	bmc.noBootOptions = true
	c, endpoint := newTestController(t, bmc)
	mode, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO)
	if err != nil || mode != "Continuous" || !state.Ready() {
		t.Fatalf("ApplyBootMedia = %q, %+v, %v; want Continuous and ready", mode, state, err)
	}
	if bmc.boot["BootSourceOverrideTarget"] == "Cd" {
		t.Error("used the Cd class target on an Aptio host")
	}
}

// Without a fixed boot order or a listed virtual CD option, the DMTF Cd target is used,
// Continuous when allowed.
func TestApplyBootMediaGenericCdTarget(t *testing.T) {
	bmc := newFakeAMI()
	bmc.noAptio, bmc.noBootOptions = true, true
	c, endpoint := newTestController(t, bmc)
	mode, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO)
	if err != nil {
		t.Fatalf("ApplyBootMedia error = %v", err)
	}
	if mode != "Continuous" || bmc.boot["BootSourceOverrideTarget"] != "Cd" || !state.Ready() {
		t.Errorf("ApplyBootMedia = %q, boot %v; want a continuous Cd override", mode, bmc.boot)
	}
}

func TestReadBootMediaBeforeAndAfterApply(t *testing.T) {
	bmc := newFakeAMI()
	c, endpoint := newTestController(t, bmc)
	state, err := c.ReadBootMedia(context.Background(), endpoint, testISO)
	if err != nil || state.Ready() || state.MediaInserted {
		t.Fatalf("ReadBootMedia before apply = %+v, %v; want nothing mounted", state, err)
	}
	if _, _, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err != nil {
		t.Fatalf("ApplyBootMedia error = %v", err)
	}
	state, err = c.ReadBootMedia(context.Background(), endpoint, testISO)
	if err != nil || !state.Ready() || !strings.HasSuffix(state.MediaImage, "/swallow-ipxe.iso") {
		t.Errorf("ReadBootMedia after apply = %+v, %v; want ready with the rewritten image", state, err)
	}
}

func TestClearBootMediaEjectsAndDisablesOverride(t *testing.T) {
	bmc := newFakeAMI()
	c, endpoint := newTestController(t, bmc)
	if _, _, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err != nil {
		t.Fatalf("ApplyBootMedia error = %v", err)
	}
	if err := c.ClearBootMedia(context.Background(), endpoint, testISO); err != nil {
		t.Fatalf("ClearBootMedia error = %v", err)
	}
	if bmc.inserted || bmc.ejects != 1 || bmc.boot["BootSourceOverrideEnabled"] != "Disabled" {
		t.Errorf("after clear: inserted=%t ejects=%d boot=%v", bmc.inserted, bmc.ejects, bmc.boot)
	}
}

// MountBootMedia mounts a dropped ISO again — ejecting the device that still names it — without
// changing the boot direction, and leaves a mounted ISO alone.
func TestMountBootMediaRemountsWithoutBootChange(t *testing.T) {
	bmc := newFakeAMI()
	bmc.rmedia, bmc.image, bmc.inserted = "Enabled", "//192.0.2.1/boot-media/ipxe/swallow-ipxe.iso/swallow-ipxe.iso", false
	c, endpoint := newTestController(t, bmc)
	state, err := c.MountBootMedia(context.Background(), endpoint, testISO)
	if err != nil || !state.MediaInserted {
		t.Fatalf("MountBootMedia(dropped) = %+v, %v; want inserted", state, err)
	}
	if bmc.ejects != 1 || len(bmc.insertBodies) != 1 {
		t.Errorf("ejects=%d inserts=%d, want the stale device ejected then one insert", bmc.ejects, len(bmc.insertBodies))
	}
	if len(bmc.biosPending) != 0 || bmc.boot["BootSourceOverrideEnabled"] != "Disabled" {
		t.Errorf("boot changed: pending=%v boot=%v", bmc.biosPending, bmc.boot)
	}
	if _, err := c.MountBootMedia(context.Background(), endpoint, testISO); err != nil || len(bmc.insertBodies) != 1 {
		t.Errorf("MountBootMedia(mounted) error=%v inserts=%d, want no second insert", err, len(bmc.insertBodies))
	}
}

// ResetHost force-restarts a running host and powers on one that is off.
func TestResetHost(t *testing.T) {
	bmc := newFakeAMI()
	c, endpoint := newTestController(t, bmc)
	if err := c.ResetHost(context.Background(), endpoint); err != nil {
		t.Fatalf("ResetHost(on) error = %v", err)
	}
	bmc.hostOff = true
	if err := c.ResetHost(context.Background(), endpoint); err != nil {
		t.Fatalf("ResetHost(off) error = %v", err)
	}
	if want := []string{"ForceRestart", "On"}; strings.Join(bmc.resets, ",") != strings.Join(want, ",") {
		t.Errorf("reset types = %v, want %v", bmc.resets, want)
	}
}

// A previous Boot Media URL (same path, other host) is ejected and replaced.
func TestApplyBootMediaReplacesPreviousBootMediaURL(t *testing.T) {
	bmc := newFakeAMI()
	bmc.rmedia, bmc.inserted = "Enabled", true
	bmc.image = "//198.51.100.7/boot-media/ipxe/swallow-ipxe.iso/swallow-ipxe.iso"
	c, endpoint := newTestController(t, bmc)
	if _, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err != nil || !state.Ready() {
		t.Fatalf("ApplyBootMedia = %+v, %v; want ready", state, err)
	}
	if bmc.ejects != 1 || !strings.Contains(bmc.image, "192.0.2.1") {
		t.Errorf("ejects=%d image=%q, want the old URL ejected and the new one mounted", bmc.ejects, bmc.image)
	}
}

func TestApplyBootMediaExplainsRefusals(t *testing.T) {
	t.Run("https rejected by firmware", func(t *testing.T) {
		bmc := newFakeAMI()
		bmc.rmedia = "Enabled"
		c, endpoint := newTestController(t, bmc)
		_, _, err := c.ApplyBootMedia(context.Background(), endpoint, "https://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso")
		if !errors.Is(err, serverdomain.ErrBootMediaRejected) || !strings.Contains(err.Error(), "http://") {
			t.Errorf("ApplyBootMedia error = %v, want a rejection that suggests http://", err)
		}
	})
	t.Run("task exception", func(t *testing.T) {
		bmc := newFakeAMI()
		bmc.failInsert = true
		c, endpoint := newTestController(t, bmc)
		_, _, err := c.ApplyBootMedia(context.Background(), endpoint, testISO)
		if !errors.Is(err, serverdomain.ErrBootMediaRejected) || !strings.Contains(err.Error(), "not available") {
			t.Errorf("ApplyBootMedia error = %v, want the task's message", err)
		}
	})
}

// InsertMedia's optional parameters are dropped only on a 400 that refuses a parameter, the one
// the BMC names first; the image and its URL format are never what gets dropped.
func TestRefusedInsertParameters(t *testing.T) {
	full := func() map[string]any {
		return map[string]any{"Image": testISO, "TransferProtocolType": "HTTP", "Inserted": true, "WriteProtected": true}
	}
	refusal := func(status int, messageID string, args ...string) error {
		return &statusError{Method: http.MethodPost, Path: "/insert", Status: status, MessageID: messageID, MessageArgs: args}
	}
	cases := []struct {
		name string
		err  error
		body map[string]any
		want []string
	}{
		{"named property", refusal(400, "Base.1.4.PropertyUnknown", "Inserted"), full(), []string{"Inserted"}},
		{"named parameter", refusal(400, "Base.1.12.ActionParameterNotSupported", "TransferProtocolType", "InsertMedia"), full(), []string{"TransferProtocolType"}},
		{"unnamed parameter refusal", refusal(400, "Base.1.12.ActionParameterUnknown"), full(), []string{"Inserted", "WriteProtected"}},
		{"names only the image", refusal(400, "Base.1.12.ActionParameterUnknown", "Image"), full(), []string{"Inserted", "WriteProtected"}},
		{"URL format", refusal(400, "Base.1.12.ActionParameterValueFormatError", "https://x", "Image"), full(), nil},
		{"other 400", refusal(400, "Base.1.12.GeneralError"), full(), nil},
		{"not a 400", refusal(500, "Base.1.12.PropertyUnknown", "Inserted"), full(), nil},
		{"nothing left to drop", refusal(400, "Base.1.4.PropertyUnknown", "Inserted"), map[string]any{"Image": testISO}, nil},
		{"success", nil, full(), nil},
	}
	for _, tc := range cases {
		if got := refusedInsertParameters(tc.err, tc.body); !slices.Equal(got, tc.want) {
			t.Errorf("%s: refusedInsertParameters = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A released GPU server is powered off, and its accelerator baseboard System then answers 500;
// the ensure step runs exactly then, so discovery must skip that System and still find the host.
func TestApplyBootMediaWhileGPUBaseboardIsUnreadable(t *testing.T) {
	bmc := newFakeAMI()
	bmc.hostOff = true
	c, endpoint := newTestController(t, bmc)
	if _, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err != nil || !state.Ready() {
		t.Fatalf("ApplyBootMedia with UBB unreadable = %+v, %v; want ready", state, err)
	}
}

// A fresh mount must settle: when the BMC drops it right after reporting it inserted, the
// controller mounts it again instead of reporting a ready BMC that will not boot the ISO.
func TestApplyBootMediaRemountsADroppedMount(t *testing.T) {
	bmc := newFakeAMI()
	bmc.dropFirstMount = true
	c, endpoint := newTestController(t, bmc)
	if _, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err != nil || !state.Ready() {
		t.Fatalf("ApplyBootMedia = %+v, %v; want ready after remounting", state, err)
	}
	if len(bmc.insertBodies) != 2 || bmc.ejects != 1 {
		t.Errorf("InsertMedia calls = %d, ejects = %d; want the stale device ejected once and mounted again", len(bmc.insertBodies), bmc.ejects)
	}
}

// A BIOS resource that fails briefly is retried; one that keeps failing fails the apply rather
// than silently switching to the Cd override, which hangs an Aptio host.
func TestApplyBootMediaNeverFallsBackToCdOnABiosReadFailure(t *testing.T) {
	t.Run("transient", func(t *testing.T) {
		bmc := newFakeAMI()
		bmc.biosFailures = 2
		c, endpoint := newTestController(t, bmc)
		if mode, state, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err != nil || mode != "Continuous" || !state.Ready() {
			t.Fatalf("ApplyBootMedia = %q, %+v, %v; want the Aptio strategy after retrying", mode, state, err)
		}
	})
	t.Run("persistent", func(t *testing.T) {
		bmc := newFakeAMI()
		bmc.biosFailures = 1000
		c, endpoint := newTestController(t, bmc)
		if _, _, err := c.ApplyBootMedia(context.Background(), endpoint, testISO); err == nil {
			t.Fatal("ApplyBootMedia succeeded without reading the BIOS boot order")
		}
		if bmc.boot["BootSourceOverrideTarget"] == "Cd" {
			t.Error("fell back to the Cd override")
		}
	})
}

// A busy BMC (HTTP 503 right after the host posts) is retried until it answers.
func TestBusyBMCIsRetried(t *testing.T) {
	bmc := newFakeAMI()
	bmc.busyResponses = 2
	c, endpoint := newTestController(t, bmc)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capability, err := c.Probe(ctx, endpoint)
	if err != nil || capability.Support != serverdomain.RedfishSupported {
		t.Fatalf("Probe through 503s = %+v, %v; want supported", capability, err)
	}
}

func TestChooseHostSystem(t *testing.T) {
	host := computerSystem{ID: "Self", UUID: "7DD64000-D6C1-11EF-8000-74563CBCFD7D", Boot: &boot{}}
	ubb := computerSystem{ID: "UBB"}
	other := computerSystem{ID: "Two", UUID: "11111111-0000-0000-0000-000000000000", Boot: &boot{}}
	cases := []struct {
		name    string
		systems []computerSystem
		hint    string
		uuid    string
		want    string
		wantErr bool
	}{
		{"only system with boot", []computerSystem{ubb, host}, "", "", "Self", false},
		{"uuid wins over order", []computerSystem{other, host}, "", "7dd64000d6c111ef800074563cbcfd7d", "Self", false},
		{"provisioner hint", []computerSystem{other, host}, "Two", "", "Two", false},
		{"ambiguous", []computerSystem{other, host}, "", "", "", true},
		{"no boot control", []computerSystem{ubb}, "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseHostSystem(tc.systems, tc.hint, tc.uuid)
			if (err != nil) != tc.wantErr || (err == nil && got.ID != tc.want) {
				t.Errorf("chooseHostSystem = %q, %v; want %q (error %t)", got.ID, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestImageMatching(t *testing.T) {
	cases := []struct {
		image, name string
		matches     bool
		samePath    bool
	}{
		{testISO, "", true, false},
		{"//192.0.2.1/boot-media/ipxe/swallow-ipxe.iso/swallow-ipxe.iso", "swallow-ipxe.iso", true, true},
		{"http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso", "", true, true},
		{"//198.51.100.7/boot-media/ipxe/swallow-ipxe.iso/swallow-ipxe.iso", "swallow-ipxe.iso", false, true},
		{"//192.0.2.1/isos/other.iso/other.iso", "other.iso", false, false},
		{"", "", false, false},
	}
	for _, tc := range cases {
		if got := amiImageMatches(tc.image, tc.name, testISO); got != tc.matches {
			t.Errorf("amiImageMatches(%q) = %t, want %t", tc.image, got, tc.matches)
		}
		if tc.image != "" && !tc.matches {
			if got := amiSameISOPath(tc.image, testISO); got != tc.samePath {
				t.Errorf("amiSameISOPath(%q) = %t, want %t", tc.image, got, tc.samePath)
			}
		}
	}
}

func TestServiceBase(t *testing.T) {
	cases := map[string]string{
		"10.140.168.230": "https://10.140.168.230",
		"https://bmc-admin:pw@192.0.2.21/redfish/v1?x": "https://192.0.2.21",
		"192.0.2.5:8443": "https://192.0.2.5:8443",
	}
	for address, want := range cases {
		if got, err := serviceBase(address); err != nil || got != want {
			t.Errorf("serviceBase(%q) = %q, %v; want %q", address, got, err, want)
		}
	}
	if _, err := serviceBase(""); err == nil {
		t.Error("serviceBase(\"\") succeeded, want an error")
	}
}
