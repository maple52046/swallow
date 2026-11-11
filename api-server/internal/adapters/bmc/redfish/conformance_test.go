package redfish

import (
	"net/http"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/server/domain/bmcporttest"
)

// Every fake BMC, one per BMC Profile, must satisfy the RedfishController port, so a change to the
// common code is checked against every family at once.
func TestControllerConformsToTheBMCPort(t *testing.T) {
	fixtures := map[string]bmcporttest.Fixture{
		"ami": func(t *testing.T, scenario bmcporttest.Scenario) (serverdomain.RedfishController, serverdomain.BMCEndpoint) {
			bmc := newFakeAMI()
			switch scenario {
			case bmcporttest.Forbidden:
				bmc.biosForbidden = true
			case bmcporttest.NoVirtualCD:
				bmc.noVirtualMedia = true
			case bmcporttest.Busy:
				bmc.busyResponses = 2
			}
			return conformanceController(t, bmc, scenario, "maas")
		},
		"supermicro": func(t *testing.T, scenario bmcporttest.Scenario) (serverdomain.RedfishController, serverdomain.BMCEndpoint) {
			bmc := newFakeSupermicro()
			switch scenario {
			case bmcporttest.Forbidden:
				bmc.mediaForbidden = true
			case bmcporttest.NoVirtualCD:
				bmc.noVirtualMedia = true
			case bmcporttest.Busy:
				bmc.busyResponses = 2
			}
			return conformanceController(t, bmc, scenario, "ADMIN")
		},
		"default": func(t *testing.T, scenario bmcporttest.Scenario) (serverdomain.RedfishController, serverdomain.BMCEndpoint) {
			bmc := newFakeDMTF()
			switch scenario {
			case bmcporttest.Forbidden:
				bmc.mediaForbidden = true
			case bmcporttest.NoVirtualCD:
				bmc.noVirtualMedia = true
			case bmcporttest.Busy:
				bmc.busyResponses = 2
			}
			return conformanceController(t, bmc, scenario, "root")
		},
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			bmcporttest.Run(t, fixture, testISO)
		})
	}
}

// conformanceController serves bmc and returns a controller and an endpoint for it, with the
// account the fake accepts unless the scenario is a rejected account.
func conformanceController(t *testing.T, bmc http.Handler, scenario bmcporttest.Scenario, user string) (serverdomain.RedfishController, serverdomain.BMCEndpoint) {
	t.Helper()
	c, endpoint := newTestController(t, bmc)
	endpoint.Username = user
	if scenario == bmcporttest.RejectedAccount {
		endpoint.Password = "wrong-password"
	}
	return c, endpoint
}
