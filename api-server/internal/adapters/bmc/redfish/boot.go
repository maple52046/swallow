package redfish

import (
	"context"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// Boot direction is built from strategies, each one way of making the host boot the virtual CD.
// A BMC Profile lists the strategies it trusts in order, and the first that can direct the boot of
// the discovered System is used (docs/development/bmc-profiles.md). This file holds the DMTF
// strategies every profile may use:
//
//   - uefiBootOrder: when the BIOS lists the virtual CD as a UEFI boot option, move it to the front
//     of BootOrder and set a one-time boot to it (UefiBootNext + BootNext). BootNext is one-shot by
//     definition, which is why the ensure Task re-applies before every OS Deployment, and the
//     BootOrder change keeps a reboot the provisioner did not start on the ISO too.
//   - classTarget: the DMTF class target of BootSourceOverrideTarget (for example "Cd"),
//     Continuous when the System allows it.
//
// A vendor strategy (a BIOS fixed boot order, say) lives in its profile's file.

// bootStrategy is one way of directing the host's boot at the virtual CD. Strategies are shared,
// stateless values; prepare binds one to a single discovery.
type bootStrategy interface {
	// prepare reads whatever extra state the strategy needs and returns the direction it would
	// apply to found, or nil when it cannot direct this System's boot. An error fails the
	// discovery: a strategy returns one only when guessing on would be unsafe.
	prepare(ctx context.Context, s *session, found *discovered) (bootDirection, error)
}

// bootDirection is a strategy bound to one discovery.
type bootDirection interface {
	// direct makes the next boots start from the virtual CD and returns the mode applied:
	// "Continuous" for a persistent setting, "Once" for a one-time boot.
	direct(ctx context.Context, c *Controller, s *session, found *discovered) (string, error)
	// directsBoot reports whether found's next boot is already directed at the virtual CD.
	directsBoot(found *discovered) bool
}

// uefiBootOrder directs the boot through the virtual CD's UEFI boot option.
type uefiBootOrder struct{}

func (uefiBootOrder) prepare(_ context.Context, _ *session, found *discovered) (bootDirection, error) {
	if found.virtualCDOption() == nil {
		return nil, nil
	}
	if !found.targetAllowed("UefiBootNext") && len(found.system.Boot.BootOrder) == 0 {
		return nil, nil
	}
	return uefiBootOrder{}, nil
}

func (uefiBootOrder) direct(ctx context.Context, c *Controller, s *session, found *discovered) (string, error) {
	ref := found.virtualCDOption().reference()
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

func (uefiBootOrder) directsBoot(found *discovered) bool {
	b := found.system.Boot
	option := found.virtualCDOption()
	if b == nil || option == nil {
		return false
	}
	active := b.BootSourceOverrideEnabled == "Once" || b.BootSourceOverrideEnabled == "Continuous"
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

// classTarget directs the boot with a DMTF class target of BootSourceOverrideTarget.
type classTarget struct{ target string }

func (t classTarget) prepare(_ context.Context, _ *session, found *discovered) (bootDirection, error) {
	if !found.targetAllowed(t.target) {
		return nil, nil
	}
	return t, nil
}

func (t classTarget) direct(ctx context.Context, c *Controller, s *session, found *discovered) (string, error) {
	mode := "Once"
	for _, allowed := range overrideModes(found.system.Boot) {
		if allowed == "Continuous" {
			mode = "Continuous"
		}
	}
	if err := c.patchBoot(ctx, s, found.system, map[string]any{
		"BootSourceOverrideTarget": t.target, "BootSourceOverrideEnabled": mode,
	}); err != nil {
		return "", err
	}
	return mode, nil
}

func (t classTarget) directsBoot(found *discovered) bool {
	b := found.system.Boot
	if b == nil {
		return false
	}
	active := b.BootSourceOverrideEnabled == "Once" || b.BootSourceOverrideEnabled == "Continuous"
	return active && b.BootSourceOverrideTarget == t.target
}

// prepareDirection binds the first of the profile's strategies that can direct the System's boot;
// found.direction stays nil when none can, or when the System has no Boot object at all.
func (d *discovered) prepareDirection(ctx context.Context, s *session) error {
	d.direction = nil
	if d.system.Boot == nil {
		return nil
	}
	for _, strategy := range d.profile.strategies {
		direction, err := strategy.prepare(ctx, s, d)
		if err != nil {
			return err
		}
		if direction != nil {
			d.direction = direction
			return nil
		}
	}
	return nil
}

// virtualCDOption returns the BIOS boot option of the BMC virtual CD as the profile recognizes it,
// or nil.
func (d *discovered) virtualCDOption() *bootOption {
	for i := range d.options {
		if d.profile.isVirtualCDOption(d.options[i]) {
			return &d.options[i]
		}
	}
	return nil
}

// canTargetCD reports whether some strategy of the profile can make the host boot the virtual CD.
func (d *discovered) canTargetCD() bool {
	return d.direction != nil
}

// overrideTargetsCD reports whether the next boot is directed at the virtual CD.
func (d *discovered) overrideTargetsCD() bool {
	return d.direction != nil && d.direction.directsBoot(d)
}

// setOverride directs the next boots at the virtual CD with the discovery's strategy and returns
// the mode applied.
func (c *Controller) setOverride(ctx context.Context, s *session, found *discovered) (string, error) {
	if found.direction == nil {
		return "", &controllerError{sentinel: serverdomain.ErrRedfishUnsupported, detail: found.profile.noDirection, operation: "direct the boot to the virtual CD", permanent: true}
	}
	return found.direction.direct(ctx, c, s, found)
}

// setOneTimeBoot points the next boot at the virtual CD's UEFI option when the BIOS lists one and
// the System allows UefiBootNext; otherwise it does nothing. Strategies call it so a reboot the
// provisioner did not start (a user power cycle) boots the ISO too.
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
