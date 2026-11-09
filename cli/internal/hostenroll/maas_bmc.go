package hostenroll

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// This file sets up a Machine's power driver from the host it runs on, for MAAS. The BMC user is
// set up over the local IPMI interface (ipmi.go), and the Machine's ipmi power parameters are
// written straight to the MAAS API with the enrollment bundle's API key, the same key
// register-machine uses. The password therefore goes from this host to MAAS only and never
// reaches swallow, which does not store power credentials (decision 054). MAAS vocabulary stays
// in this package.

// maasPolicyDefaults are the values MAAS's own BMC script uses when its maas_auto_ipmi_* settings
// are unset: the BMC user name, its privilege, and the IPMI cipher suite.
const (
	defaultIPMIUsername      = "maas"
	defaultIPMIPrivilege     = "ADMIN"
	defaultIPMICipherSuiteID = "3"
)

// ipmiUsername is a BMC user name this step accepts: IPMI allows 16 bytes, and these characters
// are safe in ipmitool arguments and command files.
var ipmiUsername = regexp.MustCompile(`^[A-Za-z0-9._-]{1,16}$`)

// bmcSetup is what the read-only checks found before anything changes: where the BMC is and the
// provider policy its new user follows. prepareBMC builds it before registration so a host that
// cannot be set up is refused before it is enrolled.
type bmcSetup struct {
	api      *maasAPI
	ipmitool string
	lan      bmcLAN
	policy   ipmiPolicy
	// bootType is the MAAS IPMI boot type: efi on a UEFI host, otherwise MAAS's automatic choice.
	bootType string
}

// ipmiPolicy is MAAS's BMC user policy (its maas_auto_ipmi_* settings).
type ipmiPolicy struct {
	username string
	// privilege is the MAAS name of the user's privilege: USER, OPERATOR, or ADMIN.
	privilege     string
	cipherSuiteID string
}

// prepareBMC runs every check that changes nothing: ipmitool and the IPMI device are present,
// MAAS's BMC policy can be followed, and the BMC has an address on a LAN channel. It installs
// nothing and loads no kernel module; a host that lacks one is refused with what to do instead.
func (e enroller) prepareBMC(ctx context.Context, workdir, region, token string, stdout io.Writer) (*bmcSetup, error) {
	ipmitool, err := e.lookPath("ipmitool")
	if err != nil {
		return nil, errors.New("--configure-bmc uses the host's own ipmitool, which is not installed; " +
			"install it and retry, or set the Power Configuration in swallow")
	}
	if !e.ipmiDevicePresent() {
		return nil, errors.New("--configure-bmc needs the local IPMI interface, but /dev/ipmi0 does not exist; " +
			"load the ipmi_si and ipmi_devintf kernel modules and retry, or set the Power Configuration in swallow")
	}
	api, err := newMAASAPI(e.httpClient, region, token)
	if err != nil {
		return nil, err
	}
	policy, err := api.ipmiPolicy(ctx)
	if err != nil {
		return nil, err
	}
	lan, err := e.findBMCLAN(ctx, workdir, ipmitool)
	if err != nil {
		return nil, err
	}
	bootType := "auto"
	if _, err := e.stat("/sys/firmware/efi"); err == nil {
		bootType = "efi"
	}
	fmt.Fprintf(stdout, "Found the BMC at %s on IPMI LAN channel %d\n", lan.address, lan.channel)
	return &bmcSetup{api: api, ipmitool: ipmitool, lan: lan, policy: policy, bootType: bootType}, nil
}

