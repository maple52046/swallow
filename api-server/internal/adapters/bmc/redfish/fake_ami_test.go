package redfish

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// fakeAMI emulates the AMI MegaRAC / Aptio behaviour recorded on hv-01 (decision 047): a GPU
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
	biosForbidden         bool
	noVirtualMedia        bool
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
			"Links":      map[string]any{"ManagedBy": []any{link("/redfish/v1/Managers/Self")}},
			"PowerState": power,
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
		if !f.noVirtualMedia {
			system["VirtualMedia"] = link("/redfish/v1/Systems/Self/VirtualMedia")
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
		if f.biosForbidden {
			writeForbidden(w)
			return
		}
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
