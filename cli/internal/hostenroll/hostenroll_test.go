package hostenroll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type recordedRun struct {
	name string
	args []string
}

// fakeHost stands in for the host being enrolled: its tools, its IPMI device and BMC, and its
// network interfaces.
type fakeHost struct {
	t              *testing.T
	euid           int
	registerOutput string
	noIPMITool     bool
	noIPMIDevice   bool
	uefi           bool
	// lan is the `ipmitool lan print <channel>` output per channel; other channels fail.
	lan map[int]string
	// users is the `ipmitool user list` output.
	users string
	macs  []string
	runs  []recordedRun
	// execFiles are the command files `ipmitool exec` read, by path, with their contents.
	execFiles map[string]string
	// routeAddress is the address the host reaches MAAS from.
	routeAddress string
	// reports counts report-results runs; onReport, when set, sees each one (the fake MAAS
	// recording the address only on a later report, for example).
	reports  int
	onReport func(report int)
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	return &fakeHost{
		t:              t,
		registerOutput: "reporting:\n  maas:\n    token_key: k\n",
		uefi:           true,
		lan:            map[int]string{1: lanPrint("10.0.0.9")},
		users: userList(
			ipmiUser{id: 1}, ipmiUser{id: 2, name: "ADMIN"}, ipmiUser{id: 3}, ipmiUser{id: 4},
		),
		macs:         []string{"aa:bb:cc:00:00:01"},
		execFiles:    map[string]string{},
		routeAddress: "10.0.0.20",
	}
}

// lanPrint renders `ipmitool lan print` the way ipmitool does ("%-24s: %s").
func lanPrint(address string) string {
	return fmt.Sprintf("%-24s: %s\n%-24s: %s\n%-24s: %s\n",
		"IP Address Source", "Static Address", "IP Address", address, "MAC Address", "aa:bb:cc:00:00:99")
}

// userList renders `ipmitool user list` the way ipmitool does ("%-4d%-17s" and the flags).
func userList(users ...ipmiUser) string {
	listed := "ID  Name\t     Callin  Link Auth\tIPMI Msg   Channel Priv Limit\n"
	for _, user := range users {
		listed += fmt.Sprintf("%-4d%-17s%-8s%-11s%-11s%-s\n", user.id, user.name, "true", "false", "false", "NO ACCESS")
	}
	return listed
}

