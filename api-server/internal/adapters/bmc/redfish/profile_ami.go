package redfish

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// amiProfile is the BMC Profile of AMI MegaRAC BMCs, observed on hv-01 (an AMI MegaRAC with
// firmware 13.06.10 and an AMI Aptio BIOS, probe log of 2026-10-03, decision 047). It is
// recognized by any of AMI's fingerprints, so an AMI BMC sold under another brand still gets it:
// the service root's Vendor "AMI", an "Ami" Oem object on the service root or the Manager, or an
// #AMIVirtualMedia.* vendor action.
//
// What it adds to the DMTF behavior:
//
//   - Remote media ships disabled (Manager Oem.Ami.VirtualMedia.RMediaStatus "Disabled") and
//     InsertMedia then fails; it is enabled with the vendor action AMIVirtualMedia.EnableRMedia.
//   - InsertMedia rewrites http://host/dir/file.iso to //host/dir/file.iso/file.iso with ImageName
//     file.iso, so the image comparison ignores the scheme and accepts that suffix form.
//   - A device that dropped its mount keeps naming the image (Inserted false) and is refused
//     ("being redirected via another CD instance") until it is ejected; a CD that still holds the
//     same ISO path from another host is an earlier Boot Media URL and is ejected too.
//   - InsertMedia reports its Task complete before the virtual CD is ready for the host: a
//     power-on 18 or 67 seconds after an eject-and-mount left the CD detached and the host booted
//     its disk, while three minutes held through the power-on.
//   - The Aptio BIOS sorts boot options by a fixed group order (aptioFixedOrder below), so that
//     strategy comes first and the DMTF "Cd" target is used only on an AMI BMC without it.
var amiProfile = &profile{
	name:              "ami",
	matches:           amiMatches,
	mountSettle:       3 * time.Minute,
	prepareMount:      amiPrepareMount,
	imageMatches:      amiImageMatches,
	ejectStaleImage:   true,
	isVirtualCDOption: virtualCDByPath,
	strategies:        []bootStrategy{aptioFixedOrder{}, uefiBootOrder{}, classTarget{target: "Cd"}},
	noDirection:       "The host System cannot override boot to a CD.",
}

// amiMatches recognizes an AMI MegaRAC BMC from any of its fingerprints.
func amiMatches(id identity) bool {
	return strings.EqualFold(strings.TrimSpace(id.vendor), "AMI") || id.hasOemKey("Ami") ||
		id.hasActionPrefix("#AMIVirtualMedia.")
}

// amiPrepareMount ejects every CD still holding isoURL's path from another host (an earlier Boot
// Media URL: it would be booted first and no longer answer), then enables remote media when it is
// off. It reports whether it changed anything.
func amiPrepareMount(ctx context.Context, c *Controller, s *session, found *discovered, isoURL string) (bool, error) {
	changed := false
	for _, media := range found.media {
		if media.isCD() && media.inserted() && amiSameISOPath(media.Image, isoURL) {
			if err := c.eject(ctx, s, media); err != nil {
				return false, err
			}
			changed = true
		}
	}
	enabled, err := c.amiEnableRemoteMedia(ctx, s, found)
	return changed || enabled, err
}

// amiManagerOem is the part of an AMI Manager's Oem object swallow reads: the remote-media switch.
type amiManagerOem struct {
	Ami struct {
		VirtualMedia *struct {
			RMediaStatus string `json:"RMediaStatus"`
		} `json:"VirtualMedia"`
	} `json:"Ami"`
}

// amiRemoteMediaStatus returns the Manager's RMediaStatus, or "" when it reports none.
func amiRemoteMediaStatus(m *manager) string {
	var oem amiManagerOem
	if m == nil || json.Unmarshal(m.Oem, &oem) != nil || oem.Ami.VirtualMedia == nil {
		return ""
	}
	return oem.Ami.VirtualMedia.RMediaStatus
}

