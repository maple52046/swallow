package domain

import "testing"

func TestClassifyGPUKind(t *testing.T) {
	tests := []struct {
		name, vendor, model string
		want                GPUKind
	}{
		{name: "unnamed AMD accelerator", vendor: "Advanced Micro Devices, Inc. [AMD/ATI]", want: GPUKindCompute},
		{name: "NVIDIA accelerator", vendor: "NVIDIA Corporation", model: "H100", want: GPUKindCompute},
		{name: "ASPEED controller", vendor: "ASPEED Technology, Inc.", model: "ASPEED Graphics Family", want: GPUKindDisplay},
		{name: "ASPEED PCI fallback", model: "1a03:2000", want: GPUKindDisplay},
		{name: "Cirrus Logic controller", vendor: "Cirrus Logic", model: "GD 5446", want: GPUKindDisplay},
		{name: "Cirrus Logic PCI fallback", model: "1013:00b8", want: GPUKindDisplay},
		{name: "Matrox G200 controller", vendor: "Matrox", model: "G200e", want: GPUKindDisplay},
		{name: "named non-BMC Matrox", vendor: "Matrox", model: "LUMA A380", want: GPUKindCompute},
		{name: "XGI BMC controller", vendor: "XGI Technology Inc.", model: "Volari Z9s", want: GPUKindDisplay},
		{name: "virtio-gpu console adapter", vendor: "Red Hat, Inc.", model: "Virtio 1.0 GPU", want: GPUKindDisplay},
		{name: "virtio-gpu PCI fallback", model: "1af4:1050", want: GPUKindDisplay},
		{name: "QXL console adapter", vendor: "Red Hat, Inc.", model: "QXL paravirtual graphic card", want: GPUKindDisplay},
		{name: "QXL PCI fallback", model: "1b36:0100", want: GPUKindDisplay},
		{name: "QEMU standard VGA", vendor: "Technical Corp.", model: "QEMU Virtual Video Controller", want: GPUKindDisplay},
		{name: "Bochs PCI fallback", model: "1234:1111", want: GPUKindDisplay},
		{name: "VMware SVGA II", vendor: "VMware", model: "SVGA II Adapter", want: GPUKindDisplay},
		{name: "VMware SVGA PCI fallback", model: "15ad:0405", want: GPUKindDisplay},
		{name: "Hyper-V video", vendor: "Microsoft Corporation", model: "Hyper-V virtual VGA", want: GPUKindDisplay},
		{name: "Hyper-V PCI fallback", model: "1414:5353", want: GPUKindDisplay},
		{name: "other Red Hat virtio device stays compute", vendor: "Red Hat, Inc.", model: "Virtio 1.0 network device", want: GPUKindCompute},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyGPUKind(test.vendor, test.model); got != test.want {
				t.Errorf("ClassifyGPUKind(%q, %q) = %q, want %q", test.vendor, test.model, got, test.want)
			}
		})
	}
}

func TestGPUClassifiedKindAndCountKeepLegacyInventoryCompatible(t *testing.T) {
	gpus := []GPU{
		{Vendor: "AMD", Model: "MI300X", Count: 8},
		{Vendor: "ASPEED Technology, Inc.", Model: "ASPEED Graphics Family", Count: 1},
		{Vendor: "AMD", Model: "MI300X", Count: 2, Kind: GPUKindDisplay},
	}

	if got := GPUCountByKind(gpus, GPUKindCompute); got != 8 {
		t.Errorf("GPUCountByKind(compute) = %d, want 8", got)
	}
	if got := GPUCountByKind(gpus, GPUKindDisplay); got != 3 {
		t.Errorf("GPUCountByKind(display) = %d, want 3", got)
	}
}
