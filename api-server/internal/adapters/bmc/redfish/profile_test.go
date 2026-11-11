package redfish

import (
	"context"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

func TestSelectProfile(t *testing.T) {
	cases := []struct {
		name string
		id   identity
		want string
	}{
		{"AMI service root vendor", identity{vendor: "AMI"}, "ami"},
		{"AMI Oem on the Manager under another brand", identity{vendor: "Gigabyte", oemKeys: []string{"Ami"}}, "ami"},
		{"AMI vendor action only", identity{actions: []string{"#ComputerSystem.Reset", "#AMIVirtualMedia.EnableRMedia"}}, "ami"},
		{"Supermicro Oem key without Vendor", identity{oemKeys: []string{"Supermicro"}}, "supermicro"},
		{"Supermicro vendor", identity{vendor: "Supermicro"}, "supermicro"},
		{"AMI fingerprints win over Supermicro ones", identity{oemKeys: []string{"Ami", "Supermicro"}}, "ami"},
		{"unknown vendor", identity{vendor: "Contoso", oemKeys: []string{"Contoso"}}, "default"},
		{"nothing reported", identity{}, "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := selectProfile(tc.id).name; got != tc.want {
				t.Errorf("selectProfile(%+v) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

// Each fake BMC declares what its profile does; changing one profile changes only its own rows.
func TestProfileBehaviour(t *testing.T) {
	ctx := context.Background()

	t.Run("ami directs through the Aptio fixed boot order", func(t *testing.T) {
		bmc := newFakeAMI()
		c, endpoint := newTestController(t, bmc)
		capability, _ := c.Probe(ctx, endpoint)
		mode, state, err := c.ApplyBootMedia(ctx, endpoint, testISO)
		if capability.Profile != "ami" || err != nil || mode != "Continuous" || !state.Ready() {
			t.Fatalf("profile %q, ApplyBootMedia = %q, %+v, %v; want ami, Continuous and ready", capability.Profile, mode, state, err)
		}
		if bmc.biosPending["FBO201"] != aptioUSBGroup || bmc.boot["BootSourceOverrideTarget"] == "Cd" {
			t.Errorf("pending BIOS %v, boot %v; want the USB group first and no Cd target", bmc.biosPending, bmc.boot)
		}
	})

	t.Run("supermicro directs through BootOrder and never reads the BIOS", func(t *testing.T) {
		bmc := newFakeSupermicro()
		c, endpoint := supermicroController(t, bmc)
		capability, _ := c.Probe(ctx, endpoint)
		if capability.Profile != "supermicro" || capability.Support != serverdomain.RedfishSupported || capability.Vendor != "Supermicro" {
			t.Fatalf("Probe = %+v, want supermicro, supported, vendor from the Oem key", capability)
		}
		mode, state, err := c.ApplyBootMedia(ctx, endpoint, testISO)
		if err != nil || mode != "Once" || !state.Ready() {
			t.Fatalf("ApplyBootMedia = %q, %+v, %v; want Once and ready", mode, state, err)
		}
		if order := bmc.boot["BootOrder"].([]any); order[0] != "Boot002A" {
			t.Errorf("BootOrder = %v, want the virtual CD first", order)
		}
		if bmc.boot["BootSourceOverrideTarget"] != "UefiBootNext" || bmc.boot["BootNext"] != "Boot002A" {
			t.Errorf("boot = %v, want a one-time boot to Boot002A", bmc.boot)
		}
		if bmc.biosReads != 0 || bmc.inserts != 0 {
			t.Errorf("BIOS reads = %d, inserts = %d; want neither (the ISO was already mounted)", bmc.biosReads, bmc.inserts)
		}
	})

	t.Run("supermicro without a listed virtual CD option is unsupported, not guessed at", func(t *testing.T) {
		bmc := newFakeSupermicro()
		bmc.image, bmc.inserted = "", false
		c, endpoint := supermicroController(t, bmc)
		capability, _ := c.Probe(ctx, endpoint)
		if capability.Support != serverdomain.RedfishUnsupported || !strings.Contains(capability.Reason, "no virtual CD boot option") {
			t.Errorf("Probe = %+v, want unsupported because the BIOS lists no virtual CD option", capability)
		}
		// Once mounted the option is listed, so an apply that mounts can still direct the boot.
		if _, state, err := c.ApplyBootMedia(ctx, endpoint, testISO); err != nil || !state.Ready() || bmc.inserts != 1 {
			t.Errorf("ApplyBootMedia = %+v, %v with %d inserts; want ready after one mount", state, err, bmc.inserts)
		}
		// The BMC names each refused property, and the mount drops exactly that one.
		if len(bmc.insertBodies) != 3 {
			t.Fatalf("InsertMedia requests = %v, want the full body, then without Inserted, then without WriteProtected", bmc.insertBodies)
		}
		_, inserted := bmc.insertBodies[1]["Inserted"]
		_, writeProtected := bmc.insertBodies[1]["WriteProtected"]
		if last := bmc.insertBodies[2]; inserted || !writeProtected || len(last) != 2 || last["Image"] != testISO || last["TransferProtocolType"] != "HTTP" {
			t.Errorf("InsertMedia requests = %v, want Image and TransferProtocolType kept to the end", bmc.insertBodies)
		}
		if bmc.boot["BootSourceOverrideTarget"] == "Cd" || bmc.boot["BootSourceOverrideTarget"] == "UsbCd" {
			t.Errorf("boot = %v, want no class target", bmc.boot)
		}
	})

	t.Run("supermicro holding another image cannot mount without an eject", func(t *testing.T) {
		bmc := newFakeSupermicro()
		bmc.image = "http://198.51.100.7/other.iso"
		c, endpoint := supermicroController(t, bmc)
		if _, _, err := c.ApplyBootMedia(ctx, endpoint, testISO); !errors.Is(err, serverdomain.ErrBootMediaRejected) || bmc.ejects != 0 {
			t.Errorf("ApplyBootMedia error = %v with %d ejects; want a rejection and the user's media left alone", err, bmc.ejects)
		}
	})

	t.Run("default directs through BootOrder and never reads the BIOS", func(t *testing.T) {
		bmc := newFakeDMTF()
		c, endpoint := dmtfController(t, bmc)
		capability, _ := c.Probe(ctx, endpoint)
		if capability.Profile != "default" || capability.Support != serverdomain.RedfishSupported {
			t.Fatalf("Probe = %+v, want default and supported", capability)
		}
		mode, state, err := c.ApplyBootMedia(ctx, endpoint, testISO)
		if err != nil || mode != "Once" || !state.Ready() {
			t.Fatalf("ApplyBootMedia = %q, %+v, %v; want Once and ready", mode, state, err)
		}
		if order := bmc.boot["BootOrder"].([]any); order[0] != "Boot0002" || bmc.boot["BootNext"] != "Boot0002" {
			t.Errorf("boot = %v, want the virtual CD first and next", bmc.boot)
		}
		if bmc.biosReads != 0 {
			t.Errorf("BIOS reads = %d, want none", bmc.biosReads)
		}
	})

	t.Run("default falls back to the Cd class target", func(t *testing.T) {
		bmc := newFakeDMTF()
		bmc.noBootOptions = true
		c, endpoint := dmtfController(t, bmc)
		mode, state, err := c.ApplyBootMedia(ctx, endpoint, testISO)
		if err != nil || mode != "Continuous" || !state.Ready() || bmc.boot["BootSourceOverrideTarget"] != "Cd" {
			t.Errorf("ApplyBootMedia = %q, %+v, %v, boot %v; want a continuous Cd override", mode, state, err, bmc.boot)
		}
	})
}

// A 401 is a rejected account; a 403 is a refused request whose path and BMC message reach the
// user, so nobody is sent to fix an account that works.
func TestProbeTellsARefusedRequestFromARejectedAccount(t *testing.T) {
	bmc := newFakeSupermicro()
	bmc.mediaForbidden = true
	c, endpoint := supermicroController(t, bmc)
	capability, err := c.Probe(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Probe error = %v", err)
	}
	if capability.Support != serverdomain.RedfishUnsupported || strings.Contains(capability.Reason, "account") ||
		!strings.Contains(capability.Reason, "GET /redfish/v1/Managers/1/VirtualMedia") || !strings.Contains(capability.Reason, "HTTP 403") {
		t.Errorf("Probe(403) = %+v, want unsupported naming the refused request", capability)
	}

	endpoint.Password = "wrong"
	if capability, _ := c.Probe(context.Background(), endpoint); capability.Support != serverdomain.RedfishUnreachable || capability.Reason != rejectedAccount {
		t.Errorf("Probe(401) = %+v, want unreachable because the account was rejected", capability)
	}
}

// A port method's error names the BMC's service root without the account.
func TestControllerErrorsNameTheEndpoint(t *testing.T) {
	bmc := newFakeSupermicro()
	c, endpoint := supermicroController(t, bmc)
	endpoint.Password = "wrong"
	_, err := c.ReadBootMedia(context.Background(), endpoint, "http://boot/x.iso")
	var failure *controllerError
	if !errors.As(err, &failure) || !strings.HasSuffix(failure.Endpoint(), "/redfish/v1") ||
		strings.Contains(failure.Endpoint(), "@") || failure.Operation() == "" || !failure.Permanent() {
		t.Errorf("ReadBootMedia(401) error = %#v, want the service root, the request, and permanence", err)
	}
}

// Controller errors say which request or step failed and whether retrying can help, for the
// ensure Task's diagnostic.
func TestClassifyKeepsOperationAndPermanence(t *testing.T) {
	cases := []struct {
		name          string
		err           error
		wantSentinel  error
		wantOperation string
		wantPermanent bool
	}{
		{"rejected account", &statusError{Method: "GET", Path: "/redfish/v1/Systems", Status: http.StatusUnauthorized},
			serverdomain.ErrBMCUnreachable, "GET /redfish/v1/Systems", true},
		{"refused request", &statusError{Method: "GET", Path: "/redfish/v1/Systems/1/Bios", Status: http.StatusForbidden, Message: "Not licensed"},
			serverdomain.ErrRedfishUnsupported, "GET /redfish/v1/Systems/1/Bios", true},
		{"server error", &statusError{Method: "PATCH", Path: "/redfish/v1/Systems/1", Status: http.StatusInternalServerError},
			serverdomain.ErrBootMediaRejected, "PATCH /redfish/v1/Systems/1", false},
		{"busy until ctx ended", &unreachableError{cause: context.DeadlineExceeded, operation: "GET /redfish/v1/"},
			serverdomain.ErrBMCUnreachable, "GET /redfish/v1/", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classify(tc.err)
			var ce *controllerError
			if !errors.As(err, &ce) || !errors.Is(err, tc.wantSentinel) {
				t.Fatalf("classify(%v) = %v, want a controllerError wrapping %v", tc.err, err, tc.wantSentinel)
			}
			if ce.Operation() != tc.wantOperation || ce.Permanent() != tc.wantPermanent {
				t.Errorf("classify(%v) operation %q permanent %t, want %q %t", tc.err, ce.Operation(), ce.Permanent(), tc.wantOperation, tc.wantPermanent)
			}
		})
	}
}

// The common files speak only the DMTF schema: a vendor's names belong in its profile file, so a
// fix for one family cannot silently change another's behavior.
func TestCommonFilesHoldNoVendorTerms(t *testing.T) {
	vendorTerm := regexp.MustCompile(`\b(AMI|Ami|Aptio|MegaRAC|Supermicro|ATEN|iDRAC|iLO|XCC)\b`)
	for _, file := range []string{"transport.go", "controller.go", "resources.go", "boot.go", "profile.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if match := vendorTerm.FindString(line); match != "" {
				t.Errorf("%s:%d names the vendor term %q; move it to that vendor's profile file", file, i+1, match)
			}
		}
	}
}

func TestDefaultProfileRequiresTheExactImage(t *testing.T) {
	if !exactImage(testISO, "", testISO) || exactImage("//192.0.2.1/boot-media/ipxe/swallow-ipxe.iso/swallow-ipxe.iso", "swallow-ipxe.iso", testISO) {
		t.Error("exactImage must accept only the URL it was given")
	}
}

func supermicroController(t *testing.T, bmc *fakeSupermicro) (*Controller, serverdomain.BMCEndpoint) {
	t.Helper()
	c, endpoint := newTestController(t, bmc)
	endpoint.Username = "ADMIN"
	return c, endpoint
}

func dmtfController(t *testing.T, bmc *fakeDMTF) (*Controller, serverdomain.BMCEndpoint) {
	t.Helper()
	c, endpoint := newTestController(t, bmc)
	endpoint.Username = "root"
	return c, endpoint
}
