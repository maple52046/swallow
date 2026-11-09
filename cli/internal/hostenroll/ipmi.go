package hostenroll

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// This file drives the host's own BMC through its local IPMI system interface (KCS, through the
// OpenIPMI device) with the host's ipmitool. It reads the BMC's LAN settings and user table and
// writes one user's name, password, privilege, and enable state. It never changes the BMC's
// network settings or K_g key, never switches the host's power, and never installs ipmitool: the
// host keeps running its workloads, which is why MAAS's own BMC script is not run here (it may
// rewrite the host's netplan for a Redfish host interface and needs packages a kept OS may lack).

const (
	// maxIPMILANChannel bounds the LAN channel scan. IPMI numbers channels from 1; 14 and 15 are
	// the current and the system interface, never a LAN.
	maxIPMILANChannel = 11
	// anonymousIPMIUser is the user ID the IPMI specification reserves for the anonymous user.
	anonymousIPMIUser = 1
	// bmcPasswordLength is the one length every BMC accepts: IPMI 1.5 allows at most 16 bytes and
	// some vendors require at least 16 (MAAS's own BMC script generates the same length).
	bmcPasswordLength = 16
	// bmcPasswordSymbols are the punctuation a BMC password may use. Every vendor policy accepts
	// them, and none is special to `ipmitool exec`, which cuts a line at '#', joins quoted words,
	// turns '~' inside quotes into a space, and splits on spaces.
	bmcPasswordSymbols = "-_.@+="
)

// ipmiDevices are the device nodes the OpenIPMI driver may create, the ones ipmitool opens.
var ipmiDevices = []string{"/dev/ipmi0", "/dev/ipmi/0", "/dev/ipmidev/0"}

// ipmiPrivilegeLevels maps MAAS's privilege names (the provider's power parameter) to the IPMI
// privilege levels ipmitool takes.
var ipmiPrivilegeLevels = map[string]int{"USER": 2, "OPERATOR": 3, "ADMIN": 4}

// bmcLAN is the BMC's LAN interface: the address the provisioner's power driver connects to and
// the channel the new user is enabled on.
type bmcLAN struct {
	channel int
	address string
}

// ipmiUser is one row of the BMC's user table; an unused slot has no name.
type ipmiUser struct {
	id   int
	name string
}

// ipmiDevicePresent reports whether the OpenIPMI device exists, i.e. whether the ipmi_si and
// ipmi_devintf kernel modules expose the local system interface.
func (e enroller) ipmiDevicePresent() bool {
	for _, device := range ipmiDevices {
		if _, err := e.stat(device); err == nil {
			return true
		}
	}
	return false
}

// ipmi runs ipmitool on the local system interface. -I open pins every call to the OpenIPMI
// device, so no environment or configuration can send it over the LAN. Arguments must never
// carry a secret: every local user can read a process's arguments.
func (e enroller) ipmi(ctx context.Context, dir, ipmitool string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := e.run(ctx, dir, &stdout, &stderr, ipmitool, append([]string{"-I", "open"}, args...)...); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("ipmitool %s: %w: %s", strings.Join(args, " "), err, detail)
		}
		return "", fmt.Errorf("ipmitool %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

// findBMCLAN returns the first IPMI LAN channel that has an IP address. A BMC without one is
// refused rather than configured: giving it an address (static or DHCP) would change the BMC's
// network, which this step never does.
func (e enroller) findBMCLAN(ctx context.Context, dir, ipmitool string) (bmcLAN, error) {
	for channel := 1; channel <= maxIPMILANChannel; channel++ {
		printed, err := e.ipmi(ctx, dir, ipmitool, "lan", "print", strconv.Itoa(channel))
		if err != nil {
			// A channel number that is not a LAN channel on this BMC fails; keep scanning.
			continue
		}
		if address := lanAddress(printed); address != "" {
			return bmcLAN{channel: channel, address: address}, nil
		}
	}
	return bmcLAN{}, errors.New("the BMC has no IP address on any IPMI LAN channel; give it one in its own setup " +
		"(swallow does not change the BMC's network), or set the Power Configuration in swallow")
}

// lanAddress reads the "IP Address" line of `ipmitool lan print`, whose lines are
// "<name padded to 24>: <value>". An unset address (0.0.0.0) counts as none.
func lanAddress(printed string) string {
	for _, line := range strings.Split(printed, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) != "IP Address" {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(value))
		if ip == nil || ip.IsUnspecified() {
			return ""
		}
		return ip.String()
	}
	return ""
}

// ipmiUsers reads the BMC's user table on channel.
func (e enroller) ipmiUsers(ctx context.Context, dir, ipmitool string, channel int) ([]ipmiUser, error) {
	listed, err := e.ipmi(ctx, dir, ipmitool, "user", "list", strconv.Itoa(channel))
	if err != nil {
		return nil, fmt.Errorf("read the BMC's users: %w", err)
	}
	return parseIPMIUsers(listed), nil
}

