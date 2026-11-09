package hostenroll

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Every generated password meets the vendor rules and is safe to put on an `ipmitool exec` line.
func TestNewBMCPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		password, err := newBMCPassword("maas")
		if err != nil {
			t.Fatalf("newBMCPassword error = %v", err)
		}
		if len(password) != bmcPasswordLength || !acceptableBMCPassword(password, "maas") {
			t.Errorf("newBMCPassword() = %q, want %d characters that BMC policies accept", password, bmcPasswordLength)
		}
		if strings.ContainsAny(password, "#'\"~ \t") {
			t.Errorf("newBMCPassword() = %q, want nothing ipmitool exec treats specially", password)
		}
		seen[password] = true
	}
	if len(seen) != 200 {
		t.Errorf("newBMCPassword returned %d distinct passwords in 200 draws", len(seen))
	}
}

func TestAcceptableBMCPassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		want     bool
	}{
		{"all classes", "Ab3-cdefghijklmn", true},
		{"no symbol", "Ab3cdefghijklmno", false},
		{"no upper case", "ab3-cdefghijklmn", false},
		{"three in a row", "Ab3-cccdefghijkl", false},
		{"character exec splits on", "Ab3-cdefgh jklmn", false},
		{"the user name", "Ab3-name", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acceptableBMCPassword(tc.password, "Ab3-name"); got != tc.want {
				t.Errorf("acceptableBMCPassword(%q) = %v, want %v", tc.password, got, tc.want)
			}
		})
	}
}

// The fixed columns read empty names, names with a space, and full 16-character names.
func TestParseIPMIUsers(t *testing.T) {
	listed := userList(ipmiUser{id: 1}, ipmiUser{id: 2, name: "ADMIN"}, ipmiUser{id: 3, name: "(Empty User)"},
		ipmiUser{id: 10, name: "sixteen-chars-ab"})
	got := parseIPMIUsers(listed)
	want := []ipmiUser{{id: 1}, {id: 2, name: "ADMIN"}, {id: 3}, {id: 10, name: "sixteen-chars-ab"}}
	if !slices.Equal(got, want) {
		t.Errorf("parseIPMIUsers() = %+v, want %+v", got, want)
	}
}

func TestPickIPMIUser(t *testing.T) {
	users := []ipmiUser{{id: 1}, {id: 2, name: "ADMIN"}, {id: 3}, {id: 4, name: "maas"}}
	if got, ok := pickIPMIUser(users, "maas"); !ok || got.id != 4 {
		t.Errorf("pickIPMIUser(maas) = %+v, %v, want the existing user 4", got, ok)
	}
	if got, ok := pickIPMIUser(users, "swallow"); !ok || got.id != 3 {
		t.Errorf("pickIPMIUser(swallow) = %+v, %v, want the first unused slot 3, never the anonymous user", got, ok)
	}
	if _, ok := pickIPMIUser([]ipmiUser{{id: 1}, {id: 2, name: "ADMIN"}}, "maas"); ok {
		t.Error("pickIPMIUser found a slot in a full table")
	}
}

func TestLANAddress(t *testing.T) {
	cases := map[string]string{
		lanPrint("10.0.0.9"): "10.0.0.9",
		lanPrint("0.0.0.0"):  "",
		fmt.Sprintf("%-24s: %s\n", "IP Address Source", "DHCP Address"): "",
	}
	for printed, want := range cases {
		if got := lanAddress(printed); got != want {
			t.Errorf("lanAddress(%q) = %q, want %q", printed, got, want)
		}
	}
}
