package maas

import (
	"encoding/json"
	"testing"
)

// TestUEFILayoutWithoutESPRequiresInspection reproduces lab-afde-mi308-3: MAAS boots it with UEFI,
// but its layout was built while it booted BIOS (root on partition 2, no EFI system partition), so
// every disk deployment failed in curtin partitioning. Only a layout that proves the mismatch is
// flagged; missing data never is.
func TestUEFILayoutWithoutESPRequiresInspection(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "UEFI machine whose boot disk has only a root partition",
			body: `{"bios_boot_method": "uefi", "boot_disk": {"id": 42, "partitions": [{"filesystem": {"fstype": "ext4", "mount_point": "/"}}]},
				"blockdevice_set": [{"id": 42, "partitions": [{"filesystem": {"fstype": "ext4", "mount_point": "/"}}]}]}`,
			want: true,
		},
		{
			name: "UEFI machine with an EFI system partition",
			body: `{"bios_boot_method": "uefi", "boot_disk": {"id": 43},
				"blockdevice_set": [{"id": 43, "partitions": [
					{"filesystem": {"fstype": "fat32", "mount_point": "/boot/efi"}},
					{"filesystem": {"fstype": "ext4", "mount_point": "/"}}]}]}`,
		},
		{
			name: "BIOS machine with a BIOS layout",
			body: `{"bios_boot_method": "pxe", "boot_disk": {"id": 7},
				"blockdevice_set": [{"id": 7, "partitions": [{"filesystem": {"fstype": "ext4", "mount_point": "/"}}]}]}`,
		},
		{
			name: "UEFI machine without a layout yet",
			body: `{"bios_boot_method": "uefi", "boot_disk": {"id": 9}, "blockdevice_set": [{"id": 9, "partitions": []}]}`,
		},
		{
			name: "UEFI machine whose boot disk is unknown",
			body: `{"bios_boot_method": "uefi", "blockdevice_set": [{"id": 9, "partitions": [{"filesystem": {"fstype": "ext4", "mount_point": "/"}}]}]}`,
		},
		{
			name: "UEFI machine whose EFI system partition is on another disk",
			body: `{"bios_boot_method": "uefi", "boot_disk": {"id": 1},
				"blockdevice_set": [
					{"id": 1, "partitions": [{"filesystem": {"fstype": "ext4", "mount_point": "/"}}]},
					{"id": 2, "partitions": [{"filesystem": {"fstype": "vfat", "mount_point": "/boot/efi/"}}]}]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var machine machineJSON
			if err := json.Unmarshal([]byte(tc.body), &machine); err != nil {
				t.Fatalf("decode machine: %v", err)
			}
			got := toDomainMachine(&machine)
			if got.InspectionRequired != tc.want {
				t.Fatalf("InspectionRequired = %v, want %v", got.InspectionRequired, tc.want)
			}
			if tc.want && got.InspectionRequiredReason != uefiLayoutWithoutESPReason {
				t.Errorf("InspectionRequiredReason = %q, want the UEFI layout explanation", got.InspectionRequiredReason)
			}
			if !tc.want && got.InspectionRequiredReason != "" {
				t.Errorf("InspectionRequiredReason = %q, want empty when not required", got.InspectionRequiredReason)
			}
		})
	}
}
