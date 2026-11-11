package redfish

import (
	"strings"
	"time"
)

// supermicroProfile is the BMC Profile of Supermicro BMCs, observed read-only on pvt250 (a
// Supermicro AS-4124GQ-TNMI, H12DGQ-NT6 board, BMC firmware 01.01.24, Redfish 1.9.0,
// 2026-11-10). It is recognized by the service root's Vendor or an Oem object keyed "Supermicro"
// (that root reports no Vendor, only the Oem key).
//
// What it changes from the DMTF behavior:
//
//   - It never reads BIOS settings: GET Systems/1/Bios answers 403 SMC.1.0.OemLicenseNotPassed
//     unless the BMC has the paid DCMS license, and nothing Boot Media needs is there.
//   - Its boot options are BootOption v1.0.3, without UefiDevicePath, so the virtual CD's option is
//     recognized by name ("UEFI: ATEN Virtual CDROM YS0J").
//   - BootOptions, BootOrder, and UefiBootNext need no license, so only uefiBootOrder is used. When
//     the BIOS does not list the virtual CD yet (it does after a POST with the ISO mounted), the
//     BMC is reported unsupported rather than guessed at with a class target.
//   - A mounted CD lists EjectMedia but no InsertMedia; the common code counts it as a virtual CD.
//
// The settle keeps the default's three minutes until a mount is measured.
var supermicroProfile = &profile{
	name:              "supermicro",
	matches:           supermicroMatches,
	mountSettle:       3 * time.Minute,
	imageMatches:      exactImage,
	isVirtualCDOption: supermicroVirtualCD,
	strategies:        []bootStrategy{uefiBootOrder{}},
	noDirection:       "The BIOS lists no virtual CD boot option; it lists one after the host has started once with the ISO mounted.",
}

// supermicroMatches recognizes a Supermicro BMC.
func supermicroMatches(id identity) bool {
	return strings.EqualFold(strings.TrimSpace(id.vendor), "Supermicro") || id.hasOemKey("Supermicro")
}

// supermicroVirtualCD recognizes the virtual CD's boot option by its device path when the BIOS
// reports one, else by its name.
func supermicroVirtualCD(o bootOption) bool {
	if strings.TrimSpace(o.UefiDevicePath) != "" {
		return virtualCDByPath(o)
	}
	name := strings.ToUpper(o.DisplayName)
	return strings.Contains(name, "ATEN VIRTUAL CDROM")
}
