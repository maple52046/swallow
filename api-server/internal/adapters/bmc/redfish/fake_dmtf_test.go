package redfish

import (
	"encoding/json"
	"net/http"
	"sync"
)

// fakeDMTF is a BMC of a vendor swallow has no profile for, following the DMTF schema only: a
// service root with Vendor "Contoso", the virtual CD under the System with both InsertMedia and
// EjectMedia, the image reported verbatim, and a UEFI boot option for the virtual CD with a device
// path. Its Bios resource answers 403 so a test can prove the default profile never reads it.
type fakeDMTF struct {
	mu            sync.Mutex
	image         string
	inserted      bool
	boot          map[string]any
	inserts       int
	biosReads     int
	busyResponses int
	// noBootOptions hides the virtual CD's boot option, leaving only the class target.
	noBootOptions  bool
	mediaForbidden bool
	noVirtualMedia bool
}

func newFakeDMTF() *fakeDMTF {
	return &fakeDMTF{
		boot: map[string]any{
			"BootSourceOverrideEnabled":                         "Disabled",
			"BootSourceOverrideEnabled@Redfish.AllowableValues": []string{"Disabled", "Once", "Continuous"},
			"BootSourceOverrideTarget":                          "None",
			"BootSourceOverrideTarget@Redfish.AllowableValues":  []string{"None", "Pxe", "Cd", "Hdd", "UefiBootNext"},
			"BootNext":    "",
			"BootOrder":   []string{"Boot0001", "Boot0002"},
			"BootOptions": map[string]string{"@odata.id": "/redfish/v1/Systems/sys/BootOptions"},
		},
	}
}

func (f *fakeDMTF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, pass, ok := r.BasicAuth(); !ok || user != "root" || pass != "secret" {
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
		write(map[string]any{"RedfishVersion": "1.17.0", "Vendor": "Contoso", "Product": "Contoso BMC", "Systems": link("/redfish/v1/Systems")})
	case "GET /redfish/v1/Systems":
		write(map[string]any{"Members": []any{link("/redfish/v1/Systems/sys")}})
	case "GET /redfish/v1/Systems/sys":
		system := map[string]any{
			"@odata.id": "/redfish/v1/Systems/sys", "Id": "sys", "PowerState": "On", "Boot": f.boot,
			"Bios":    link("/redfish/v1/Systems/sys/Bios"),
			"Links":   map[string]any{"ManagedBy": []any{link("/redfish/v1/Managers/bmc")}},
			"Actions": map[string]any{"#ComputerSystem.Reset": map[string]string{"target": "/redfish/v1/Systems/sys/Actions/ComputerSystem.Reset"}},
		}
		if !f.noVirtualMedia {
			system["VirtualMedia"] = link("/redfish/v1/Systems/sys/VirtualMedia")
		}
		write(system)
	case "PATCH /redfish/v1/Systems/sys":
		var body struct{ Boot map[string]any }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for key, value := range body.Boot {
			f.boot[key] = value
		}
		w.WriteHeader(http.StatusNoContent)
	case "GET /redfish/v1/Systems/sys/Bios":
		f.biosReads++
		writeForbidden(w)
	case "GET /redfish/v1/Systems/sys/BootOptions":
		members := []any{link("/redfish/v1/Systems/sys/BootOptions/1")}
		if !f.noBootOptions {
			members = append(members, link("/redfish/v1/Systems/sys/BootOptions/2"))
		}
		write(map[string]any{"Members": members})
	case "GET /redfish/v1/Systems/sys/BootOptions/1":
		write(map[string]any{"Id": "1", "BootOptionReference": "Boot0001", "DisplayName": "ubuntu",
			"UefiDevicePath": `HD(1,GPT,0B6E,0x800,0x100000)/\EFI\ubuntu\shimx64.efi`})
	case "GET /redfish/v1/Systems/sys/BootOptions/2":
		write(map[string]any{"Id": "2", "BootOptionReference": "Boot0002", "DisplayName": "Virtual CD-ROM",
			"UefiDevicePath": "PciRoot(0x0)/Pci(0x14,0x0)/USB(0x5,0x0)/CDROM(0x1,0x0,0x0)"})
	case "GET /redfish/v1/Managers/bmc":
		write(map[string]any{"@odata.id": "/redfish/v1/Managers/bmc", "FirmwareVersion": "2.1.0"})
	case "GET /redfish/v1/Systems/sys/VirtualMedia":
		if f.mediaForbidden {
			writeForbidden(w)
			return
		}
		write(map[string]any{"Members": []any{link("/redfish/v1/Systems/sys/VirtualMedia/cd")}})
	case "GET /redfish/v1/Systems/sys/VirtualMedia/cd":
		write(map[string]any{"@odata.id": "/redfish/v1/Systems/sys/VirtualMedia/cd", "Id": "cd", "MediaTypes": []string{"CD", "DVD"},
			"Image": f.image, "Inserted": f.inserted, "Actions": map[string]any{
				"#VirtualMedia.InsertMedia": map[string]string{"target": "/redfish/v1/Systems/sys/VirtualMedia/cd/Actions/VirtualMedia.InsertMedia"},
				"#VirtualMedia.EjectMedia":  map[string]string{"target": "/redfish/v1/Systems/sys/VirtualMedia/cd/Actions/VirtualMedia.EjectMedia"},
			}})
	case "POST /redfish/v1/Systems/sys/VirtualMedia/cd/Actions/VirtualMedia.InsertMedia":
		var body struct{ Image string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.image, f.inserted = body.Image, true
		f.inserts++
		w.WriteHeader(http.StatusNoContent)
	case "POST /redfish/v1/Systems/sys/VirtualMedia/cd/Actions/VirtualMedia.EjectMedia":
		f.image, f.inserted = "", false
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// writeForbidden answers 403 with the Redfish error a BMC gives a request its license or the
// account's privilege does not cover; its message is bmcporttest.ForbiddenMessage.
func writeForbidden(w http.ResponseWriter) {
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "Base.1.8.InsufficientPrivilege",
		"@Message.ExtendedInfo": []any{map[string]string{
			"MessageId": "Base.1.8.InsufficientPrivilege", "Severity": "Critical", "Message": "Not licensed to perform this request.",
		}}}})
}