// parseIPMIUsers reads `ipmitool user list`, which prints each user as "%-4d%-17s" (ID, name)
// followed by the access flags. The fixed columns keep an empty name, or a name with a space such
// as some HP BMCs' "(Empty User)", from shifting the fields. Lines without an ID (the header) are
// skipped.
func parseIPMIUsers(listed string) []ipmiUser {
	var users []ipmiUser
	for _, line := range strings.Split(listed, "\n") {
		if len(line) < 4 {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSpace(line[:4]))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(line[4:min(len(line), 21)])
		if name == "(Empty User)" {
			name = ""
		}
		users = append(users, ipmiUser{id: id, name: name})
	}
	return users
}

// pickIPMIUser chooses the slot for username the way MAAS's BMC script does: the user already
// named username, otherwise the first unused slot. The anonymous user is never chosen, and no
// other named user is ever reused, so the BMC's existing accounts keep working.
func pickIPMIUser(users []ipmiUser, username string) (ipmiUser, bool) {
	var unused *ipmiUser
	for i := range users {
		user := &users[i]
		if user.id == anonymousIPMIUser {
			continue
		}
		if user.name == username {
			return *user, true
		}
		if user.name == "" && unused == nil {
			unused = user
		}
	}
	if unused == nil {
		return ipmiUser{}, false
	}
	return *unused, true
}

// setUpIPMIUser gives slot user the name username and password, the privilege level on the LAN
// channel, and IPMI messaging, then enables it. The order follows what BMCs require: some refuse
// to enable a user before its name and password are set, and some need the privilege limit before
// LAN IPMI messaging. Enabling comes last so the user cannot log in half-configured.
func (e enroller) setUpIPMIUser(ctx context.Context, dir, ipmitool string, lan bmcLAN, user ipmiUser, username, password string, privilege int) error {
	id := strconv.Itoa(user.id)
	channel := strconv.Itoa(lan.channel)
	level := strconv.Itoa(privilege)
	if user.name != username {
		if _, err := e.ipmi(ctx, dir, ipmitool, "user", "set", "name", id, username); err != nil {
			return err
		}
	}
	if err := e.setIPMIPassword(ctx, dir, ipmitool, id, password); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"user", "priv", id, level, channel},
		{"channel", "setaccess", channel, id, "callin=on", "ipmi=on", "link=on", "privilege=" + level},
		{"user", "enable", id},
	} {
		if _, err := e.ipmi(ctx, dir, ipmitool, args...); err != nil {
			return err
		}
	}
	return nil
}

// setIPMIPassword sets user id's password through an `ipmitool exec` command file, because
// ipmitool reads a new user password only from its arguments, which every local user can read.
// The file is owner-only inside the private working directory and removed as soon as ipmitool
// returns; the password is masked in any error ipmitool reports.
func (e enroller) setIPMIPassword(ctx context.Context, dir, ipmitool, id, password string) error {
	path := filepath.Join(dir, "bmc-password.ipmitool")
	if err := os.WriteFile(path, []byte("user set password "+id+" "+password+"\n"), 0o600); err != nil {
		return fmt.Errorf("write the BMC password command: %w", err)
	}
	defer os.Remove(path)
	if _, err := e.ipmi(ctx, dir, ipmitool, "exec", path); err != nil {
		return fmt.Errorf("set the password of BMC user %s: %s", id, strings.ReplaceAll(err.Error(), password, "[REDACTED]"))
	}
	return nil
}

// newBMCPassword returns a random password from crypto/rand that BMC vendor policies accept:
// bmcPasswordLength characters with a lower-case letter, an upper-case letter, a digit, and a
// symbol, no character three times in a row, and not username or username reversed. A draw that
// misses a rule is redrawn; with this alphabet about three in four draws pass, so the attempt
// bound is never reached in practice.
func newBMCPassword(username string) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" + bmcPasswordSymbols
	limit := big.NewInt(int64(len(alphabet)))
	for attempt := 0; attempt < 64; attempt++ {
		password := make([]byte, bmcPasswordLength)
		for i := range password {
			n, err := rand.Int(rand.Reader, limit)
			if err != nil {
				return "", fmt.Errorf("generate the BMC password: %w", err)
			}
			password[i] = alphabet[n.Int64()]
		}
		if acceptableBMCPassword(string(password), username) {
			return string(password), nil
		}
	}
	return "", errors.New("generate the BMC password: no draw met the BMC password rules")
}

// acceptableBMCPassword applies the vendor rules newBMCPassword promises.
func acceptableBMCPassword(password, username string) bool {
	if password == username || password == reverse(username) {
		return false
	}
	var lower, upper, digit, symbol bool
	for i, c := range password {
		switch {
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= '0' && c <= '9':
			digit = true
		case strings.ContainsRune(bmcPasswordSymbols, c):
			symbol = true
		default:
			return false
		}
		if i >= 2 && password[i-1] == password[i] && password[i-2] == password[i] {
			return false
		}
	}
	return lower && upper && digit && symbol
}

// reverse returns s with its bytes in reverse order; BMC user names are ASCII.
func reverse(s string) string {
	reversed := []byte(s)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return string(reversed)
}
