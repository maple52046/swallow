package redfish

import (
	"encoding/json"
	"net/http"
	"sync"
)

// fakeSupermicro emulates the Supermicro BMC read on pvt250 (AS-4124GQ-TNMI, BMC firmware
// 01.01.24, 2026-11-10): a service root naming its vendor only as an Oem key, the virtual CD under
// the Manager, a Bios resource refused with 403 OemLicenseNotPassed without the DCMS license,
// BootOption v1.0.3 entries without UefiDevicePath, and a mounted CD that lists EjectMedia but no
// InsertMedia. The fake lists the virtual CD's boot option only while an ISO is mounted, the
// strictest case: the real BIOS lists it after a POST with the ISO mounted, and still did right
// after an eject. InsertMedia refuses Inserted with 400 PropertyUnknown naming it, as pvt250 did
// on 2026-11-11; it refuses WriteProtected the same way, which was not observed (the BMC stopped
// at Inserted), again the stricter case.
type fakeSupermicro struct {
	mu       sync.Mutex
	image    string
	inserted bool
	boot     map[string]any
	inserts  int
	// insertBodies are the InsertMedia request bodies, refused ones included.
	insertBodies  []map[string]any
	ejects        int
	biosReads     int
	busyResponses int
	// mediaForbidden refuses the VirtualMedia collection with 403, as a license would.
	mediaForbidden bool
	noVirtualMedia bool
}

// newFakeSupermicro returns the BMC as pvt250 was found: the ISO mounted from the web UI and the
// virtual CD's option listed, here behind the OS disk in BootOrder.
func newFakeSupermicro() *fakeSupermicro {
	return &fakeSupermicro{
		image:    testISO,
		inserted: true,
		boot: map[string]any{
			"BootSourceOverrideEnabled":                         "Disabled",
			"BootSourceOverrideEnabled@Redfish.AllowableValues": []string{"Disabled", "Once", "Continuous"},
			"BootSourceOverrideTarget":                          "None",
			"BootSourceOverrideTarget@Redfish.AllowableValues": []string{"None", "Pxe", "Floppy", "Cd", "Usb", "Hdd", "BiosSetup",
				"UsbCd", "UefiBootNext", "UefiHttp"},
			"BootSourceOverrideMode": "Legacy",
			"BootNext":               "",
			"BootOrder":              []string{"Boot0003", "Boot002A", "Boot0004"},
			"BootOptions":            map[string]string{"@odata.id": "/redfish/v1/Systems/1/BootOptions"},
		},
	}
}

