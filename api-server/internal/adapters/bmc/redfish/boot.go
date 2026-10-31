package redfish

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"time"
)

// Boot strategy, from what real BMCs do (decision 047; probe log of 2026-10-03 on an AMI MegaRAC
// with an AMI Aptio BIOS):
//
//  1. Aptio fixed boot order. The BIOS sorts boot options by device group at every POST, using
//     the BIOS attributes FBO201..FBO2nn ("UEFI Hard Disk", "UEFI CD/DVD", "UEFI USB Device", ...).
//     The BMC's virtual CD is a USB CD-ROM, so it belongs to "UEFI USB Device"; with that group
//     after "UEFI Hard Disk" the host boots its disk, and a Redfish BootOrder change is re-sorted
//     away. Putting "UEFI USB Device" first is persistent (Continuous) and survives the
//     provisioner's own power-on: MAAS's IPMI driver writes a one-time "force PXE" boot flag
//     before every power-on, which overrides any one-time Redfish setting, and when that PXE
//     attempt finds no PXE server the BIOS falls back to the group order — the virtual CD. With
//     no ISO mounted the USB group is empty and the disk boots as before. The change is written
//     to the BIOS settings object and takes effect at the next POST.
//     The class target "Cd" must not be used here: it means the empty "UEFI CD/DVD" group (the
//     host then hangs with no bootable CD), and AMI implements a Continuous "Cd" override by
//     moving that group to the front, which undoes step 1.
//  2. A BIOS that lists the virtual CD as a UEFI boot option but has no fixed boot order: move it
//     to the front of BootOrder and set a one-time boot to it (UefiBootNext + BootNext). AMI
//     refuses Continuous for a UefiTarget override, and BootNext is one-shot by definition, which
//     is why the ensure Task re-applies before every OS Deployment.
//  3. Otherwise the DMTF class target "Cd", Continuous when the System allows it.
//
// In every case swallow also sets the one-time boot of step 2 when it can, so a reboot that the
// provisioner did not start (an operator power cycle) boots the ISO too.

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
// an Aptio host, or report a ready BMC as not ready (observed on tainan-ci while the host was off).
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

// virtualCDOption returns the BIOS boot option for the BMC virtual CD, or nil.
func (d *discovered) virtualCDOption() *bootOption {
	for i := range d.options {
		if d.options[i].isVirtualCD() {
			return &d.options[i]
		}
	}
	return nil
}

// canTargetCD reports whether some strategy can make the host boot the virtual CD.
func (d *discovered) canTargetCD() bool {
	if d.system.Boot == nil {
		return false
	}
	if d.aptio != nil {
		return true
	}
	if d.virtualCDOption() != nil && (d.targetAllowed("UefiBootNext") || len(d.system.Boot.BootOrder) > 0) {
		return true
	}
	return d.targetAllowed("Cd")
}

// overrideTargetsCD reports whether the next boot is directed at the virtual CD.
func (d *discovered) overrideTargetsCD() bool {
	b := d.system.Boot
	if b == nil {
		return false
	}
	if d.aptio != nil {
		return d.aptio.first() == aptioUSBGroup
	}
	active := b.BootSourceOverrideEnabled == "Once" || b.BootSourceOverrideEnabled == "Continuous"
	option := d.virtualCDOption()
	if option == nil {
		return active && b.BootSourceOverrideTarget == "Cd"
	}
	ref := option.reference()
	switch {
	case active && b.BootSourceOverrideTarget == "UefiBootNext" && b.BootNext == ref:
		return true
	case active && b.BootSourceOverrideTarget == "UefiTarget" && b.UefiTargetBootSourceOverride == option.UefiDevicePath:
		return true
	case len(b.BootOrder) > 0 && b.BootOrder[0] == ref:
		return true
	}
	return false
}

// setOverride directs the next boots at the virtual CD and returns the mode applied: "Continuous"
// for a persistent setting, "Once" for a one-time boot.
func (c *Controller) setOverride(ctx context.Context, s *session, found *discovered) (string, error) {
	if found.aptio != nil {
		if changes := found.aptio.withUSBFirst(); len(changes) > 0 {
			// The settings object has no ETag before its first write; "*" is the only precondition
			// AMI accepts there, and it still refuses a concurrent conflicting write.
			header := http.Header{"If-Match": []string{"*"}}
			if _, err := s.do(ctx, http.MethodPatch, found.aptio.settingsPath, map[string]any{"Attributes": changes}, header); err != nil {
				return "", classify(err)
			}
			for name, value := range changes {
				found.aptio.values[name] = value.(string)
			}
		}
		if err := c.setOneTimeBoot(ctx, s, found); err != nil {
			return "", err
		}
		return "Continuous", nil
	}
	if option := found.virtualCDOption(); option != nil {
		ref := option.reference()
		if order := found.system.Boot.BootOrder; len(order) > 0 && order[0] != ref {
			if err := c.patchBoot(ctx, s, found.system, map[string]any{"BootOrder": moveToFront(order, ref)}); err != nil {
				return "", err
			}
			if err := s.get(ctx, found.system.ODataID, &found.system); err != nil {
				return "", classify(err)
			}
		}
		if err := c.setOneTimeBoot(ctx, s, found); err != nil {
			return "", err
		}
		return "Once", nil
	}
	mode := "Once"
	for _, allowed := range overrideModes(found.system.Boot) {
		if allowed == "Continuous" {
			mode = "Continuous"
		}
	}
	if err := c.patchBoot(ctx, s, found.system, map[string]any{
		"BootSourceOverrideTarget": "Cd", "BootSourceOverrideEnabled": mode,
	}); err != nil {
		return "", err
	}
	return mode, nil
}

// setOneTimeBoot points the next boot at the virtual CD's UEFI option when the BIOS lists one and
// the System allows UefiBootNext; otherwise it does nothing.
func (c *Controller) setOneTimeBoot(ctx context.Context, s *session, found *discovered) error {
	option := found.virtualCDOption()
	if option == nil || !found.targetAllowed("UefiBootNext") {
		return nil
	}
	var fresh computerSystem
	if err := s.get(ctx, found.system.ODataID, &fresh); err != nil {
		return classify(err)
	}
	return c.patchBoot(ctx, s, fresh, map[string]any{
		"BootSourceOverrideTarget": "UefiBootNext", "BootNext": option.reference(), "BootSourceOverrideEnabled": "Once",
	})
}

// moveToFront returns order with ref first and the rest in their original order.
func moveToFront(order []string, ref string) []string {
	moved := make([]string, 0, len(order))
	moved = append(moved, ref)
	for _, entry := range order {
		if entry != ref {
			moved = append(moved, entry)
		}
	}
	return moved
}
