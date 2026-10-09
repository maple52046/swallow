package libvirt

import (
	"strings"
	"testing"
)

// tainanDomain is the shape of the tainan-ci lab domains: UEFI q35, a SATA CD-ROM booting first, a
// GPU passthrough with a boot order, a bridged NIC after it, and qemu: namespaced elements.
const tainanDomain = `<domain type='kvm' xmlns:qemu='http://libvirt.org/schemas/domain/qemu/1.0'>
  <name>lab-afde-mi308-1</name>
  <uuid>5d685113-0078-444c-8707-5f9567736758</uuid>
  <os firmware='efi'>
    <type arch='x86_64' machine='pc-q35-noble'>hvm</type>
    <loader readonly='yes' type='pflash'>/usr/share/OVMF/OVMF_CODE_4M.fd</loader>
  </os>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='/var/lib/libvirt/boot/swallow-ipxe.iso'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
      <boot order='1'/>
    </disk>
    <interface type='bridge'>
      <mac address='52:54:00:AF:DE:01'/>
      <source bridge='br0'/>
      <boot order='3'/>
    </interface>
    <hostdev mode='subsystem' type='pci' managed='no'>
      <source><address domain='0x0000' bus='0x77' slot='0x00' function='0x0'/></source>
      <boot order='2'/>
    </hostdev>
  </devices>
  <qemu:commandline>
    <qemu:arg value='-fw_cfg'/>
    <qemu:arg value='name=opt/ovmf/X-PciMmio64Mb,string=4194304'/>
  </qemu:commandline>
</domain>`

func mustParse(t *testing.T, xml string) *domainDefinition {
	t.Helper()
	parsed, err := parseDomain([]byte(xml))
	if err != nil {
		t.Fatalf("parseDomain error = %v", err)
	}
	return parsed
}

func TestDomainFacts(t *testing.T) {
	d := mustParse(t, tainanDomain)
	if d.name() != "lab-afde-mi308-1" || d.uuid() != "5d685113-0078-444c-8707-5f9567736758" || d.architecture() != "x86_64" {
		t.Errorf("facts = %q %q %q", d.name(), d.uuid(), d.architecture())
	}
	if macs := d.macAddresses(); len(macs) != 1 || macs[0] != "52:54:00:af:de:01" {
		t.Errorf("macs = %v, want the lower-cased NIC MAC", macs)
	}
	if d.cdrom() == nil || !d.cdromBootsFirst() {
		t.Error("the CD-ROM should boot first")
	}
}

// Rewriting keeps prefixed names, and putting a volume on the CD-ROM keeps it first while the
// other devices keep their order.
func TestSetBootMediaKeepsOrderAndNamespaces(t *testing.T) {
	d := mustParse(t, tainanDomain)
	const volume = "/var/lib/libvirt/images/swallow-ipxe-iso-1.iso"
	if err := d.setBootMedia(volume); err != nil {
		t.Fatalf("setBootMedia error = %v", err)
	}
	out := string(d.root.marshal())
	for _, want := range []string{`xmlns:qemu="http://libvirt.org/schemas/domain/qemu/1.0"`, `<qemu:commandline>`, `<qemu:arg value="-fw_cfg"/>`, `</qemu:commandline>`} {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten domain lacks %s:\n%s", want, out)
		}
	}
	again := mustParse(t, out)
	if !again.holdsVolume("swallow-ipxe-iso-1.iso") || !again.cdromBootsFirst() {
		t.Error("the volume is not on the CD-ROM booting first")
	}
	devices := again.devices()
	hostdev, iface := devices.child("hostdev"), devices.child("interface")
	if bootOrder(hostdev) != 2 || bootOrder(iface) != 3 {
		t.Errorf("orders hostdev=%d interface=%d, want 2 and 3", bootOrder(hostdev), bootOrder(iface))
	}
}

// A domain without a CD-ROM gets one on the machine's bus, booting first; `<os><boot>` entries are
// turned into per-device orders behind it.
func TestSetBootMediaAddsCDROMAndConvertsOSBoot(t *testing.T) {
	d := mustParse(t, `<domain type='kvm'>
  <name>plain</name>
  <os><type arch='x86_64' machine='pc-i440fx-8.2'>hvm</type><boot dev='network'/><boot dev='hd'/></os>
  <devices>
    <disk type='file' device='disk'><source file='/var/lib/libvirt/images/plain.qcow2'/><target dev='hda' bus='ide'/></disk>
    <interface type='network'><mac address='52:54:00:00:00:01'/><source network='default'/></interface>
  </devices>
</domain>`)
	if err := d.setBootMedia("/var/lib/libvirt/images/x.iso"); err != nil {
		t.Fatalf("setBootMedia error = %v", err)
	}
	again := mustParse(t, string(d.root.marshal()))
	if boots := again.root.child("os").elements("boot"); len(boots) != 0 {
		t.Errorf("os boot entries remain: %d", len(boots))
	}
	cd := again.cdrom()
	if cd == nil || cd.child("target").attr("bus") != "ide" || cd.child("target").attr("dev") != "hdb" {
		t.Fatalf("cdrom = %+v, want a new IDE hdb", cd)
	}
	if !again.cdromBootsFirst() {
		t.Error("the new CD-ROM does not boot first")
	}
	if iface, disk := again.devices().child("interface"), again.devices().child("disk"); bootOrder(iface) != 2 || bootOrder(disk) != 3 {
		t.Errorf("orders interface=%d disk=%d, want 2 and 3 (network before hd, as before)", bootOrder(iface), bootOrder(disk))
	}
}

// Clearing ejects the CD-ROM and closes the gap in the boot order.
func TestClearBootMedia(t *testing.T) {
	d := mustParse(t, tainanDomain)
	d.clearBootMedia()
	again := mustParse(t, string(d.root.marshal()))
	if cd := again.cdrom(); cd == nil || cd.child("source") != nil || cd.child("boot") != nil {
		t.Errorf("cdrom after clear = %s", cd.marshal())
	}
	if hostdev, iface := again.devices().child("hostdev"), again.devices().child("interface"); bootOrder(hostdev) != 1 || bootOrder(iface) != 2 {
		t.Errorf("orders hostdev=%d interface=%d, want 1 and 2", bootOrder(hostdev), bootOrder(iface))
	}
	if again.cdromBootsFirst() {
		t.Error("an ejected CD-ROM still boots first")
	}
}

func TestBootMediaState(t *testing.T) {
	d := mustParse(t, tainanDomain)
	state := bootMediaState(d, "swallow-ipxe.iso")
	if !state.MediaInserted || !state.Ready() || state.OverrideEnabled != "Continuous" || state.MediaImage != "/var/lib/libvirt/boot/swallow-ipxe.iso" {
		t.Errorf("state = %+v, want the CD-ROM's ISO ready", state)
	}
	if other := bootMediaState(d, "swallow-ipxe-other.iso"); other.MediaInserted || other.Ready() {
		t.Errorf("state for another volume = %+v, want not inserted", other)
	}
}