func (f *fakeSupermicro) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, pass, ok := r.BasicAuth(); !ok || user != "ADMIN" || pass != "secret" {
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
		write(map[string]any{"RedfishVersion": "1.9.0", "Oem": map[string]any{"Supermicro": map[string]any{}},
			"Systems": link("/redfish/v1/Systems"), "Managers": link("/redfish/v1/Managers")})
	case "GET /redfish/v1/Systems":
		write(map[string]any{"Members": []any{link("/redfish/v1/Systems/1")}})
	case "GET /redfish/v1/Systems/1":
		write(map[string]any{
			"@odata.id": "/redfish/v1/Systems/1", "Id": "1", "UUID": "A1B2C3D4-0000-0000-0000-3CECEF000001",
			"Manufacturer": "Supermicro", "Model": "AS -4124GQ-TNMI", "PowerState": "On", "Boot": f.boot,
			"Bios":    link("/redfish/v1/Systems/1/Bios"),
			"Links":   map[string]any{"ManagedBy": []any{link("/redfish/v1/Managers/1")}},
			"Actions": map[string]any{"#ComputerSystem.Reset": map[string]string{"target": "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset"}},
		})
	case "PATCH /redfish/v1/Systems/1":
		var body struct{ Boot map[string]any }
		_ = json.NewDecoder(r.Body).Decode(&body)
		for key, value := range body.Boot {
			f.boot[key] = value
		}
		w.WriteHeader(http.StatusOK)
	case "GET /redfish/v1/Systems/1/Bios":
		f.biosReads++
		w.WriteHeader(http.StatusForbidden)
		write(map[string]any{"error": map[string]any{"code": "Base.v1_4_0.GeneralError", "@Message.ExtendedInfo": []any{map[string]string{
			"MessageId": "SMC.1.0.OemLicenseNotPassed", "Severity": "Warning",
			"Message": "Not licensed to perform this request. The following licenses DCMS  were needed",
		}}}})
	case "GET /redfish/v1/Systems/1/BootOptions":
		members := []any{link("/redfish/v1/Systems/1/BootOptions/0003"), link("/redfish/v1/Systems/1/BootOptions/0004")}
		if f.inserted {
			members = append(members, link("/redfish/v1/Systems/1/BootOptions/002A"))
		}
		write(map[string]any{"Members": members})
	case "GET /redfish/v1/Systems/1/BootOptions/0003":
		write(map[string]any{"Id": "0003", "BootOptionReference": "Boot0003", "DisplayName": "UEFI OS (debian)"})
	case "GET /redfish/v1/Systems/1/BootOptions/0004":
		write(map[string]any{"Id": "0004", "BootOptionReference": "Boot0004", "DisplayName": "UEFI: PXE IP4 Mellanox Network Adapter"})
	case "GET /redfish/v1/Systems/1/BootOptions/002A":
		write(map[string]any{"Id": "002A", "BootOptionReference": "Boot002A", "DisplayName": "UEFI: ATEN Virtual CDROM YS0J"})
	case "GET /redfish/v1/Managers/1":
		manager := map[string]any{"@odata.id": "/redfish/v1/Managers/1", "FirmwareVersion": "01.01.24",
			"Oem": map[string]any{"Supermicro": map[string]any{}}}
		if !f.noVirtualMedia {
			manager["VirtualMedia"] = link("/redfish/v1/Managers/1/VirtualMedia")
		}
		write(manager)
	case "GET /redfish/v1/Managers/1/VirtualMedia":
		if f.mediaForbidden {
			writeForbidden(w)
			return
		}
		write(map[string]any{"Members": []any{link("/redfish/v1/Managers/1/VirtualMedia/CD1")}})
	case "GET /redfish/v1/Managers/1/VirtualMedia/CD1":
		actions := map[string]any{
			"#VirtualMedia.EjectMedia": map[string]string{"target": "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia"},
			"Oem":                      map[string]any{},
		}
		if !f.inserted {
			actions["#VirtualMedia.InsertMedia"] = map[string]string{"target": "/redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia"}
		}
		write(map[string]any{"@odata.id": "/redfish/v1/Managers/1/VirtualMedia/CD1", "Id": "CD1", "MediaTypes": []string{"CD", "DVD"},
			"Image": f.image, "ImageName": imageNameOf(f.image), "Inserted": f.inserted, "Actions": actions})
	case "POST /redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.insertBodies = append(f.insertBodies, body)
		for _, refused := range []string{"Inserted", "WriteProtected"} {
			if _, present := body[refused]; present {
				writePropertyUnknown(w, refused)
				return
			}
		}
		f.image, _ = body["Image"].(string)
		f.inserted = true
		f.inserts++
		w.WriteHeader(http.StatusNoContent)
	case "POST /redfish/v1/Managers/1/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia":
		f.image, f.inserted = "", false
		f.ejects++
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// writePropertyUnknown answers 400 with the DMTF PropertyUnknown message pvt250 gave for Inserted.
func writePropertyUnknown(w http.ResponseWriter, property string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"code": "Base.v1_4_0.GeneralError", "message": "A general error has occurred. See ExtendedInfo for more information.",
		"@Message.ExtendedInfo": []any{map[string]any{
			"MessageId": "Base.1.4.PropertyUnknown", "Severity": "Warning", "MessageArgs": []string{property},
			"Message": "The property " + property + " is not in the list of valid properties for the resource.",
		}},
	}})
}