func (h *fakeHost) enroller() enroller {
	return enroller{
		httpClient: http.DefaultClient,
		geteuid:    func() int { return h.euid },
		lookPath: func(name string) (string, error) {
			if name == "ipmitool" && h.noIPMITool {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		},
		hostname: func() (string, error) { return "db-03.lab.example", nil },
		run:      h.run,
		stat: func(path string) (os.FileInfo, error) {
			if (path == "/dev/ipmi0" && !h.noIPMIDevice) || (path == "/sys/firmware/efi" && h.uefi) {
				return nil, nil
			}
			return nil, os.ErrNotExist
		},
		hostMACs:     func() ([]string, error) { return h.macs, nil },
		routeAddress: func(string) (string, error) { return h.routeAddress, nil },
	}
}

func (h *fakeHost) run(_ context.Context, _ string, stdout, stderr io.Writer, name string, args ...string) error {
	h.runs = append(h.runs, recordedRun{name: name, args: append([]string(nil), args...)})
	if filepath.Base(name) == "ipmitool" {
		return h.ipmitool(stdout, stderr, args)
	}
	switch args[0] {
	case "register-machine":
		_, _ = io.WriteString(stdout, h.registerOutput)
	case "get-machine-token":
		// get-machine-token <region> <api key> <system id> <credentials file>
		if err := os.WriteFile(args[4], []byte("reporting:\n  maas:\n    token_key: k\n"), 0o600); err != nil {
			return err
		}
	case "report-results":
		if _, err := os.Stat(args[2]); err != nil {
			return errors.New("credentials missing")
		}
		h.reports++
		if h.onReport != nil {
			h.onReport(h.reports)
		}
	}
	return nil
}

func (h *fakeHost) ipmitool(stdout, stderr io.Writer, args []string) error {
	if len(args) < 3 || args[0] != "-I" || args[1] != "open" {
		h.t.Errorf("ipmitool %v does not pin the local interface (-I open)", args)
		return errors.New("bad interface")
	}
	command := args[2:]
	switch {
	case command[0] == "lan" && command[1] == "print":
		channel, _ := strconv.Atoi(command[2])
		printed, ok := h.lan[channel]
		if !ok {
			_, _ = io.WriteString(stderr, "Invalid channel "+command[2])
			return errors.New("exit status 1")
		}
		_, _ = io.WriteString(stdout, printed)
	case command[0] == "user" && command[1] == "list":
		_, _ = io.WriteString(stdout, h.users)
	case command[0] == "exec":
		content, err := os.ReadFile(command[1])
		if err != nil {
			return err
		}
		h.execFiles[command[1]] = string(content)
	}
	return nil
}

// ipmiCommands returns the ipmitool commands run, without the -I open prefix, space-joined.
func (h *fakeHost) ipmiCommands() []string {
	var commands []string
	for _, run := range h.runs {
		if filepath.Base(run.name) == "ipmitool" {
			commands = append(commands, strings.Join(run.args[2:], " "))
		}
	}
	return commands
}

// ran reports whether maas-run-scripts ran operation.
func (h *fakeHost) ran(operation string) bool {
	return slices.ContainsFunc(h.runs, func(run recordedRun) bool {
		return filepath.Base(run.name) == "maas-run-scripts" && run.args[0] == operation
	})
}

// fakeMAAS is a MAAS region: it serves maas-run-scripts and the API calls the BMC step makes,
// requiring the API key a:b:c.
type fakeMAAS struct {
	t        *testing.T
	server   *httptest.Server
	machines []map[string]any
	config   map[string]any
	// powerState is what query_power_state answers; powerStatus, when set, is its HTTP status.
	powerState  string
	powerStatus int
	updates     []map[string]string
}

func newFakeMAAS(t *testing.T) *fakeMAAS {
	t.Helper()
	m := &fakeMAAS{
		t: t,
		machines: []map[string]any{{
			"system_id": "abc123", "hostname": "db-03", "power_type": "", "ip_addresses": []string{"10.0.0.20"},
			"interface_set": []map[string]any{{"mac_address": "aa:bb:cc:00:00:01"}},
		}},
		config:     map[string]any{},
		powerState: "on",
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.server.Close)
	return m
}

func (m *fakeMAAS) url() string { return m.server.URL + "/MAAS" }

func (m *fakeMAAS) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/MAAS/maas-run-scripts" {
		_, _ = io.WriteString(w, "#!/usr/bin/env python3\n")
		return
	}
	path, ok := strings.CutPrefix(r.URL.Path, "/MAAS/api/2.0")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if auth := r.Header.Get("Authorization"); !strings.Contains(auth, `oauth_consumer_key="a"`) ||
		!strings.Contains(auth, `oauth_token="b"`) || !strings.Contains(auth, `oauth_signature="&c"`) {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	switch {
	case path == "/maas/" && r.URL.Query().Get("op") == "get_config":
		_ = json.NewEncoder(w).Encode(m.config[r.URL.Query().Get("name")])
	case path == "/machines/" && r.Method == http.MethodGet:
		var matched []map[string]any
		for _, machine := range m.machines {
			if machine["hostname"] == r.URL.Query().Get("hostname") {
				matched = append(matched, machine)
			}
		}
		_ = json.NewEncoder(w).Encode(matched)
	case strings.HasPrefix(path, "/machines/"):
		m.serveMachine(w, r, strings.Trim(strings.TrimPrefix(path, "/machines/"), "/"))
	default:
		http.NotFound(w, r)
	}
}