// configureBMC gives the Machine of this host an ipmi power driver. The Machine is the one just
// registered under name, or the one systemID names after checking it is this host. A Machine
// that already has an automatic driver is left alone, so running this again never rotates a
// working password. Otherwise the BMC user is set up, MAAS records the power parameters, and MAAS
// must then read the power through them; when it cannot (usually because its rack controller
// does not reach the BMC), the Machine's previous power driver is restored and the error says
// that the BMC user stays.
func (e enroller) configureBMC(ctx context.Context, workdir string, setup *bmcSetup, name, systemID string, stdout io.Writer) error {
	machine, err := e.findMachine(ctx, setup.api, name, systemID)
	if err != nil {
		return err
	}
	if automaticPowerType(machine.PowerType) {
		fmt.Fprintf(stdout, "MAAS already powers %s through %s; its BMC was left as it is.\n", machine.Hostname, machine.PowerType)
		return nil
	}

	users, err := e.ipmiUsers(ctx, workdir, setup.ipmitool, setup.lan.channel)
	if err != nil {
		return err
	}
	username := setup.policy.username
	user, ok := pickIPMIUser(users, username)
	if !ok {
		return fmt.Errorf("the BMC has no user named %q and no unused user slot; free one in the BMC, or set the Power Configuration in swallow", username)
	}
	password, err := newBMCPassword(username)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Setting up IPMI user %q (user ID %d) on the BMC at %s\n", username, user.id, setup.lan.address)
	if err := e.setUpIPMIUser(ctx, workdir, setup.ipmitool, setup.lan, user, username, password, ipmiPrivilegeLevels[setup.policy.privilege]); err != nil {
		return err
	}

	fields := map[string]string{
		"power_type":                       "ipmi",
		"power_parameters_power_driver":    "LAN_2_0",
		"power_parameters_power_address":   setup.lan.address,
		"power_parameters_power_user":      username,
		"power_parameters_power_pass":      password,
		"power_parameters_power_boot_type": setup.bootType,
		"power_parameters_cipher_suite_id": setup.policy.cipherSuiteID,
		"power_parameters_privilege_level": setup.policy.privilege,
	}
	if err := setup.api.updateMachine(ctx, machine.SystemID, fields); err != nil {
		return fmt.Errorf("record the BMC in MAAS: %s; IPMI user %q is set up on the BMC", strings.ReplaceAll(err.Error(), password, "[REDACTED]"), username)
	}

	state, err := setup.api.queryPowerState(ctx, machine.SystemID)
	if err == nil && (state == "on" || state == "off") {
		fmt.Fprintf(stdout, "MAAS now powers %s through IPMI at %s and reads its power as %s.\n", machine.Hostname, setup.lan.address, state)
		return nil
	}
	reason := "it reports the power as " + state
	switch {
	case err != nil:
		reason = err.Error()
	case state == "":
		reason = "it reports no power state"
	}
	previous := machine.PowerType
	if restoreErr := setup.api.updateMachine(ctx, machine.SystemID, map[string]string{"power_type": previous}); restoreErr != nil {
		return fmt.Errorf("MAAS cannot read the power through the BMC at %s (%s), and restoring its previous power driver failed: %v; "+
			"fix or replace the Power Configuration in swallow", setup.lan.address, reason, restoreErr)
	}
	return fmt.Errorf("MAAS cannot read the power through the BMC at %s (%s), so its previous power driver (%s) was restored. "+
		"IPMI user %q stays on the BMC: check that the MAAS rack controller reaches %s, then run this again or set the Power Configuration in swallow",
		setup.lan.address, reason, powerTypeLabel(previous), username, setup.lan.address)
}

