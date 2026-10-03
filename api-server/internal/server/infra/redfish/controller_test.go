package redfish

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// fakeAMI emulates the AMI MegaRAC / Aptio behaviour recorded on tainan-ci (decision 047): a GPU
// baseboard System beside the host, remote media disabled until an OEM action enables it, an
// asynchronous InsertMedia that rewrites the image URL, ETag-guarded System PATCHes, and a BIOS
// fixed boot order whose USB group (the virtual CD) sorts after the hard disk.
type fakeAMI struct {
	mu                    sync.Mutex
	rmedia                string
	image                 string
	inserted              bool
	insertBodies          []map[string]any
	ejects                int
	boot                  map[string]any
	biosCurrent           map[string]any
	biosPending           map[string]any
	busyResponses         int
	patchesWithoutIfMatch int
	noAptio               bool
	noBootOptions         bool
	failInsert            bool
	hostOff               bool
	dropFirstMount        bool
	cd1Reads              int
	biosFailures          int
	resets                []string
}

func newFakeAMI() *fakeAMI {
	return &fakeAMI{
		rmedia: "Disabled",
		boot: map[string]any{
			"BootSourceOverrideEnabled":                         "Disabled",
			"BootSourceOverrideEnabled@Redfish.AllowableValues": []string{"Disabled", "Once", "Continuous"},
			"BootSourceOverrideTarget":                          "None",
			"BootSourceOverrideTarget@Redfish.AllowableValues":  []string{"None", "Pxe", "Cd", "Usb", "Hdd", "UefiTarget", "UefiBootNext"},
			"BootNext":    "",
			"BootOrder":   []string{"Boot0008", "Boot0009"},
			"BootOptions": map[string]string{"@odata.id": "/redfish/v1/Systems/Self/BootOptions"},
		},
		biosCurrent: map[string]any{"FBO201": "UEFI CD/DVD", "FBO202": "UEFI Hard Disk", "FBO203": "UEFI USB Device", "FBO204": "Disabled"},
		biosPending: map[string]any{},
	}
}

