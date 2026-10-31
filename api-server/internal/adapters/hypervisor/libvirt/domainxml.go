package libvirt

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// node is one element of a libvirt XML document kept as it was read: names keep their namespace
// prefix (`qemu:commandline`), attributes their order, and text and comments their content, so a
// rewritten domain differs only in what swallow changed. encoding/xml's struct mapping would
// rewrite prefixed names into default-namespace declarations.
type node struct {
	name     string
	attrs    []xml.Attr
	children []item
}

// item is one child of a node: an element, character data, or a comment.
type item struct {
	elem    *node
	text    string
	comment string
}

// parseXML reads one document into its root element.
func parseXML(data []byte) (*node, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var stack []*node
	var root *node
	for {
		token, err := decoder.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse libvirt XML: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			n := &node{name: qualified(t.Name), attrs: append([]xml.Attr(nil), t.Attr...)}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, item{elem: n})
			} else if root == nil {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errors.New("parse libvirt XML: unbalanced end element")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, item{text: string(t)})
			}
		case xml.Comment:
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, item{comment: string(t)})
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, errors.New("parse libvirt XML: no complete root element")
	}
	return root, nil
}

func qualified(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}

// marshal writes the element and its subtree.
func (n *node) marshal() []byte {
	var out bytes.Buffer
	n.write(&out)
	return out.Bytes()
}

func (n *node) write(out *bytes.Buffer) {
	out.WriteString("<" + n.name)
	for _, a := range n.attrs {
		out.WriteString(" " + qualified(a.Name) + `="`)
		_ = xml.EscapeText(out, []byte(a.Value))
		out.WriteString(`"`)
	}
	if len(n.children) == 0 {
		out.WriteString("/>")
		return
	}
	out.WriteString(">")
	for _, child := range n.children {
		switch {
		case child.elem != nil:
			child.elem.write(out)
		case child.comment != "":
			out.WriteString("<!--" + child.comment + "-->")
		default:
			_ = xml.EscapeText(out, []byte(child.text))
		}
	}
	out.WriteString("</" + n.name + ">")
}

func (n *node) attr(name string) string {
	for _, a := range n.attrs {
		if qualified(a.Name) == name {
			return a.Value
		}
	}
	return ""
}