// findMachine returns this host's Machine. With systemID it reads that Machine and requires it to
// be this host, by a shared MAC address or, when MAAS recorded no interfaces, the same hostname:
// writing this BMC to another host's Machine would let MAAS switch the wrong server. Without it,
// the Machine is the one registered under name a moment ago (register-machine refuses a name that
// is taken, so exactly one matches).
func (e enroller) findMachine(ctx context.Context, api *maasAPI, name, systemID string) (*maasMachine, error) {
	if systemID == "" {
		var machines []maasMachine
		if err := api.get(ctx, "/machines/", url.Values{"hostname": {name}}, &machines); err != nil {
			return nil, fmt.Errorf("find Machine %s in MAAS: %w", name, err)
		}
		if len(machines) != 1 {
			return nil, fmt.Errorf("find Machine %s in MAAS: %d machines have that hostname", name, len(machines))
		}
		return &machines[0], nil
	}

	var machine maasMachine
	if err := api.get(ctx, "/machines/"+url.PathEscape(systemID)+"/", nil, &machine); err != nil {
		return nil, fmt.Errorf("read Machine %s from MAAS: %w", systemID, err)
	}
	macs, err := e.hostMACs()
	if err != nil {
		return nil, err
	}
	for _, iface := range machine.Interfaces {
		if slices.Contains(macs, strings.ToLower(iface.MACAddress)) {
			return &machine, nil
		}
	}
	if len(machine.Interfaces) == 0 && strings.EqualFold(machine.Hostname, name) {
		return &machine, nil
	}
	return nil, fmt.Errorf("the Machine %s is %q, which shares no MAC address with this host (%s); check --system-id", systemID, machine.Hostname, name)
}

// automaticPowerType reports whether MAAS can switch and read power through powerType: every
// driver except none ("") and manual.
func automaticPowerType(powerType string) bool {
	return powerType != "" && powerType != "manual"
}

// powerTypeLabel names a non-automatic power type in a message.
func powerTypeLabel(powerType string) string {
	if powerType == "" {
		return "none"
	}
	return powerType
}

// maasMachine is the part of a MAAS machine object the enrollment reads back.
type maasMachine struct {
	SystemID  string `json:"system_id"`
	Hostname  string `json:"hostname"`
	PowerType string `json:"power_type"`
	// IPAddresses are the addresses MAAS links to the Machine's interfaces; swallow's Server takes
	// its addresses from them.
	IPAddresses []string `json:"ip_addresses"`
	Interfaces  []struct {
		MACAddress string `json:"mac_address"`
	} `json:"interface_set"`
}

// maasAPI is the slice of the MAAS 2.0 API the BMC step uses, authenticated with the enrollment
// bundle's API key. The key must belong to a MAAS administrator: only administrators may write
// power parameters, the same requirement swallow's provisioner Integration has.
type maasAPI struct {
	client      *http.Client
	root        string
	consumerKey string
	tokenKey    string
	tokenSecret string
}

// newMAASAPI parses token, a MAAS API key "consumer:token:secret", for the region URL region.
func newMAASAPI(client *http.Client, region, token string) (*maasAPI, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, errors.New("--configure-bmc needs --token to be a MAAS API key (consumer:token:secret)")
	}
	return &maasAPI{client: client, root: region + "/api/2.0", consumerKey: parts[0], tokenKey: parts[1], tokenSecret: parts[2]}, nil
}

// ipmiPolicy reads MAAS's maas_auto_ipmi_* settings, falling back to MAAS's defaults. A K_g key
// is refused: setting the BMC's K_g changes how every BMC user logs in, which this step never
// does, and MAAS could not log in without it. MAAS's global BMC password is not used either:
// every host gets its own random password.
func (a *maasAPI) ipmiPolicy(ctx context.Context) (ipmiPolicy, error) {
	username, err := a.config(ctx, "maas_auto_ipmi_user")
	if err != nil {
		return ipmiPolicy{}, err
	}
	privilege, err := a.config(ctx, "maas_auto_ipmi_user_privilege_level")
	if err != nil {
		return ipmiPolicy{}, err
	}
	cipherSuiteID, err := a.config(ctx, "maas_auto_ipmi_cipher_suite_id")
	if err != nil {
		return ipmiPolicy{}, err
	}
	kg, err := a.config(ctx, "maas_auto_ipmi_k_g_bmc_key")
	if err != nil {
		return ipmiPolicy{}, err
	}
	if kg != "" {
		return ipmiPolicy{}, errors.New("MAAS sets a K_g BMC key for its IPMI users, which --configure-bmc does not set on a running host; " +
			"set the Power Configuration in swallow instead")
	}
	policy := ipmiPolicy{username: username, privilege: strings.ToUpper(privilege), cipherSuiteID: cipherSuiteID}
	if policy.username == "" {
		policy.username = defaultIPMIUsername
	}
	if !ipmiUsername.MatchString(policy.username) {
		return ipmiPolicy{}, fmt.Errorf("MAAS's IPMI user name %q is not one --configure-bmc can set (up to 16 letters, digits, '.', '_', or '-')", policy.username)
	}
	if _, ok := ipmiPrivilegeLevels[policy.privilege]; !ok {
		policy.privilege = defaultIPMIPrivilege
	}
	if cipherSuiteID == "" {
		policy.cipherSuiteID = defaultIPMICipherSuiteID
	}
	return policy, nil
}

