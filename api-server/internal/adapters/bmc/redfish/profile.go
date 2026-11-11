package redfish

import (
	"context"
	"strings"
	"time"
)

// identity is what a BMC says about itself, from which its BMC Profile is recognized. It is read
// during discovery and holds names only, never a credential.
type identity struct {
	// vendor is the service root's Vendor property, as the BMC reports it.
	vendor string
	// oemKeys are the vendor names the Oem objects of the service root and the Manager are keyed by.
	oemKeys []string
	// actions are the action names of the host System and its Manager, vendor actions included.
	actions []string
}

// hasOemKey reports whether an Oem object is keyed by key, ignoring case.
func (id identity) hasOemKey(key string) bool {
	for _, k := range id.oemKeys {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// hasActionPrefix reports whether an action's name starts with prefix, ignoring case.
func (id identity) hasActionPrefix(prefix string) bool {
	for _, name := range id.actions {
		if len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// profile is a BMC Profile: how swallow talks to one BMC family beyond the DMTF schema
// (docs/development/bmc-profiles.md). The common code calls these hooks and holds no vendor
// knowledge; a vendor's names, OEM fields, and firmware quirks live only in its profile_<vendor>.go,
// each with where it was observed. Profiles are shared, read-only values: per-call state belongs in
// discovered.
type profile struct {
	// name is the published `redfish.profile` value; it never changes once released.
	name string
	// matches recognizes the family from the BMC's identity; nil for the default profile.
	matches func(identity) bool
	// mountSettle is how long a freshly mounted ISO must stay inserted before the host may be
	// powered on (see Controller.mountSettle).
	mountSettle time.Duration
	// prepareMount readies the virtual media before a mount (a vendor's remote-media switch, say)
	// and reports whether it changed anything, so the caller discovers again. Nil does nothing.
	prepareMount func(ctx context.Context, c *Controller, s *session, found *discovered, isoURL string) (bool, error)
	// imageMatches reports whether a device's reported image (and image name) is isoURL.
	imageMatches func(image, imageName, isoURL string) bool
	// ejectStaleImage ejects a free device that still names an image before mounting on it, for
	// firmware that refuses to mount over a dropped image.
	ejectStaleImage bool
	// isVirtualCDOption recognizes the BIOS boot option of the BMC's virtual CD.
	isVirtualCDOption func(bootOption) bool
	// strategies are the boot-direction strategies tried in order; the first that can direct this
	// System's boot is used.
	strategies []bootStrategy
	// noDirection is the user reason when no strategy can direct the boot to the virtual CD.
	noDirection string
}

// defaultProfile is used for a BMC no vendor profile recognizes. It uses only DMTF-standard
// behavior: it reads no BIOS settings, applies no vendor workaround, and requires the BMC to report
// the mounted image exactly as it was given. Its settle is the conservative one measured on the
// slowest BMC known.
var defaultProfile = &profile{
	name:              "default",
	mountSettle:       3 * time.Minute,
	imageMatches:      exactImage,
	isVirtualCDOption: virtualCDByPath,
	strategies:        []bootStrategy{uefiBootOrder{}, classTarget{target: "Cd"}},
	noDirection:       "The host System cannot override boot to a CD.",
}

// recognized lists the vendor profiles in the order they are tried; the first whose matches
// accepts the identity is used. A BMC that matches several fingerprints gets the earliest, so a
// new profile goes after the ones whose fingerprints it may share.
var recognized = []*profile{amiProfile, supermicroProfile}

// selectProfile returns the profile for a BMC's identity, or defaultProfile when none matches.
func selectProfile(id identity) *profile {
	for _, p := range recognized {
		if p.matches != nil && p.matches(id) {
			return p
		}
	}
	return defaultProfile
}

// exactImage reports whether the device reports isoURL verbatim.
func exactImage(image, _, isoURL string) bool {
	return strings.TrimSpace(image) == isoURL
}
