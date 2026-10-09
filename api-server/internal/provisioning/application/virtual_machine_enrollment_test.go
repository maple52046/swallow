package application

import (
	"errors"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

func TestVirtualMachineEnrollmentRequestNormalize(t *testing.T) {
	valid := VirtualMachineEnrollmentRequest{IntegrationID: " maas-a ", HypervisorServerID: "hv-1", Domains: []string{"lab-1", "lab-2"}, Account: "ubuntu"}
	got, err := valid.Normalize()
	if err != nil || got.IntegrationID != "maas-a" {
		t.Fatalf("Normalize = %+v, %v", got, err)
	}
	for name, change := range map[string]func(r *VirtualMachineEnrollmentRequest){
		"no integration":    func(r *VirtualMachineEnrollmentRequest) { r.IntegrationID = "" },
		"no hypervisor":     func(r *VirtualMachineEnrollmentRequest) { r.HypervisorServerID = "" },
		"no domains":        func(r *VirtualMachineEnrollmentRequest) { r.Domains = nil },
		"duplicate domain":  func(r *VirtualMachineEnrollmentRequest) { r.Domains = []string{"a", "a"} },
		"slash in a domain": func(r *VirtualMachineEnrollmentRequest) { r.Domains = []string{"a/b"} },
		"padded domain":     func(r *VirtualMachineEnrollmentRequest) { r.Domains = []string{" a"} },
		"bad account":       func(r *VirtualMachineEnrollmentRequest) { r.Account = "Root User" },
	} {
		r := valid
		r.Domains = append([]string(nil), valid.Domains...)
		change(&r)
		if _, err := r.Normalize(); !errors.Is(err, provisioningdomain.ErrInvalidVirtualMachineEnrollment) {
			t.Errorf("%s: Normalize error = %v, want ErrInvalidVirtualMachineEnrollment", name, err)
		}
	}
}