func (m *fakeMAAS) serveMachine(w http.ResponseWriter, r *http.Request, systemID string) {
	index := slices.IndexFunc(m.machines, func(machine map[string]any) bool { return machine["system_id"] == systemID })
	if index < 0 {
		http.Error(w, "No Machine matches the given query.", http.StatusNotFound)
		return
	}
	machine := m.machines[index]
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("op") == "query_power_state":
		if m.powerStatus != 0 {
			http.Error(w, "Failed talking to the BMC", m.powerStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"state": m.powerState})
	case r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(machine)
	case r.Method == http.MethodPut:
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			m.t.Errorf("machine update is not multipart: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		fields := map[string]string{}
		for key, values := range r.MultipartForm.Value {
			fields[key] = values[0]
		}
		m.updates = append(m.updates, fields)
		machine["power_type"] = fields["power_type"]
		_ = json.NewEncoder(w).Encode(machine)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// Enroll downloads maas-run-scripts from the region, registers the host under its short name with
// the token, then reports results with the credentials register-machine printed, leaves nothing
// behind, and says how to give the Machine the power driver registration does not set.
func TestEnrollMAAS(t *testing.T) {
	maas := newFakeMAAS(t)
	host := newFakeHost(t)
	var stdout strings.Builder
	err := host.enroller().enroll(context.Background(), Options{
		Provisioner: "maas", Endpoint: maas.url() + "/api/2.0/", Token: "a:b:c", Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if len(host.runs) != 2 {
		t.Fatalf("runs = %+v, want register-machine then report-results", host.runs)
	}
	register := strings.Join(host.runs[0].args, " ")
	if want := "register-machine --hostname db-03 " + maas.url() + " a:b:c"; register != want {
		t.Errorf("register-machine args = %q, want %q", register, want)
	}
	if host.runs[1].args[0] != "report-results" || host.runs[1].args[1] != "--config" {
		t.Errorf("report-results args = %v", host.runs[1].args)
	}
	if _, err := os.Stat(filepath.Dir(host.runs[0].name)); !os.IsNotExist(err) {
		t.Errorf("working directory %s was left behind", filepath.Dir(host.runs[0].name))
	}
	if strings.Contains(stdout.String(), "a:b:c") {
		t.Error("progress output must not print the token")
	}
	for _, want := range []string{"without a power driver", "swallow servers power-configuration set", "--configure-bmc --system-id"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output = %q, want the next step to mention %q", stdout.String(), want)
		}
	}
	if len(maas.updates) != 0 {
		t.Errorf("machine updates = %v, want none without --configure-bmc", maas.updates)
	}
}

// MAAS can process a report without linking the host's address to its interface; the enrollment
// reads the Machine back, finds the address missing, and reports once more, which links it.
func TestEnrollMAASReportsAgainWhenTheAddressIsMissing(t *testing.T) {
	maas := newFakeMAAS(t)
	maas.machines[0]["ip_addresses"] = []string{}
	host := newFakeHost(t)
	host.onReport = func(report int) {
		if report == 2 {
			maas.machines[0]["ip_addresses"] = []string{"172.17.0.1", "10.0.0.20"}
		}
	}
	var stdout strings.Builder
	err := host.enroller().enroll(context.Background(), Options{Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", Stdout: &stdout})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if host.reports != 2 {
		t.Errorf("reports = %d, want the report sent again once", host.reports)
	}
	for _, want := range []string{"MAAS did not record 10.0.0.20", "MAAS now records 10.0.0.20 for db-03"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output = %q, want %q", stdout.String(), want)
		}
	}
}

// An address MAAS still leaves out after the second report is a warning naming the Machine to
// look at; the host is enrolled, so it is not an error. Only the routed address counts: a Docker
// bridge address on the Machine is not the host's.
func TestEnrollMAASWarnsWhenTheAddressStaysMissing(t *testing.T) {
	maas := newFakeMAAS(t)
	maas.machines[0]["ip_addresses"] = []string{"172.17.0.1"}
	host := newFakeHost(t)
	var stdout strings.Builder
	err := host.enroller().enroll(context.Background(), Options{Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", Stdout: &stdout})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if host.reports != 2 {
		t.Errorf("reports = %d, want one second report", host.reports)
	}
	if !strings.Contains(stdout.String(), "Warning: MAAS still has no address 10.0.0.20 for db-03") ||
		!strings.Contains(stdout.String(), "MAAS Machine abc123") {
		t.Errorf("output = %q, want a warning naming the address and the Machine", stdout.String())
	}
}

// A host enrolled before reports its hardware again under its Machine with --system-id, which is
// how a host whose address MAAS left out gets it: get-machine-token issues the Machine's
// credentials, and the report and its address check follow; nothing is registered.
func TestReportAgainForAnEnrolledHost(t *testing.T) {
	maas := newFakeMAAS(t)
	maas.machines[0]["ip_addresses"] = []string{}
	host := newFakeHost(t)
	host.onReport = func(int) { maas.machines[0]["ip_addresses"] = []string{"10.0.0.20"} }
	var stdout strings.Builder
	err := host.enroller().enroll(context.Background(), Options{
		Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", SystemID: "abc123", Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if host.ran("register-machine") || host.reports != 1 {
		t.Errorf("runs = %+v, want one report and no registration", host.runs)
	}
	token := slices.IndexFunc(host.runs, func(run recordedRun) bool { return run.args[0] == "get-machine-token" })
	if token < 0 || host.runs[token].args[3] != "abc123" {
		t.Errorf("runs = %+v, want get-machine-token for Machine abc123", host.runs)
	}
	if !strings.Contains(stdout.String(), "Reported the hardware of db-03 again under Machine abc123") ||
		strings.Contains(stdout.String(), "MAAS did not record") {
		t.Errorf("output = %q, want the report again and the address recorded at once", stdout.String())
	}
}

// With --configure-bmc the BMC is found before the host is registered; after registration the
// policy user is set up in the first unused slot through the local interface, MAAS records the
// BMC as the Machine's ipmi driver, and MAAS reading the power ends the step. The password
// reaches the BMC only through a removed command file and MAAS only through the update body.
func TestEnrollMAASConfiguresBMC(t *testing.T) {
	maas := newFakeMAAS(t)
	host := newFakeHost(t)
	var stdout strings.Builder
	err := host.enroller().enroll(context.Background(), Options{
		Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", ConfigureBMC: true, Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if len(maas.updates) != 1 {
		t.Fatalf("machine updates = %v, want one", maas.updates)
	}
	update := maas.updates[0]
	password := update["power_parameters_power_pass"]
	if !acceptableBMCPassword(password, "maas") || len(password) != bmcPasswordLength {
		t.Errorf("power_pass = %q, want a %d-character password BMCs accept", password, bmcPasswordLength)
	}
	want := map[string]string{
		"power_type":                       "ipmi",
		"power_parameters_power_driver":    "LAN_2_0",
		"power_parameters_power_address":   "10.0.0.9",
		"power_parameters_power_user":      "maas",
		"power_parameters_power_pass":      password,
		"power_parameters_power_boot_type": "efi",
		"power_parameters_cipher_suite_id": "3",
		"power_parameters_privilege_level": "ADMIN",
	}
	for key, value := range want {
		if update[key] != value {
			t.Errorf("machine update %s = %q, want %q", key, update[key], value)
		}
	}

	commands := host.ipmiCommands()
	if len(commands) != 7 || !strings.HasPrefix(commands[3], "exec ") {
		t.Fatalf("ipmitool commands = %q, want lan print, user list, set name, exec, priv, setaccess, enable", commands)
	}
	wantCommands := []string{
		"lan print 1", "user list 1", "user set name 3 maas", commands[3],
		"user priv 3 4 1", "channel setaccess 1 3 callin=on ipmi=on link=on privilege=4", "user enable 3",
	}
	if !slices.Equal(commands, wantCommands) {
		t.Errorf("ipmitool commands = %q, want %q", commands, wantCommands)
	}
	execPath := strings.TrimPrefix(commands[3], "exec ")
	if got, want := host.execFiles[execPath], "user set password 3 "+password+"\n"; got != want {
		t.Errorf("exec file = %q, want %q", got, want)
	}
	if _, err := os.Stat(execPath); !os.IsNotExist(err) {
		t.Errorf("password command file %s was left behind", execPath)
	}
	for _, run := range host.runs {
		if slices.Contains(run.args, password) || slices.Contains(run.args, "chassis") {
			t.Errorf("run %s %v carries the password or touches the chassis", run.name, run.args)
		}
	}
	if strings.Contains(stdout.String(), password) {
		t.Error("output must not print the BMC password")
	}
	registered := slices.IndexFunc(host.runs, func(run recordedRun) bool { return run.args[0] == "register-machine" })
	listed := slices.IndexFunc(host.runs, func(run recordedRun) bool { return slices.Contains(run.args, "list") })
	if registered < 1 || listed < registered {
		t.Errorf("runs = %+v, want the BMC found before registering and its users changed after", host.runs)
	}
	if !strings.Contains(stdout.String(), "reads its power as on") {
		t.Errorf("output = %q, want it to report the power MAAS read", stdout.String())
	}
}

// --system-id skips registration and sets up the BMC of the Machine that shares a MAC address
// with this host, reusing the slot the policy user already has.
func TestConfigureBMCOfEnrolledHost(t *testing.T) {
	maas := newFakeMAAS(t)
	maas.machines[0]["hostname"] = "db-03-old"
	maas.machines[0]["power_type"] = "manual"
	maas.config["maas_auto_ipmi_user"] = "swallow"
	maas.config["maas_auto_ipmi_user_privilege_level"] = "operator"
	host := newFakeHost(t)
	host.uefi = false
	host.users = userList(ipmiUser{id: 1}, ipmiUser{id: 2, name: "ADMIN"}, ipmiUser{id: 3}, ipmiUser{id: 5, name: "swallow"})
	err := host.enroller().enroll(context.Background(), Options{
		Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", ConfigureBMC: true, SystemID: "abc123",
	})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if host.ran("register-machine") || !host.ran("get-machine-token") || host.reports != 1 {
		t.Errorf("runs = %+v, want no registration and one report under the Machine with --system-id", host.runs)
	}
	commands := host.ipmiCommands()
	if slices.ContainsFunc(commands, func(c string) bool { return strings.HasPrefix(c, "user set name") }) {
		t.Errorf("ipmitool commands = %q, want the existing user's name kept", commands)
	}
	if !slices.Contains(commands, "user priv 5 3 1") {
		t.Errorf("ipmitool commands = %q, want user 5 given operator privilege", commands)
	}
	if len(maas.updates) != 1 || maas.updates[0]["power_parameters_power_user"] != "swallow" ||
		maas.updates[0]["power_parameters_privilege_level"] != "OPERATOR" || maas.updates[0]["power_parameters_power_boot_type"] != "auto" {
		t.Errorf("machine updates = %v, want the swallow user with OPERATOR and boot type auto", maas.updates)
	}
}

// MAAS can drop a host's interfaces when it refuses the hardware report (an overlapping subnet),
// so a Machine without any is matched by its hostname.
func TestConfigureBMCMatchesHostnameWithoutInterfaces(t *testing.T) {
	maas := newFakeMAAS(t)
	maas.machines[0]["interface_set"] = []map[string]any{}
	host := newFakeHost(t)
	err := host.enroller().enroll(context.Background(), Options{
		Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", ConfigureBMC: true, SystemID: "abc123",
	})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if len(maas.updates) != 1 || maas.updates[0]["power_type"] != "ipmi" {
		t.Errorf("machine updates = %v, want the ipmi driver recorded", maas.updates)
	}
}

// A Machine MAAS already powers is left alone, so running the step again never rotates a working
// BMC password.
func TestConfigureBMCLeavesAPoweredMachine(t *testing.T) {
	maas := newFakeMAAS(t)
	maas.machines[0]["power_type"] = "ipmi"
	host := newFakeHost(t)
	var stdout strings.Builder
	err := host.enroller().enroll(context.Background(), Options{
		Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", ConfigureBMC: true, SystemID: "abc123", Stdout: &stdout,
	})
	if err != nil {
		t.Fatalf("enroll error = %v", err)
	}
	if got, want := host.ipmiCommands(), []string{"lan print 1"}; !slices.Equal(got, want) {
		t.Errorf("ipmitool commands = %q, want only %q", got, want)
	}
	if len(maas.updates) != 0 {
		t.Errorf("machine updates = %v, want none", maas.updates)
	}
	if !strings.Contains(stdout.String(), "already powers db-03 through ipmi") {
		t.Errorf("output = %q, want it to say the BMC was left alone", stdout.String())
	}
}

// When MAAS cannot read the power through the new driver, the Machine's previous driver is
// restored and the error says the host is enrolled and the BMC user stays.
func TestConfigureBMCRestoresWhenMAASCannotReadPower(t *testing.T) {
	cases := []struct {
		name        string
		powerState  string
		powerStatus int
		want        string
	}{
		{"power state error", "error", 0, "reports the power as error"},
		{"query refused", "", http.StatusServiceUnavailable, "HTTP 503"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			maas := newFakeMAAS(t)
			maas.powerState, maas.powerStatus = tc.powerState, tc.powerStatus
			host := newFakeHost(t)
			err := host.enroller().enroll(context.Background(), Options{
				Provisioner: "maas", Endpoint: maas.url(), Token: "a:b:c", ConfigureBMC: true,
			})
			if err == nil {
				t.Fatal("enroll error = nil, want MAAS's failed power read")
			}
			for _, want := range []string{"db-03 is enrolled", tc.want, "previous power driver (none) was restored", `IPMI user "maas" stays`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("enroll error = %v, want it to mention %q", err, want)
				}
			}
			if len(maas.updates) != 2 || maas.updates[1]["power_type"] != "" || len(maas.updates[1]) != 1 {
				t.Errorf("machine updates = %v, want the ipmi update then power_type restored to none", maas.updates)
			}
			if strings.Contains(err.Error(), maas.updates[0]["power_parameters_power_pass"]) {
				t.Error("error must not carry the BMC password")
			}
		})
	}
}

func TestEnrollRefuses(t *testing.T) {
	cases := []struct {
		name string
		// arrange adjusts the host and MAAS; opts gets the region URL as Endpoint unless it names one.
		arrange func(host *fakeHost, maas *fakeMAAS)
		opts    Options
		want    string
		// registered is whether register-machine ran; enrolled is whether the error must say the
		// host is enrolled (a BMC step that failed after registration).
		registered, enrolled bool
	}{
		{name: "unknown provisioner", opts: Options{Provisioner: "ironic", Token: "t"}, want: "no existing-host enrollment"},
		{name: "missing token", opts: Options{Provisioner: "maas"}, want: "--token is required"},
		{name: "not root", arrange: func(h *fakeHost, _ *fakeMAAS) { h.euid = 1000 }, opts: Options{Provisioner: "maas", Token: "t"}, want: "run as root"},
		{name: "no credentials returned", arrange: func(h *fakeHost, _ *fakeMAAS) { h.registerOutput = "error: hostname exists" },
			opts: Options{Provisioner: "maas", Token: "t"}, want: "no machine credentials", registered: true},
		{name: "region unreachable", opts: Options{Provisioner: "maas", Endpoint: "/wrong", Token: "t"}, want: "HTTP 404"},
		{name: "system id of another host without configure-bmc", arrange: func(h *fakeHost, m *fakeMAAS) {
			h.macs = []string{"aa:bb:cc:00:00:42"}
			m.machines[0]["hostname"] = "web-01"
		}, opts: Options{Provisioner: "maas", Token: "a:b:c", SystemID: "abc123"}, want: "shares no MAC address"},
		{name: "system id with a token that is not an API key", opts: Options{Provisioner: "maas", Token: "t", SystemID: "abc123"}, want: "MAAS API key"},
		{name: "token is not an API key", opts: Options{Provisioner: "maas", Token: "t", ConfigureBMC: true}, want: "MAAS API key"},
		{name: "no ipmitool", arrange: func(h *fakeHost, _ *fakeMAAS) { h.noIPMITool = true },
			opts: Options{Provisioner: "maas", Token: "a:b:c", ConfigureBMC: true}, want: "ipmitool, which is not installed"},
		{name: "no IPMI device", arrange: func(h *fakeHost, _ *fakeMAAS) { h.noIPMIDevice = true },
			opts: Options{Provisioner: "maas", Token: "a:b:c", ConfigureBMC: true}, want: "/dev/ipmi0 does not exist"},
		{name: "BMC without an address", arrange: func(h *fakeHost, _ *fakeMAAS) { h.lan = map[int]string{1: lanPrint("0.0.0.0")} },
			opts: Options{Provisioner: "maas", Token: "a:b:c", ConfigureBMC: true}, want: "no IP address on any IPMI LAN channel"},
		{name: "MAAS sets a K_g key", arrange: func(_ *fakeHost, m *fakeMAAS) { m.config["maas_auto_ipmi_k_g_bmc_key"] = "secret" },
			opts: Options{Provisioner: "maas", Token: "a:b:c", ConfigureBMC: true}, want: "K_g"},
		{name: "system id of another host", arrange: func(h *fakeHost, m *fakeMAAS) {
			h.macs = []string{"aa:bb:cc:00:00:42"}
			m.machines[0]["hostname"] = "web-01"
		},
			opts: Options{Provisioner: "maas", Token: "a:b:c", ConfigureBMC: true, SystemID: "abc123"}, want: "shares no MAC address"},
		// A hostname only stands in for the MACs when MAAS recorded no interfaces for the Machine.
		{name: "system id with this hostname but other MACs", arrange: func(h *fakeHost, _ *fakeMAAS) { h.macs = []string{"aa:bb:cc:00:00:42"} },
			opts: Options{Provisioner: "maas", Token: "a:b:c", ConfigureBMC: true, SystemID: "abc123"}, want: "shares no MAC address"},
		{name: "no unused BMC user slot", arrange: func(h *fakeHost, _ *fakeMAAS) {
			h.users = userList(ipmiUser{id: 1}, ipmiUser{id: 2, name: "ADMIN"}, ipmiUser{id: 3, name: "root"})
		}, opts: Options{Provisioner: "maas", Token: "a:b:c", ConfigureBMC: true}, want: "no unused user slot", registered: true, enrolled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			maas := newFakeMAAS(t)
			host := newFakeHost(t)
			if tc.arrange != nil {
				tc.arrange(host, maas)
			}
			opts := tc.opts
			opts.Endpoint = maas.url() + opts.Endpoint
			err := host.enroller().enroll(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("enroll error = %v, want it to mention %q", err, tc.want)
			}
			if got := host.ran("register-machine"); got != tc.registered {
				t.Errorf("registered = %v, want %v", got, tc.registered)
			}
			if tc.opts.SystemID != "" && (host.ran("get-machine-token") || host.reports != 0) {
				t.Errorf("runs = %+v, want nothing reported under a Machine that was refused", host.runs)
			}
			if tc.enrolled && !strings.Contains(fmt.Sprint(err), "db-03 is enrolled") {
				t.Errorf("enroll error = %v, want it to say the host is enrolled", err)
			}
			if len(maas.updates) != 0 {
				t.Errorf("machine updates = %v, want none", maas.updates)
			}
		})
	}
}