// config reads one MAAS setting as text; an unset setting is "".
func (a *maasAPI) config(ctx context.Context, name string) (string, error) {
	var value any
	if err := a.get(ctx, "/maas/", url.Values{"op": {"get_config"}, "name": {name}}, &value); err != nil {
		return "", fmt.Errorf("read the MAAS setting %s: %w", name, err)
	}
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(v), nil
	default:
		return strings.TrimSpace(fmt.Sprint(v)), nil
	}
}

// updateMachine writes fields to a Machine with the machine update (PUT). Every field is sent,
// empty ones included: an empty power_type is how MAAS is told the Machine has no power driver.
func (a *maasAPI) updateMachine(ctx context.Context, systemID string, fields map[string]string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if err := writer.WriteField(key, fields[key]); err != nil {
			return fmt.Errorf("encode the machine update: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("encode the machine update: %w", err)
	}
	return a.do(ctx, http.MethodPut, "/machines/"+url.PathEscape(systemID)+"/", nil, &body, writer.FormDataContentType(), nil)
}

// queryPowerState asks MAAS to read the Machine's power through its power driver now. It changes
// nothing; the result is MAAS's state name (on, off, error, or unknown).
func (a *maasAPI) queryPowerState(ctx context.Context, systemID string) (string, error) {
	var out struct {
		State string `json:"state"`
	}
	if err := a.get(ctx, "/machines/"+url.PathEscape(systemID)+"/", url.Values{"op": {"query_power_state"}}, &out); err != nil {
		return "", err
	}
	return out.State, nil
}

// get issues an authenticated GET and decodes its JSON body into out.
func (a *maasAPI) get(ctx context.Context, path string, query url.Values, out any) error {
	return a.do(ctx, http.MethodGet, path, query, nil, "", out)
}

// do sends one request signed the way MAAS expects: OAuth 1.0 PLAINTEXT with an empty consumer
// secret, so the signature is "&<token secret>" and covers neither the URL nor the body. An error
// carries the HTTP status and MAAS's explanation, never the key.
func (a *maasAPI) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string, out any) error {
	endpoint := a.root + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return fmt.Errorf("build the MAAS request: %w", err)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("sign the MAAS request: %w", err)
	}
	request.Header.Set("Authorization", fmt.Sprintf(
		"OAuth oauth_version=%q, oauth_signature_method=%q, oauth_consumer_key=%q, oauth_token=%q, oauth_signature=%q, oauth_nonce=%q, oauth_timestamp=%q",
		"1.0", "PLAINTEXT", a.consumerKey, a.tokenKey, "&"+url.QueryEscape(a.tokenSecret), hex.EncodeToString(nonce), strconv.FormatInt(time.Now().Unix(), 10)))
	request.Header.Set("Accept", "application/json")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := a.client.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		// A body cut short by a read error still explains the failure, so the read error is dropped.
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		if text := strings.TrimSpace(string(detail)); text != "" {
			return fmt.Errorf("MAAS answered HTTP %d: %s", response.StatusCode, text)
		}
		return fmt.Errorf("MAAS answered HTTP %d", response.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode the MAAS response to %s %s: %w", method, path, err)
	}
	return nil
}