// amiEnableRemoteMedia switches on remote media when it is off and reports whether it changed
// anything. The vendor action answers 200 with an "error" body of severity OK and takes effect
// after a few seconds, so the Manager is re-read until it reports Enabled.
func (c *Controller) amiEnableRemoteMedia(ctx context.Context, s *session, found *discovered) (bool, error) {
	if !strings.EqualFold(amiRemoteMediaStatus(found.manager), "Disabled") {
		return false, nil
	}
	target := oemActionTarget(found.system.Actions, "#AMIVirtualMedia.EnableRMedia")
	if target == "" {
		target = oemActionTarget(found.manager.Actions, "#AMIVirtualMedia.EnableRMedia")
	}
	if target == "" {
		return false, &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: "The BMC's remote media service is disabled and it offers no action to enable it.", operation: "enable remote media", permanent: true}
	}
	if _, err := s.do(ctx, http.MethodPost, target, map[string]string{"RMediaState": "Enable"}, nil); err != nil {
		return false, classify(err)
	}
	deadline := time.Now().Add(c.settleWait)
	for time.Now().Before(deadline) {
		if err := sleep(ctx, c.poll); err != nil {
			return false, classify(err)
		}
		var m manager
		if err := s.get(ctx, found.manager.ODataID, &m); err != nil {
			return false, classify(err)
		}
		if strings.EqualFold(amiRemoteMediaStatus(&m), "Enabled") {
			return true, nil
		}
	}
	return false, &controllerError{sentinel: serverdomain.ErrBootMediaRejected, detail: "The BMC did not enable its remote media service.", operation: "enable remote media"}
}

// amiImageMatches reports whether a device's reported image is isoURL, verbatim or in the
// rewritten form AMI reports (//host/dir/file.iso/file.iso, ImageName file.iso).
func amiImageMatches(image, imageName, isoURL string) bool {
	image = strings.TrimSpace(image)
	if image == "" {
		return false
	}
	if image == isoURL {
		return true
	}
	want, err := url.Parse(isoURL)
	if err != nil || want.Host == "" {
		return false
	}
	base := path.Base(want.Path)
	wantPath := "//" + strings.ToLower(want.Host) + want.Path
	got := image
	if i := strings.Index(got, "://"); i >= 0 {
		got = got[i+1:]
	}
	if host, rest, ok := strings.Cut(strings.TrimPrefix(got, "//"), "/"); ok {
		got = "//" + strings.ToLower(host) + "/" + rest
	}
	if got == wantPath || got == wantPath+"/"+base {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(imageName), base) && strings.HasPrefix(got, wantPath)
}

// amiSameISOPath reports whether a device's image is isoURL's path served from a different host,
// in either the verbatim or the rewritten form.
func amiSameISOPath(image, isoURL string) bool {
	want, err := url.Parse(isoURL)
	if err != nil || want.Path == "" {
		return false
	}
	got := strings.TrimSpace(image)
	if i := strings.Index(got, "://"); i >= 0 {
		got = got[i+1:]
	}
	got = strings.TrimPrefix(got, "//")
	_, rest, ok := strings.Cut(got, "/")
	if !ok {
		return false
	}
	rest = "/" + rest
	return rest == want.Path || rest == want.Path+"/"+path.Base(want.Path)
}

// aptioFixedOrder directs the boot through the AMI Aptio BIOS fixed boot order.
//
// The BIOS sorts boot options by device group at every POST, using the BIOS attributes
// FBO201..FBO2nn ("UEFI Hard Disk", "UEFI CD/DVD", "UEFI USB Device", ...). The BMC's virtual CD
// is a USB CD-ROM, so it belongs to "UEFI USB Device"; with that group after "UEFI Hard Disk" the
// host boots its disk, and a Redfish BootOrder change is re-sorted away. Putting "UEFI USB Device"
// first is persistent (Continuous) and survives the provisioner's own power-on: MAAS's IPMI driver
// writes a one-time "force PXE" boot flag before every power-on, which overrides any one-time
// Redfish setting, and when that PXE attempt finds no PXE server the BIOS falls back to the group
// order — the virtual CD. With no ISO mounted the USB group is empty and the disk boots as before.
// The change is written to the BIOS settings object and takes effect at the next POST.
//
// The class target "Cd" must not be used on such a host: it means the empty "UEFI CD/DVD" group
// (the host then hangs with no bootable CD), and AMI implements a Continuous "Cd" override by
// moving that group to the front, which undoes this strategy. That is why a BIOS read failure
// fails the discovery instead of falling through to the next strategy.
type aptioFixedOrder struct{}

func (aptioFixedOrder) prepare(ctx context.Context, s *session, found *discovered) (bootDirection, error) {
	order, err := s.readAptioBootOrder(ctx, found.system)
	if err != nil || order == nil {
		return nil, err
	}
	return &aptioDirection{order: order}, nil
}

// aptioDirection is aptioFixedOrder bound to one discovery's fixed boot order.
type aptioDirection struct{ order *aptioBootOrder }