func (n *node) setAttr(name, value string) {
	for i, a := range n.attrs {
		if qualified(a.Name) == name {
			n.attrs[i].Value = value
			return
		}
	}
	n.attrs = append(n.attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
}

func (n *node) child(name string) *node {
	for _, c := range n.children {
		if c.elem != nil && c.elem.name == name {
			return c.elem
		}
	}
	return nil
}

func (n *node) elements(name string) []*node {
	var out []*node
	for _, c := range n.children {
		if c.elem != nil && (name == "" || c.elem.name == name) {
			out = append(out, c.elem)
		}
	}
	return out
}

func (n *node) text() string {
	var out strings.Builder
	for _, c := range n.children {
		if c.elem == nil && c.comment == "" {
			out.WriteString(c.text)
		}
	}
	return strings.TrimSpace(out.String())
}

func (n *node) remove(target *node) {
	for i, c := range n.children {
		if c.elem == target {
			n.children = append(n.children[:i], n.children[i+1:]...)
			return
		}
	}
}

func (n *node) removeAll(name string) {
	kept := n.children[:0]
	for _, c := range n.children {
		if c.elem == nil || c.elem.name != name {
			kept = append(kept, c)
		}
	}
	n.children = kept
}

func (n *node) add(child *node) {
	n.children = append(n.children, item{elem: child})
}

func element(name string, attrs ...string) *node {
	n := &node{name: name}
	for i := 0; i+1 < len(attrs); i += 2 {
		n.setAttr(attrs[i], attrs[i+1])
	}
	return n
}

// domainDefinition is a domain's persistent definition, read for its facts and rewritten for Boot
// Media.
type domainDefinition struct {
	root *node
}

func parseDomain(data []byte) (*domainDefinition, error) {
	root, err := parseXML(data)
	if err != nil {
		return nil, err
	}
	if root.name != "domain" {
		return nil, fmt.Errorf("parse libvirt XML: root is <%s>, not <domain>", root.name)
	}
	return &domainDefinition{root: root}, nil
}

func (d *domainDefinition) name() string {
	if n := d.root.child("name"); n != nil {
		return n.text()
	}
	return ""
}

func (d *domainDefinition) uuid() string {
	if n := d.root.child("uuid"); n != nil {
		return n.text()
	}
	return ""
}

func (d *domainDefinition) osType() *node {
	if osNode := d.root.child("os"); osNode != nil {
		return osNode.child("type")
	}
	return nil
}

func (d *domainDefinition) architecture() string {
	if t := d.osType(); t != nil {
		return t.attr("arch")
	}
	return ""
}

func (d *domainDefinition) machine() string {
	if t := d.osType(); t != nil {
		return t.attr("machine")
	}
	return ""
}

func (d *domainDefinition) devices() *node {
	return d.root.child("devices")
}

// macAddresses are the domain's interface MAC addresses in definition order, lower case.
func (d *domainDefinition) macAddresses() []string {
	devices := d.devices()
	if devices == nil {
		return nil
	}
	var macs []string
	for _, iface := range devices.elements("interface") {
		if mac := iface.child("mac"); mac != nil {
			if address := strings.ToLower(strings.TrimSpace(mac.attr("address"))); address != "" {
				macs = append(macs, address)
			}
		}
	}
	return macs
}

// cdrom is the domain's first CD-ROM device, or nil.
func (d *domainDefinition) cdrom() *node {
	devices := d.devices()
	if devices == nil {
		return nil
	}
	for _, disk := range devices.elements("disk") {
		if disk.attr("device") == "cdrom" {
			return disk
		}
	}
	return nil
}

// cdromSource is the image the CD-ROM holds: its file path, or the volume name of a volume source.
func cdromSource(cd *node) string {
	if cd == nil {
		return ""
	}
	source := cd.child("source")
	if source == nil {
		return ""
	}
	if file := source.attr("file"); file != "" {
		return file
	}
	return source.attr("volume")
}

// bootOrder is a device's per-device boot order, 0 when it has none.
func bootOrder(device *node) int {
	boot := device.child("boot")
	if boot == nil {
		return 0
	}
	order, err := strconv.Atoi(boot.attr("order"))
	if err != nil || order < 1 {
		return 0
	}
	return order
}

// cdromBootsFirst reports whether the CD-ROM is the first boot device: the lowest per-device boot
// order, or the first `<os><boot dev>` when the definition uses those instead.
func (d *domainDefinition) cdromBootsFirst() bool {
	cd := d.cdrom()
	if cd == nil {
		return false
	}
	if osNode := d.root.child("os"); osNode != nil {
		if boots := osNode.elements("boot"); len(boots) > 0 {
			return boots[0].attr("dev") == "cdrom"
		}
	}
	order := bootOrder(cd)
	if order == 0 {
		return false
	}
	for _, device := range d.devices().elements("") {
		if device != cd {
			if other := bootOrder(device); other != 0 && other < order {
				return false
			}
		}
	}
	return true
}

// holdsVolume reports whether the CD-ROM holds the named volume.
func (d *domainDefinition) holdsVolume(volume string) bool {
	source := cdromSource(d.cdrom())
	return source != "" && path.Base(source) == volume
}

// setBootMedia puts the file imagePath on the CD-ROM, adding a CD-ROM when the domain has none,
// and makes it the first boot device, keeping the order of the other boot devices after it.
func (d *domainDefinition) setBootMedia(imagePath string) error {
	devices := d.devices()
	if devices == nil {
		return errors.New("the domain definition has no <devices>")
	}
	d.convertOSBoot()
	cd := d.cdrom()
	if cd == nil {
		cd = d.newCDROM()
		devices.add(cd)
	}
	cd.setAttr("type", "file")
	if cd.child("driver") == nil {
		cd.add(element("driver", "name", "qemu", "type", "raw"))
	}
	if source := cd.child("source"); source != nil {
		source.attrs = []xml.Attr{{Name: xml.Name{Local: "file"}, Value: imagePath}}
		source.children = nil
	} else {
		cd.add(element("source", "file", imagePath))
	}
	if cd.child("readonly") == nil {
		cd.add(element("readonly"))
	}
	d.renumberBoot(cd)
	return nil
}

// clearBootMedia ejects the CD-ROM and removes its boot order, keeping the order of the other boot
// devices. A domain without a CD-ROM is left as it is.
func (d *domainDefinition) clearBootMedia() {
	cd := d.cdrom()
	if cd == nil {
		return
	}
	if osNode := d.root.child("os"); osNode != nil {
		for _, boot := range osNode.elements("boot") {
			if boot.attr("dev") == "cdrom" {
				osNode.remove(boot)
			}
		}
	}
	cd.removeAll("source")
	cd.removeAll("boot")
	d.renumberBoot(nil)
}

// convertOSBoot turns `<os><boot dev>` entries into per-device boot orders, because libvirt refuses
// a definition that mixes both and Boot Media needs a per-device order on the CD-ROM.
func (d *domainDefinition) convertOSBoot() {
	osNode := d.root.child("os")
	devices := d.devices()
	if osNode == nil || devices == nil {
		return
	}
	boots := osNode.elements("boot")
	if len(boots) == 0 {
		return
	}
	order := 0
	assign := func(device *node) {
		if device != nil && device.child("boot") == nil {
			order++
			device.add(element("boot", "order", strconv.Itoa(order)))
		}
	}
	firstDisk := func(kind string) *node {
		for _, disk := range devices.elements("disk") {
			device := disk.attr("device")
			if device == "" {
				device = "disk"
			}
			if device == kind {
				return disk
			}
		}
		return nil
	}
	for _, boot := range boots {
		switch boot.attr("dev") {
		case "hd":
			assign(firstDisk("disk"))
		case "cdrom":
			assign(firstDisk("cdrom"))
		case "fd":
			assign(firstDisk("floppy"))
		case "network":
			for _, iface := range devices.elements("interface") {
				assign(iface)
			}
		}
		osNode.remove(boot)
	}
}

// renumberBoot gives first boot order 1 (when not nil) and the other devices that have a boot
// order 2, 3, … in their current order.
func (d *domainDefinition) renumberBoot(first *node) {
	devices := d.devices()
	type ordered struct {
		device *node
		order  int
	}
	var others []ordered
	for _, device := range devices.elements("") {
		if device == first {
			continue
		}
		if order := bootOrder(device); order != 0 {
			others = append(others, ordered{device, order})
		}
	}
	sort.SliceStable(others, func(i, j int) bool { return others[i].order < others[j].order })
	next := 1
	if first != nil {
		first.removeAll("boot")
		first.add(element("boot", "order", "1"))
		next = 2
	}
	for _, o := range others {
		o.device.child("boot").setAttr("order", strconv.Itoa(next))
		next++
	}
}

// newCDROM builds a CD-ROM on the bus the machine type supports: SATA on q35, SCSI on the ARM virt
// machine (adding a virtio-scsi controller when there is none), IDE otherwise.
func (d *domainDefinition) newCDROM() *node {
	machine := d.machine()
	bus, prefix := "ide", "hd"
	switch {
	case strings.Contains(machine, "q35"):
		bus, prefix = "sata", "sd"
	case strings.HasPrefix(machine, "virt"):
		bus, prefix = "scsi", "sd"
		d.ensureSCSIController()
	}
	used := map[string]bool{}
	for _, disk := range d.devices().elements("disk") {
		if target := disk.child("target"); target != nil {
			used[target.attr("dev")] = true
		}
	}
	dev := prefix + "a"
	for letter := 'a'; letter <= 'z'; letter++ {
		if candidate := prefix + string(letter); !used[candidate] {
			dev = candidate
			break
		}
	}
	cd := element("disk", "type", "file", "device", "cdrom")
	cd.add(element("driver", "name", "qemu", "type", "raw"))
	cd.add(element("target", "dev", dev, "bus", bus))
	cd.add(element("readonly"))
	return cd
}

func (d *domainDefinition) ensureSCSIController() {
	devices := d.devices()
	for _, controller := range devices.elements("controller") {
		if controller.attr("type") == "scsi" {
			return
		}
	}
	devices.add(element("controller", "type", "scsi", "model", "virtio-scsi"))
}
