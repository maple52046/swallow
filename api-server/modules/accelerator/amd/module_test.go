package amd

import (
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// AMD compute GPUs are workload capacity whether the provider names the vendor or falls back to the
// PCI id; AMD display controllers and other vendors' GPUs are not.
func TestIsComputeRecognizesAMDWorkloadGPUs(t *testing.T) {
	tests := []struct {
		name string
		gpu  serverdomain.GPU
		want bool
	}{
		{"named vendor", serverdomain.GPU{Vendor: "Advanced Micro Devices, Inc. [AMD/ATI]", Model: "Aqua Vanjaram [Instinct MI308X]"}, true},
		{"PCI fallback", serverdomain.GPU{Vendor: "", Model: "1002:74a5"}, true},
		{"AMD display controller", serverdomain.GPU{Vendor: "Advanced Micro Devices", Model: "Radeon", Kind: serverdomain.GPUKindDisplay}, false},
		{"another vendor", serverdomain.GPU{Vendor: "Red Hat, Inc.", Model: "Virtio 1.0 GPU"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Accelerator{}).IsCompute(tc.gpu); got != tc.want {
				t.Errorf("IsCompute(%+v) = %v, want %v", tc.gpu, got, tc.want)
			}
		})
	}
}