func (a *aptioDirection) direct(ctx context.Context, c *Controller, s *session, found *discovered) (string, error) {
	if changes := a.order.withUSBFirst(); len(changes) > 0 {
		// The settings object has no ETag before its first write; "*" is the only precondition
		// AMI accepts there, and it still refuses a concurrent conflicting write.
		header := http.Header{"If-Match": []string{"*"}}
		if _, err := s.do(ctx, http.MethodPatch, a.order.settingsPath, map[string]any{"Attributes": changes}, header); err != nil {
			return "", classify(err)
		}
		for name, value := range changes {
			a.order.values[name] = value.(string)
		}
	}
	if err := c.setOneTimeBoot(ctx, s, found); err != nil {
		return "", err
	}
	return "Continuous", nil
}

// directsBoot holds with the Redfish override Disabled: the fixed boot order alone directs the
// boot, so the override's own mode must not be required.
func (a *aptioDirection) directsBoot(*discovered) bool {
	return a.order.first() == aptioUSBGroup
}

// aptioUSBGroup is the Aptio fixed-boot-order value of the group holding BMC virtual media.
const aptioUSBGroup = "UEFI USB Device"

// aptioUEFIPriority matches the Aptio UEFI fixed-boot-order attributes (FBO201, FBO202, ...).
var aptioUEFIPriority = regexp.MustCompile(`^FBO2\d\d$`)

// aptioBootOrder is the Aptio UEFI fixed boot order of a System: the attribute names in priority
// order and their current values with any pending (next-POST) values applied.
type aptioBootOrder struct {
	settingsPath string
	names        []string
	values       map[string]string
}

// first returns the highest-priority group.
func (o *aptioBootOrder) first() string {
	if o == nil || len(o.names) == 0 {
		return ""
	}
	return o.values[o.names[0]]
}

// withUSBFirst returns the attribute changes that move the USB group to the front, keeping the
// relative order of the other groups. Each group appears once, so the values are a permutation.
func (o *aptioBootOrder) withUSBFirst() map[string]any {
	ordered := []string{aptioUSBGroup}
	for _, name := range o.names {
		if value := o.values[name]; value != aptioUSBGroup {
			ordered = append(ordered, value)
		}
	}
	changes := map[string]any{}
	for i, name := range o.names {
		if i < len(ordered) && o.values[name] != ordered[i] {
			changes[name] = ordered[i]
		}
	}
	return changes
}

// readAptioBootOrder reads the System's fixed boot order, or nil when the BIOS has none (not
// Aptio, no Bios resource, or no USB group to move). The pending settings object is read too,
// because a change written before the next POST is already the effective intent.
//
// Only a missing Bios resource (404/405) means "no fixed boot order". Any other failure is
// retried briefly and then returned: silently falling back would pick the Cd override, which hangs
// an Aptio host, or report a ready BMC as not ready (observed on hv-01 while the host was off).
func (s *session) readAptioBootOrder(ctx context.Context, system computerSystem) (*aptioBootOrder, error) {
	if system.Bios == nil || system.Bios.ID == "" {
		return nil, nil
	}
	var current biosSettings
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = s.get(ctx, system.Bios.ID, &current); err == nil {
			break
		}
		var status *statusError
		if errors.As(err, &status) && (status.Status == http.StatusNotFound || status.Status == http.StatusMethodNotAllowed) {
			return nil, nil
		}
		if ctx.Err() != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(s.pause):
		}
	}
	if err != nil {
		return nil, err
	}
	order := &aptioBootOrder{values: map[string]string{}}
	hasUSB := false
	for name, raw := range current.Attributes {
		value, ok := raw.(string)
		if !ok || !aptioUEFIPriority.MatchString(name) {
			continue
		}
		order.names = append(order.names, name)
		order.values[name] = value
		hasUSB = hasUSB || value == aptioUSBGroup
	}
	if !hasUSB {
		return nil, nil
	}
	sort.Strings(order.names)
	order.settingsPath = system.Bios.ID + "/SD"
	if current.Settings != nil && current.Settings.SettingsObject.ID != "" {
		order.settingsPath = current.Settings.SettingsObject.ID
	}
	var pending biosSettings
	if err := s.get(ctx, order.settingsPath, &pending); err == nil {
		for name, raw := range pending.Attributes {
			if value, ok := raw.(string); ok && aptioUEFIPriority.MatchString(name) {
				order.values[name] = value
			}
		}
	} else if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return order, nil
}