func (f *fakeAMI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, pass, ok := r.BasicAuth(); !ok || user != "maas" || pass != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.busyResponses > 0 {
		f.busyResponses--
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	link := func(id string) map[string]string { return map[string]string{"@odata.id": id} }
	switch r.Method + " " + r.URL.Path {
	case "GET /redfish/v1/":
		write(map[string]any{"RedfishVersion": "1.15.1", "Vendor": "AMI", "Product": "AMI Redfish Server", "Systems": link("/redfish/v1/Systems")})
	case "GET /redfish/v1/Systems":
		write(map[string]any{"Members": []any{link("/redfish/v1/Systems/Self"), link("/redfish/v1/Systems/UBB")}})
	case "GET /redfish/v1/Systems/UBB":
		if f.hostOff {
			// The accelerator baseboard is unreadable while the host is powered off.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		write(map[string]any{"@odata.id": "/redfish/v1/Systems/UBB", "Id": "UBB", "Model": "AMD Instinct MI308X UBB"})
	case "GET /redfish/v1/Systems/Self":
		power := "On"
		if f.hostOff {
			power = "Off"
		}
		system := map[string]any{
			"@odata.id": "/redfish/v1/Systems/Self", "@odata.etag": `W/"1"`, "Id": "Self",
			"UUID": "7DD64000-D6C1-11EF-8000-74563CBCFD7D", "Boot": f.boot,
			"VirtualMedia": link("/redfish/v1/Systems/Self/VirtualMedia"),
			"Links":        map[string]any{"ManagedBy": []any{link("/redfish/v1/Managers/Self")}},
			"PowerState":   power,
			"Actions": map[string]any{
				"#ComputerSystem.Reset": map[string]string{"target": "/redfish/v1/Systems/Self/Actions/ComputerSystem.Reset"},
				"Oem": map[string]any{
					"#AMIVirtualMedia.EnableRMedia": map[string]string{"target": "/redfish/v1/Systems/Self/Actions/Oem/AMIVirtualMedia.EnableRMedia"},
				},
			},
		}
		if !f.noAptio {
			system["Bios"] = link("/redfish/v1/Systems/Self/Bios")
		}
		write(system)
	case "PATCH /redfish/v1/Systems/Self":
		if r.Header.Get("If-Match") != `W/"1"` {
			f.patchesWithoutIfMatch++
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		var body struct{ Boot map[string]any }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for key, value := range body.Boot {
			f.boot[key] = value
		}
		w.WriteHeader(http.StatusNoContent)
	case "GET /redfish/v1/Systems/Self/BootOptions":
		if f.noBootOptions {
			write(map[string]any{"Members": []any{}})
			return
		}
		write(map[string]any{"Members": []any{link("/redfish/v1/Systems/Self/BootOptions/0008"), link("/redfish/v1/Systems/Self/BootOptions/0009")}})
	case "GET /redfish/v1/Systems/Self/BootOptions/0008":
		write(map[string]any{"Id": "0008", "DisplayName": "ubuntu", "UefiDevicePath": `HD(1,GPT,62EF,0x800,0x219800)/\EFI\ubuntu\shimx64.efi`})
	case "GET /redfish/v1/Systems/Self/BootOptions/0009":
		write(map[string]any{"Id": "0009", "DisplayName": "UEFI: AMI Virtual CDROM0 1.00, Partition 1",
			"UefiDevicePath": "PciRoot(0x3)/Pci(0x7,0x1)/Pci(0x0,0x4)/USB(0x0,0x0)/USB(0x1,0x0)/CDROM(0x1,0x22,0xB40)/HD(1,MBR,0x00000000,0x0,0xB40)"})
	case "GET /redfish/v1/Systems/Self/Bios":
		if f.biosFailures > 0 {
			f.biosFailures--
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		write(map[string]any{"@odata.id": "/redfish/v1/Systems/Self/Bios", "Attributes": f.biosCurrent})
	case "GET /redfish/v1/Systems/Self/Bios/SD":
		if len(f.biosPending) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		write(map[string]any{"Attributes": f.biosPending})
	case "PATCH /redfish/v1/Systems/Self/Bios/SD":
		if r.Header.Get("If-Match") != "*" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		var body struct{ Attributes map[string]any }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for key, value := range body.Attributes {
			f.biosPending[key] = value
		}
		w.WriteHeader(http.StatusNoContent)
	case "GET /redfish/v1/Managers/Self":
		write(map[string]any{"@odata.id": "/redfish/v1/Managers/Self", "FirmwareVersion": "13.06.10",
			"Oem": map[string]any{"Ami": map[string]any{"VirtualMedia": map[string]string{"RMediaStatus": f.rmedia}}}})
	case "POST /redfish/v1/Systems/Self/Actions/ComputerSystem.Reset":
		var body struct{ ResetType string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.resets = append(f.resets, body.ResetType)
		w.WriteHeader(http.StatusNoContent)
	case "POST /redfish/v1/Systems/Self/Actions/Oem/AMIVirtualMedia.EnableRMedia":
		f.rmedia = "Enabled"
		write(map[string]any{"error": map[string]any{"code": "Ami.1.0.DelayInActionCompletion", "@Message.ExtendedInfo": []any{map[string]string{"Severity": "OK"}}}})
	case "GET /redfish/v1/Systems/Self/VirtualMedia":
		write(map[string]any{"Members": []any{link("/redfish/v1/Systems/Self/VirtualMedia/CD1"), link("/redfish/v1/Systems/Self/VirtualMedia/RemovableStick1")}})
	case "GET /redfish/v1/Systems/Self/VirtualMedia/RemovableStick1":
		write(map[string]any{"Id": "RemovableStick1", "MediaTypes": []string{"USBStick"}, "Inserted": false})
	case "GET /redfish/v1/Systems/Self/VirtualMedia/CD1":
		if f.dropFirstMount && f.inserted {
			// The first mount reads as inserted once, then the BMC drops it (the host was powered
			// on before the virtual CD attached).
			f.cd1Reads++
			if f.cd1Reads >= 2 {
				f.inserted, f.dropFirstMount = false, false
			}
		}
		write(map[string]any{"@odata.id": "/redfish/v1/Systems/Self/VirtualMedia/CD1", "Id": "CD1", "MediaTypes": []string{"CD"},
			"Image": f.image, "ImageName": imageNameOf(f.image), "Inserted": f.inserted,
			"Actions": map[string]any{
				"#VirtualMedia.InsertMedia": map[string]string{"target": "/redfish/v1/Managers/Self/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia"},
				"#VirtualMedia.EjectMedia":  map[string]string{"target": "/redfish/v1/Managers/Self/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia"},
			}})
	case "POST /redfish/v1/Managers/Self/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.insertBodies = append(f.insertBodies, body)
		image, _ := body["Image"].(string)
		if strings.HasPrefix(image, "https://") {
			w.WriteHeader(http.StatusBadRequest)
			write(map[string]any{"error": map[string]any{"@Message.ExtendedInfo": []any{map[string]string{
				"MessageId": "Base.1.12.ActionParameterValueFormatError", "Severity": "Warning",
				"Message": "The value for the parameter Image is of a different format than the parameter can accept.",
			}}}})
			return
		}
		// AMI refuses a device that still names a dropped image ("being redirected via another CD
		// instance") until it is ejected.
		staleImage := f.image != "" && !f.inserted
		if f.rmedia != "Enabled" || f.failInsert || staleImage {
			w.WriteHeader(http.StatusAccepted)
			write(map[string]string{"@odata.id": "/redfish/v1/TaskService/Tasks/failed"})
			return
		}
		// AMI rewrites http://host/dir/file.iso to //host/dir/file.iso/file.iso.
		f.image = strings.TrimPrefix(image, "http:") + "/" + imageNameOf(image)
		f.inserted = true
		w.WriteHeader(http.StatusAccepted)
		write(map[string]string{"@odata.id": "/redfish/v1/TaskService/Tasks/3"})
	case "GET /redfish/v1/TaskService/Tasks/3":
		write(map[string]any{"TaskState": "Completed", "TaskStatus": "OK"})
	case "GET /redfish/v1/TaskService/Tasks/failed":
		write(map[string]any{"TaskState": "Exception", "TaskStatus": "OK", "Messages": []any{map[string]string{
			"Message": "The Image is not available in the given path"}}})
	case "POST /redfish/v1/Managers/Self/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia":
		f.image, f.inserted = "", false
		f.ejects++
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func imageNameOf(image string) string {
	if image == "" {
		return ""
	}
	return image[strings.LastIndex(image, "/")+1:]
}

const testISO = "http://192.0.2.1/boot-media/ipxe/swallow-ipxe.iso"

func newTestController(t *testing.T, bmc *fakeAMI) (*Controller, serverdomain.BMCEndpoint) {
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
		if got := imageMatches(tc.image, tc.name, testISO); got != tc.matches {
			t.Errorf("imageMatches(%q) = %t, want %t", tc.image, got, tc.matches)
		}
		if tc.image != "" && !tc.matches {
			if got := sameISOPath(tc.image, testISO); got != tc.samePath {
				t.Errorf("sameISOPath(%q) = %t, want %t", tc.image, got, tc.samePath)
			}
		}
	}
}

func TestServiceBase(t *testing.T) {
	cases := map[string]string{
		"10.170.168.230": "https://10.170.168.230",
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
